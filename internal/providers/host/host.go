// Package host collects Linux host telemetry from /proc and /sysfs — CPU load,
// memory, disk usage + IO rates, network throughput, and temperature via
// hwmon. Every parser is a pure function over a string so it can be unit
// tested with fixture data on a non-Linux dev box.
package host

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hedchr/fanctrl/internal/metrics"
)

// Provider collects host telemetry from the Linux filesystem.
type Provider struct {
	// Root overrides the filesystem root for testing (default "/").
	Root string
	// CPUCount is the number of logical CPUs (for normalization). 0 = detect.
	CPUCount int

	mu       sync.Mutex
	prevAgg  aggregateCounters
	prevCore []aggregateCounters
	prevDisk map[string]diskAbs
	prevNet  map[string]netAbs
	lastTime time.Time
	prevDiskTime time.Time
}

// NewProvider builds a host provider.
func NewProvider(cpuCount int) *Provider {
	return &Provider{Root: "/", CPUCount: cpuCount, prevDisk: map[string]diskAbs{}, prevNet: map[string]netAbs{}}
}

// Name implements Provider.
func (p *Provider) Name() string { return "host" }

// Close implements Provider.
func (p *Provider) Close() error { return nil }

func (p *Provider) root() string {
	if p.Root == "" {
		return "/"
	}
	return p.Root
}

// Collect reads a snapshot from /proc and /sysfs.
func (p *Provider) Collect(ctx context.Context) (metrics.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return metrics.Snapshot{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	var snap metrics.Snapshot
	snap.Time = now

	// CPU
	if cpu, err := p.readCPU(); err == nil {
		snap.CPU = cpu
	}
	if ci, err := p.readCPUInfo(); err == nil {
		snap.CPU.Model = ci.model
		if snap.CPU.Cores == 0 {
			snap.CPU.Cores = ci.cores
		}
		if snap.CPU.Threads == 0 {
			snap.CPU.Threads = ci.threads
		}
	}
	if mem, err := p.readMemInfo(); err == nil {
		snap.CPU.MemTotal = mem.total
		snap.CPU.MemUsed = mem.used
		snap.CPU.MemAvail = mem.avail
	}
	if t, max, err := p.readCPUTemp(); err == nil {
		snap.CPU.CpuTemp = t
		snap.CPU.CpuTempMax = max
	}

	// Disk (fs usage + IO rates via /proc/diskstats delta).
	snap.Disks = p.readDisks(now)
	snap.Drives = p.readDrives()

	// Network (throughput delta via /proc/net/dev).
	snap.Nets = p.readNetworks(now)

	// Record time for the next differential computation.
	p.lastTime = now
	p.prevDiskTime = now

	// Uptime
	snap.Extra = []metrics.Scalar{
		{Name: "Uptime", Value: snap.CPU.Uptime, Unit: "s", Kind: metrics.KindCount},
	}

	return snap, nil
}

// Discover returns the detected host inventory (CPU topology, hwmon temp
// sensors, mounts, interfaces).
func (p *Provider) Discover(_ context.Context) metrics.Discovery {
	d := metrics.Discovery{Source: "host"}
	// CPU topology
	if ci, err := p.readCPUInfo(); err == nil {
		d.CPU.Model = ci.model
		d.CPU.Cores = ci.cores
		d.CPU.Threads = ci.threads
	}
	// hwmon temp sensors
	base := filepath.Join(p.root(), "sys", "class", "hwmon")
	if entries, err := os.ReadDir(base); err == nil {
		for _, e := range entries {
			dir := filepath.Join(base, e.Name())
			name, _ := os.ReadFile(filepath.Join(dir, "name"))
			chip := strings.TrimSpace(string(name))
			for i := 1; i <= 5; i++ {
				if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf("temp%d_input", i))); err == nil {
					d.Thermals = append(d.Thermals, metrics.DiscoveredSensor{
						Name: fmt.Sprintf("%s temp%d", chip, i),
						HMON: e.Name(),
					})
				}
			}
		}
	}
	// Physical NVMe drives (model, serial, temp)
	for _, drv := range p.readDrives() {
		meta := fmt.Sprintf("%s (%s", drv.Device, drv.Model)
		if drv.Serial != "" {
			meta += fmt.Sprintf(", SN %s", drv.Serial)
		}
		if drv.Temp > 0 {
			meta += fmt.Sprintf(", %.0fC", drv.Temp)
		}
		d.Disks = append(d.Disks, meta+")")
	}
	// Interfaces
	for _, n := range p.readNetworks(time.Now()) {
		d.Nets = append(d.Nets, n.Interface)
	}
	return d
}

// ---- CPU ----

// aggregateCounters is the sum of all CPU time fields.
type aggregateCounters struct {
	busy  float64
	total float64
}

// parseCPULine parses the 10 numeric fields of a /proc/stat cpu line
// (user nice system idle iowait irq softirq steal guest guest_nice).
// Line already has the "cpuN" prefix stripped.
func parseCPULine(fields []string) aggregateCounters {
	var c aggregateCounters
	for i, s := range fields {
		v, _ := strconv.ParseFloat(s, 64)
		c.total += v
		switch i {
		case 0, 1, 2, 5, 6, 7: // user, nice, system, irq, softirq, steal
			c.busy += v
		}
	}
	return c
}

// busyPercent computes busy% between an aggregate and a previous aggregate.
func busyPercent(cur, prev aggregateCounters) float64 {
	dt := cur.total - prev.total
	if dt <= 0 {
		return 0
	}
	db := cur.busy - prev.busy
	if db < 0 {
		db = 0
	}
	return db / dt * 100
}

// readCPU returns the CPU aggregation, per-core busy array, core/thread counts,
// and uptime.
func (p *Provider) readCPU() (metrics.CPU, error) {
	f, err := os.Open(filepath.Join(p.root(), "proc", "stat"))
	if err != nil {
		return metrics.CPU{}, err
	}
	defer f.Close()

	var cpu metrics.CPU
	var agg aggregateCounters
	var per []aggregateCounters
	var btime int64

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "cpu ") {
			agg = parseCPULine(strings.Fields(line)[1:])
		} else if strings.HasPrefix(line, "cpu") {
			per = append(per, parseCPULine(strings.Fields(line)[1:]))
		} else if strings.HasPrefix(line, "btime") {
			if fields := strings.Fields(line); len(fields) > 1 {
				btime, _ = strconv.ParseInt(fields[1], 10, 64)
			}
		}
	}

	if btime > 0 {
		cpu.Uptime = float64(time.Now().Unix() - btime)
	}
	if p.CPUCount > 0 {
		cpu.Threads = p.CPUCount
	} else {
		cpu.Threads = len(per)
	}
	cpu.Cores = len(per)
	cpu.PerCoreLoad = make([]float64, len(per))

	// Per-core deltas use a parallel history keyed by core index.
	if len(p.prevCore) != len(per) {
		p.prevCore = make([]aggregateCounters, len(per))
	}
	for i, c := range per {
		cpu.PerCoreLoad[i] = busyPercent(c, p.prevCore[i])
		p.prevCore[i] = c
	}

	// Overall
	cpu.LoadPct = busyPercent(agg, p.prevAgg)
	p.prevAgg = agg

	return cpu, nil
}

type cpuInfo struct {
	model   string
	cores   int
	threads int
}

func (p *Provider) readCPUInfo() (cpuInfo, error) {
	f, err := os.Open(filepath.Join(p.root(), "proc", "cpuinfo"))
	if err != nil {
		return cpuInfo{}, err
	}
	defer f.Close()
	var ci cpuInfo
	coresSeen := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "model name") || strings.HasPrefix(line, "Hardware") {
			if parts := strings.SplitN(line, ":", 2); len(parts) == 2 {
				ci.model = strings.TrimSpace(parts[1])
			}
		}
		if strings.HasPrefix(line, "processor") {
			ci.threads++
		}
		if strings.HasPrefix(line, "core id") {
			if parts := strings.SplitN(line, ":", 2); len(parts) == 2 {
				coresSeen[strings.TrimSpace(parts[1])] = true
			}
		}
	}
	ci.cores = len(coresSeen)
	return ci, nil
}

// ---- memory ----

type memInfo struct {
	total float64
	used  float64
	avail float64
}

func (p *Provider) readMemInfo() (memInfo, error) {
	f, err := os.Open(filepath.Join(p.root(), "proc", "meminfo"))
	if err != nil {
		return memInfo{}, err
	}
	defer f.Close()
	var mi memInfo
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		val, _ := strconv.ParseFloat(fields[1], 64)
		kb := val * 1024
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			mi.total = kb
		case "MemAvailable":
			mi.avail = kb
		}
	}
	mi.used = mi.total - mi.avail
	return mi, nil
}

// ---- CPU temp via hwmon ----

func (p *Provider) readCPUTemp() (float64, float64, error) {
	base := filepath.Join(p.root(), "sys", "class", "hwmon")
	entries, err := os.ReadDir(base)
	if err != nil {
		return 0, 0, err
	}
	var best float64
	for _, e := range entries {
		dir := filepath.Join(base, e.Name())
		name, _ := os.ReadFile(filepath.Join(dir, "name"))
		if !isTempChip(string(name)) {
			continue
		}
		if v, err := readHWMONTemp(dir); err == nil && v > best {
			best = v
		}
	}
	if best == 0 {
		return 0, 0, fmt.Errorf("no hwmon temp found")
	}
	return best, 95, nil
}

func isTempChip(name string) bool {
	switch strings.TrimSpace(name) {
	case "k10temp", "coretemp", "zenpower", "cpu_thermal":
		return true
	}
	return false
}

func readHWMONTemp(dir string) (float64, error) {
	// temp1_input is the package temp on most AMD/Intel systems.
	for i := 1; i <= 5; i++ {
		data, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("temp%d_input", i)))
		if err != nil {
			continue
		}
		milli, _ := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
		if milli > 1000 && milli < 120000 {
			return milli / 1000.0, nil
		}
	}
	return 0, fmt.Errorf("no temp input")
}

// ---- Disk ----

// diskAbs holds the last absolute read/write sector byte counters for a device.
type diskAbs struct {
	read  float64
	write float64
}

type mountInfo struct {
	device string
	mount  string
	fstype string
	total  float64
	free   float64
}

func (p *Provider) readDisks(now time.Time) []metrics.Disk {
	var out []metrics.Disk
	dt := now.Sub(p.lastTime).Seconds()

	for _, m := range p.readMounts() {
		d := metrics.Disk{Mount: m.mount, Device: m.device, FSType: m.fstype, TotalBytes: m.total, FreeBytes: m.free}
		if r, ok := p.rateForDisk(now, dt, strings.TrimPrefix(m.device, "/dev/")); ok {
			d.ReadRate = r.read
			d.WriteRate = r.write
		}
		out = append(out, d)
	}
	return out
}

// readDrives returns physical NVMe drive info from /sys/class/nvme/nvme*:
// model, serial, firmware, namespace size, and hwmon temperature. Each entry
// is one physical drive (namespace), e.g. nvme0n1.
func (p *Provider) readDrives() []metrics.Drive {
	var out []metrics.Drive
	base := filepath.Join(p.root(), "sys", "class", "nvme")
	entries, err := os.ReadDir(base)
	if err != nil {
		return out
	}
	for _, e := range entries {
		ctrl := e.Name() // nvme0, nvme1, ...
		ctrlDir := filepath.Join(base, ctrl)

		// Model / serial / firmware live on the controller.
		model := strings.TrimSpace(string(readFile(p.root(), ctrlDir, "model")))
		serial := strings.TrimSpace(string(readFile(p.root(), ctrlDir, "serial")))
		fw := strings.TrimSpace(string(readFile(p.root(), ctrlDir, "firmware")))

		// Namespaces: nvme0 -> nvme0n1 (and nvme0n2 if multiple).
		if namespaces, err := os.ReadDir(ctrlDir); err == nil {
			for _, ns := range namespaces {
				name := ns.Name()
				if !strings.HasPrefix(name, ctrl+"n") {
					continue
				}
				temp := p.nvmeTempInDir(filepath.Join(ctrlDir, name))
				if temp <= 0 {
					temp = p.nvmeTempInDir(ctrlDir)
				}
				out = append(out, metrics.Drive{
					Device:    name,
					Model:     model,
					Serial:    serial,
					Firmware:  fw,
					SizeBytes: p.blockSize(name),
					Temp:      temp,
				})
			}
		}
	}
	return out
}

// blockSize returns the size in bytes of a block device (name like nvme0n1)
// from /sys/class/block/<name>/size (512-byte sectors).
func (p *Provider) blockSize(name string) float64 {
	data := readFile(p.root(), filepath.Join("sys", "class", "block", name), "size")
	sectors, _ := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	if sectors <= 0 {
		return 0
	}
	return sectors * 512
}

// readFile reads a file under root+dir+file, returning nil on error. When dir
// is absolute (already includes root), it is used as-is; otherwise it is
// joined under root.
func readFile(root, dir, file string) []byte {
	var p string
	if filepath.IsAbs(dir) {
		p = filepath.Join(dir, file)
	} else {
		p = filepath.Join(root, dir, file)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	return data
}

// nvmeTempInDir reads the first tempN_input (millidegrees) under dir's hwmon
// subdirectory, or 0.
func (p *Provider) nvmeTempInDir(dir string) float64 {
	// Look for hwmon subdirectories directly under dir.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var hwmons []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "hwmon") {
			hwmons = append(hwmons, e.Name())
		}
	}
	if len(hwmons) == 0 {
		return 0
	}
	// Try the first hwmon, then the others.
	for _, h := range hwmons {
		hdir := filepath.Join(dir, h)
		for i := 1; i <= 10; i++ {
			data, err := os.ReadFile(filepath.Join(hdir, fmt.Sprintf("temp%d_input", i)))
			if err != nil {
				continue
			}
			milli, _ := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
			if milli > 1000 && milli < 125000 {
				return milli / 1000.0
			}
		}
	}
	return 0
}

// rateForDisk computes byte/s read/write for a block device name using an
// absolute-counter delta since the previous snapshot. When the device is new
// (no history), first delta is 0.
func (p *Provider) rateForDisk(now time.Time, dt float64, name string) (diskRate, bool) {
	abs, ok := p.readDiskAbs(name)
	if !ok {
		return diskRate{}, false
	}
	// Defensive: ensure the history map exists even if the Provider was built
	// with a bare struct literal instead of NewProvider.
	if p.prevDisk == nil {
		p.prevDisk = map[string]diskAbs{}
	}
	prev, ok := p.prevDisk[name]
	p.prevDisk[name] = abs

	if !ok || p.prevDiskTime.IsZero() {
		return diskRate{}, false
	}
	if dt <= 0 {
		return diskRate{}, false
	}
	return diskRate{
		read:  clampNonNeg(abs.read-prev.read) / dt,
		write: clampNonNeg(abs.write-prev.write) / dt,
	}, true
}

func clampNonNeg(v float64) float64 {
	if v < 0 {
		return 0
	}
	return v
}

func (p *Provider) readDiskAbs(name string) (diskAbs, bool) {
	f, err := os.Open(filepath.Join(p.root(), "proc", "diskstats"))
	if err != nil {
		return diskAbs{}, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 14 {
			continue
		}
		if fields[2] != name {
			continue
		}
		sr, _ := strconv.ParseFloat(fields[5], 64)
		sw, _ := strconv.ParseFloat(fields[9], 64)
		return diskAbs{read: sr * 512, write: sw * 512}, true
	}
	return diskAbs{}, false
}

type diskRate struct {
	read  float64
	write float64
}

func (p *Provider) readMounts() []mountInfo {
	var mounts []mountInfo
	f, err := os.Open(filepath.Join(p.root(), "proc", "mounts"))
	if err != nil {
		return nil
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 {
			continue
		}
		dev, mnt, fs := fields[0], fields[1], fields[2]
		if !strings.HasPrefix(dev, "/dev/") {
			continue
		}
		switch fs {
		case "proc", "sysfs", "devtmpfs", "tmpfs", "overlay", "cgroup", "cgroup2", "devpts", "mqueue", "securityfs", "debugfs", "none":
			continue
		}
		target := filepath.Join(p.root(), strings.TrimPrefix(mnt, "/"))
		var total, free float64
		if st, err := statfs(target); err == nil {
			total = st.Total()
			free = st.Avail()
		}
		mounts = append(mounts, mountInfo{device: dev, mount: mnt, fstype: fs, total: total, free: free})
	}
	return mounts
}

// ---- Network ----

// netAbs holds last absolute rx/tx byte counters for an interface.
type netAbs struct {
	rx float64
	tx float64
}

func (p *Provider) readNetworks(now time.Time) []metrics.Net {
	dt := now.Sub(p.lastTime).Seconds()
	var out []metrics.Net
	f, err := os.Open(filepath.Join(p.root(), "proc", "net", "dev"))
	if err != nil {
		return nil
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Scan() // header
	for sc.Scan() {
		line := sc.Text()
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		iface := strings.TrimSpace(line[:idx])
		fields := strings.Fields(line[idx+1:])
		if len(fields) < 16 {
			continue
		}
		rx, _ := strconv.ParseFloat(fields[0], 64)
		tx, _ := strconv.ParseFloat(fields[8], 64)
		cur := netAbs{rx: rx, tx: tx}

		var rrx, rtx float64
		if prev, ok := p.prevNet[iface]; ok && dt > 0 && !p.lastTime.IsZero() {
			rrx = clampNonNeg(cur.rx-prev.rx) / dt
			rtx = clampNonNeg(cur.tx-prev.tx) / dt
		}
		p.prevNet[iface] = cur
		out = append(out, metrics.Net{Interface: iface, RxRate: rrx, TxRate: rtx, Up: rrx > 0 || rtx > 0})
	}
	return out
}

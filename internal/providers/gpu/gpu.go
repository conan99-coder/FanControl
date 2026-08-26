// Package gpu collects NVIDIA GPU telemetry via the nvidia-smi CLI. It uses a
// subprocess (not cgo NVML) so the binary cross-compiles cleanly from a Windows
// dev box to a Linux rig. Parsers are pure functions tested with fixture output.
package gpu

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/hedchr/fanctrl/internal/metrics"
)

// Provider collects GPU data via nvidia-smi.
type Provider struct {
	// SMI is the path/name of the nvidia-smi binary (default "nvidia-smi").
	SMI string
	// Interval between GPU polls (GPU polling is heavier than sysfs).
	Interval time.Duration
	// now is overridable in tests.
	now      func() time.Time
	lastPoll time.Time
	// Query used for the read-only snapshot.
	queryArgs []string
	// csvQuery is the ordered list of fields we request; maps to columns.
	csvQuery []string
	// fallbackArgs is a minimal field set used if the driver rejects the primary.
	fallbackArgs   []string
	fallbackFields []string
}

// defaultMaxTemp is the throttle threshold used when nvidia-smi does not report
// one (Blackwell/enterprise cards typically throttle ~ 90-95C).
const defaultMaxTemp = 90.0

// Fields we request, in order (must match the CSV parser below). Each is a
// broadly-supported nvidia-smi query field. We deliberately avoid obscure
// fields (e.g. temperature.gpu.threshold.temperature) that some drivers reject
// with exit 2, which would drop the WHOLE query. MaxTemp is supplied from a
// sensible constant below.
var defaultCSVQuery = []string{
	"index", "name", "temperature.gpu", "utilization.gpu",
	"power.draw", "power.limit", "fan.speed", "memory.used", "memory.total",
	"utilization.memory",
}

// fallbackCSVQuery is a minimal field set used if the primary query is rejected
// by the driver (e.g. an unknown field). Same leading order as the parser.
var fallbackCSVQuery = []string{
	"index", "name", "temperature.gpu", "utilization.gpu", "fan.speed",
}

// NewProvider builds a GPU provider. If smi is empty, "nvidia-smi" is used.
func NewProvider(smi string) *Provider {
	if smi == "" {
		smi = "nvidia-smi"
	}
	return &Provider{
		SMI:            smi,
		Interval:       time.Second,
		now:            time.Now,
		csvQuery:       defaultCSVQuery,
		queryArgs:      buildQueryArgs(defaultCSVQuery),
		fallbackFields: fallbackCSVQuery,
		fallbackArgs:   buildQueryArgs(fallbackCSVQuery),
	}
}

func buildQueryArgs(fields []string) []string {
	args := []string{"--query-gpu=" + strings.Join(fields, ","), "--format=csv,noheader,nounits"}
	return args
}

// Name implements Provider.
func (p *Provider) Name() string { return "gpu" }

// Close implements Provider.
func (p *Provider) Close() error { return nil }

// Discover returns the detected GPU inventory (index, name, fan-control probe).
func (p *Provider) Discover(ctx context.Context) metrics.Discovery {
	d := metrics.Discovery{Source: "gpu"}
	gpus := p.collectGPUs(ctx)
	fp := ProbeFanControl(ctx, p.SMI)
	for _, g := range gpus {
		d.GPUs = append(d.GPUs, metrics.DiscoveredGPU{Index: g.Index, Name: g.Name, FanControl: fp})
	}
	return d
}

// Collect returns GPU telemetry. On any nvidia-smi failure it returns the error
// so the poller can mark the GPU source unavailable.
func (p *Provider) Collect(ctx context.Context) (metrics.Snapshot, error) {
	gpus := p.collectGPUs(ctx)
	if len(gpus) == 0 {
		return metrics.Snapshot{}, fmt.Errorf("no GPUs reported by nvidia-smi")
	}
	snap := metrics.Snapshot{Time: p.now()}
	snap.GPUs = gpus
	return snap, nil
}

// collectGPUs tries the primary field set and falls back to a minimal set if the
// driver rejects the primary (some query fields return exit 2 on certain
// drivers, which would otherwise drop GPU data entirely).
func (p *Provider) collectGPUs(ctx context.Context) []metrics.GPU {
	for _, alt := range []struct {
		args   []string
		fields []string
	}{
		{p.queryArgs, p.csvQuery},
		{p.fallbackArgs, p.fallbackFields},
	} {
		out, err := p.runArgs(ctx, alt.args)
		if err != nil {
			continue
		}
		gpus, err := parseCSV(out, alt.fields)
		if err != nil {
			continue
		}
		applyDefaults(gpus, len(alt.fields))
		// Stamp actual fan-control capability once per snapshot.
		fp := ProbeFanControl(ctx, p.SMI)
		for i := range gpus {
			gpus[i].FanControl = fp
		}
		return gpus
	}
	return nil
}

func (p *Provider) run(ctx context.Context) ([]byte, error) {
	return p.runArgs(ctx, p.queryArgs)
}

func (p *Provider) runArgs(ctx context.Context, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, p.SMI, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("nvidia-smi query failed: %w", err)
	}
	return buf.Bytes(), nil
}

// applyDefaults backfills a sane max-throttle temp when nvidia-smi did not
// report one (e.g. a reduced fallback query, or a driver that omitted it).
func applyDefaults(gpus []metrics.GPU, _ int) {
	for i := range gpus {
		if gpus[i].MaxTemp == 0 {
			gpus[i].MaxTemp = defaultMaxTemp
		}
	}
}

// parseCSV parses the csv,noheader,nounits output into GPU structs. Order of
// columns matches defaultCSVQuery. Missing/blank fields are tolerated (some
// fields like fan.speed return "[N/A]" on cards that lock it).
func parseCSV(data []byte, fields []string) ([]metrics.GPU, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.TrimLeadingSpace = true
	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse nvidia-smi csv: %w", err)
	}
	var gpus []metrics.GPU
	for _, rec := range records {
		if len(rec) < len(fields) {
			rec = append(rec, make([]string, len(fields)-len(rec))...)
		}
		g := metrics.GPU{}
		g.Index = parseInt(rec[0])
		g.Name = rec[1]
		g.Temp = parseFloat(rec[2])
		g.Util = parseFloat(rec[3])
		g.Power = parseFloat(rec[4])
		g.PowerLimit = parseFloat(rec[5])
		g.FanPct = parseFloat(rec[6])
		g.VRAMUsed = parseFloat(rec[7]) * 1024 * 1024 // MiB -> bytes
		g.VRAMTotal = parseFloat(rec[8]) * 1024 * 1024
		g.MemoryUtil = parseFloat(rec[9])
		// MaxTemp is only present in the full field set; a reduced fallback
		// omits it and applyDefaults fills a sensible column.
		if len(fields) > 10 {
			g.MaxTemp = parseFloat(rec[10])
		}
		// Fan control support is probed separately; default false here.
		g.FanControl = false
		gpus = append(gpus, g)
	}
	return gpus, nil
}

func parseInt(s string) int {
	v, _ := strconv.Atoi(strings.TrimSpace(s))
	return v
}

func parseFloat(s string) float64 {
	s = strings.TrimSpace(s)
	// nvidia-smi returns "[N/A]" or "[Not Supported]" for locked fields.
	if s == "" || strings.HasPrefix(s, "[") {
		return 0
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// ProbeFanControl determines whether GPU fan writes are plausibly available by
// checking that every GPU reports a usable numeric fan speed. A card that
// reports [N/A] or 0 for fan.speed (locked / unsupported) is treated as not
// fan-controllable; writing to such a card would fail or be ignored, so we
// degrade to read-only.
func ProbeFanControl(ctx context.Context, smi string) bool {
	out, err := exec.CommandContext(ctx, smi,
		"--query-gpu=fan.speed", "--format=csv,noheader,nounits").Output()
	if err != nil {
		// nvidia-smi absent or failed: no reliable signal -> assume not controllable.
		return false
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return false
	}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// "100" -> controllable. "[N/A]", "[Not Supported]", or 0 -> not.
		if strings.HasPrefix(trimmed, "[") {
			return false
		}
		if v := parseFloat(trimmed); v <= 0 {
			return false
		}
	}
	return true
}

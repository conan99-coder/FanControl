// Package mock provides a deterministic Provider that emits realistic telemetry
// without touching hardware. It drives the entire app (dashboard, auth, SSE,
// history, and the fan-control flow against a fake BMC) on any machine, so the
// full stack can be validated on a Windows dev box with zero risk. It can also
// simulate a thermal spike to exercise the safety governor.
package mock

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/hedchr/fanctrl/internal/metrics"
)

// Provider is a deterministic fake source.
type Provider struct {
	mu    sync.Mutex
	start time.Time
	tick  int
	Spike bool // when true, temperatures ramp toward the hard limit
	Now   func() time.Time
}

// NewProvider builds a mock provider.
func NewProvider() *Provider {
	return &Provider{start: time.Now(), Now: time.Now}
}

// Name implements Provider.
func (p *Provider) Name() string { return "mock" }

// Close implements Provider.
func (p *Provider) Close() error { return nil }

// Discover returns the mock's synthetic inventory.
func (p *Provider) Discover(_ context.Context) metrics.Discovery {
	return metrics.Discovery{
		Source: "mock",
		CPU:    metrics.DiscoveredCPU{Model: "AMD EPYC Threadripper PRO (mock)", Cores: 32, Threads: 64},
		Thermals: []metrics.DiscoveredSensor{
			{ID: 1, Name: "CPU"},
			{ID: 2, Name: "SYS"},
			{ID: 3, Name: "GPU0 Inlet"},
		},
		Fans: []metrics.DiscoveredSensor{
			{ID: 184, Name: "CPU Fan"},
			{ID: 187, Name: "Chassis 1"},
			{ID: 189, Name: "Chassis 2"},
		},
		GPUs: []metrics.DiscoveredGPU{
			{Index: 0, Name: "NVIDIA RTX 6000 Pro Blackwell WS (mock)", FanControl: true},
			{Index: 1, Name: "NVIDIA RTX 6000 Pro Blackwell WS (mock)", FanControl: true},
		},
		Disks: []string{"/dev/nvme0n1 (/)", "/dev/nvme1n1 (/data)"},
		Nets:  []string{"eno1"},
	}
}

func (p *Provider) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// Collect produces a snapshot driven by tick count.
func (p *Provider) Collect(_ context.Context) (metrics.Snapshot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tick++
	now := p.now()

	// A gentle sinusoidal CPU load baseline plus a slow drift.
	load := 35 + 18*math.Sin(float64(p.tick)/18) + float64(p.tick%7)
	if p.Spike {
		load = 92 + float64(p.tick%6)
	}
	if load > 100 {
		load = 100
	}

	memTotal := float64(256 * 1024 * 1024 * 1024) // 256 GiB (Threadripper PRO)
	memUsed := memTotal * (0.48 + 0.08*math.Sin(float64(p.tick)/40))

	// GPU temp: normally ~60-70C, spikes toward hard if Spike.
	gpuBase := 63 + 5*math.Sin(float64(p.tick)/25)
	gpuTemp := gpuBase
	gpuFan := 34 + float64(p.tick%8)
	if p.Spike {
		gpuTemp = math.Min(95, gpuBase+0.9*float64(p.tick))
		gpuFan = math.Min(100, 34+float64(p.tick))
	}

	return metrics.Snapshot{
		Time: now,
		CPU: metrics.CPU{
			Model:       "AMD EPYC Threadripper PRO",
			Cores:       32,
			Threads:     64,
			LoadPct:     load,
			Uptime:      now.Sub(p.start).Seconds(),
			MemTotal:    memTotal,
			MemUsed:     memUsed,
			MemAvail:    memTotal - memUsed,
			CpuTemp:     68 + 6*math.Sin(float64(p.tick)/30),
			CpuTempMax:  95,
			PerCoreLoad: make([]float64, 8),
		},
		GPUs: []metrics.GPU{
			{Index: 0, Name: "NVIDIA RTX 6000 Pro Blackwell WS", Temp: gpuTemp, Util: load * 0.9, Power: 285 + 20*math.Sin(float64(p.tick)/12), PowerLimit: 300, FanPct: gpuFan, FanControl: true, VRAMUsed: 32e9, VRAMTotal: 96e9, MemoryUtil: 60, MaxTemp: 90},
			{Index: 1, Name: "NVIDIA RTX 6000 Pro Blackwell WS", Temp: gpuBase - 3, Util: load * 0.75, Power: 250 + 15*math.Sin(float64(p.tick)/15), PowerLimit: 300, FanPct: 30 + float64(p.tick%6), FanControl: true, VRAMUsed: 48e9, VRAMTotal: 96e9, MemoryUtil: 82, MaxTemp: 90},
		},
		Disks: []metrics.Disk{
			{Mount: "/", Device: "nvme0n1", FSType: "ext4", TotalBytes: 2e12, FreeBytes: 1.2e12, ReadRate: 40e6, WriteRate: 12e6},
			{Mount: "/data", Device: "nvme1n1", FSType: "ext4", TotalBytes: 8e12, FreeBytes: 3.5e12, ReadRate: 90e6, WriteRate: 55e6},
		},
		Drives: []metrics.Drive{
			{Device: "nvme0n1", Model: "Samsung SSD 990 PRO 2TB", Serial: "S6P2NX0T712345A", Firmware: "4B2QJXD7", SizeBytes: 2e12, Temp: 43},
			{Device: "nvme1n1", Model: "Samsung SSD 990 PRO 2TB", Serial: "S6P2NX0T776543B", Firmware: "4B2QJXD7", SizeBytes: 2e12, Temp: 48},
		},
		Nets: []metrics.Net{
			{Interface: "eno1", RxRate: 60e6, TxRate: 42e6, Up: true},
		},
		Fans: []metrics.Fan{
			{ID: 184, Name: "CPU Fan", RPM: 1500 + float64(p.tick*7%600), Duty: 45, AutoDuty: 52},
			{ID: 187, Name: "Chassis 1", RPM: 1200 + float64(p.tick*5%400), Duty: 40, AutoDuty: 46},
			{ID: 189, Name: "Chassis 2", RPM: 1300 + float64(p.tick*5%400), Duty: 42, AutoDuty: 48},
		},
		Thermals: []metrics.Thermal{
			{ID: 1, Name: "CPU", Temp: 68 + 6*math.Sin(float64(p.tick)/30), Max: 95},
			{ID: 2, Name: "SYS", Temp: 45 + 3*math.Sin(float64(p.tick)/50), Max: 70},
			{ID: 3, Name: "GPU0 Inlet", Temp: gpuBase, Max: 90},
		},
		Extra: []metrics.Scalar{
			{Name: "P_12V", Value: 12.155, Unit: "V", Kind: metrics.KindVolts, Max: 13.845, Min: 10.205},
			{Name: "P_5V", Value: 5.06, Unit: "V", Kind: metrics.KindVolts, Max: 5.775, Min: 4.235},
			{Name: "P_3V3", Value: 3.332, Unit: "V", Kind: metrics.KindVolts, Max: 3.8024, Min: 2.8028},
			{Name: "P_5V_STBY", Value: 5.14, Unit: "V", Kind: metrics.KindVolts, Max: 5.775, Min: 4.235},
			{Name: "Uptime", Value: now.Sub(p.start).Seconds(), Unit: "s", Kind: metrics.KindCount},
		},
		VastRigs: []metrics.VastRig{
			{ID: 148260, Hostname: "endif01", GPUName: "RTX PRO 6000 WS", NumGPUs: 2, ListedGPUCost: 1.3, EarnHour: 1.87 + 0.15*math.Sin(float64(p.tick)/60), EarnDay: 44.9 + 3.6*math.Sin(float64(p.tick)/60), RentalsRunning: 2, ClientEndDate: float64(now.Add(36 * 24 * time.Hour).Unix()), EndDate: float64(now.Add(60 * 24 * time.Hour).Unix()), Verification: "verified", Reliability: 0.979, Geolocation: "Sweden, SE"},
		},
		VastGpus: []metrics.VastGpu{
			{Name: "RTX 5090", RentedVerified: 5810, AvailVerified: 1182, Usage: 83.1, PriceP10: 0.295, PriceMedian: 0.34, PriceP90: 0.6552, TFLOPSPerDollar: 321.2},
			{Name: "RTX PRO 6000 WS", RentedVerified: 520, AvailVerified: 57, Usage: 90.1, PriceP10: 0.8895, PriceMedian: 1.0725, PriceP90: 1.445, TFLOPSPerDollar: 111.0},
		},
		Containers: []metrics.Container{
			{ID: "3f2a1b0c9d8e", Name: "C.49321308", Image: "pytorch/pytorch:2.4.0-cuda12.4", Status: "Up 36 hours", CPUsPct: 42.5, MemUsedBytes: 1.2e9, MemTotalBytes: 62e9},
			{ID: "9e8d7c6b5a4f", Name: "C.49321247", Image: "nvidia/cuda:11.0.3-devel-ubuntu18.04", Status: "Up 36 hours", CPUsPct: 18.2, MemUsedBytes: 0.5e9, MemTotalBytes: 62e9},
		},
	}, nil
}

// Controller is a fake BMC fan controller that records writes so the audit path
// and UI flow can be exercised without a real board.
type Controller struct {
	mu       sync.Mutex
	Profiles []metrics.FanProfile
	Active   string // ACtive profile name
	Mode     string // fan mode: Auto/Full/Half
	fanDuty  map[int]float64
	gpuFan   map[int]float64
	Writes   []string // human-readable audit log
	// FailWrites, when true, makes every write return an error to test error UI.
	FailWrites bool
}

// NewController builds a fake BMC controller.
func NewController() *Controller {
	return &Controller{
		Profiles: []metrics.FanProfile{
			{Name: "default", Policies: []metrics.Policy{{FanSensors: []int{184, 187, 189}, Duty: []float64{30, 80, 100}, Ref: []float64{45, 70, 85}, Sensor: []int{1}, InitDuty: 30, PolicyType: 2}}},
			{Name: "CPU", Mode: "CPU", Policies: []metrics.Policy{{FanSensors: []int{184, 187, 189}, Duty: []float64{35, 90, 100}, Ref: []float64{50, 75, 90}, Sensor: []int{1}, InitDuty: 35, PolicyType: 2}}},
			{Name: "NEW_PROFILE", Policies: []metrics.Policy{{FanSensors: []int{184, 187, 189}, Duty: []float64{25, 75, 100}, Ref: []float64{40, 68, 85}, Sensor: []int{1}, InitDuty: 25, PolicyType: 2}}},
		},
		Active:  "CPU",
		Mode:    metrics.FanModeAuto,
		fanDuty: map[int]float64{},
		gpuFan:  map[int]float64{},
	}
}

// Capabilities mirrors the verified MC62-G40 board: mode switching + GPU fans
// supported; duty override NOT (BMC rejects PUT on Fanprofile, 501). Kept in
// sync so local dev shows the same UI state as the real rig.
func (c *Controller) Capabilities() metrics.Capabilities {
	return metrics.Capabilities{Profiles: true, DutyOverride: false, GPUFanControl: true, GPUPowerControl: true}
}

// ListFanProfiles implements Controller.
func (c *Controller) ListFanProfiles(_ context.Context) ([]metrics.FanProfile, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Profiles, nil
}

// ActiveFanProfile implements Controller.
func (c *Controller) ActiveFanProfile(_ context.Context) (metrics.FanProfileState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return metrics.FanProfileState{Active: c.Mode, Mode: c.Active}, nil
}

// SetFanMode implements Controller (fake: records the mode).
func (c *Controller) SetFanMode(_ context.Context, mode string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.FailWrites {
		return fmt.Errorf("mock: write failure injected")
	}
	switch mode {
	case metrics.FanModeAuto, metrics.FanModeHalf, metrics.FanModeFull:
	default:
		return fmt.Errorf("invalid fan mode %q", mode)
	}
	c.Mode = mode
	c.Writes = append(c.Writes, fmt.Sprintf("set fan mode -> %s", mode))
	return nil
}

// SetFanDuty implements Controller.
func (c *Controller) SetFanDuty(_ context.Context, fanID int, duty float64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.FailWrites {
		return fmt.Errorf("mock: write failure injected")
	}
	if duty < 0 || duty > 100 {
		return fmt.Errorf("duty %v out of range", duty)
	}
	c.fanDuty[fanID] = duty
	c.Writes = append(c.Writes, fmt.Sprintf("set fan %d duty -> %.0f%%", fanID, duty))
	return nil
}

// SetGPUFan implements Controller.
func (c *Controller) SetGPUFan(_ context.Context, gpuIndex int, pct float64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.FailWrites {
		return fmt.Errorf("mock: write failure injected")
	}
	c.gpuFan[gpuIndex] = pct
	c.Writes = append(c.Writes, fmt.Sprintf("set GPU %d fan -> %.0f%%", gpuIndex, pct))
	return nil
}

// SetGPUPowerLimit implements Controller (fake: records the write).
func (c *Controller) SetGPUPowerLimit(_ context.Context, gpuIndex int, watts float64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.FailWrites {
		return fmt.Errorf("mock: write failure injected")
	}
	c.Writes = append(c.Writes, fmt.Sprintf("set GPU %d power limit -> %.0fW", gpuIndex, watts))
	return nil
}

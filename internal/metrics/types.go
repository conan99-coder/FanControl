// Package metrics defines the core telemetry data model for FanControl.
// Every provider (mock, host, gpu, redfish) produces a Snapshot, and every
// consumer (poller, server, dashboard) reads that model. Keeping it here keeps
// providers interchangeable — the whole point of the Provider abstraction.
package metrics

import "time"

// Kind classifies a scalar reading so the dashboard can pick an appropriate
// widget (gauge, bar, number, sparkline) and formatting.
type Kind int

const (
	KindCount   Kind = iota
	KindPercent      // utilization, duty %, load
	KindTemp         // °C
	KindFanRPM       // fan speed in RPM
	KindPower        // watts
	KindBytes        // base-2 byte quantity stored (disk used / free)
	KindRate         // bytes-per-second or ops-per-second (already computed rate)
	KindVolts
)

// Scalar is a single named measurement with units and optional gauge bounds.
type Scalar struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
	Kind  Kind    `json:"kind"`
	Min   float64 `json:"min"` // low gauge bound (0 if irrelevant)
	Max   float64 `json:"max"` // high gauge bound (0 if irrelevant)
}

// Fan is a controllable/observable fan attached to the motherboard BMC.
type Fan struct {
	ID     int     `json:"id"`   // BMC sensor id
	Name   string  `json:"name"` // human label / tach name if known
	RPM    float64 `json:"rpm"`  // current speed in RPM
	Duty   float64 `json:"duty"` // manual duty % if a value was set/known
	MaxRPM float64 `json:"maxRpm"`
	// AutoDuty is the estimated duty % the active profile's curve produces at
	// the current temperature (interpolated from arrRef/arrDuty). 0 = unknown.
	AutoDuty float64 `json:"autoDuty"`
}

// Thermal is a temperature point from the BMC (CPU/cpu package/chassis/etc).
type Thermal struct {
	ID   int     `json:"id"` // BMC sensor id
	Name string  `json:"name"`
	Temp float64 `json:"temp"` // °C
	Max  float64 `json:"max"`  // threshold, if reported
	Min  float64 `json:"min"`  // low threshold, if reported
}

// GPU holds a snapshot of one NVIDIA GPU.
type GPU struct {
	Index      int     `json:"index"` // NVML index / nvidia-smi ordering
	Name       string  `json:"name"`
	Temp       float64 `json:"temp"`       // °C
	Util       float64 `json:"util"`       // %
	Power      float64 `json:"power"`      // W
	PowerLimit float64 `json:"powerLimit"` // W
	FanPct     float64 `json:"fanPct"`     // %
	FanControl bool    `json:"fanControl"` // whether fan writes are supported
	VRAMUsed   float64 `json:"vramUsed"`   // bytes
	VRAMTotal  float64 `json:"vramTotal"`  // bytes
	MemoryUtil float64 `json:"memoryUtil"` // %
	MaxTemp    float64 `json:"maxTemp"`    // throttle temperature threshold
}

// Disk holds a snapshot of one mounted filesystem + its IO rates.
type Disk struct {
	Mount      string  `json:"mount"`  // mount point
	Device     string  `json:"device"` // underlying block device
	FSType     string  `json:"fsType"`
	TotalBytes float64 `json:"totalBytes"`
	FreeBytes  float64 `json:"freeBytes"`
	ReadRate   float64 `json:"readRate"`  // bytes/sec
	WriteRate  float64 `json:"writeRate"` // bytes/sec
}

// Drive is a physical NVMe drive (from /sys/class/nvme), distinct from a
// mounted filesystem. Serial + model identify the hardware; Temp is the drive's
// own temperature.
type Drive struct {
	Device    string  `json:"device"` // e.g. nvme0n1
	Model     string  `json:"model"`
	Serial    string  `json:"serial"`
	Firmware  string  `json:"firmware,omitempty"`
	SizeBytes float64 `json:"sizeBytes"`
	Temp      float64 `json:"temp"` // °C
}

// Net holds throughput for one network interface.
type Net struct {
	Interface string  `json:"interface"`
	RxRate    float64 `json:"rxRate"` // bytes/sec
	TxRate    float64 `json:"txRate"` // bytes/sec
	Up        bool    `json:"up"`
}

// VastRig is a read-only summary of one Vast.ai machine we host (from
// `vastai show machines --raw`). Earnings/rates are what the host earns /
// lists; contract end dates are the renter contracts (client) and the machine
// listing (end). Never includes secrets or the machine's public IP.
type VastRig struct {
	ID             int     `json:"id"`
	Hostname       string  `json:"hostname"`
	GPUName        string  `json:"gpuName"`
	NumGPUs        int     `json:"numGpus"`
	ListedGPUCost  float64 `json:"listedGpuCost"` // $/h listed for the GPU
	EarnHour       float64 `json:"earnHour"`      // $/h currently earned
	EarnDay        float64 `json:"earnDay"`       // $/day currently earned
	RentalsRunning int     `json:"rentalsRunning"`
	ClientEndDate  float64 `json:"clientEndDate"` // unix seconds, 0 = none
	EndDate        float64 `json:"endDate"`       // unix seconds, 0 = none
	Verification   string  `json:"verification"`
	Reliability    float64 `json:"reliability"` // 0..1
	Geolocation    string  `json:"geolocation"`
}

// Container is a read-only summary of one Docker container on the rig — the
// renters' Vast instances are containers named C.<instance_id>. Metadata only
// (name, image/template, status, CPU/mem); never touches container contents,
// files, or logs (tenant data).
type Container struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Image         string  `json:"image"`
	Status        string  `json:"status"` // e.g. "Up 36 hours"
	CPUsPct       float64 `json:"cpusPct"`
	MemUsedBytes  float64 `json:"memUsedBytes"`
	MemTotalBytes float64 `json:"memTotalBytes"` // 0 if unknown
}

// VastGpu is one row of the Vast.ai GPU marketplace metrics (supply, demand,
// pricing per GPU type) from `vastai metrics gpu`.
type VastGpu struct {
	Name            string  `json:"name"`
	RentedVerified  int     `json:"rentedVerified"`
	AvailVerified   int     `json:"availVerified"`
	Usage           float64 `json:"usage"` // current utilization %
	PriceP10        float64 `json:"priceP10"`
	PriceMedian     float64 `json:"priceMedian"`
	PriceP90        float64 `json:"priceP90"`
	TFLOPSPerDollar float64 `json:"tflopsPerDollar"`
}

// Snapshot is the complete telemetry picture for one poll tick.
type Snapshot struct {
	Time       time.Time   `json:"time"`
	CPU        CPU         `json:"cpu"`
	GPUs       []GPU       `json:"gpus"`
	Disks      []Disk      `json:"disks"`
	Drives     []Drive     `json:"drives"`
	Nets       []Net       `json:"nets"`
	Fans       []Fan       `json:"fans"`
	Thermals   []Thermal   `json:"thermals"`
	Extra      []Scalar    `json:"extra"`
	VastRigs   []VastRig   `json:"vastRigs"`
	VastGpus   []VastGpu   `json:"vastGpus"`
	Containers []Container `json:"containers"`
}

// CPU holds CPU telemetry.
type CPU struct {
	Model       string    `json:"model"`
	Cores       int       `json:"cores"`
	Threads     int       `json:"threads"`
	LoadPct     float64   `json:"loadPct"`  // aggregate load
	Uptime      float64   `json:"uptime"`   // seconds
	MemTotal    float64   `json:"memTotal"` // bytes
	MemUsed     float64   `json:"memUsed"`  // bytes
	MemAvail    float64   `json:"memAvail"` // bytes
	CpuTemp     float64   `json:"cpuTemp"`  // °C
	CpuTempMax  float64   `json:"cpuTempMax"`
	PerCoreLoad []float64 `json:"perCoreLoad"` // per-core busy %
}

// Aggregate returns a flat ordered list of Scalars for consumers that prefer a
// simple list over the structured sections (e.g. the raw /api/metrics dump).
func (s Snapshot) Scalars() []Scalar {
	var out []Scalar
	for _, f := range s.Fans {
		out = append(out, Scalar{Name: "Fan " + f.Name, Value: f.RPM, Unit: "rpm", Kind: KindFanRPM})
	}
	for _, t := range s.Thermals {
		out = append(out, Scalar{Name: t.Name, Value: t.Temp, Unit: "C", Kind: KindTemp, Max: t.Max})
	}
	for _, g := range s.GPUs {
		out = append(out,
			Scalar{Name: g.Name + " Temp", Value: g.Temp, Unit: "C", Kind: KindTemp, Max: g.MaxTemp},
			Scalar{Name: g.Name + " Util", Value: g.Util, Unit: "%", Kind: KindPercent, Max: 100},
			Scalar{Name: g.Name + " Power", Value: g.Power, Unit: "W", Kind: KindPower, Max: g.PowerLimit},
			Scalar{Name: g.Name + " Fan", Value: g.FanPct, Unit: "%", Kind: KindPercent, Max: 100},
		)
	}
	out = append(out, s.Extra...)
	return out
}

package metrics

import "context"

// Provider collects a Snapshot from a single source. Real providers (host,
// gpu, redfish) read live hardware; the mock provider emits deterministic data.
// This is the seam that lets us run the whole app against fake data on a
// Windows dev box and real data on the Linux rig.
type Provider interface {
	// Name identifies the provider (used in logs and the --provider flag).
	Name() string
	// Collect returns a fresh snapshot. Providers merge into the full picture;
	// a Snapshot's zero-value sections are left untouched by other providers.
	Collect(ctx context.Context) (Snapshot, error)
	// Close releases any resources held by the provider.
	Close() error
}

// Controller performs control-plane actions (mostly fan writes). It is
// deliberately separate from Provider so that dry-run and read-only modes can
// swap in a no-op Controller without touching the read path.
type Controller interface {
	// ListFanProfiles returns the BMC fan profiles available on the board.
	ListFanProfiles(ctx context.Context) ([]FanProfile, error)
	// ActiveFanProfile returns the currently active profile name + mode.
	ActiveFanProfile(ctx context.Context) (FanProfileState, error)
	// SetFanMode switches the global fan mode. Confirmed allowable values on
	// the MC62-G40 (SetFanModeActionInfo): "Full" | "Half" | "Auto".
	SetFanMode(ctx context.Context, mode string) error
	// SetFanDuty overrides the duty (%) for one fan sensor id in the active
	// profile's curve, writing the whole Fanprofile via the BMC.
	SetFanDuty(ctx context.Context, fanID int, duty float64) error
	// SetGPUFan sets target fan % on a GPU via NVML (nvidia-smi -c).
	SetGPUFan(ctx context.Context, gpuIndex int, pct float64) error
	// SetGPUPowerLimit sets the GPU power limit in watts (nvidia-smi -pl).
	SetGPUPowerLimit(ctx context.Context, gpuIndex int, watts float64) error
	// Capabilities reports what this controller can actually do (probed).
	Capabilities() Capabilities
}

// Fan modes confirmed via SetFanModeActionInfo on the MC62-G40 BMC.
const (
	FanModeAuto = "Auto"
	FanModeHalf = "Half"
	FanModeFull = "Full"
)

// ListingPatch describes a partial listing update for a Vast.ai machine.
// Nil fields are left unchanged.
type ListingPatch struct {
	PriceGpu      *float64 // $/h per GPU
	PriceDisk     *float64 // $/GB/month
	PriceInetUp   *float64 // $/GB upload bandwidth
	PriceInetDown *float64 // $/GB download bandwidth
	PriceMinBid   *float64 // $/h minimum bid floor
	EndDateUnix   *int64   // listing expiration (unix seconds)
}

// VastOps performs host-side writes against the Vast.ai platform (listing
// prices, unlisting, maintenance scheduling). Separate from Controller so the
// fan-control service stays fan-focused; the control layer gates both the same
// way (monitor/dry-run/audit).
type VastOps interface {
	// UpdateListing updates a machine's listing via `vastai list machines`.
	UpdateListing(ctx context.Context, machineID int, p ListingPatch) error
	// UnlistMachine removes the machine's ask contracts (off the market).
	UnlistMachine(ctx context.Context, machineID int) error
	// ScheduleMaintenance notifies renters of a maintenance window.
	ScheduleMaintenance(ctx context.Context, machineID int, sdateUnix int64, durationHours float64, category string) error
}

// FanProfile mirrors the BMC FanprofileService profile shape.
type FanProfile struct {
	Name     string   `json:"name"`
	Policies []Policy `json:"policies"`
	// Mode is the active-mode policy selector string when this is the active one.
	// Present only on state responses.
	Mode string `json:"mode,omitempty"`
}

// Policy is one fan policy within a profile.
type Policy struct {
	FanSensors []int     `json:"fanSensors"`
	Duty       []float64 `json:"duty"`
	Ref        []float64 `json:"ref"` // temp ref points (°C)
	Sensor     []int     `json:"sensor"`
	InitDuty   float64   `json:"initDuty"`
	PolicyType int       `json:"policyType"`
}

// FanProfileState is the currently active profile + mode.
type FanProfileState struct {
	Active string `json:"active"`
	Mode   string `json:"mode"`
}

// Capabilities describes what a controller supports, determined by probing
// (e.g. whether the GPU accepts fan writes, whether the BMC exposes profiles).
type Capabilities struct {
	Profiles        bool `json:"profiles"`        // BMC profile switching available
	DutyOverride    bool `json:"dutyOverride"`    // arrDuty editing available
	GPUFanControl   bool `json:"gpuFanControl"`   // nvidia-smi -c fan writes work
	GPUPowerControl bool `json:"gpuPowerControl"` // nvidia-smi -pl power limit writes work
}

package metrics

import "context"

// Discovery is a structured inventory of what a provider detected. It is used
// for the sensor-discovery dump on the real rig (the read-only validation step)
// so the operator can confirm the exact sensor IDs / device paths / GPU indices
// before any fan-control wiring is trusted.
type Discovery struct {
	// Source is the provider name that produced this inventory.
	Source string `json:"source"`
	// Thermals lists detected temperature sensors with their IDs.
	Thermals []DiscoveredSensor `json:"thermals,omitempty"`
	// Fans lists detected fans with their sensor IDs.
	Fans []DiscoveredSensor `json:"fans,omitempty"`
	// GPUs lists detected GPUs with their NVML index and names.
	GPUs []DiscoveredGPU `json:"gpus,omitempty"`
	// Disks lists detected block devices / mounts.
	Disks []string `json:"disks,omitempty"`
	// Nets lists detected network interfaces.
	Nets []string `json:"nets,omitempty"`
	// CPU describes the detected CPU topology.
	CPU DiscoveredCPU `json:"cpu"`
	// Meta holds provider-specific metadata (e.g. BMC model, driver version).
	Meta map[string]string `json:"meta,omitempty"`
}

// DiscoveredSensor is one detected sensor.
type DiscoveredSensor struct {
	ID   int    `json:"id,omitempty"`
	Name string `json:"name"`
	HMON string `json:"hmon,omitempty"` // hwmon chip/dir, if applicable
}

// DiscoveredGPU is one detected GPU.
type DiscoveredGPU struct {
	Index      int    `json:"index"`
	Name       string `json:"name"`
	FanControl bool   `json:"fan_control"` // whether fan writes are available
}

// DiscoveredCPU describes CPU topology.
type DiscoveredCPU struct {
	Model   string `json:"model"`
	Cores   int    `json:"cores"`
	Threads int    `json:"threads"`
}

// Discoverer is an optional interface providers may implement to expose their
// detected inventory. The poller aggregates all providers that support it.
type Discoverer interface {
	Discover(ctx context.Context) Discovery
}

// Package composite merges a BMC controller (fan profiles, duty, ~or mock~)
// with a GPU controller so a single metrics.Controller serves every write.
// Fan-profile operations go to the BMC; GPU fan writes go to the GPU
// controller. Capabilities are the union of both. If one backend is nil (e.g.
// no GPU, or no BMC configured), those operations are reported unavailable.
package composite

import (
	"context"
	"errors"

	"github.com/hedchr/fanctrl/internal/metrics"
)

// errNoBMC is returned when no BMC controller is configured.
var errNoBMC = errors.New("no BMC configured")

// errNoGPU is returned when no GPU controller is configured.
var errNoGPU = errors.New("no GPU controller configured")

// Controller is the merged controller.
type Controller struct {
	bmc metrics.Controller
	gpu metrics.Controller
}

// New builds a composite controller. Either backend may be nil.
func New(bmc, gpu metrics.Controller) *Controller {
	return &Controller{bmc: bmc, gpu: gpu}
}

// Capabilities merges both backends' capabilities.
func (c *Controller) Capabilities() metrics.Capabilities {
	var caps metrics.Capabilities
	if c.bmc != nil {
		b := c.bmc.Capabilities()
		caps.Profiles = caps.Profiles || b.Profiles
		caps.DutyOverride = caps.DutyOverride || b.DutyOverride
		caps.GPUFanControl = caps.GPUFanControl || b.GPUFanControl
	}
	if c.gpu != nil {
		g := c.gpu.Capabilities()
		caps.GPUFanControl = caps.GPUFanControl || g.GPUFanControl
		caps.GPUPowerControl = caps.GPUPowerControl || g.GPUPowerControl
	}
	return caps
}

// ListFanProfiles delegates to the BMC.
func (c *Controller) ListFanProfiles(ctx context.Context) ([]metrics.FanProfile, error) {
	if c.bmc == nil {
		return nil, errNoBMC
	}
	return c.bmc.ListFanProfiles(ctx)
}

// ActiveFanProfile delegates to the BMC.
func (c *Controller) ActiveFanProfile(ctx context.Context) (metrics.FanProfileState, error) {
	if c.bmc == nil {
		return metrics.FanProfileState{}, errNoBMC
	}
	return c.bmc.ActiveFanProfile(ctx)
}

// SetFanMode delegates to the BMC.
func (c *Controller) SetFanMode(ctx context.Context, mode string) error {
	if c.bmc == nil {
		return errNoBMC
	}
	return c.bmc.SetFanMode(ctx, mode)
}

// SetFanDuty delegates to the BMC.
func (c *Controller) SetFanDuty(ctx context.Context, fanID int, duty float64) error {
	if c.bmc == nil {
		return errNoBMC
	}
	return c.bmc.SetFanDuty(ctx, fanID, duty)
}

// SetGPUFan delegates to the GPU controller.
func (c *Controller) SetGPUFan(ctx context.Context, gpuIndex int, pct float64) error {
	if c.gpu == nil {
		return errNoGPU
	}
	return c.gpu.SetGPUFan(ctx, gpuIndex, pct)
}

// SetGPUPowerLimit delegates to the GPU controller.
func (c *Controller) SetGPUPowerLimit(ctx context.Context, gpuIndex int, watts float64) error {
	if c.gpu == nil {
		return errNoGPU
	}
	return c.gpu.SetGPUPowerLimit(ctx, gpuIndex, watts)
}

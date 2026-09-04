// Package gpu also implements control-plane fan writes via nvidia-smi. Fan
// control is exposed with `nvidia-smi -i <index> -c <pct>` (or `<pct>0` targets
// auto). Support is probed at construction: if a GPU reports fan.speed as
// [N/A]/locked, GPU fan control is reported unavailable and writes are refused
// so we never yell at a card that can't obey.
package gpu

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/hedchr/fanctrl/internal/metrics"
)

// Controller performs GPU fan writes via nvidia-smi.
type Controller struct {
	smi       string
	supported bool // whether any GPU accepts fan writes
}

// NewController builds a GPU fan controller. It probes whether the cards report
// a usable numeric fan speed; if they do, fan writes are treated as available.
func NewController(smi string) *Controller {
	if smi == "" {
		smi = "nvidia-smi"
	}
	c := &Controller{smi: smi}
	// Probe whether fan control is plausible: an unlocked card reports a
	// numeric fan speed for each GPU.
	c.supported = ProbeFanControl(context.Background(), smi)
	return c
}

// Capabilities reports GPU fan control only if the probe passed. Power-limit
// writes are reported available (nvidia-smi -pl is supported on these cards).
func (c *Controller) Capabilities() metrics.Capabilities {
	return metrics.Capabilities{GPUFanControl: c.supported, GPUPowerControl: true}
}

// SetGPUPowerLimit sets the GPU power limit in watts via `nvidia-smi -i <idx>
// -pl <watts>`. The driver clamps/validates the range and returns an error for
// values outside the card's min/max.
func (c *Controller) SetGPUPowerLimit(ctx context.Context, gpuIndex int, watts float64) error {
	if watts < 50 || watts > 1000 {
		return fmt.Errorf("watts must be 50-1000, got %v", watts)
	}
	cmd := exec.CommandContext(ctx, c.smi, "-i", strconv.Itoa(gpuIndex), "-pl", strconv.Itoa(int(watts)))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nvidia-smi -pl: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// SetGPUFan sets fan % for a GPU via `nvidia-smi -i <idx> -c <pct>`.
func (c *Controller) SetGPUFan(ctx context.Context, gpuIndex int, pct float64) error {
	if !c.supported {
		return fmt.Errorf("GPU fan control not supported (probe failed)")
	}
	if pct < 0 || pct > 100 {
		return fmt.Errorf("pct must be 0-100, got %v", pct)
	}
	// pct=0 requests the driver's automatic curve; 1-100 sets a fixed speed.
	cmd := exec.CommandContext(ctx, c.smi, "-i", fmt.Sprintf("%d", gpuIndex), "-c", strconv.Itoa(int(pct)))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nvidia-smi -c: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// The remaining Controller methods are not supported by the GPU controller.
// They are handled by the composite controller via the BMC. Returning a clear
// error here prevents accidental misuse if someone wires this as the sole
// controller.

func (c *Controller) ListFanProfiles(context.Context) ([]metrics.FanProfile, error) {
	return nil, fmt.Errorf("GPU controller does not manage fan profiles")
}

func (c *Controller) ActiveFanProfile(context.Context) (metrics.FanProfileState, error) {
	return metrics.FanProfileState{}, fmt.Errorf("GPU controller does not manage fan profiles")
}

func (c *Controller) SetFanMode(context.Context, string) error {
	return fmt.Errorf("GPU controller does not manage fan modes")
}

func (c *Controller) SetFanDuty(context.Context, int, float64) error {
	return fmt.Errorf("GPU controller does not manage fan duty")
}

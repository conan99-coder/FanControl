package poller

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hedchr/fanctrl/internal/config"
	"github.com/hedchr/fanctrl/internal/metrics"
)

// Governor watches the latest snapshot for hard temperature breaches. When one
// is detected, it invokes a revert callback (e.g. switch to the safe "CPU"
// profile / enable automatic fan behavior) and records the trigger. It stays
// in "cooling" state until the cooldown elapses and temps are back under the
// hard limit.
type governor struct {
	cfg config.Thresholds
	log *slog.Logger

	mu        sync.Mutex
	revert    func(context.Context, string) error
	tripped   bool
	triggered time.Time
	cooldown  map[string]time.Time // metric -> time reverted
}

// newGovernor builds a governor with the given thresholds.
func newGovernor(cfg config.Thresholds) *governor {
	return &governor{
		cfg:      cfg,
		log:      slog.Default(),
		cooldown: map[string]time.Time{},
	}
}

// SetRevert installs the callback invoked when the governor must act. It is set
// by the wiring layer so the governor stays control-agnostic.
func (g *governor) SetRevert(fn func(context.Context, string) error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.revert = fn
}

// SetThresholds hot-applies new safety thresholds.
func (g *governor) SetThresholds(cfg config.Thresholds) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cfg = cfg
}

// Evaluate checks a snapshot and triggers a revert if a hard limit is exceeded.
// It returns nil if no action is needed, or an error describing the revert that
// was (or attempted to be) performed.
func (g *governor) Evaluate(ctx context.Context, s metrics.Snapshot) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.revert == nil {
		return nil
	}

	hard := g.cfg.GPUTempHard
	for _, gpu := range s.GPUs {
		if gpu.Temp >= hard {
			return g.fire(ctx, fmt.Sprintf("GPU%d %.0fC", gpu.Index, gpu.Temp))
		}
	}
	if s.CPU.CpuTemp >= g.cfg.CPUTempHard && s.CPU.CpuTemp > 0 {
		return g.fire(ctx, fmt.Sprintf("CPU %.0fC", s.CPU.CpuTemp))
	}
	return nil
}

// fire invokes the revert callback once per cooldown window.
func (g *governor) fire(ctx context.Context, reason string) error {
	now := time.Now()
	if last, ok := g.cooldown[reason]; ok && now.Sub(last) < g.cfg.Cooldown {
		// Still cooling; don't spam.
		return nil
	}
	g.cooldown[reason] = now
	g.tripped = true
	g.triggered = now
	g.log.Warn("safety governor triggered; reverting fan control", "reason", reason)

	if err := g.revert(ctx, reason); err != nil {
		g.log.Error("governor revert failed", "reason", reason, "err", err)
		return fmt.Errorf("governor revert failed: %w", err)
	}
	return nil
}

// Tripped reports whether the governor has recently fired.
func (g *governor) Tripped() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.tripped
}

// LastTrigger returns the last trigger reason/time.
func (g *governor) LastTrigger() (string, time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.tripped {
		return "", time.Time{}
	}
	// Return a (reason approximated by last cooldown key).
	var reason string
	for k := range g.cooldown {
		reason = k
		break
	}
	return reason, g.triggered
}

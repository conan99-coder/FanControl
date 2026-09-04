// Package control provides the top-level fan-control service that the HTTP
// layer talks to, applying the safety modes (read-only, dry-run) and the
// governor. It wraps a real or mock Controller and a Poller.
package control

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/hedchr/fanctrl/internal/metrics"
	"github.com/hedchr/fanctrl/internal/poller"
)

// Service orchestrates control actions with safety gates.
type Service struct {
	// ctrl is the underlying controller (real or mock).
	ctrl metrics.Controller
	// vast is the optional Vast.ai host-ops backend (listing/maintenance).
	vast metrics.VastOps
	// poller gives access to the latest snapshot and governor.
	p       *poller.Poller
	dry     bool
	ro      bool
	mu      sync.RWMutex
	monitor bool // true => Monitor mode (writes refused); default ON
	log     *slog.Logger
	audit   *AuditLog
}

// Options configures the Service.
type Options struct {
	DryRun   bool
	ReadOnly bool
	Audit    *AuditLog
	// VastOps is the optional Vast.ai host-ops backend (listing updates,
	// unlisting, maintenance scheduling). Nil => those endpoints unavailable.
	VastOps metrics.VastOps
	// MonitorMode controls the initial mode. The zero value (false) means
	// "default to Monitor ON" — the safe state: display values only, every
	// write refused until an admin switches to Control. To start in Control
	// mode, set this to true and then SetMonitor(false) at startup.
	MonitorMode *bool
}

// New builds a Service.
func New(ctrl metrics.Controller, p *poller.Poller, o Options, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	// Monitor defaults ON unless a value is explicitly provided — never default
	// to writes.
	mon := true
	if o.MonitorMode != nil {
		mon = *o.MonitorMode
	}
	s := &Service{ctrl: ctrl, vast: o.VastOps, p: p, dry: o.DryRun, ro: o.ReadOnly, monitor: mon, log: log, audit: o.Audit}
	// Wire the governor's revert to a safe default: switch to the "CPU" profile
	// (this is what the board ships with as a sane default). If dry-run, or
	// read-only, or monitor-mode, log instead of writing.
	s.p.Governor().SetRevert(func(ctx context.Context, reason string) error {
		if s.dry || s.ro || s.Monitor() {
			s.log.Info("governor would revert (dry/read-only/monitor)", "reason", reason)
			return nil
		}
		return s.ctrl.SetFanMode(ctx, metrics.FanModeAuto)
	})
	return s
}

// Monitor reports whether the service is in Monitor mode (writes refused).
func (s *Service) Monitor() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.monitor
}

// SetController hot-replaces the underlying controller.
func (s *Service) SetController(c metrics.Controller) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ctrl = c
}

// SetVastOps hot-replaces the Vast.ai host-ops backend.
func (s *Service) SetVastOps(v metrics.VastOps) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vast = v
}

// errNoVastOps is returned when no Vast host-ops backend is configured.
var errNoVastOps = errors.New("vast host-ops not configured")

// SetVastListing updates a machine's listing (prices/expiration), respecting
// the same gates as fan writes.
func (s *Service) SetVastListing(ctx context.Context, actor string, machineID int, p metrics.ListingPatch) error {
	if s.Monitor() {
		return errMonitor
	}
	if s.ro {
		return errReadOnly
	}
	if s.vast == nil {
		return errNoVastOps
	}
	if s.dry {
		s.log.Info("dry-run: would update vast listing", "actor", actor, "machine", machineID)
		s.auditRecord(actor, "vast_listing", map[string]any{"machine": machineID}, "dry-run")
		return nil
	}
	if err := s.vast.UpdateListing(ctx, machineID, p); err != nil {
		return err
	}
	s.auditRecord(actor, "vast_listing", map[string]any{"machine": machineID}, "ok")
	return nil
}

// UnlistVastMachine unlists a machine (off the market), respecting the gates.
func (s *Service) UnlistVastMachine(ctx context.Context, actor string, machineID int) error {
	if s.Monitor() {
		return errMonitor
	}
	if s.ro {
		return errReadOnly
	}
	if s.vast == nil {
		return errNoVastOps
	}
	if s.dry {
		s.log.Info("dry-run: would unlist vast machine", "actor", actor, "machine", machineID)
		s.auditRecord(actor, "vast_unlist", map[string]any{"machine": machineID}, "dry-run")
		return nil
	}
	if err := s.vast.UnlistMachine(ctx, machineID); err != nil {
		return err
	}
	s.auditRecord(actor, "vast_unlist", map[string]any{"machine": machineID}, "ok")
	return nil
}

// ScheduleVastMaintenance schedules a maintenance window, respecting the gates.
func (s *Service) ScheduleVastMaintenance(ctx context.Context, actor string, machineID int, sdateUnix int64, durationHours float64, category string) error {
	if s.Monitor() {
		return errMonitor
	}
	if s.ro {
		return errReadOnly
	}
	if s.vast == nil {
		return errNoVastOps
	}
	if s.dry {
		s.log.Info("dry-run: would schedule maintenance", "actor", actor, "machine", machineID, "category", category)
		s.auditRecord(actor, "vast_maintenance", map[string]any{"machine": machineID, "category": category}, "dry-run")
		return nil
	}
	if err := s.vast.ScheduleMaintenance(ctx, machineID, sdateUnix, durationHours, category); err != nil {
		return err
	}
	s.auditRecord(actor, "vast_maintenance", map[string]any{"machine": machineID, "category": category}, "ok")
	return nil
}

// SetDryRun toggles dry-run at runtime (hot-apply).
func (s *Service) SetDryRun(dry bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dry = dry
}

// SetReadOnly toggles read-only at runtime (hot-apply).
func (s *Service) SetReadOnly(ro bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ro = ro
}

// Record appends an audit entry (exported for the settings API).
func (s *Service) Record(actor, action string, detail map[string]any, result string) {
	s.auditRecord(actor, action, detail, result)
}

// SetMonitor switches between Monitor (true) and Control (false). Returns the
// resulting state; callers should gate this behind admin auth.
func (s *Service) SetMonitor(on bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.monitor = on
	s.log.Info("fan control mode changed", "monitor", on)
	return s.monitor
}

// Capabilities delegates to the controller.
func (s *Service) Capabilities() metrics.Capabilities {
	return s.ctrl.Capabilities()
}

// ListProfiles lists fan profiles.
func (s *Service) ListProfiles(ctx context.Context) ([]metrics.FanProfile, error) {
	return s.ctrl.ListFanProfiles(ctx)
}

// ActiveProfile returns the active profile.
func (s *Service) ActiveProfile(ctx context.Context) (metrics.FanProfileState, error) {
	return s.ctrl.ActiveFanProfile(ctx)
}

// SetFanMode switches the global fan mode (Auto/Full/Half), respecting gates.
func (s *Service) SetFanMode(ctx context.Context, actor, mode string) error {
	if s.Monitor() {
		return errMonitor
	}
	if s.ro {
		return errReadOnly
	}
	if s.dry {
		s.log.Info("dry-run: would set fan mode", "actor", actor, "mode", mode)
		s.auditRecord(actor, "set_fan_mode", map[string]any{"mode": mode}, "dry-run")
		return nil
	}
	if err := s.ctrl.SetFanMode(ctx, mode); err != nil {
		return err
	}
	s.auditRecord(actor, "set_fan_mode", map[string]any{"mode": mode}, "ok")
	return nil
}

// SetFanDuty overrides duty for a fan sensor, respecting safety gates.
func (s *Service) SetFanDuty(ctx context.Context, actor string, fanID int, duty float64) error {
	if s.Monitor() {
		return errMonitor
	}
	if s.ro {
		return errReadOnly
	}
	if s.dry {
		s.log.Info("dry-run: would set fan duty", "actor", actor, "fan", fanID, "duty", duty)
		s.auditRecord(actor, "set_fan_duty", map[string]any{"fan": fanID, "duty": duty}, "dry-run")
		return nil
	}
	if err := s.ctrl.SetFanDuty(ctx, fanID, duty); err != nil {
		return err
	}
	s.auditRecord(actor, "set_fan_duty", map[string]any{"fan": fanID, "duty": duty}, "ok")
	return nil
}

// SetGPUFan sets GPU fan %, respecting safety gates.
func (s *Service) SetGPUFan(ctx context.Context, actor string, gpuIndex int, pct float64) error {
	if s.Monitor() {
		return errMonitor
	}
	if s.ro {
		return errReadOnly
	}
	if !s.ctrl.Capabilities().GPUFanControl {
		return errGPUFanLocked
	}
	if s.dry {
		s.log.Info("dry-run: would set gpu fan", "actor", actor, "gpu", gpuIndex, "pct", pct)
		s.auditRecord(actor, "set_gpu_fan", map[string]any{"gpu": gpuIndex, "pct": pct}, "dry-run")
		return nil
	}
	if err := s.ctrl.SetGPUFan(ctx, gpuIndex, pct); err != nil {
		return err
	}
	s.auditRecord(actor, "set_gpu_fan", map[string]any{"gpu": gpuIndex, "pct": pct}, "ok")
	return nil
}

// SetGPUPowerLimit sets a GPU power limit (watts), respecting safety gates.
func (s *Service) SetGPUPowerLimit(ctx context.Context, actor string, gpuIndex int, watts float64) error {
	if s.Monitor() {
		return errMonitor
	}
	if s.ro {
		return errReadOnly
	}
	if s.dry {
		s.log.Info("dry-run: would set gpu power limit", "actor", actor, "gpu", gpuIndex, "watts", watts)
		s.auditRecord(actor, "set_gpu_power_limit", map[string]any{"gpu": gpuIndex, "watts": watts}, "dry-run")
		return nil
	}
	if err := s.ctrl.SetGPUPowerLimit(ctx, gpuIndex, watts); err != nil {
		return err
	}
	s.auditRecord(actor, "set_gpu_power_limit", map[string]any{"gpu": gpuIndex, "watts": watts}, "ok")
	return nil
}

// Audit returns the recent audit trail.
func (s *Service) Audit() []AuditEntry {
	if s.audit == nil {
		return nil
	}
	return s.audit.Recent()
}

// Status reports the current dynamic state (modes + governor).
func (s *Service) Status() Status {
	reason, when := s.p.Governor().LastTrigger()
	return Status{
		ReadOnly:        s.ro,
		DryRun:          s.dry,
		Monitor:         s.Monitor(),
		GovernorTripped: s.p.Governor().Tripped(),
		GovernorReason:  reason,
		GovernorTime:    when,
		Capabilities:    s.ctrl.Capabilities(),
		Thresholds:      s.p.Thresholds(),
	}
}

func (s *Service) auditRecord(actor, action string, detail map[string]any, result string) {
	if s.audit == nil {
		return
	}
	s.audit.Add(AuditEntry{Actor: actor, Action: action, Detail: detail, Result: result})
}

package control

import (
	"context"
	"testing"

	"github.com/hedchr/fanctrl/internal/config"
	"github.com/hedchr/fanctrl/internal/metrics"
	"github.com/hedchr/fanctrl/internal/poller"
	"github.com/hedchr/fanctrl/internal/providers/mock"
)

// newTestService wires a mock controller + poller into a Service with the given
// safety options.
func newTestService(o Options) (*Service, *mock.Controller) {
	mp := mock.NewProvider()
	mc := mock.NewController()
	p := poller.New([]metrics.Provider{mp}, cfgThresholds(), poller.NewRing(10), nil)
	p.Tick(context.Background())
	return New(mc, p, o, nil), mc
}

func cfgThresholds() config.Thresholds {
	return config.Thresholds{
		GPUTempHard: 88, GPUTempWarn: 82,
		CPUTempHard: 92, CPUTempWarn: 78,
		Cooldown: 60000000000, // 1 min
	}
}

// noGPUController wraps the mock controller but reports GPU fan control as
// unavailable so we can test the locked-capability path.
type noGPUController struct {
	*mock.Controller
}

func (c *noGPUController) Capabilities() metrics.Capabilities {
	cap := c.Controller.Capabilities()
	cap.GPUFanControl = false
	return cap
}

func TestDryRunNoopsWrites(t *testing.T) {
	s, mc := newTestService(Options{DryRun: true, Audit: NewAuditLog(10)})
	s.SetMonitor(false) // Control mode: exercise the dry-run gate
	if err := s.SetFanMode(context.Background(), "alice", "Half"); err != nil {
		t.Fatalf("dry-run set fan mode errored: %v", err)
	}
	// Mode must be unchanged (mock starts at Auto).
	st, _ := mc.ActiveFanProfile(context.Background())
	if st.Active != "Auto" {
		t.Errorf("dry-run wrote to controller: mode = %q, want Auto", st.Active)
	}
	// But it should be audited as dry-run.
	if len(s.Audit()) != 1 || s.Audit()[0].Result != "dry-run" {
		t.Errorf("expected 1 dry-run audit entry, got %+v", s.Audit())
	}
}

func TestReadOnlyDeniesWrites(t *testing.T) {
	s, _ := newTestService(Options{ReadOnly: true, Audit: NewAuditLog(10)})
	s.SetMonitor(false) // Control mode: exercise the read-only gate
	if err := s.SetFanMode(context.Background(), "alice", "Full"); err != errReadOnly {
		t.Errorf("expected errReadOnly, got %v", err)
	}
	if err := s.SetFanDuty(context.Background(), "alice", 184, 60); err != errReadOnly {
		t.Errorf("expected errReadOnly for duty, got %v", err)
	}
	if err := s.SetGPUFan(context.Background(), "alice", 0, 50); err != errReadOnly {
		t.Errorf("expected errReadOnly for gpu fan, got %v", err)
	}
}

func TestAuditRecordsRealWrites(t *testing.T) {
	// DryRun off: a write should reach the mock controller and be audited "ok".
	s, mc := newTestService(Options{DryRun: false, Audit: NewAuditLog(10)})
	s.SetMonitor(false) // Control mode: writes allowed
	if err := s.SetFanDuty(context.Background(), "bob", 184, 70); err != nil {
		t.Fatalf("set fan duty errored: %v", err)
	}
	st, _ := mc.ActiveFanProfile(context.Background())
	_ = st
	if len(s.Audit()) != 1 || s.Audit()[0].Result != "ok" {
		t.Errorf("expected 1 'ok' audit entry, got %+v", s.Audit())
	}
}

func TestGPUFanLockedWhenUnsupported(t *testing.T) {
	// A controller that doesn't advertise GPU fan control should refuse writes.
	mp := mock.NewProvider()
	stub := &noGPUController{mock.NewController()}
	p := poller.New([]metrics.Provider{mp}, cfgThresholds(), poller.NewRing(10), nil)
	p.Tick(context.Background())
	s := New(stub, p, Options{DryRun: false, Audit: NewAuditLog(10)}, nil)
	s.SetMonitor(false) // Control mode: exercise the capability gate
	if err := s.SetGPUFan(context.Background(), "alice", 0, 50); err != errGPUFanLocked {
		t.Errorf("expected errGPUFanLocked, got %v", err)
	}
}

func TestMonitorDefaultsOn(t *testing.T) {
	// The safe default is Monitor: even a write-capable setup refuses writes
	// until an admin explicitly switches to Control.
	s, _ := newTestService(Options{DryRun: false})
	if !s.Monitor() {
		t.Fatal("Monitor mode must default to ON")
	}
	if err := s.SetFanMode(context.Background(), "alice", "Full"); err != errMonitor {
		t.Fatalf("expected errMonitor in default mode, got %v", err)
	}
	if err := s.SetFanDuty(context.Background(), "alice", 184, 60); err != errMonitor {
		t.Fatalf("expected errMonitor for duty, got %v", err)
	}
	if err := s.SetGPUFan(context.Background(), "alice", 0, 50); err != errMonitor {
		t.Fatalf("expected errMonitor for gpu fan, got %v", err)
	}
}

func TestMonitorBlocksEvenWhenDryRunOff(t *testing.T) {
	// Monitor must win over dry-run-off: no write, no 'would-have' log either.
	s, mc := newTestService(Options{DryRun: false, Audit: NewAuditLog(10)})
	s.SetMonitor(true) // already on, but be explicit
	if err := s.SetFanDuty(context.Background(), "alice", 184, 90); err != errMonitor {
		t.Fatalf("expected errMonitor, got %v", err)
	}
	if len(s.Audit()) != 0 {
		t.Errorf("monitor-mode write should not be audited as dry-run, got %+v", s.Audit())
	}
	_ = mc
}

func TestSetMonitorToggle(t *testing.T) {
	s, _ := newTestService(Options{DryRun: false})
	// Control mode allows writes.
	s.SetMonitor(false)
	if s.Monitor() {
		t.Fatal("SetMonitor(false) should turn Monitor off")
	}
	if err := s.SetFanMode(context.Background(), "bob", "Full"); err != nil {
		t.Fatalf("control-mode write errored: %v", err)
	}
	// Back to Monitor: writes refused again.
	s.SetMonitor(true)
	if err := s.SetFanMode(context.Background(), "bob", "Half"); err != errMonitor {
		t.Fatalf("expected errMonitor after re-enable, got %v", err)
	}
}

func TestStatusReportsMonitor(t *testing.T) {
	s, _ := newTestService(Options{DryRun: false})
	st := s.Status()
	if !st.Monitor {
		t.Error("Status.Monitor should be true (default)")
	}
	s.SetMonitor(false)
	if st := s.Status(); st.Monitor {
		t.Error("Status.Monitor should be false after SetMonitor(false)")
	}
}

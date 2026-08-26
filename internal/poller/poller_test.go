package poller

import (
	"context"
	"testing"
	"time"

	"github.com/hedchr/fanctrl/internal/config"
	"github.com/hedchr/fanctrl/internal/metrics"
)

// fakeProvider returns a fixed snapshot.
type fakeProvider struct {
	name string
	snap metrics.Snapshot
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) Collect(context.Context) (metrics.Snapshot, error) {
	return f.snap, nil
}
func (f *fakeProvider) Close() error { return nil }

func TestMerge(t *testing.T) {
	var dst metrics.Snapshot
	// GPU provider sets GPUs; CPU provider sets CPU. Merge must combine both
	// without wiping the other.
	srcGPU := metrics.Snapshot{GPUs: []metrics.GPU{{Index: 0, Temp: 60}}}
	merge(&dst, srcGPU)
	if len(dst.GPUs) != 1 || dst.GPUs[0].Temp != 60 {
		t.Fatalf("gpu merge failed: %+v", dst)
	}
	srcCPU := metrics.Snapshot{CPU: metrics.CPU{LoadPct: 40, Cores: 32}}
	merge(&dst, srcCPU)
	if dst.CPU.LoadPct != 40 || dst.CPU.Cores != 32 {
		t.Fatalf("cpu merge failed: %+v", dst.CPU)
	}
	if len(dst.GPUs) != 1 {
		t.Fatalf("merge wiped gpus: %+v", dst.GPUs)
	}
}

func TestPollerTick(t *testing.T) {
	p := &Poller{
		providers: []metrics.Provider{
			&fakeProvider{name: "a", snap: metrics.Snapshot{GPUs: []metrics.GPU{{Index: 0, Temp: 55}}}},
		},
		cfg: config.Thresholds{GPUTempHard: 88, GPUTempWarn: 82, Cooldown: time.Minute},
		hist: NewRing(10),
	}
	p.Tick(context.Background())
	snap := p.Snapshot()
	if len(snap.GPUs) != 1 {
		t.Fatalf("expected 1 gpu after tick, got %d", len(snap.GPUs))
	}
	if p.hist.Len() != 1 {
		t.Errorf("history length = %d, want 1", p.hist.Len())
	}
}

func TestRingRecentChronological(t *testing.T) {
	r := NewRing(3)
	base := time.Now()
	for i := 0; i < 5; i++ {
		r.Append(metrics.Snapshot{Time: base.Add(time.Duration(i) * time.Second), CPU: metrics.CPU{LoadPct: float64(i)}})
	}
	recent := r.Recent(3)
	if len(recent) != 3 {
		t.Fatalf("len = %d, want 3", len(recent))
	}
	// Should be the last three samples (2,3,4) in order.
	for i, want := range []float64{2, 3, 4} {
		if recent[i].CPU.LoadPct != want {
			t.Errorf("recent[%d].LoadPct = %v, want %v", i, recent[i].CPU.LoadPct, want)
		}
	}
}

func TestRingRecentWhenNotFull(t *testing.T) {
	r := NewRing(10)
	r.Append(metrics.Snapshot{CPU: metrics.CPU{LoadPct: 1}})
	got := r.Recent(10)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
}

func TestGovernorRevertsOnHardTemp(t *testing.T) {
	var reverted string
	g := newGovernor(config.Thresholds{GPUTempHard: 88, GPUTempWarn: 82, Cooldown: time.Minute})
	g.SetRevert(func(_ context.Context, reason string) error {
		reverted = reason
		return nil
	})
	err := g.Evaluate(context.Background(), metrics.Snapshot{GPUs: []metrics.GPU{{Index: 0, Temp: 90}}})
	if err != nil {
		t.Fatalf("governor evaluate error: %v", err)
	}
	if reverted == "" {
		t.Fatal("governor did not trigger revert")
	}
}

func TestGovernorIgnoresUnderLimit(t *testing.T) {
	g := newGovernor(config.Thresholds{GPUTempHard: 88, GPUTempWarn: 82})
	var reverted string
	g.SetRevert(func(_ context.Context, reason string) error {
		reverted = reason
		return nil
	})
	_ = g.Evaluate(context.Background(), metrics.Snapshot{GPUs: []metrics.GPU{{Index: 0, Temp: 60}}})
	if reverted != "" {
		t.Fatalf("should not revert, got %q", reverted)
	}
}

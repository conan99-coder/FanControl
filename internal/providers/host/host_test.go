package host

import (
	"testing"
)

func TestParseCPULine(t *testing.T) {
	// user nice system idle iowait irq softirq steal guest guest_nice
	fields := []string{"100", "10", "50", "840", "0", "1", "2", "0", "0", "0"}
	c := parseCPULine(fields)
	// total = all fields = 1003
	if c.total != 1003 {
		t.Errorf("total = %v, want 1003", c.total)
	}
	// busy = user+nice+system+irq+softirq+steal = 100+10+50+1+2+0 = 163
	if c.busy != 163 {
		t.Errorf("busy = %v, want 163", c.busy)
	}
}

func TestBusyPercent(t *testing.T) {
	prev := aggregateCounters{busy: 100, total: 1000}
	cur := aggregateCounters{busy: 200, total: 1200}
	pct := busyPercent(cur, prev)
	// db = 100, dt = 200 -> 50%
	if pct != 50 {
		t.Errorf("busyPercent = %v, want 50", pct)
	}
}

func TestBusyPercent_NoDelta(t *testing.T) {
	if pct := busyPercent(aggregateCounters{busy: 5}, aggregateCounters{busy: 5, total: 5}); pct != 0 {
		t.Errorf("expected 0 for no delta, got %v", pct)
	}
}

func TestIsTempChip(t *testing.T) {
	cases := map[string]bool{
		"k10temp":   true,
		"coretemp":  true,
		"zenpower":  true,
		"i915":      false,
		"nvme":      false,
		"  k10temp ": true,
	}
	for name, want := range cases {
		if got := isTempChip(name); got != want {
			t.Errorf("isTempChip(%q) = %v, want %v", name, got, want)
		}
	}
}

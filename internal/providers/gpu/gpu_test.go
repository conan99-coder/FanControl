package gpu

import (
	"strings"
	"testing"

	"github.com/hedchr/fanctrl/internal/metrics"
)

// Sample nvidia-smi csv,noheader,nounits output resembling a real 6000 Pro.
// Columns match defaultCSVQuery (10 fields; no max-temp column).
const sampleCSV = "0, NVIDIA RTX 6000 Pro Blackwell WS, 62, 45, 285.4, 300.0, 38, 32768, 98304, 60\n1, NVIDIA RTX 6000 Pro Blackwell WS, 58, 30, 251.2, 300.0, 34, 49152, 98304, 82\n"

func TestParseCSV(t *testing.T) {
	gpus, err := parseCSV([]byte(sampleCSV), defaultCSVQuery)
	if err != nil {
		t.Fatalf("parseCSV error: %v", err)
	}
	if len(gpus) != 2 {
		t.Fatalf("expected 2 GPUs, got %d", len(gpus))
	}
	g0 := gpus[0]
	if g0.Index != 0 {
		t.Errorf("index = %d, want 0", g0.Index)
	}
	if g0.Temp != 62 {
		t.Errorf("temp = %v, want 62", g0.Temp)
	}
	if g0.Util != 45 {
		t.Errorf("util = %v, want 45", g0.Util)
	}
	if g0.Power != 285.4 {
		t.Errorf("power = %v, want 285.4", g0.Power)
	}
	if g0.PowerLimit != 300 {
		t.Errorf("powerLimit = %v, want 300", g0.PowerLimit)
	}
	if g0.FanPct != 38 {
		t.Errorf("fan = %v, want 38", g0.FanPct)
	}
	if g0.VRAMUsed != 32768*1024*1024 {
		t.Errorf("vramUsed = %v, want %v", g0.VRAMUsed, 32768*1024*1024)
	}
	// MaxTemp is not a default query column, so parseCSV leaves it 0; the
	// provider fills a sane default via applyDefaults.
	if g0.MaxTemp != 0 {
		t.Errorf("maxTemp = %v, want 0 (not in primary query)", g0.MaxTemp)
	}
}

func TestApplyDefaults(t *testing.T) {
	gpus := []metrics.GPU{{Index: 0, Temp: 62}, {Index: 1, Temp: 74}}
	applyDefaults(gpus, len(defaultCSVQuery))
	for i, g := range gpus {
		if g.MaxTemp != defaultMaxTemp {
			t.Errorf("gpu %d maxTemp = %v, want %v", i, g.MaxTemp, defaultMaxTemp)
		}
	}
}

// A card that locks fan control reports "[N/A]" or "[Not Supported]"; the
// parser must tolerate it and yield 0 rather than erroring.
func TestParseCSV_LockedFan(t *testing.T) {
	csv := "0, NVIDIA RTX 6000 Pro Blackwell WS, 62, 45, 285, 300, [N/A], 32768, 98304, 60\n"
	gpus, err := parseCSV([]byte(csv), defaultCSVQuery)
	if err != nil {
		t.Fatalf("parseCSV error: %v", err)
	}
	if gpus[0].FanPct != 0 {
		t.Errorf("fan = %v, want 0 for locked card", gpus[0].FanPct)
	}
}

func TestParseCSV_ShortRow(t *testing.T) {
	// A less-detailed query or truncated output should not panic.
	csv := "0, NVIDIA RTX 6000 Pro, 62\n"
	gpus, err := parseCSV([]byte(csv), defaultCSVQuery)
	if err != nil {
		t.Fatalf("parseCSV error: %v", err)
	}
	if len(gpus) != 1 {
		t.Fatalf("expected 1 gpu, got %d", len(gpus))
	}
	if !strings.Contains(gpus[0].Name, "RTX") {
		t.Errorf("name = %q, want contains RTX", gpus[0].Name)
	}
}

// parseProbe decodes fan.speed probe output into a controllability verdict.
// This mirrors ProbeFanControl's decision logic so it's testable without a GPU.
func parseProbe(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	for _, line := range strings.Split(trimmed, "\n") {
		l := strings.TrimSpace(line)
		if l == "" {
			continue
		}
		if strings.HasPrefix(l, "[") {
			return false
		}
		if v := parseFloat(l); v <= 0 {
			return false
		}
	}
	return true
}

func TestProbeFanControlLogic(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want bool
	}{
		{"numeric fans", "100\n100\n", true},
		{"single numeric", "100", true},
		{"locked", "[N/A]\n[N/A]", false},
		{"not supported", "[Not Supported]", false},
		{"zero", "0", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		if got := parseProbe(tc.out); got != tc.want {
			t.Errorf("%s: parseProbe(%q) = %v, want %v", tc.name, tc.out, got, tc.want)
		}
	}
}

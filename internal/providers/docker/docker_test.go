package docker

import (
	"testing"
)

const psSample = `{"Command":"...","CreatedAt":"2025-08-30 12:00:00 +0200 CEST","ID":"3f2a1b0c9d8e","Image":"image-registry.veridion.com/ml/orion-llms_v.0.02/ssh","Labels":"","Names":"C.49321308","Ports":"","RunningFor":"36 hours","State":"running","Status":"Up 36 hours"}
{"Command":"...","CreatedAt":"2025-08-30 12:00:00 +0200 CEST","ID":"9e8d7c6b5a4f","Image":"pytorch/pytorch:2.4.0-cuda12.4","Labels":"","Names":"C.49321247","Ports":"","RunningFor":"36 hours","State":"running","Status":"Up 36 hours"}`

const statsSample = `{"BlockIO":"0B / 0B","CPUPerc":"42.55%","Container":"3f2a1b0c9d8e...","ID":"3f2a1b0c9d8e...","MemPerc":"1.92%","MemUsage":"1.234GiB / 62.55GiB","Name":"C.49321308","NetIO":"1MB / 2MB","PIDs":"12"}
{"BlockIO":"0B / 0B","CPUPerc":"18.24%","Container":"9e8d7c6b5a4f...","ID":"9e8d7c6b5a4f...","MemPerc":"0.92%","MemUsage":"512MiB / 62.55GiB","Name":"C.49321247","NetIO":"1MB / 2MB","PIDs":"9"}`

func TestParsePS(t *testing.T) {
	cs, err := parsePS([]byte(psSample))
	if err != nil {
		t.Fatalf("parsePS: %v", err)
	}
	if len(cs) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(cs))
	}
	if cs[0].Name != "C.49321308" || cs[0].Image == "" || cs[0].Status != "Up 36 hours" {
		t.Errorf("bad container: %+v", cs[0])
	}
}

func TestParsePSEmpty(t *testing.T) {
	cs, err := parsePS([]byte(""))
	if err != nil {
		t.Fatalf("parsePS empty: %v", err)
	}
	if len(cs) != 0 {
		t.Fatalf("expected 0 containers, got %d", len(cs))
	}
}

func TestParseStatsAndMetric(t *testing.T) {
	byName := parseStats([]byte(statsSample))
	if len(byName) != 2 {
		t.Fatalf("expected 2 stats records, got %d", len(byName))
	}
	cpu, used, total := statsMetric(byName["C.49321308"])
	if cpu != 42.55 {
		t.Errorf("cpu = %v, want 42.55", cpu)
	}
	if used < 1.32e9 || used > 1.33e9 {
		t.Errorf("used = %v, want ~1.234GiB (1.325e9)", used)
	}
	if total < 62e9 || total > 68e9 {
		t.Errorf("total = %v, want ~62.55GiB", total)
	}
}

func TestParseSize(t *testing.T) {
	cases := map[string]float64{
		"512MiB":   512 << 20,
		"1.234GiB": 1.234 * (1 << 30),
		"12GB":     12e9,
		"1024":     0, // unitless strings are not valid sizes
		"":         0,
	}
	for in, want := range cases {
		if got := parseSize(in); got != want {
			t.Errorf("parseSize(%q) = %v, want %v", in, got, want)
		}
	}
}

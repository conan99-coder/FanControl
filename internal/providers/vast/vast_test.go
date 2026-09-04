package vast

import (
	"testing"

	"github.com/hedchr/fanctrl/internal/metrics"
)

// Sample output shape of `vastai show machines --raw` (values/fields mirror
// the real MC62-G40 rig; identifier replaced).
const sample = `{
 "machines": [
  {
   "hostname": "endif01",
   "id": 148260,
   "gpu_name": "RTX PRO 6000 WS",
   "num_gpus": 2,
   "listed_gpu_cost": 1.3,
   "earn_hour": 2.038824,
   "earn_day": 38.578485,
   "current_rentals_running": 2,
   "current_rentals_on_demand": 2,
   "client_end_date": 1788375600.0,
   "end_date": 1788544800.0,
   "verification": "verified",
   "reliability2": 0.9792782,
   "geolocation": "Sweden, SE",
   "public_ipaddr": "192.0.2.1",
   "mobo_name": "MC62-G40-00"
  }
 ]
}`

func TestParseMachines(t *testing.T) {
	rigs, err := parseMachines([]byte(sample))
	if err != nil {
		t.Fatalf("parseMachines: %v", err)
	}
	if len(rigs) != 1 {
		t.Fatalf("expected 1 rig, got %d", len(rigs))
	}
	r := rigs[0]
	if r.Hostname != "endif01" || r.ID != 148260 {
		t.Errorf("bad identity: %+v", r)
	}
	if r.GPUName != "RTX PRO 6000 WS" || r.NumGPUs != 2 {
		t.Errorf("bad gpu info: %+v", r)
	}
	if r.ListedGPUCost != 1.3 {
		t.Errorf("listed cost = %v, want 1.3", r.ListedGPUCost)
	}
	if r.EarnHour != 2.038824 || r.EarnDay != 38.578485 {
		t.Errorf("bad earnings: %+v", r)
	}
	if r.RentalsRunning != 2 {
		t.Errorf("rentals running = %d, want 2", r.RentalsRunning)
	}
	if r.ClientEndDate != 1788375600.0 || r.EndDate != 1788544800.0 {
		t.Errorf("bad end dates: %+v", r)
	}
	if r.Verification != "verified" || r.Reliability < 0.9 {
		t.Errorf("bad verification/reliability: %+v", r)
	}
}

func TestParseMachinesEmpty(t *testing.T) {
	rigs, err := parseMachines([]byte(`{"machines": []}`))
	if err != nil {
		t.Fatalf("parseMachines empty: %v", err)
	}
	if len(rigs) != 0 {
		t.Fatalf("expected 0 rigs, got %d", len(rigs))
	}
}

const metricsSample = `{
 "success": true,
 "gpus": [
  {"gpu_name":"RTX 5090","total":6992,"available":1182,"rented_verified":5810,"avail_verified":1182,"usage":83.1,"price_p10":0.295,"price_median":0.34,"price_p90":0.6552,"tflops_per_dollar":321.2},
  {"gpu_name":"RTX PRO 6000 WS","total":577,"available":57,"rented_verified":520,"avail_verified":57,"usage":90.1,"price_p10":0.8895,"price_median":1.0725,"price_p90":1.445,"tflops_per_dollar":111.0}
 ]
}`

func TestParseGPUMetrics(t *testing.T) {
	gpus, err := parseGPUMetrics([]byte(metricsSample))
	if err != nil {
		t.Fatalf("parseGPUMetrics: %v", err)
	}
	if len(gpus) != 2 {
		t.Fatalf("expected 2 gpu rows, got %d", len(gpus))
	}
	g := gpus[0]
	if g.Name != "RTX 5090" || g.RentedVerified != 5810 || g.AvailVerified != 1182 {
		t.Errorf("bad counts: %+v", g)
	}
	if g.Usage != 83.1 || g.PriceMedian != 0.34 || g.PriceP10 != 0.295 || g.PriceP90 != 0.6552 || g.TFLOPSPerDollar != 321.2 {
		t.Errorf("bad metrics: %+v", g)
	}
}

func TestFilterGpusPrefix(t *testing.T) {
	gpus, err := parseGPUMetrics([]byte(metricsSample))
	if err != nil {
		t.Fatalf("parseGPUMetrics: %v", err)
	}
	// "begins with", case-insensitive: a prefix must match multiple names.
	filtered := filterGpus(gpus, []string{"rtx pro 6000"})
	if len(filtered) != 1 || filtered[0].Name != "RTX PRO 6000 WS" {
		t.Fatalf("prefix filter failed: %+v", filtered)
	}
	// Empty/blank filters keep everything.
	if all := filterGpus(gpus, []string{"", "  "}); len(all) != 2 {
		t.Fatalf("blank filter should keep all, got %d", len(all))
	}
	// No match -> empty.
	if none := filterGpus(gpus, []string{"H100"}); len(none) != 0 {
		t.Fatalf("non-matching filter should return empty, got %d", len(none))
	}
}

func TestParseMachinesBadJSON(t *testing.T) {
	if _, err := parseMachines([]byte(`not json`)); err == nil {
		t.Fatal("expected error for bad JSON")
	}
}

func TestRigFieldsAreMetricsValue(t *testing.T) {
	// Compile-time guard: VastRig is a metrics type (used in snapshots).
	var _ metrics.VastRig
}

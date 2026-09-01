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

func TestParseMachinesBadJSON(t *testing.T) {
	if _, err := parseMachines([]byte(`not json`)); err == nil {
		t.Fatal("expected error for bad JSON")
	}
}

func TestRigFieldsAreMetricsValue(t *testing.T) {
	// Compile-time guard: VastRig is a metrics type (used in snapshots).
	var _ metrics.VastRig
}

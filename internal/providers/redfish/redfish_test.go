package redfish

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sampleFanprofile is the real GBT response captured from the MC62-G40 BMC
// (trimmed to the essential structure: 3 profiles, representative policies).
const sampleFanprofile = `{
  "@odata.context": "/redfish/v1/$metadata#GBTFanprofile.GBTFanprofile",
  "@odata.id": "/redfish/v1/Chassis/Self/Thermal/FanprofileService/Fanprofile",
  "@odata.type": "#GBTFanprofile.v1_0_0.GBTFanprofile",
  "Id": "Fanprofile",
  "Name": "Fanprofile",
  "arrProfile": [
    {
      "arrPolicy": [
        {
          "arrDuty": [30,85,100],
          "arrFanSensor": [160,161,162,184,186,187,188,189,190],
          "arrHexDeviceID": [],
          "arrHexVendorID": [],
          "arrRef": [45,70,85],
          "arrSensor": [1],
          "iAmbientSensor": 0,
          "iAmbientSensorTemp": 0,
          "iCpuTdp": 0,
          "iHysteresis": 0,
          "iInSDR": 1,
          "iInitDuty": 30,
          "iPCIEDeviceEnable": 0,
          "iPolicyType": 2,
          "iSensorCode": 1
        }
      ],
      "strName": "default",
      "strVersion": "1.00"
    },
    {
      "arrPolicy": [
        {
          "arrDuty": [30,85,100],
          "arrFanSensor": [160,161,162,184,186,187,188,189,190],
          "arrRef": [45,70,85],
          "arrSensor": [1],
          "iInitDuty": 30,
          "iPolicyType": 2,
          "iInSDR": 1
        }
      ],
      "strName": "CPU",
      "strVersion": "1.00"
    },
    {
      "arrPolicy": [],
      "strName": "NEW_PROFILE",
      "strVersion": "1.00"
    }
  ],
  "strMode": "CPU",
  "strVersion": "1.00"
}`

const sampleFanMode = `{
  "@odata.id": "/redfish/v1/Chassis/Self/Thermal/FanprofileService/FanMode",
  "@odata.type": "#GBTFanMode.v1_0_0.GBTFanMode",
  "FanMode": "nil",
  "Id": "FanMode",
  "Name": "FanMode",
  "Actions": {
    "SetFanMode": {
      "@Redfish.ActionInfo": "/redfish/v1/Chassis/Self/Thermal/FanprofileService/FanMode/SetFanModeActionInfo",
      "target": "/redfish/v1/Chassis/Self/Thermal/FanprofileService/FanMode/Actions/FanMode.SetFanMode"
    }
  }
}`

// newTestClient serves the GBT samples at the standard fan paths.
func newTestClient(t *testing.T) (*Client, *http.ServeMux) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/redfish/v1/Chassis/Self/Thermal/FanprofileService/Fanprofile", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			// Echo the PUT body so the test can inspect what we sent.
			_ = json.NewEncoder(w).Encode(map[string]string{"ok": "put"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleFanprofile))
	})
	mux.HandleFunc("/redfish/v1/Chassis/Self/Thermal/FanprofileService/FanMode", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleFanMode))
	})
	mux.HandleFunc("/redfish/v1/Chassis/Self/Thermal/FanprofileService/FanMode/Actions/FanMode.SetFanMode", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok": true}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, "admin", "pw", false)
	return c, mux
}

func TestGbtListFanProfiles(t *testing.T) {
	c, _ := newTestClient(t)
	profs, err := c.ListFanProfiles(context.Background())
	if err != nil {
		t.Fatalf("ListFanProfiles: %v", err)
	}
	if len(profs) != 3 {
		t.Fatalf("expected 3 profiles, got %d", len(profs))
	}
	if profs[0].Name != "default" {
		t.Errorf("profile[0].Name = %q, want default", profs[0].Name)
	}
	if profs[1].Name != "CPU" {
		t.Errorf("profile[1].Name = %q, want CPU", profs[1].Name)
	}
	if len(profs[0].Policies) == 0 {
		t.Error("default profile should have policies")
	}
	p := profs[0].Policies[0]
	if len(p.Duty) != 3 || p.Duty[0] != 30 || p.Duty[2] != 100 {
		t.Errorf("policy duty = %v, want [30 85 100]", p.Duty)
	}
	if len(p.FanSensors) != 9 || p.FanSensors[0] != 160 {
		t.Errorf("policy fanSensors = %v, want starting 160", p.FanSensors)
	}
}

func TestGbtActiveFanProfile(t *testing.T) {
	c, _ := newTestClient(t)
	st, err := c.ActiveFanProfile(context.Background())
	if err != nil {
		t.Fatalf("ActiveFanProfile: %v", err)
	}
	// FanMode resource reports "nil" -> Active = Auto; profile strMode = CPU.
	if st.Active != "Auto" {
		t.Errorf("active mode = %q, want Auto", st.Active)
	}
	if st.Mode != "CPU" {
		t.Errorf("mode (profile) = %q, want CPU", st.Mode)
	}
}

func TestGbtSetFanMode(t *testing.T) {
	c, _ := newTestClient(t)
	// fetchFanMode happens on the FanMode child; the action POST goes to the
	// SetFanMode target. Missing a body check would only assert the 2xx. To
	// verify the body, register a capture in a fresh server.
	var got map[string]string
	mux := http.NewServeMux()
	mux.HandleFunc("/redfish/v1/Chassis/Self/Thermal/FanprofileService/FanMode/Actions/FanMode.SetFanMode", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok": true}`))
	})
	mux.HandleFunc("/redfish/v1/Chassis/Self/Thermal/FanprofileService/FanMode", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleFanMode))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c = NewClient(srv.URL, "admin", "pw", false)

	if err := c.SetFanMode(context.Background(), "Half"); err != nil {
		t.Fatalf("SetFanMode: %v", err)
	}
	if got["FanMode"] != "Half" {
		t.Errorf("SetFanMode body = %v, want FanMode=Half", got)
	}
	// Invalid mode must be rejected without hitting the BMC.
	if err := c.SetFanMode(context.Background(), "quiet"); err == nil {
		t.Error("expected error for invalid mode")
	}
}

func TestGbtSetFanDuty(t *testing.T) {
	// Verified live: this BMC rejects PUT on the Fanprofile resource (501),
	// so duty override must return a clear error WITHOUT any HTTP write.
	c, _ := newTestClient(t)
	err := c.SetFanDuty(context.Background(), 184, 55)
	if err == nil {
		t.Fatal("expected error for duty override (PUT unsupported on this board)")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("error should mention unsupported, got: %v", err)
	}
	// Capabilities must reflect the limitation.
	if caps := c.Capabilities(); caps.DutyOverride {
		t.Error("Capabilities.DutyOverride should be false (verified PUT rejected)")
	}
}

// TestMapFansUsesSensorNumber ensures fan IDs come from the authoritative
// SensorNumber field on the Thermal resource (which matches the BMC web UI).
func TestMapFansUsesSensorNumber(t *testing.T) {
	fs := []Fan{
		{Name: "CPU0_FAN", Reading: 1950, SensorNumber: 184, Status: Status{State: "Enabled"}},
		{Name: "FCH_FAN", Reading: 2250, SensorNumber: 185, Status: Status{State: "Enabled"}},
		{Name: "SYS_FAN1", Reading: 1950, SensorNumber: 186, Status: Status{State: "Enabled"}},
		{Name: "SYS_FAN5", Reading: 2100, SensorNumber: 190, Status: Status{State: "Enabled"}},
		{Name: "SYS_FAN7", Reading: 1950, SensorNumber: 161, Status: Status{State: "Enabled"}},
		{Name: "SYS_FAN6", Reading: 0, SensorNumber: 160, Status: Status{State: "Absent"}},
	}
	out := mapFans(fs)
	wantIDs := map[string]int{
		"CPU0_FAN": 184,
		"FCH_FAN":  185,
		"SYS_FAN1": 186,
		"SYS_FAN5": 190,
		"SYS_FAN7": 161,
		"SYS_FAN6": 160,
	}
	for _, f := range out {
		if want, ok := wantIDs[f.Name]; ok {
			if f.ID != want {
				t.Errorf("fan %s id = %d, want %d (SensorNumber)", f.Name, f.ID, want)
			}
		}
	}
	// Absent fan must report 0 RPM (idle) even if the reading were non-zero.
	for _, f := range out {
		if f.Name == "SYS_FAN6" && f.RPM != 0 {
			t.Errorf("SYS_FAN6 rpm = %v, want 0 (Absent)", f.RPM)
		}
	}
}

// TestMapThermalsSkipsDTS ensures non-thermal sensors like CPU0_DTS (no
// threshold, implausible reading) are excluded from the temperature list.
func TestMapThermalsSkipsDTS(t *testing.T) {
	ts := []Temperature{
		{Name: "CPU0_TEMP", ReadingCelsius: 84, UpperThreshold: 98, SensorNumber: 1},
		{Name: "PCH_TEMP", ReadingCelsius: 58, UpperThreshold: 93, SensorNumber: 2},
		{Name: "CPU0_DTS", ReadingCelsius: 16, UpperThreshold: 0, SensorNumber: 12},
	}
	out := mapThermals(ts)
	if len(out) != 2 {
		t.Fatalf("expected 2 thermals (DTS excluded), got %d", len(out))
	}
	for _, m := range out {
		if m.Name == "CPU0_DTS" {
			t.Error("CPU0_DTS must be excluded from temperatures")
		}
	}
	// Thermals carry the authoritative SensorNumber.
	if out[0].ID != 1 || out[0].Temp != 84 {
		t.Errorf("CPU0_TEMP = id %d temp %.0f, want id 1 temp 84", out[0].ID, out[0].Temp)
	}
}

// TestFanNameToID pins the verified BMC sensor id <-> name map so any future
// change is caught: this mapping is what keeps a fan write from hitting the
// wrong fan.
// TestFanNameToID pins the verified BMC sensor id <-> name map (used only as a
// fallback when the Thermal resource lacks SensorNumber).
func TestFanNameToID(t *testing.T) {
	cases := map[string]int{
		"SYS_FAN6": 160,
		"SYS_FAN7": 161,
		"SYS_FAN8": 162,
		"CPU0_FAN": 184,
		"FCH_FAN":  185,
		"SYS_FAN1": 186,
		"SYS_FAN2": 187,
		"SYS_FAN3": 188,
		"SYS_FAN4": 189,
		"SYS_FAN5": 190,
	}
	for name, want := range cases {
		if got := fanNameToID[name]; got != want {
			t.Errorf("fanNameToID[%q] = %d, want %d", name, got, want)
		}
	}
	// Round-trip: every id maps back to the same name.
	for name, id := range cases {
		if got := fanIDToName[id]; got != name {
			t.Errorf("fanIDToName[%d] = %q, want %q", id, got, name)
		}
	}
}

func TestInterpolate(t *testing.T) {
	// Curve: 45C -> 30%, 70C -> 85%, 85C -> 100%
	refs := []float64{45, 70, 85}
	duties := []float64{30, 85, 100}
	cases := []struct {
		temp float64
		want float64
	}{
		{40, 30}, // below first point -> clamped to 30
		{45, 30}, // exactly first
		{60, 63}, // 30 + (60-45)/(70-45)*(85-30) = 63
		{70, 85},
		{80, 95},       // 85 + (80-70)/(85-70)*(100-85) = 95
		{90, 100},      // above last -> 100
	}
	for _, tc := range cases {
		got := interpolate(refs, duties, tc.temp)
		if abs(got-tc.want) > 0.01 {
			t.Errorf("interpolate(%v) = %v, want %v", tc.temp, got, tc.want)
		}
	}
}

// TestEstimateAutoDuties covers the real captured policy shape: a fan is
// controlled by a policy whose arrSensor temp drives its duty.
func TestEstimateAutoDuties(t *testing.T) {
	prof := gbtFanProfile{
		Profiles: []gbtProfile{
			{
				StrName: "CPU",
				Policies: []bmcPolicy{
					{
						ArrDuty:      []float64{30, 85, 100},
						ArrFanSensor: []int{184, 186, 187},
						ArrRef:       []float64{45, 70, 85},
						ArrSensor:    []int{1}, // CPU0_TEMP sensor number
						IPolicyType:  2,
					},
				},
			},
		},
	}
	// CPU0_TEMP at 76C -> between 70 (85%) and 85 (100%): 85 + 6/15*15 = 91
	thermals := []Temperature{
		{Name: "CPU0_TEMP", SensorNumber: 1, ReadingCelsius: 76},
	}
	duties := estimateAutoDuties(prof, thermals)
	if d := duties[184]; abs(d-91) > 0.01 {
		t.Errorf("fan 184 auto duty = %v, want ~91 (CPU 76C)", d)
	}
	if d := duties[186]; abs(d-91) > 0.01 {
		t.Errorf("fan 186 auto duty = %v, want ~91", d)
	}
	// Unknown sensor -> no estimate.
	thermals2 := []Temperature{{Name: "PCH_TEMP", SensorNumber: 2, ReadingCelsius: 58}}
	duties2 := estimateAutoDuties(prof, thermals2)
	if _, ok := duties2[184]; ok {
		t.Error("expected no estimate when referenced sensor temp is unknown")
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}


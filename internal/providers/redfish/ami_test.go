package redfish

import (
	"encoding/json"
	"testing"

	"github.com/hedchr/fanctrl/internal/metrics"
)

// sampleAMISensors is a trimmed slice of the real /api/detail_sensors_readings
// response from the MC62-G40 (voltages + one temp + one fan). It includes the
// "NA" string thresholds the BMC uses for absent limits.
const sampleAMISensors = `[
 {"id":25,"sensor_number":64,"name":"P_12V","type":"voltage","type_number":2,"reading":12.155,"unit":"volts","lower_critical_threshold":"NA","higher_critical_threshold":13.845},
 {"id":26,"sensor_number":65,"name":"P_5V","type":"voltage","type_number":2,"reading":5.06,"unit":"volts","lower_critical_threshold":4.235,"higher_critical_threshold":"NA"},
 {"id":27,"sensor_number":66,"name":"P_3V3","type":"voltage","type_number":2,"reading":3.332,"unit":"volts","lower_critical_threshold":"NA","higher_critical_threshold":3.8024},
 {"id":29,"sensor_number":79,"name":"P_VBAT","type":"voltage","type_number":2,"reading":3.1644,"unit":"volts","lower_critical_threshold":"NA","higher_critical_threshold":"NA"},
 {"id":30,"sensor_number":81,"name":"VR_P0_VOUT","type":"voltage","type_number":2,"reading":1.344,"unit":"volts","lower_critical_threshold":"NA","higher_critical_threshold":1.792},
 {"id":1,"sensor_number":1,"name":"CPU0_TEMP","type":"temperature","type_number":1,"reading":76.0,"unit":"deg_c","higher_critical_threshold":98},
 {"id":31,"sensor_number":160,"name":"SYS_FAN6","type":"fan","type_number":4,"reading":0.0,"unit":"rpm","lower_critical_threshold":"NA"}
]`

func TestVoltagesFrom(t *testing.T) {
	var sensors []amiSensor
	if err := json.Unmarshal([]byte(sampleAMISensors), &sensors); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	volts := voltagesFrom(sensors)
	if len(volts) != 5 {
		t.Fatalf("expected 5 voltages, got %d", len(volts))
	}
	byName := map[string]metrics.Scalar{}
	for _, v := range volts {
		byName[v.Name] = v
	}
	// P_12V: low crit is "NA" -> Min 0; high 13.845.
	if v := byName["P_12V"]; v.Value != 12.155 || v.Unit != "V" || v.Kind != metrics.KindVolts || v.Max != 13.845 || v.Min != 0 {
		t.Errorf("P_12V = %+v, want value 12.155 V, Max 13.845, Min 0", v)
	}
	// P_5V: numeric low crit parsed; high "NA" -> 0.
	if v := byName["P_5V"]; v.Value != 5.06 || v.Min != 4.235 || v.Max != 0 {
		t.Errorf("P_5V = %+v, want Min 4.235 Max 0", v)
	}
	// P_3V3: "NA" low -> 0; numeric high.
	if v := byName["P_3V3"]; v.Value != 3.332 || v.Min != 0 || v.Max != 3.8024 {
		t.Errorf("P_3V3 = %+v, want Max 3.8024", v)
	}
	// VR_P0_VOUT (no low critical, treated as 0 min)
	if v := byName["VR_P0_VOUT"]; v.Value != 1.344 || v.Max != 1.792 {
		t.Errorf("VR_P0_VOUT = %+v", v)
	}
	// Temperature and fan sensors must NOT be included.
	if _, ok := byName["CPU0_TEMP"]; ok {
		t.Error("temperature sensor leaked into voltages")
	}
	if _, ok := byName["SYS_FAN6"]; ok {
		t.Error("fan sensor leaked into voltages")
	}
}

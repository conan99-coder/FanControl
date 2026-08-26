// Package redfish implements the Gigabyte MC62-G40 BMC Redfish client. It
// reads thermal + fan sensors and controls fans through the FanprofileService.
// Per BMC-API-NOTES.md, Redfish uses HTTP Basic auth (no CSRF), prefers
// /redfish/v1/Chassis/{n}/Thermal for live readings, and the fan control lives
// at /redfish/v1/Chassis/Self/Thermal/FanprofileService.
package redfish

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/hedchr/fanctrl/internal/metrics"
)

// Client talks to the BMC over Redfish.
type Client struct {
	baseURL   string
	user      string
	password  string
	http      *http.Client
	fanPath   string // relative path to FanprofileService
	thermPath string // relative path to Thermal
}

// Option configures a Client.
type Option func(*Client)

// WithThermalPath overrides the thermal resource path (defaults to
// /redfish/v1/Chassis/1/Thermal).
func WithThermalPath(p string) Option { return func(c *Client) { c.thermPath = p } }

// NewClient builds a Redfish client.
func NewClient(baseURL, user, password string, insecureTLS bool, opts ...Option) *Client {
	c := &Client{
		baseURL:   strings.TrimRight(baseURL, "/"),
		user:      user,
		password:  password,
		http:      &http.Client{Timeout: 10 * time.Second},
		fanPath:   "/redfish/v1/Chassis/Self/Thermal/FanprofileService",
		thermPath: "/redfish/v1/Chassis/1/Thermal",
	}
	if insecureTLS {
		c.http.Transport = insecureTransport()
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Name returns the provider name.
func (c *Client) Name() string { return "redfish" }

// Close releases the underlying idle connections.
func (c *Client) Close() error {
	c.http.CloseIdleConnections()
	return nil
}

// do performs a request with basic auth, JSON body (optional), and decodes the
// response into out (optional).
func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.user, c.password)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("redfish %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("redfish %s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// Discover returns the BMC's detected thermal + fan sensors.
func (c *Client) Discover(ctx context.Context) metrics.Discovery {
	d := metrics.Discovery{Source: "redfish", Meta: map[string]string{"bmc": c.baseURL}}
	// Profile support is probed by a non-destructive GET.
	if _, err := c.ListFanProfiles(ctx); err == nil {
		d.Meta["fan_profiles"] = "available"
	} else {
		d.Meta["fan_profiles"] = "unavailable"
	}
	var t Thermal
	if err := c.do(ctx, http.MethodGet, c.thermPath, nil, &t); err == nil {
		for _, s := range mapThermals(t.TemperatureCelsius) {
			d.Thermals = append(d.Thermals, metrics.DiscoveredSensor{Name: s.Name, ID: s.ID})
		}
		for _, f := range mapFans(t.Fans) {
			d.Fans = append(d.Fans, metrics.DiscoveredSensor{Name: f.Name, ID: f.ID})
		}
	}
	return d
}

// ---- Reading side ----

// Thermal is the Redfish Thermal resource we care about.
type Thermal struct {
	TemperatureCelsius  []Temperature  `json:"Temperatures"`
	Fans                []Fan          `json:"Fans"`
}

// Temperature is one sensor.
type Temperature struct {
	Name             string  `json:"Name"`
	ReadingCelsius   float64 `json:"ReadingCelsius"`
	UpperThreshold   float64 `json:"UpperThresholdCritical"`
	LowerThreshold   float64 `json:"LowerThresholdCritical"`
	MemberID         string  `json:"MemberId"`
	SensorNumber     int     `json:"SensorNumber"` // authoritative BMC sensor id
}

// Fan is one fan sensor in Redfish. SensorNumber is the authoritative BMC sensor
// id (matches the BMC web UI); Status.State distinguishes Enabled/Absent.
type Fan struct {
	Name             string  `json:"Name"`
	Reading          float64 `json:"Reading"`
	ReadingUnits     string  `json:"ReadingUnits"`
	MinReading       float64 `json:"MinReadingRange"`
	MaxReading       float64 `json:"MaxReadingRange"`
	MemberID         string  `json:"MemberId"`
	SensorNumber     int     `json:"SensorNumber"`
	Status           Status  `json:"Status"`
}

// Status is a minimal Redfish status object (only State is used).
type Status struct {
	State string `json:"State"` // "Enabled", "Absent", ...
}

// Collect reads thermal + fan sensors, mapping them into metrics.
func (c *Client) Collect(ctx context.Context) (metrics.Snapshot, error) {
	var t Thermal
	if err := c.do(ctx, http.MethodGet, c.thermPath, nil, &t); err != nil {
		return metrics.Snapshot{}, err
	}
	snap := metrics.Snapshot{Time: time.Now()}
	snap.Thermals = mapThermals(t.TemperatureCelsius)
	snap.Fans = mapFans(t.Fans)
	// Estimate the active auto-duty for each fan from the profile curves at the
	// current temperatures (the BMC doesn't report live duty %).
	if prof, err := c.fetchFanProfile(ctx); err == nil {
		if duties := estimateAutoDuties(prof, t.TemperatureCelsius); len(duties) > 0 {
			for i := range snap.Fans {
				if d, ok := duties[snap.Fans[i].ID]; ok {
					snap.Fans[i].AutoDuty = d
				}
			}
		}
	}
	return snap, nil
}

func mapThermals(ts []Temperature) []metrics.Thermal {
	var out []metrics.Thermal
	for i, t := range ts {
		// Skip sensors that aren't meaningful temperatures: those with no
		// critical threshold AND an implausible value/name. Verified on the
		// MC62-G40: CPU0_DTS reports "16C" with no threshold — it is a DTS
		// offset/acpi value, not a temperature, and must not be shown.
		if t.UpperThreshold <= 0 && (strings.Contains(strings.ToUpper(t.Name), "DTS") || t.ReadingCelsius < 20) {
			continue
		}
		m := metrics.Thermal{
			Name: t.Name,
			Temp: t.ReadingCelsius,
			Max:  t.UpperThreshold,
			Min:  t.LowerThreshold,
		}
		// SensorNumber is the authoritative BMC id; MemberId is the array index.
		if t.SensorNumber > 0 {
			m.ID = t.SensorNumber
		} else if t.MemberID != "" {
			fmt.Sscanf(t.MemberID, "%d", &m.ID)
		} else {
			m.ID = i
		}
		if m.Name == "" {
			m.Name = fmt.Sprintf("Thermal %d", m.ID)
		}
		out = append(out, m)
	}
	return out
}

// fanNameToID maps the BMC's fan sensor names to their sensor IDs. It is used
// as a FALLBACK for firmware that does not report SensorNumber on fans; the
// authoritative ID comes from the Redfish SensorNumber field. Verified against
// the live MC62-G40 BMC web UI (2026-08-26).
var fanNameToID = map[string]int{
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

// fanIDToName inverts fanNameToID for display and diagnostics.
var fanIDToName = func() map[int]string {
	m := make(map[int]string, len(fanNameToID))
	for k, v := range fanNameToID {
		m[v] = k
	}
	return m
}()

// mapFans converts Thermal fan resources into metrics.Fan. The fan's ID is the
// authoritative BMC sensor ID from SensorNumber (fallback: verified name map).
// Status.State marks absent/idle fans so the UI can disable their controls.
func mapFans(fs []Fan) []metrics.Fan {
	var out []metrics.Fan
	for i, f := range fs {
		m := metrics.Fan{
			ID:     i,
			Name:   f.Name,
			RPM:    f.Reading,
			MaxRPM: f.MaxReading,
		}
		// Authoritative BMC sensor id: SensorNumber, then verified name map,
		// then MemberId (array index) as last resort.
		if f.SensorNumber > 0 {
			m.ID = f.SensorNumber
		} else if id, ok := fanNameToID[strings.ToUpper(f.Name)]; ok {
			m.ID = id
		} else if f.MemberID != "" {
			fmt.Sscanf(f.MemberID, "%d", &m.ID)
		}
		// Idle AKA absent/disabled fans (SYS_FAN6/8/4 have no reading): report
		// so the UI can render them as idle instead of RPM 0.
		if strings.EqualFold(f.Status.State, "Absent") {
			m.Duty = 0
			m.RPM = 0
			m.Name = f.Name // keep name; caller may mark idle via RPM<=0
		}
		if m.Name == "" {
			m.Name = fmt.Sprintf("Fan %d", m.ID)
		}
		out = append(out, m)
	}
	return out
}

// ---- Control side (GBT Fanprofile / FanMode) ----
//
// The MC62-G40 exposes a Gigabyte "GBT" FanprofileService:
//
//	FanprofileService (service root, children only)
//	  Fanprofile  -> arrProfile[] (each: strName + arrPolicy[]), strMode
//	  FanMode     -> Actions.SetFanMode target
//
// Everything readable/editable lives on the Fanprofile resource; mode switching
// is an action on FanMode.

const fanprofileChild = "/Fanprofile"
const fanmodeChild = "/FanMode"

// gbtFanProfile is the Fanprofile resource (arrProfile + strMode).
type gbtFanProfile struct {
	ID        string         `json:"Id"`
	Name      string         `json:"Name"`
	StrMode   string         `json:"strMode"`
	StrVer    string         `json:"strVersion"`
	Profiles  []gbtProfile   `json:"arrProfile"`
}

type gbtProfile struct {
	StrName    string      `json:"strName"`
	StrVer     string      `json:"strVersion"`
	Policies   []bmcPolicy `json:"arrPolicy"`
}

// fanModeResource is the FanMode resource (action target).
type fanModeResource struct {
	FanMode string `json:"FanMode"` // "nil" when no explicit mode is set
	Actions struct {
		SetFanMode struct {
			Target string `json:"target"`
		} `json:"SetFanMode"`
	} `json:"Actions"`
}

// gbtFanProfileContainer mirrors the FanprofileService response (links).
type gbtFanProfileContainer struct {
	Profile struct {
		OdataID string `json:"@odata.id"`
	} `json:"Fanprofile"`
	FanMode struct {
		OdataID string `json:"@odata.id"`
	} `json:"FanMode"`
}

// ListFanProfiles returns the profiles with their policy curves.
func (c *Client) ListFanProfiles(ctx context.Context) ([]metrics.FanProfile, error) {
	full, err := c.fetchFanProfile(ctx)
	if err != nil {
		return nil, fmt.Errorf("list fan profiles: %w", err)
	}
	out := make([]metrics.FanProfile, 0, len(full.Profiles))
	for _, p := range full.Profiles {
		out = append(out, metrics.FanProfile{Name: p.StrName, Policies: convertPolicies(p.Policies)})
	}
	return out, nil
}

// fetchFanProfile GETs the Fanprofile resource (child of FanprofileService).
func (c *Client) fetchFanProfile(ctx context.Context) (gbtFanProfile, error) {
	var full gbtFanProfile
	if err := c.do(ctx, http.MethodGet, c.fanPath+fanprofileChild, nil, &full); err != nil {
		return gbtFanProfile{}, err
	}
	return full, nil
}

// fetchFanMode GETs the FanMode resource (child of FanprofileService) to find
// the SetFanMode action target.
func (c *Client) fetchFanMode(ctx context.Context) (fanModeResource, error) {
	var fm fanModeResource
	if err := c.do(ctx, http.MethodGet, c.fanPath+fanmodeChild, nil, &fm); err != nil {
		return fanModeResource{}, err
	}
	return fm, nil
}

// estimateAutoDuties computes, per fan sensor id, the duty % the active
// profile's policy curves would produce at the current temperatures. The BMC
// does NOT report the live duty; this interpolates the arrDuty/arrRef curve for
// each fan's controlling policy at the referenced sensor's current temp —
// the same math the fan controller uses. Unknown sensors/policies yield 0.
func estimateAutoDuties(prof gbtFanProfile, thermals []Temperature) map[int]float64 {
	// Build sensor_number -> temp lookup.
	tempOf := map[int]float64{}
	for _, t := range thermals {
		if t.SensorNumber > 0 {
			tempOf[t.SensorNumber] = t.ReadingCelsius
		}
	}
	out := map[int]float64{}
	for _, p := range prof.Profiles {
		for _, pol := range p.Policies {
			duty := policyDutyAtTemp(pol, tempOf)
			if duty < 0 {
				continue
			}
			for _, fan := range pol.ArrFanSensor {
				// Take the max across policies that control the same fan.
				if duty > out[fan] {
					out[fan] = duty
				}
			}
		}
	}
	return out
}

// policyDutyAtTemp interpolates the policy's arrDuty curve at the temperature of
// its first referenced sensor. Returns -1 if the policy is not active (no
// sensor temp known or no curve).
func policyDutyAtTemp(pol bmcPolicy, tempOf map[int]float64) float64 {
	if len(pol.ArrRef) == 0 || len(pol.ArrDuty) == 0 || len(pol.ArrRef) != len(pol.ArrDuty) {
		return -1
	}
	// Use the first sensor that has a known temperature (max = hottest control
	// sensor is safest for an estimate).
	var temp float64
	var found bool
	for _, s := range pol.ArrSensor {
		if t, ok := tempOf[s]; ok && t > temp {
			temp = t
			found = true
		}
	}
	if !found {
		return -1
	}
	return interpolate(pol.ArrRef, pol.ArrDuty, temp)
}

// interpolate linearly maps x against xs/ys (piecewise), clamping to the ends.
func interpolate(xs, ys []float64, x float64) float64 {
	if x <= xs[0] {
		return ys[0]
	}
	for i := 1; i < len(xs); i++ {
		if x <= xs[i] {
			span := xs[i] - xs[i-1]
			if span <= 0 {
				return ys[i]
			}
			return ys[i-1] + (x-xs[i-1])/span*(ys[i]-ys[i-1])
		}
	}
	return ys[len(ys)-1]
}

// bmcPolicy mirrors the BMC's policy object exactly (camelCase keys as the
// Gigabyte MegaRAC firmware emits them). It is translated to/from
// metrics.Policy.
type bmcPolicy struct {
	ArrDuty       []float64 `json:"arrDuty"`
	ArrFanSensor  []int     `json:"arrFanSensor"`
	ArrRef        []float64 `json:"arrRef"`
	ArrSensor     []int     `json:"arrSensor"`
	IInitDuty     float64   `json:"iInitDuty"`
	IPolicyType   int       `json:"iPolicyType"`
	IInSDR        int       `json:"iInSDR"`
}

func toMetricsPolicy(b bmcPolicy) metrics.Policy {
	return metrics.Policy{
		FanSensors: b.ArrFanSensor,
		Duty:       b.ArrDuty,
		Ref:        b.ArrRef,
		Sensor:     b.ArrSensor,
		InitDuty:   b.IInitDuty,
		PolicyType: b.IPolicyType,
	}
}

func toBMCPolicy(m metrics.Policy) bmcPolicy {
	return bmcPolicy{
		ArrDuty:      m.Duty,
		ArrFanSensor: m.FanSensors,
		ArrRef:       m.Ref,
		ArrSensor:    m.Sensor,
		IInitDuty:    m.InitDuty,
		IPolicyType:  m.PolicyType,
	}
}

func convertPolicies(ps []bmcPolicy) []metrics.Policy {
	out := make([]metrics.Policy, len(ps))
	for i, p := range ps {
		out[i] = toMetricsPolicy(p)
	}
	return out
}

// ActiveFanProfile returns the active fan mode (Auto/Full/Half) plus the active
// profile name. The FanMode resource is the mode knob ("nil" means Auto); the
// Fanprofile's strMode selects which curve profile Auto uses.
func (c *Client) ActiveFanProfile(ctx context.Context) (metrics.FanProfileState, error) {
	// Mode knob (authoritative for what the UI should show as the switch state).
	mode := metrics.FanModeAuto
	if fm, err := c.fetchFanMode(ctx); err == nil && fm.FanMode != "" && fm.FanMode != "nil" {
		mode = fm.FanMode
	}
	// Active curve profile (informational).
	profile := ""
	if prof, err := c.fetchFanProfile(ctx); err == nil {
		profile = prof.StrMode
	}
	return metrics.FanProfileState{Active: mode, Mode: profile}, nil
}

// SetFanMode switches the global fan mode via the FanMode.SetFanMode action.
// Confirmed allowable values on this board (SetFanModeActionInfo):
// "Full" | "Half" | "Auto". The body field is "FanMode".
func (c *Client) SetFanMode(ctx context.Context, mode string) error {
	switch mode {
	case metrics.FanModeAuto, metrics.FanModeHalf, metrics.FanModeFull:
	default:
		return fmt.Errorf("invalid fan mode %q (allowable: Full, Half, Auto)", mode)
	}
	fm, err := c.fetchFanMode(ctx)
	if err != nil {
		return fmt.Errorf("set fan mode (get mode): %w", err)
	}
	target := fm.Actions.SetFanMode.Target
	if target == "" {
		return fmt.Errorf("SetFanMode action target not advertised")
	}
	target = normalizeURL(c.baseURL, target)
	return c.do(ctx, http.MethodPost, target, map[string]any{"FanMode": mode}, nil)
}

// SetFanDuty is NOT supported on this board. Verified against the live BMC:
// PUT (and OPTIONS) return 501 "does not support the PUT method for any
// resource" on the Fanprofile resource, so editing arrDuty via Redfish is
// impossible. We keep the method for interface completeness but return a clear
// error instead of attempting a doomed write.
func (c *Client) SetFanDuty(_ context.Context, _ int, _ float64) error {
	return fmt.Errorf("fan duty override not supported: this BMC rejects PUT on the Fanprofile resource (501 Not Implemented); use the Auto/Half/Full mode switcher or the BMC web UI")
}

// SetGPUFan is not handled here (GPU fans go through NVML/nvidia-smi). The
// redfish controller returns a clear error, and the composite controller falls
// back to the GPU controller.
func (c *Client) SetGPUFan(_ context.Context, _ int, _ float64) error {
	return fmt.Errorf("redfish controller cannot set GPU fan")
}

// Capabilities reflects what is ACTUALLY supported on this board (verified
// live): mode switching (Auto/Half/Full) works; duty override is NOT (BMC
// rejects PUT on Fanprofile with 501); GPU fan control is delegated to the GPU
// controller.
func (c *Client) Capabilities() metrics.Capabilities {
	return metrics.Capabilities{Profiles: true, DutyOverride: false, GPUFanControl: false}
}

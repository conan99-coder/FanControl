// AMI sensor readings provider.
//
// The MC62-G40 BMC exposes voltages (P_12V, P_5V, P_3V3, ...) ONLY through the
// AMI web API /api/detail_sensors_readings — not via Redfish Thermal. This
// provider implements the AMI session+CSRF flow from BMC-API-NOTES.md and emits
// voltage scalars into the snapshot.
package redfish

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/hedchr/fanctrl/internal/metrics"
)

// amiClient reads the AMI sensor readings endpoint with session + CSRF.
type amiClient struct {
	baseURL   string
	user      string
	password  string
	http      *http.Client // with cookie jar for QSESSIONID/__Host-garc
	mu        sync.Mutex
	csrf      string
	loggedIn  time.Time
	lastErr   error
}

// AMIOption configures an amiClient.
type AMIOption func(*amiClient)

// NewAMIClient builds an AMI sensor client.
func NewAMIClient(baseURL, user, password string, insecureTLS bool) *amiClient {
	jar := newCookieJar()
	c := &amiClient{
		baseURL:  strings.TrimRight(baseURL, "/"),
		user:     user,
		password: password,
		http:     &http.Client{Timeout: 10 * time.Second, Jar: jar},
	}
	if insecureTLS {
		c.http.Transport = insecureTransport()
	}
	return c
}

// Name implements Provider.
func (c *amiClient) Name() string { return "ami" }

// Close implements Provider.
func (c *amiClient) Close() error {
	c.http.CloseIdleConnections()
	return nil
}

// ensureSession logs in if needed and returns a valid CSRF token. It is safe
// for concurrent use; login is refreshed every 30 minutes or on demand.
func (c *amiClient) ensureSession(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.csrf != "" && time.Since(c.loggedIn) < 30*time.Minute {
		return c.csrf, nil
	}
	csrf, err := c.login(ctx)
	c.lastErr = err
	if err != nil {
		return "", err
	}
	c.csrf = csrf
	c.loggedIn = time.Now()
	return csrf, nil
}

// login performs the AMI two-step login: seed anonymous cookie, then POST the
// session form. Returns the CSRFToken.
func (c *amiClient) login(ctx context.Context) (string, error) {
	// Step 1: seed the anonymous QSESSIONID cookie.
	req1, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/", nil)
	if err != nil {
		return "", err
	}
	resp1, err := c.http.Do(req1)
	if err != nil {
		return "", fmt.Errorf("ami seed session: %w", err)
	}
	_, _ = io.Copy(io.Discard, resp1.Body)
	resp1.Body.Close()

	// Step 2: POST /api/session (form-encoded).
	form := url.Values{}
	form.Set("username", c.user)
	form.Set("password", c.password)
	req2, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/session", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req2.Header.Set("X-CSRFTOKEN", "null")
	req2.Header.Set("Origin", c.baseURL)

	resp2, err := c.http.Do(req2)
	if err != nil {
		return "", fmt.Errorf("ami login: %w", err)
	}
	defer resp2.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp2.Body, 1<<20))
	if resp2.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ami login status %d: %s", resp2.StatusCode, strings.TrimSpace(string(body)))
	}
	var parsed struct {
		CSRFToken string `json:"CSRFToken"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("ami login parse: %w", err)
	}
	if parsed.CSRFToken == "" {
		return "", fmt.Errorf("ami login: no CSRFToken in response")
	}
	return parsed.CSRFToken, nil
}

// collect fetches the sensor readings and returns voltage scalars.
func (c *amiClient) collect(ctx context.Context) ([]metrics.Scalar, error) {
	csrf, err := c.ensureSession(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/detail_sensors_readings", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-CSRFTOKEN", csrf)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ami sensors: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		// Session may have expired; force re-login next time.
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			c.mu.Lock()
			c.csrf = ""
			c.mu.Unlock()
		}
		return nil, fmt.Errorf("ami sensors status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var sensors []amiSensor
	if err := json.Unmarshal(body, &sensors); err != nil {
		return nil, fmt.Errorf("ami sensors parse: %w", err)
	}
	return voltagesFrom(sensors), nil
}

// amiSensor is one element of /api/detail_sensors_readings.
type amiSensor struct {
	ID           int       `json:"id"`
	SensorNumber int       `json:"sensor_number"`
	Name         string    `json:"name"`
	Type         string    `json:"type"`
	TypeNumber   int       `json:"type_number"`
	Reading      float64   `json:"reading"`
	RawReading   float64   `json:"raw_reading"`
	Unit         string    `json:"unit"`
	LowCrit      flexFloat `json:"lower_critical_threshold"`
	HighCrit     flexFloat `json:"higher_critical_threshold"`
	Accessible   int       `json:"accessible"`
}

// flexFloat accepts a JSON number OR the literal "NA" (the BMC uses "NA" for
// absent thresholds). Zero is returned for "NA"/missing.
type flexFloat float64

// UnmarshalJSON handles both numeric values and the "NA" string.
func (f *flexFloat) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	if s == "null" || s == "" || strings.EqualFold(s, "\"NA\"") || strings.EqualFold(s, "NA") {
		*f = 0
		return nil
	}
	// Try numeric JSON first (may be quoted like "12.5" in some firmware).
	var num float64
	if err := json.Unmarshal(data, &num); err == nil {
		*f = flexFloat(num)
		return nil
	}
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		if v, err := parseFloatStr(str); err == nil {
			*f = flexFloat(v)
			return nil
		}
	}
	*f = 0
	return nil
}

// parseFloatStr parses a plain number string, tolerating spaces.
func parseFloatStr(s string) (float64, error) {
	var v float64
	_, err := fmt.Sscanf(strings.TrimSpace(s), "%g", &v)
	return v, err
}

// Float returns the value as float64.
func (f flexFloat) Float() float64 { return float64(f) }

// voltagesFrom picks voltage sensors with a meaningful reading.
func voltagesFrom(sensors []amiSensor) []metrics.Scalar {
	var out []metrics.Scalar
	for _, s := range sensors {
		if s.Type != "voltage" {
			continue
		}
		if s.Reading <= 0 {
			continue
		}
		sc := metrics.Scalar{
			Name:  s.Name,
			Value: s.Reading,
			Unit:  "V",
			Kind:  metrics.KindVolts,
			Max:   s.HighCrit.Float(),
			Min:   s.LowCrit.Float(),
		}
		out = append(out, sc)
	}
	return out
}

// Collect implements Provider, returning voltages in Extra.
func (c *amiClient) Collect(ctx context.Context) (metrics.Snapshot, error) {
	volts, err := c.collect(ctx)
	if err != nil {
		return metrics.Snapshot{}, err
	}
	if len(volts) == 0 {
		return metrics.Snapshot{}, fmt.Errorf("no voltage sensors reported")
	}
	return metrics.Snapshot{
		Time:  time.Now(),
		Extra: volts,
	}, nil
}

// Discover reports available voltage sensors.
func (c *amiClient) Discover(ctx context.Context) metrics.Discovery {
	d := metrics.Discovery{Source: "ami", Meta: map[string]string{"bmc": c.baseURL}}
	volts, err := c.collect(ctx)
	if err != nil {
		d.Meta["voltages"] = "unavailable"
		return d
	}
	d.Meta["voltages"] = "available"
	for _, v := range volts {
		d.Meta["voltage_"+v.Name] = fmt.Sprintf("%.3fV", v.Value)
	}
	return d
}

// newCookieJar returns an in-memory cookie jar (QSESSIONID + __Host-garc).
func newCookieJar() http.CookieJar {
	return newMemoryJar()
}

// memoryJar is a minimal in-memory CookieJar to avoid importing
// net/http/cookiejar's public interface complexities in tests.
type memoryJar struct {
	mu    sync.Mutex
	cookies map[string][]*http.Cookie
}

func newMemoryJar() *memoryJar { return &memoryJar{cookies: map[string][]*http.Cookie{}} }

func (j *memoryJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.mu.Lock()
	defer j.mu.Unlock()
	host := u.Hostname()
	j.cookies[host] = cookies
}

func (j *memoryJar) Cookies(u *url.URL) []*http.Cookie {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.cookies[u.Hostname()]
}

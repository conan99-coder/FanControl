// Package config loads FanControl's YAML configuration and the separate
// secret-bearing files. Serialization and validation live here so the rest of
// the app can rely on a fully-populated Config struct.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the full, validated runtime configuration.
type Config struct {
	// Listen is the address the HTTP server binds (e.g. "0.0.0.0:8080").
	Listen string `yaml:"listen"`
	// PollInterval is how often the poller collects a snapshot.
	PollInterval time.Duration `yaml:"poll_interval"`
	// Provider selects the fetch mode: "real" (host+gpu+redfish) or "mock".
	Provider string `yaml:"provider"`
	// DryRun disables all control-plane writes while still collecting real data.
	DryRun bool `yaml:"dry_run"`
	// ReadOnly binds localhost and disables write endpoints entirely.
	ReadOnly bool `yaml:"read_only"`
	// Auth configures the login + roles.
	Auth AuthConfig `yaml:"auth"`
	// BMC configures the Redfish connection to the Gigabyte MC62-G40.
	BMC BMCConfig `yaml:"bmc"`
	// GPU configures nvidia-smi usage.
	GPU GPUConfig `yaml:"gpu"`
	// Vast configures the read-only Vast.ai hosting telemetry (earnings,
	// rental rate, contract end dates via `vastai show machines --raw`).
	Vast VastConfig `yaml:"vast"`
	// Thresholds defines warn + hard limits used by the governor.
	Thresholds Thresholds `yaml:"thresholds"`
	// History controls the on-disk ring buffer.
	History HistoryConfig `yaml:"history"`
	// Layout is the default widget layout (overridable per user).
	Layout WidgetLayout `yaml:"layout"`
}

// AuthConfig controls login + roles.
type AuthConfig struct {
	// Enabled toggles authentication. When false (demo/localhost), any user is
	// an admin. Never disable this when exposed beyond localhost.
	Enabled bool `yaml:"enabled"`
	// SessionTTL is how long a login session lasts.
	SessionTTL time.Duration `yaml:"session_ttl"`
	// Users is the static user list. Passwords may be provided as bcrypt hashes
	// (recommended) or plaintext (discouraged, still supported for bootstrap).
	Users []User `yaml:"users"`
	// SecretPath points to the session-signing secret (0600 file or env var
	// name prefixed with "env:"). Avoid committing a hardcoded secret.
	SecretPath string `yaml:"secret_path"`
	// AllowUnauthenticatedWrites is an EXPLICIT opt-out for the safety rule that
	// requires auth on write-capable non-localhost binds. Setting this true says
	// "I know anyone who can reach this port can change fan modes" — only enable
	// if protected by an upstream proxy/firewall. Logged loudly at startup.
	AllowUnauthenticatedWrites bool `yaml:"allow_unauthenticated_writes"`
}

// User is a static account.
type User struct {
	Name     string `yaml:"name"`
	Password string `yaml:"password"` // bcrypt hash; plaintext if it fails bcrypt
	Role     string `yaml:"role"`     // "admin" or "viewer"
	Hash     bool   `yaml:"hash"`     // true if Password is a bcrypt hash
}

// BMCConfig controls the Redfish connection.
type BMCConfig struct {
	URL      string `yaml:"url"`
	Username string `yaml:"username"`
	// PasswordPath is a 0600 file (or env:VAR) holding the BMC password.
	PasswordPath string `yaml:"password_path"`
	// InsecureTLS accepts the BMC's self-signed certificate. Required unless the
	// BMC has a trusted cert.
	InsecureTLS bool `yaml:"insecure_tls"`
	// Profile is the default fan profile to select on startup (may be empty).
	Profile string `yaml:"profile"`
}

// GPUConfig controls nvidia-smi collection.
type GPUConfig struct {
	// Enabled toggles GPU collection entirely (e.g. no NVIDIA driver).
	Enabled bool `yaml:"enabled"`
	// Query is the nvidia-smi path. Defaults to "nvidia-smi" on PATH.
	Query string `yaml:"query"`
	// QueryInterval throttles GPU polls (GPU polling is heavier than sysfs).
	QueryInterval time.Duration `yaml:"query_interval"`
}

// VastConfig configures the optional read-only Vast.ai hosting provider.
type VastConfig struct {
	// Enabled toggles the Vast.ai telemetry provider (earnings/rates/contracts).
	Enabled bool `yaml:"enabled"`
	// CLI is the vastai command (or absolute path) used to fetch machine data.
	CLI string `yaml:"cli"`
	// APIKeyPath is a 0600 file (or env:VAR) with the Vast API key. Passed to
	// the CLI via VAST_API_KEY / --api-key; never served by the app.
	APIKeyPath string `yaml:"api_key_path"`
	// Interval throttles CLI invocations (default 60s).
	Interval time.Duration `yaml:"interval"`
}

// Thresholds defines the safety limits.
type Thresholds struct {
	// GPUTempWarn and GPUTempHard are the temperature (C) gates for GPU.
	GPUTempWarn float64 `yaml:"gpu_temp_warn"`
	GPUTempHard float64 `yaml:"gpu_temp_hard"`
	// CPUTempWarn / CPUTempHard are the CPU gates.
	CPUTempWarn float64 `yaml:"cpu_temp_warn"`
	CPUTempHard float64 `yaml:"cpu_temp_hard"`
	// DiskUsedWarn is the %-full gate for disk warnings.
	DiskUsedWarn float64 `yaml:"disk_used_warn"`
	// AfterHardTemp, the governor reverts to the safe profile for this long.
	Cooldown time.Duration `yaml:"cooldown"`
}

// HistoryConfig controls the on-disk ring buffer.
type HistoryConfig struct {
	Enabled bool   `yaml:"enabled"`
	Path    string `yaml:"path"`
	// Points is how many data points to keep per series.
	Points int `yaml:"points"`
	// Interval is how often to persist (snapshot every N ticks).
	Interval time.Duration `yaml:"interval"`
}

// WidgetLayout is the persisted default dashboard layout.
type WidgetLayout struct {
	Columns   int      `yaml:"columns"`
	RowHeight int      `yaml:"row_height"`
	Widgets   []Widget `yaml:"widgets"`
}

// Widget describes one dashboard widget (type, position, optional config).
type Widget struct {
	ID   string `yaml:"id"`
	Type string `yaml:"type"` // cpu | gpu | disk | net | temps | fans | summary
	X    int    `yaml:"x"`
	Y    int    `yaml:"y"`
	W    int    `yaml:"w"`
	H    int    `yaml:"h"`
	GPU  int    `yaml:"gpu,omitempty"` // which GPU index, for gpu widgets
	Show bool   `yaml:"show"`
}

// Default returns a sane default configuration, matching the recommended plan.
func Default() Config {
	return Config{
		Listen:       "0.0.0.0:8080",
		PollInterval: 2 * time.Second,
		Provider:     "real",
		DryRun:       true, // safety: never write until explicitly enabled
		ReadOnly:     false,
		Auth: AuthConfig{
			Enabled:    true,
			SessionTTL: 24 * time.Hour,
		},
		BMC: BMCConfig{
			InsecureTLS: true,
		},
		GPU: GPUConfig{
			Enabled:       true,
			Query:         "nvidia-smi",
			QueryInterval: time.Second,
		},
		Vast: VastConfig{
			Enabled:  false,
			CLI:      "vastai",
			Interval: time.Minute,
		},
		Thresholds: Thresholds{
			GPUTempWarn:  82,
			GPUTempHard:  88,
			CPUTempWarn:  78,
			CPUTempHard:  92,
			DiskUsedWarn: 90,
			Cooldown:     5 * time.Minute,
		},
		History: HistoryConfig{
			Enabled:  true,
			Path:     "/var/lib/fanctrl/history.json",
			Points:   600,
			Interval: 2 * time.Second,
		},
		Layout: WidgetLayout{
			Columns:   12,
			RowHeight: 60,
			Widgets:   DefaultLayout(),
		},
	}
}

// DefaultLayout returns a reasonable widget grid for the rig.
func DefaultLayout() []Widget {
	return []Widget{
		{ID: "summary", Type: "summary", X: 0, Y: 0, W: 12, H: 1, Show: true},
		{ID: "gpu0", Type: "gpu", X: 0, Y: 1, W: 4, H: 3, GPU: 0, Show: true},
		{ID: "gpu1", Type: "gpu", X: 4, Y: 1, W: 4, H: 3, GPU: 1, Show: true},
		{ID: "cpu", Type: "cpu", X: 8, Y: 1, W: 4, H: 3, Show: true},
		{ID: "fans", Type: "fans", X: 0, Y: 4, W: 6, H: 3, Show: true},
		{ID: "temps", Type: "temps", X: 6, Y: 4, W: 6, H: 3, Show: true},
		{ID: "disk", Type: "disk", X: 0, Y: 7, W: 6, H: 2, Show: true},
		{ID: "net", Type: "net", X: 6, Y: 7, W: 6, H: 2, Show: true},
	}
}

// Load reads config from path (if non-empty) and applies defaults, then
// validates. A missing file is not an error (defaults are used), matching the
// "run it right now against defaults" developer flow.
func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("read config %s: %w", path, err)
		}
		dec := yaml.NewDecoder(strings.NewReader(string(data)))
		dec.KnownFields(true) // reject unknown keys to catch typos
		if err := dec.Decode(&cfg); err != nil {
			return Config{}, fmt.Errorf("parse config %s: %w", path, err)
		}
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks invariants and applies safety rules.
func (c *Config) Validate() error {
	if c.Listen == "" {
		return fmt.Errorf("listen must be set")
	}
	if c.PollInterval <= 0 {
		return fmt.Errorf("poll_interval must be > 0")
	}
	switch c.Provider {
	case "real", "mock":
	default:
		return fmt.Errorf("provider must be 'real' or 'mock', got %q", c.Provider)
	}
	// Safety: if exposing beyond localhost, auth must be enabled.
	if c.Auth.Enabled {
		if len(c.Auth.Users) == 0 {
			// Demon/insecure? Only allow when provider is mock (local demo).
			return fmt.Errorf("auth enabled but no users configured")
		}
	}
	// A non-localhost bind with auth disabled is dangerous because the write
	// endpoints (fan control) would be reachable by anyone. EXCEPTION: in
	// dry-run mode the control service no-ops every write, so exposing read-only
	// data on the LAN is acceptable. Otherwise require auth or localhost. The
	// operator can EXPLICITLY opt out via AllowUnauthenticatedWrites (logged
	// loudly at startup) — used when an upstream proxy/firewall gates access.
	host := strings.Split(c.Listen, ":")[0]
	if !c.Auth.Enabled && c.Provider == "real" && !c.DryRun && host != "127.0.0.1" && host != "localhost" {
		if !c.Auth.AllowUnauthenticatedWrites {
			return fmt.Errorf("refusing to bind %q with auth disabled; enable auth, bind to localhost, run dry_run, or set auth.allow_unauthenticated_writes (explicit opt-out)", c.Listen)
		}
	}
	if c.BMC.URL != "" && c.BMC.PasswordPath == "" {
		return fmt.Errorf("bmc.password_path is required when bmc.url is set")
	}
	if c.Thresholds.GPUTempHard <= c.Thresholds.GPUTempWarn {
		return fmt.Errorf("thresholds.gpu_temp_hard must be > gpu_temp_warn")
	}
	return nil
}

// ResolveSecret reads a secret from a path or an "env:VAR" reference. This is
// used for the BMC password and the session secret so they never sit in a
// committed config file.
func ResolveSecret(ref string) (string, error) {
	if strings.HasPrefix(ref, "env:") {
		name := strings.TrimPrefix(ref, "env:")
		return os.Getenv(name), nil
	}
	data, err := os.ReadFile(ref)
	if err != nil {
		return "", fmt.Errorf("read secret %s: %w", ref, err)
	}
	return strings.TrimSpace(string(data)), nil
}

// role constants
const (
	RoleAdmin  = "admin"
	RoleViewer = "viewer"
)

// MarshalJSON is provided so config structs don't accidentally leak secrets via
// JSON (e.g. debug dumps). Not strictly required, but a cheap safety net.
func (c BMCConfig) MarshalJSON() ([]byte, error) {
	type alias BMCConfig
	return json.Marshal(&struct {
		alias
		Password string `json:"password,omitempty"`
	}{alias: (alias)(c), Password: "***"})
}

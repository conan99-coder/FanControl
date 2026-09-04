// Settings is the runtime configuration manager backing the settings page. It
// holds the live config (after hot-applies), persists to config.yaml, writes
// the secret files, and can trigger a service restart. Secrets are write-only:
// they are never returned by the API.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/hedchr/fanctrl/internal/config"
	"github.com/hedchr/fanctrl/internal/providers/redfish"
)

// ApplyFunc hot-applies a validated config to the running services (providers,
// thresholds, auth users, control flags).
type ApplyFunc func(cfg config.Config) error

// RestartFunc restarts the service (used after non-hot-appliable changes).
type RestartFunc func() error

// AuditFunc records a settings action in the audit log.
type AuditFunc func(actor, action string, detail map[string]any, result string)

// Settings manages config editing.
type Settings struct {
	mu      sync.RWMutex
	path    string // config file path ("" = no file, writes disabled)
	cfg     config.Config
	apply   ApplyFunc
	restart RestartFunc
	audit   AuditFunc
}

// NewSettings builds the settings manager.
func NewSettings(path string, cfg config.Config, apply ApplyFunc, restart RestartFunc, audit AuditFunc) *Settings {
	return &Settings{path: path, cfg: cfg, apply: apply, restart: restart, audit: audit}
}

// Current returns the live config (copy).
func (s *Settings) Current() config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// Update replaces the live config after a successful hot-apply.
func (s *Settings) Update(cfg config.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
}

// ---- DTOs (json; secrets never included) ----

type userDTO struct {
	Name     string `json:"name"`
	Password string `json:"password,omitempty"` // write-only
	Role     string `json:"role"`
	Hash     bool   `json:"hash"`
}

type thresholdsDTO struct {
	GPUTempWarn  float64 `json:"gpuTempWarn"`
	GPUTempHard  float64 `json:"gpuTempHard"`
	CPUTempWarn  float64 `json:"cpuTempWarn"`
	CPUTempHard  float64 `json:"cpuTempHard"`
	DiskUsedWarn float64 `json:"diskUsedWarn"`
	Cooldown     string  `json:"cooldown"`
}

type widgetDTO struct {
	Type string `json:"type"`
	Show bool   `json:"show"`
}

type settingsDTO struct {
	ConfigPath string `json:"configPath"`

	Listen       string `json:"listen"`
	PollInterval string `json:"pollInterval"`
	Provider     string `json:"provider"`
	DryRun       bool   `json:"dryRun"`
	ReadOnly     bool   `json:"readOnly"`

	AuthEnabled                    bool      `json:"authEnabled"`
	AuthSessionTTL                 string    `json:"authSessionTtl"`
	AuthAllowUnauthenticatedWrites bool      `json:"authAllowUnauthenticatedWrites"`
	AuthUsers                      []userDTO `json:"authUsers"`
	AuthSecretPath                 string    `json:"authSecretPath"`

	BMCURL          string `json:"bmcUrl"`
	BMCUsername     string `json:"bmcUsername"`
	BMCPasswordPath string `json:"bmcPasswordPath"`
	BMCHasPassword  bool   `json:"bmcHasPassword"`
	BMCInsecureTLS  bool   `json:"bmcInsecureTls"`
	BMCProfile      string `json:"bmcProfile"`

	GPUEnabled       bool   `json:"gpuEnabled"`
	GPUQuery         string `json:"gpuQuery"`
	GPUQueryInterval string `json:"gpuQueryInterval"`

	VastEnabled    bool   `json:"vastEnabled"`
	VastCLI        string `json:"vastCli"`
	VastAPIKeyPath string `json:"vastApiKeyPath"`
	VastHasKey     bool   `json:"vastHasKey"`
	VastInterval   string `json:"vastInterval"`
	// VastMarketFilter is a comma-separated list of GPU names for the market
	// widget (empty = all).
	VastMarketFilter string `json:"vastMarketFilter"`

	DockerEnabled  bool   `json:"dockerEnabled"`
	DockerCLI      string `json:"dockerCli"`
	DockerInterval string `json:"dockerInterval"`

	Thresholds thresholdsDTO `json:"thresholds"`
	Widgets    []widgetDTO   `json:"widgets"`
}

// patchDTO mirrors settingsDTO with pointers for partial updates. Users and
// widgets are replaced wholesale when provided.
type patchDTO struct {
	Listen       *string `json:"listen"`
	PollInterval *string `json:"pollInterval"`
	Provider     *string `json:"provider"`
	DryRun       *bool   `json:"dryRun"`
	ReadOnly     *bool   `json:"readOnly"`

	AuthEnabled                    *bool     `json:"authEnabled"`
	AuthSessionTTL                 *string   `json:"authSessionTtl"`
	AuthAllowUnauthenticatedWrites *bool     `json:"authAllowUnauthenticatedWrites"`
	AuthUsers                      []userDTO `json:"authUsers"`
	AuthSecretPath                 *string   `json:"authSecretPath"`

	BMCURL          *string `json:"bmcUrl"`
	BMCUsername     *string `json:"bmcUsername"`
	BMCPasswordPath *string `json:"bmcPasswordPath"`
	BMCInsecureTLS  *bool   `json:"bmcInsecureTls"`
	BMCProfile      *string `json:"bmcProfile"`

	GPUEnabled       *bool   `json:"gpuEnabled"`
	GPUQuery         *string `json:"gpuQuery"`
	GPUQueryInterval *string `json:"gpuQueryInterval"`

	VastEnabled      *bool   `json:"vastEnabled"`
	VastCLI          *string `json:"vastCli"`
	VastAPIKeyPath   *string `json:"vastApiKeyPath"`
	VastInterval     *string `json:"vastInterval"`
	VastMarketFilter *string `json:"vastMarketFilter"`

	DockerEnabled  *bool   `json:"dockerEnabled"`
	DockerCLI      *string `json:"dockerCli"`
	DockerInterval *string `json:"dockerInterval"`

	Thresholds *thresholdsPatchDTO `json:"thresholds"`
	Widgets    []widgetDTO         `json:"widgets"`
}

type thresholdsPatchDTO struct {
	GPUTempWarn  *float64 `json:"gpuTempWarn"`
	GPUTempHard  *float64 `json:"gpuTempHard"`
	CPUTempWarn  *float64 `json:"cpuTempWarn"`
	CPUTempHard  *float64 `json:"cpuTempHard"`
	DiskUsedWarn *float64 `json:"diskUsedWarn"`
	Cooldown     *string  `json:"cooldown"`
}

// ---- conversion ----

func (s *Settings) dto() settingsDTO {
	cfg := s.Current()
	d := settingsDTO{
		ConfigPath:                     s.path,
		Listen:                         cfg.Listen,
		PollInterval:                   cfg.PollInterval.String(),
		Provider:                       cfg.Provider,
		DryRun:                         cfg.DryRun,
		ReadOnly:                       cfg.ReadOnly,
		AuthEnabled:                    cfg.Auth.Enabled,
		AuthSessionTTL:                 cfg.Auth.SessionTTL.String(),
		AuthAllowUnauthenticatedWrites: cfg.Auth.AllowUnauthenticatedWrites,
		AuthSecretPath:                 cfg.Auth.SecretPath,
		BMCURL:                         cfg.BMC.URL,
		BMCUsername:                    cfg.BMC.Username,
		BMCPasswordPath:                cfg.BMC.PasswordPath,
		BMCHasPassword:                 config.SecretConfigured(cfg.BMC.PasswordPath),
		BMCInsecureTLS:                 cfg.BMC.InsecureTLS,
		BMCProfile:                     cfg.BMC.Profile,
		GPUEnabled:                     cfg.GPU.Enabled,
		GPUQuery:                       cfg.GPU.Query,
		GPUQueryInterval:               cfg.GPU.QueryInterval.String(),
		VastEnabled:                    cfg.Vast.Enabled,
		VastCLI:                        cfg.Vast.CLI,
		VastAPIKeyPath:                 cfg.Vast.APIKeyPath,
		VastHasKey:                     config.SecretConfigured(cfg.Vast.APIKeyPath),
		VastInterval:                   cfg.Vast.Interval.String(),
		VastMarketFilter:               strings.Join(cfg.Vast.MarketFilter, ", "),
		DockerEnabled:                  cfg.Docker.Enabled,
		DockerCLI:                      cfg.Docker.CLI,
		DockerInterval:                 cfg.Docker.Interval.String(),
		Thresholds: thresholdsDTO{
			GPUTempWarn:  cfg.Thresholds.GPUTempWarn,
			GPUTempHard:  cfg.Thresholds.GPUTempHard,
			CPUTempWarn:  cfg.Thresholds.CPUTempWarn,
			CPUTempHard:  cfg.Thresholds.CPUTempHard,
			DiskUsedWarn: cfg.Thresholds.DiskUsedWarn,
			Cooldown:     cfg.Thresholds.Cooldown.String(),
		},
	}
	d.AuthUsers = make([]userDTO, 0, len(cfg.Auth.Users))
	for _, u := range cfg.Auth.Users {
		d.AuthUsers = append(d.AuthUsers, userDTO{Name: u.Name, Role: u.Role, Hash: u.Hash})
	}
	layout := cfg.Layout.Widgets
	if len(layout) == 0 {
		layout = config.DefaultLayout()
	}
	for _, w := range layout {
		d.Widgets = append(d.Widgets, widgetDTO{Type: w.Type, Show: w.Show})
	}
	return d
}

func applyPatch(cfg *config.Config, p patchDTO) error {
	setStr := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	setBool := func(dst *bool, v *bool) {
		if v != nil {
			*dst = *v
		}
	}
	setDur := func(dst *time.Duration, v *string) error {
		if v == nil {
			return nil
		}
		d, err := time.ParseDuration(*v)
		if err != nil {
			return fmt.Errorf("invalid duration %q", *v)
		}
		*dst = d
		return nil
	}

	setStr(&cfg.Listen, p.Listen)
	if err := setDur(&cfg.PollInterval, p.PollInterval); err != nil {
		return err
	}
	setStr(&cfg.Provider, p.Provider)
	setBool(&cfg.DryRun, p.DryRun)
	setBool(&cfg.ReadOnly, p.ReadOnly)

	setBool(&cfg.Auth.Enabled, p.AuthEnabled)
	if err := setDur(&cfg.Auth.SessionTTL, p.AuthSessionTTL); err != nil {
		return err
	}
	setBool(&cfg.Auth.AllowUnauthenticatedWrites, p.AuthAllowUnauthenticatedWrites)
	setStr(&cfg.Auth.SecretPath, p.AuthSecretPath)
	if p.AuthUsers != nil {
		existing := map[string]config.User{}
		for _, u := range cfg.Auth.Users {
			existing[u.Name] = u
		}
		users := make([]config.User, 0, len(p.AuthUsers))
		for _, u := range p.AuthUsers {
			if u.Name == "" || u.Role == "" {
				return fmt.Errorf("auth users need a name and role")
			}
			// An empty password keeps the previously stored value.
			password, hash := u.Password, u.Hash
			if password == "" {
				if prev, ok := existing[u.Name]; ok {
					password, hash = prev.Password, prev.Hash
				}
			}
			users = append(users, config.User{Name: u.Name, Role: u.Role, Hash: hash, Password: password})
		}
		cfg.Auth.Users = users
	}

	setStr(&cfg.BMC.URL, p.BMCURL)
	setStr(&cfg.BMC.Username, p.BMCUsername)
	setStr(&cfg.BMC.PasswordPath, p.BMCPasswordPath)
	setBool(&cfg.BMC.InsecureTLS, p.BMCInsecureTLS)
	setStr(&cfg.BMC.Profile, p.BMCProfile)

	setBool(&cfg.GPU.Enabled, p.GPUEnabled)
	setStr(&cfg.GPU.Query, p.GPUQuery)
	if err := setDur(&cfg.GPU.QueryInterval, p.GPUQueryInterval); err != nil {
		return err
	}

	setBool(&cfg.Vast.Enabled, p.VastEnabled)
	setStr(&cfg.Vast.CLI, p.VastCLI)
	setStr(&cfg.Vast.APIKeyPath, p.VastAPIKeyPath)
	if err := setDur(&cfg.Vast.Interval, p.VastInterval); err != nil {
		return err
	}
	if p.VastMarketFilter != nil {
		var filter []string
		for _, part := range strings.Split(*p.VastMarketFilter, ",") {
			if name := strings.TrimSpace(part); name != "" {
				filter = append(filter, name)
			}
		}
		cfg.Vast.MarketFilter = filter
	}

	setBool(&cfg.Docker.Enabled, p.DockerEnabled)
	setStr(&cfg.Docker.CLI, p.DockerCLI)
	if err := setDur(&cfg.Docker.Interval, p.DockerInterval); err != nil {
		return err
	}

	if p.Thresholds != nil {
		t := &cfg.Thresholds
		setF := func(dst *float64, v *float64) {
			if v != nil {
				*dst = *v
			}
		}
		setF(&t.GPUTempWarn, p.Thresholds.GPUTempWarn)
		setF(&t.GPUTempHard, p.Thresholds.GPUTempHard)
		setF(&t.CPUTempWarn, p.Thresholds.CPUTempWarn)
		setF(&t.CPUTempHard, p.Thresholds.CPUTempHard)
		setF(&t.DiskUsedWarn, p.Thresholds.DiskUsedWarn)
		if err := setDur(&t.Cooldown, p.Thresholds.Cooldown); err != nil {
			return err
		}
	}

	if p.Widgets != nil {
		layout := config.WidgetLayout{Columns: cfg.Layout.Columns, RowHeight: cfg.Layout.RowHeight}
		existing := cfg.Layout.Widgets
		if len(existing) == 0 {
			existing = config.DefaultLayout()
		}
		for _, w := range existing {
			show := w.Show
			for _, p := range p.Widgets {
				if p.Type == w.Type {
					show = p.Show
					break
				}
			}
			layout.Widgets = append(layout.Widgets, config.Widget{
				ID: w.ID, Type: w.Type, X: w.X, Y: w.Y, W: w.W, H: w.H, GPU: w.GPU, Show: show,
			})
		}
		cfg.Layout = layout
	}
	return nil
}

// ---- handlers ----

func (s *Server) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	_ = r
	writeJSON(w, http.StatusOK, s.settings.dto())
}

func (s *Server) handleSettingsPut(w http.ResponseWriter, r *http.Request) {
	if s.settings.path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no config file loaded (start with --config); writes are disabled"})
		return
	}
	var p patchDTO
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	cfg := s.settings.Current()
	if err := applyPatch(&cfg, p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := cfg.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := cfg.Save(s.settings.path); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if s.settings.apply != nil {
		if err := s.settings.apply(cfg); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "config saved but apply failed: " + err.Error()})
			return
		}
	}
	s.settings.Update(cfg)
	s.auditSettings(r, "settings_update", map[string]any{"fields": patchSummary(p)}, "ok")
	writeJSON(w, http.StatusOK, s.settings.dto())
}

func (s *Server) handleSettingsSecret(w http.ResponseWriter, r *http.Request) {
	if s.settings.path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no config file loaded; writes are disabled"})
		return
	}
	var req struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	cfg := s.settings.Current()
	ref := ""
	what := ""
	switch r.URL.Path {
	case "/api/settings/secrets/bmc":
		ref, what = cfg.BMC.PasswordPath, "bmc password"
	case "/api/settings/secrets/vast":
		ref, what = cfg.Vast.APIKeyPath, "vast api key"
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown secret"})
		return
	}
	if err := config.WriteSecret(ref, req.Value); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if s.settings.apply != nil {
		if err := s.settings.apply(cfg); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "secret saved but apply failed: " + err.Error()})
			return
		}
	}
	s.auditSettings(r, "settings_secret", map[string]any{"secret": what}, "ok")
	writeJSON(w, http.StatusOK, map[string]string{"ok": "saved"})
}

func (s *Server) handleSettingsRestart(w http.ResponseWriter, r *http.Request) {
	_ = r
	if s.settings.restart == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "restart not available (not running under systemd?)"})
		return
	}
	s.auditSettings(r, "settings_restart", nil, "ok")
	go func() {
		// Give the HTTP response a moment to flush before the process restarts.
		time.Sleep(200 * time.Millisecond)
		if err := s.settings.restart(); err != nil {
			s.log.Error("settings restart failed", "err", err)
		}
	}()
	writeJSON(w, http.StatusOK, map[string]string{"ok": "restarting"})
}

func (s *Server) handleSettingsTestBMC(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL         string `json:"url"`
		Username    string `json:"username"`
		Password    string `json:"password"`
		InsecureTLS bool   `json:"insecureTls"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "url required"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	rc := redfish.NewClient(req.URL, req.Username, req.Password, req.InsecureTLS)
	if _, err := rc.Collect(ctx); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSettingsTestVast(w http.ResponseWriter, r *http.Request) {
	var req struct {
		APIKey string `json:"apiKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.APIKey) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "apiKey required"})
		return
	}
	cfg := s.settings.Current()
	cli := cfg.Vast.CLI
	if cli == "" {
		cli = "vastai"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, cli, "show", "machines", "--raw", "--api-key", strings.TrimSpace(req.APIKey))
	out, err := cmd.Output()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	var parsed struct {
		Machines []json.RawMessage `json:"machines"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "unexpected CLI output: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "machines": len(parsed.Machines)})
}

// auditSettings records a settings action with the session actor.
func (s *Server) auditSettings(r *http.Request, action string, detail map[string]any, result string) {
	if s.settings.audit == nil {
		return
	}
	actor := "anonymous"
	if sess, ok := sessionFrom(r.Context()); ok {
		actor = sess.User
	}
	s.settings.audit(actor, action, detail, result)
}

// patchSummary returns a compact field list for the audit log (no values).
func patchSummary(p patchDTO) []string {
	var fields []string
	add := func(name string, set bool) {
		if set {
			fields = append(fields, name)
		}
	}
	add("listen", p.Listen != nil)
	add("pollInterval", p.PollInterval != nil)
	add("provider", p.Provider != nil)
	add("dryRun", p.DryRun != nil)
	add("readOnly", p.ReadOnly != nil)
	add("authEnabled", p.AuthEnabled != nil)
	add("authUsers", p.AuthUsers != nil)
	add("authAllowUnauthenticatedWrites", p.AuthAllowUnauthenticatedWrites != nil)
	add("bmcUrl", p.BMCURL != nil)
	add("bmcUsername", p.BMCUsername != nil)
	add("bmcProfile", p.BMCProfile != nil)
	add("gpu", p.GPUEnabled != nil || p.GPUQuery != nil || p.GPUQueryInterval != nil)
	add("vast", p.VastEnabled != nil || p.VastCLI != nil || p.VastAPIKeyPath != nil || p.VastInterval != nil || p.VastMarketFilter != nil)
	add("docker", p.DockerEnabled != nil || p.DockerCLI != nil || p.DockerInterval != nil)
	add("thresholds", p.Thresholds != nil)
	add("widgets", p.Widgets != nil)
	return fields
}

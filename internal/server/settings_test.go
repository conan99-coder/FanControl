package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hedchr/fanctrl/internal/auth"
	"github.com/hedchr/fanctrl/internal/config"
	"github.com/hedchr/fanctrl/internal/control"
	"github.com/hedchr/fanctrl/internal/metrics"
	"github.com/hedchr/fanctrl/internal/poller"
	"github.com/hedchr/fanctrl/internal/providers/mock"
)

// testServer builds a Server wired like main, over the mock provider.
func testServer(t *testing.T, cfg config.Config, path string, apply ApplyFunc) *Server {
	t.Helper()
	mockP := mock.NewProvider()
	p := poller.New([]metrics.Provider{mockP}, cfg.Thresholds, nil, nil)
	audit := control.NewAuditLog(50)
	ctrl := control.New(mock.NewController(), p, control.Options{Audit: audit}, nil)
	store := auth.NewStore(cfg.Auth.Users, nil, time.Hour)
	settings := NewSettings(path, cfg, apply, nil, ctrl.Record)
	return New(p, ctrl, store, cfg.Auth.Enabled, nil, settings, nil)
}

func TestSettingsPutRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := config.Default()
	cfg.Listen = "127.0.0.1:8080"
	cfg.Provider = "mock"
	cfg.Auth.Enabled = false
	cfg.DryRun = true
	applied := false
	s := testServer(t, cfg, path, func(c config.Config) error { applied = true; return nil })

	body := `{"thresholds":{"gpuTempWarn":70},"pollInterval":"5s","widgets":[{"type":"volts","show":false}]}`
	req := httptest.NewRequest(http.MethodPut, "/api/settings/update", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT settings = %d: %s", rr.Code, rr.Body.String())
	}
	if !applied {
		t.Error("apply callback was not invoked")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("config file not written: %v", err)
	}
	if !strings.Contains(string(data), "gpu_temp_warn: 70") {
		t.Errorf("saved config missing threshold: %s", data)
	}

	// GET returns the new values and never the secret values.
	req = httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	var dto settingsDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if dto.Thresholds.GPUTempWarn != 70 || dto.PollInterval != "5s" {
		t.Errorf("dto mismatch: %+v", dto)
	}
	for _, u := range dto.AuthUsers {
		if u.Password != "" {
			t.Error("settings GET leaked a password")
		}
	}
	if dto.BMCHasPassword {
		t.Error("bmcHasPassword should be false without a password file")
	}
	voltsHidden := false
	for _, w := range dto.Widgets {
		if w.Type == "volts" {
			voltsHidden = !w.Show
		}
	}
	if !voltsHidden {
		t.Error("volts widget should be hidden after update")
	}
}

func TestSettingsRequireAdmin(t *testing.T) {
	cfg := config.Default() // auth enabled, default admin/admin
	cfg.Provider = "mock"
	cfg.DryRun = true
	cfg.Listen = "127.0.0.1:8080"
	s := testServer(t, cfg, filepath.Join(t.TempDir(), "c.yaml"), nil)

	// Unauthenticated settings write is refused.
	req := httptest.NewRequest(http.MethodPut, "/api/settings/update", strings.NewReader(`{"dryRun":true}`))
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated PUT = %d, want 401", rr.Code)
	}

	// Default admin/admin login works.
	login := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"username":"admin","password":"admin"}`))
	login.Header.Set("Content-Type", "application/json")
	lr := httptest.NewRecorder()
	s.Handler().ServeHTTP(lr, login)
	if lr.Code != http.StatusOK {
		t.Fatalf("login admin/admin = %d: %s", lr.Code, lr.Body.String())
	}
	cookie := ""
	for _, c := range lr.Result().Cookies() {
		if c.Name == "fanctrl_session" {
			cookie = c.Value
		}
	}
	if cookie == "" {
		t.Fatal("no session cookie issued")
	}

	// Authenticated write succeeds.
	req = httptest.NewRequest(http.MethodPut, "/api/settings/update", strings.NewReader(`{"dryRun":true}`))
	req.AddCookie(&http.Cookie{Name: "fanctrl_session", Value: cookie})
	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("authenticated PUT = %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSettingsReadPublicWhenAuthEnabled(t *testing.T) {
	cfg := config.Default()
	cfg.Provider = "mock"
	cfg.DryRun = true
	cfg.Listen = "127.0.0.1:8080"
	s := testServer(t, cfg, "", nil)

	// Read endpoints are reachable without a session (read-only dashboard).
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("anonymous status = %d, want 200", rr.Code)
	}
}

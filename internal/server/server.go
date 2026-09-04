// Package server exposes the HTTP API and serves the embedded SPA. It wires
// together the poller (read path) and the control service (write path), applies
// auth middleware, and streams live snapshots over SSE.
package server

import (
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/hedchr/fanctrl/internal/auth"
	"github.com/hedchr/fanctrl/internal/config"
	"github.com/hedchr/fanctrl/internal/control"
	"github.com/hedchr/fanctrl/internal/metrics"
	"github.com/hedchr/fanctrl/internal/poller"
)

// Server is the HTTP server.
type Server struct {
	mux      *http.ServeMux
	p        *poller.Poller
	ctrl     *control.Service
	auth     *auth.Store
	cfg      *serverConfig
	settings *Settings
	log      *slog.Logger
	assets   fs.FS
}

type serverConfig struct {
	mu          sync.RWMutex
	AuthEnabled bool
}

func (c *serverConfig) authEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.AuthEnabled
}

// New builds a Server.
func New(p *poller.Poller, ctrl *control.Service, authStore *auth.Store, authEnabled bool, assets fs.FS, settings *Settings, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	s := &Server{
		mux:      http.NewServeMux(),
		p:        p,
		ctrl:     ctrl,
		auth:     authStore,
		cfg:      &serverConfig{AuthEnabled: authEnabled},
		settings: settings,
		log:      log,
		assets:   assets,
	}
	s.routes()
	return s
}

// SetAuthEnabled hot-toggles the auth requirement.
func (s *Server) SetAuthEnabled(on bool) {
	s.cfg.mu.Lock()
	defer s.cfg.mu.Unlock()
	s.cfg.AuthEnabled = on
}

// Handler returns the root http.Handler.
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	// Public metadata endpoint (no auth) so the SPA can decide whether to show
	// a login. Must be registered before the auth-wrapped read routes.
	s.mux.HandleFunc("/api/meta", s.handleMeta)

	// Auth
	s.mux.HandleFunc("/api/login", s.method("POST", s.handleLogin))
	s.mux.Handle("/api/logout", s.authWrap(s.method("POST", s.handleLogout)))
	s.mux.Handle("/api/me", s.authWrap(http.HandlerFunc(s.handleMe)))

	// Read
	s.mux.Handle("/api/metrics", s.wrap(s.handleMetrics))
	s.mux.Handle("/api/history", s.wrap(s.handleHistory))
	s.mux.Handle("/api/status", s.wrap(s.handleStatus))
	s.mux.Handle("/api/health", s.wrap(s.handleHealth))
	s.mux.Handle("/api/discovery", s.wrap(s.handleDiscovery))

	// Settings (admin-only)
	s.mux.Handle("/api/settings", s.adminWrap(s.method("GET", s.handleSettingsGet)))
	s.mux.Handle("/api/settings/update", s.adminWrap(s.method("PUT", s.handleSettingsPut)))
	s.mux.Handle("/api/settings/secrets/bmc", s.adminWrap(s.method("POST", s.handleSettingsSecret)))
	s.mux.Handle("/api/settings/secrets/vast", s.adminWrap(s.method("POST", s.handleSettingsSecret)))
	s.mux.Handle("/api/settings/restart", s.adminWrap(s.method("POST", s.handleSettingsRestart)))
	s.mux.Handle("/api/settings/test/bmc", s.adminWrap(s.method("POST", s.handleSettingsTestBMC)))
	s.mux.Handle("/api/settings/test/vast", s.adminWrap(s.method("POST", s.handleSettingsTestVast)))

	// Control (admin-only)
	s.mux.Handle("/api/mode", s.adminWrap(s.method("POST", s.handleSetMode)))
	s.mux.Handle("/api/fan/profiles", s.adminWrap(http.HandlerFunc(s.handleProfiles)))
	s.mux.Handle("/api/fan/active", s.adminWrap(http.HandlerFunc(s.handleActiveProfile)))
	s.mux.Handle("/api/fan/mode", s.adminWrap(s.method("POST", s.handleSetFanMode)))
	s.mux.Handle("/api/fan/duty", s.adminWrap(s.method("POST", s.handleSetDuty)))
	s.mux.Handle("/api/fan/gpu", s.adminWrap(s.method("POST", s.handleSetGPUFan)))
	s.mux.Handle("/api/gpu/power", s.adminWrap(s.method("POST", s.handleSetGPUPower)))
	s.mux.Handle("/api/audit", s.adminWrap(http.HandlerFunc(s.handleAudit)))

	// SSE stream
	s.mux.Handle("/api/stream", s.wrap(s.handleStream))

	// Embedded SPA + static assets (only if embedded)
	if s.assets != nil {
		s.mux.Handle("/", s.spa())
	}
}

// ---- middleware ----

func (s *Server) wrap(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
		if !s.cfg.authEnabled() {
			h(w, r)
			return
		}
		// Reads require a valid session when auth is enabled: the dashboard
		// (and its live stream) carries the session cookie reliably, while
		// cached HTTP Basic Auth from a reverse proxy is NOT reliably attached
		// to XHR/fetch requests — causing password-prompt loops.
		sess, ok := s.authenticate(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		ctx := context.WithValue(r.Context(), ctxSessionKey{}, sess)
		h(w, r.WithContext(ctx))
	})
}

func (s *Server) adminWrap(h http.Handler) http.Handler {
	if !s.cfg.authEnabled() {
		// No auth configured -> everyone is admin (demo/localhost mode).
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, ok := s.authenticate(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		if sess.Role != "admin" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden: admin required"})
			return
		}
		ctx := context.WithValue(r.Context(), ctxSessionKey{}, sess)
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) authWrap(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, ok := s.authenticate(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		ctx := context.WithValue(r.Context(), ctxSessionKey{}, sess)
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) method(m string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != m {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		h(w, r)
	}
}

func (s *Server) authenticate(r *http.Request) (auth.Session, bool) {
	token := sessionToken(r)
	if token == "" {
		return auth.Session{}, false
	}
	return s.auth.Validate(token)
}

// sessionToken extracts the bearer token from the Authorization header or the
// session cookie.
func sessionToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	if c, err := r.Cookie("fanctrl_session"); err == nil {
		return c.Value
	}
	return ""
}

// ctxSessionKey is a context key for session state.
type ctxSessionKey struct{}

// sessionFrom returns the session if present.
func sessionFrom(ctx context.Context) (auth.Session, bool) {
	s, ok := ctx.Value(ctxSessionKey{}).(auth.Session)
	return s, ok
}

// ---- handlers ----

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	token, u, err := s.auth.Authenticate(req.Username, req.Password)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "fanctrl_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"token": token,
		"user":  map[string]string{"name": u.Name, "role": u.Role},
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if t := sessionToken(r); t != "" {
		if sess, ok := s.auth.Validate(t); ok {
			_ = sess
			s.auth.Revoke(t)
		}
	}
	http.SetCookie(w, &http.Cookie{Name: "fanctrl_session", Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

// handleMe restores an existing session (used on page load so a refresh keeps
// the user signed in). 401 when not logged in.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFrom(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": sess.User, "role": sess.Role})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.p.Snapshot())
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	_ = r
	// Return the last N snapshots (bounded) as a time-series-friendly array.
	recent := s.p.HistoryRecent(300)
	if recent == nil {
		recent = []metrics.Snapshot{}
	}
	writeJSON(w, http.StatusOK, recent)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	_ = r
	st := s.ctrl.Status()
	// Enrich with feature sources + widget visibility from the live config so
	// the SPA can hide widgets whose data source is disabled.
	out := map[string]any{
		"read_only":        st.ReadOnly,
		"dry_run":          st.DryRun,
		"monitor":          st.Monitor,
		"governor_tripped": st.GovernorTripped,
		"governor_reason":  st.GovernorReason,
		"capabilities":     st.Capabilities,
		"thresholds":       st.Thresholds,
	}
	if s.settings != nil {
		cfg := s.settings.Current()
		if cfg.Provider == "mock" {
			// The mock emulates every source: show the full dashboard.
			out["sources"] = map[string]bool{"bmc": true, "gpu": true, "vast": true, "docker": true}
		} else {
			out["sources"] = map[string]bool{
				"bmc":    cfg.BMC.URL != "",
				"gpu":    cfg.GPU.Enabled,
				"vast":   cfg.Vast.Enabled,
				"docker": cfg.Docker.Enabled,
			}
		}
		layout := cfg.Layout.Widgets
		if len(layout) == 0 {
			layout = config.DefaultLayout()
		}
		widgets := make([]map[string]any, 0, len(layout))
		for _, w := range layout {
			widgets = append(widgets, map[string]any{"type": w.Type, "show": w.Show})
		}
		out["widgets"] = widgets
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	_ = r
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    s.p.LastError() == nil,
		"error": errString(s.p.LastError()),
	})
}

// handleMeta is a public, unauthenticated endpoint describing the server's
// security posture. The SPA uses auth_enabled to decide whether to render the
// login screen or go straight to the dashboard with an anonymous admin role.
func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	_ = r
	writeJSON(w, http.StatusOK, map[string]any{
		"auth_enabled": s.cfg.AuthEnabled,
		"version":      "0.1.0",
	})
}

func (s *Server) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	d := s.p.Discovery(r.Context())
	if d == nil {
		d = []metrics.Discovery{}
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleProfiles(w http.ResponseWriter, r *http.Request) {
	profs, err := s.ctrl.ListProfiles(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	// Never send JSON null for an empty list — the SPA expects an array.
	if profs == nil {
		profs = []metrics.FanProfile{}
	}
	writeJSON(w, http.StatusOK, profs)
}

func (s *Server) handleActiveProfile(w http.ResponseWriter, r *http.Request) {
	st, err := s.ctrl.ActiveProfile(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// handleSetMode switches between Monitor (display-only) and Control modes.
// Admin-only; the control service enforces Monitor server-side.
func (s *Server) handleSetMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Monitor *bool `json:"monitor"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Monitor == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "monitor (bool) required"})
		return
	}
	on := s.ctrl.SetMonitor(*req.Monitor)
	writeJSON(w, http.StatusOK, map[string]any{"monitor": on})
}

func (s *Server) handleSetFanMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Mode == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "mode required (Auto|Full|Half)"})
		return
	}
	sess, _ := sessionFrom(r.Context())
	if err := s.ctrl.SetFanMode(r.Context(), sess.User, req.Mode); err != nil {
		writeJSON(w, http.StatusFailedDependency, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (s *Server) handleSetDuty(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FanID int     `json:"fan"`
		Duty  float64 `json:"duty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	sess, _ := sessionFrom(r.Context())
	if err := s.ctrl.SetFanDuty(r.Context(), sess.User, req.FanID, req.Duty); err != nil {
		writeJSON(w, http.StatusFailedDependency, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (s *Server) handleSetGPUFan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GPU int     `json:"gpu"`
		Pct float64 `json:"pct"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	sess, _ := sessionFrom(r.Context())
	if err := s.ctrl.SetGPUFan(r.Context(), sess.User, req.GPU, req.Pct); err != nil {
		writeJSON(w, http.StatusFailedDependency, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

// handleSetGPUPower sets a GPU power limit (watts). Admin-only; gated by the
// control service (monitor/read-only/dry-run) like every other write.
func (s *Server) handleSetGPUPower(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GPU   int     `json:"gpu"`
		Watts float64 `json:"watts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Watts <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "gpu and watts required"})
		return
	}
	sess, _ := sessionFrom(r.Context())
	if err := s.ctrl.SetGPUPowerLimit(r.Context(), sess.User, req.GPU, req.Watts); err != nil {
		writeJSON(w, http.StatusFailedDependency, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	_ = r
	a := s.ctrl.Audit()
	if a == nil {
		a = []control.AuditEntry{}
	}
	writeJSON(w, http.StatusOK, a)
}

// handleStream pushes snapshots over SSE at the poll interval.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}
	setCORS(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	ctx := r.Context()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			data, _ := json.Marshal(s.p.Snapshot())
			_, _ = w.Write([]byte("data: " + string(data) + "\n\n"))
			fl.Flush()
		}
	}
}

// spa serves the embedded SPA, falling back to index.html for client routes.
func (s *Server) spa() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if info, err := fs.Stat(s.assets, p); err == nil && !info.IsDir() {
			data, err := fs.ReadFile(s.assets, p)
			if err == nil {
				w.Header().Set("Content-Type", contentType(p))
				_, _ = w.Write(data)
				return
			}
		}
		// Fallback to index.html (SPA client routing).
		data, err := fs.ReadFile(s.assets, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	})
}

func contentType(p string) string {
	switch path.Ext(p) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".js":
		return "application/javascript"
	case ".css":
		return "text/css"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".ico":
		return "image/x-icon"
	case ".json":
		return "application/json"
	case ".woff2":
		return "font/woff2"
	case ".map":
		return "application/json"
	default:
		return "application/octet-stream"
	}
}

// ---- helpers ----

func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

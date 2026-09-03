package server

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/hedchr/fanctrl/internal/config"
	"github.com/hedchr/fanctrl/internal/control"
	"github.com/hedchr/fanctrl/internal/metrics"
	"github.com/hedchr/fanctrl/internal/poller"
)

// fakeAssets returns an fs.FS with a minimal index.html + a js asset, so the
// spa handler can be tested without the real build output.
func fakeAssets() fs.FS {
	return fstest.MapFS{
		"index.html":                 &fstest.MapFile{Data: []byte("<!doctype html><html><head></head><body><div id=\"root\"></div></body></html>")},
		"assets/index-DAqezshv.js":   &fstest.MapFile{Data: []byte("console.log('hi')")},
		"assets/index-gKEx9fKO.css":  &fstest.MapFile{Data: []byte("body{}")},
	}
}

func TestSpaServesHtmlNotDownload(t *testing.T) {
	s := &Server{mux: http.NewServeMux(), assets: fakeAssets()}
	s.mux.Handle("/", s.spa())

	cases := []struct {
		path     string
		wantType string
	}{
		{"/", "text/html; charset=utf-8"},
		{"/index.html", "text/html; charset=utf-8"},
		{"/assets/index-DAqezshv.js", "application/javascript"},
		{"/assets/index-gKEx9fKO.css", "text/css"},
		{"/some-client-route", "text/html; charset=utf-8"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rr := httptest.NewRecorder()
		s.mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", tc.path, rr.Code)
			continue
		}
		ct := rr.Header().Get("Content-Type")
		if ct != tc.wantType {
			t.Errorf("GET %s content-type = %q, want %q", tc.path, ct, tc.wantType)
		}
	}
}

func TestHandleMeta(t *testing.T) {
	s := &Server{mux: http.NewServeMux(), assets: fakeAssets(), cfg: &serverConfig{}}
	s.mux.HandleFunc("/api/meta", s.handleMeta)

	// auth disabled
	s.SetAuthEnabled(false)
	req := httptest.NewRequest(http.MethodGet, "/api/meta", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)
	body := rr.Body.String()
	if rr.Code != http.StatusOK || !strings.Contains(body, `"auth_enabled":false`) {
		t.Errorf("meta (auth off) = %d %s, want 200 auth_enabled:false", rr.Code, body)
	}

	// auth enabled
	s.SetAuthEnabled(true)
	req = httptest.NewRequest(http.MethodGet, "/api/meta", nil)
	rr = httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)
	body = rr.Body.String()
	if !strings.Contains(body, `"auth_enabled":true`) {
		t.Errorf("meta (auth on) = %d %s, want auth_enabled:true", rr.Code, body)
	}
}

// nilCtrl is a minimal control service stub that returns nil profiles, which
// must be serialized as [] (never JSON null) so the SPA doesn't crash.
type nilCtrl struct{}

func (nilCtrl) Name() string { return "nil" }
func (nilCtrl) ListFanProfiles(context.Context) ([]metrics.FanProfile, error) { return nil, nil }
func (nilCtrl) ActiveFanProfile(context.Context) (metrics.FanProfileState, error) {
	return metrics.FanProfileState{}, nil
}
func (nilCtrl) SetFanMode(context.Context, string) error { return nil }
func (nilCtrl) SetFanDuty(context.Context, int, float64) error { return nil }
func (nilCtrl) SetGPUFan(context.Context, int, float64) error { return nil }
func (nilCtrl) Capabilities() metrics.Capabilities             { return metrics.Capabilities{} }

// handleProfiles is reached via a real Server; wire a server with the stub ctrl.
func TestHandleProfilesNeverNull(t *testing.T) {
	// A poller is needed for the control service; its provider never runs.
	p := poller.New(nil, config.Thresholds{}, poller.NewRing(10), nil)
	s := &Server{
		mux:  http.NewServeMux(),
		ctrl: control.New(nilCtrl{}, p, control.Options{DryRun: true}, nil),
	}
	s.mux.HandleFunc("/api/fan/profiles", s.handleProfiles)
	req := httptest.NewRequest(http.MethodGet, "/api/fan/profiles", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)
	body := rr.Body.String()
	if rr.Code != http.StatusOK {
		t.Fatalf("handleProfiles = %d, want 200", rr.Code)
	}
	if !strings.Contains(body, "[") || strings.Contains(body, "null") {
		t.Errorf("handleProfiles body should be an empty array, got: %s", body)
	}
}

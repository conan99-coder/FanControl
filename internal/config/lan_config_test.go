package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidateLanConfig confirms the shipped open-access config
// (deploy/fanctrl.lan.yaml) parses + validates: 0.0.0.0 bind, writes enabled,
// no auth, with the EXPLICIT allow_unauthenticated_writes opt-out.
func TestValidateLanConfig(t *testing.T) {
	root, err := findRepoRoot()
	if err != nil {
		t.Skipf("repo root: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "deploy", "fanctrl.lan.yaml"))
	if err != nil {
		t.Fatalf("read lan config: %v", err)
	}
	dir := t.TempDir()
	pw := filepath.Join(dir, "bmc_pw")
	_ = os.WriteFile(pw, []byte("x"), 0600)
	patched := strings.ReplaceAll(string(raw), "/etc/fanctrl/bmc_password", filepath.ToSlash(pw))
	path := filepath.Join(dir, "config.yaml")
	_ = os.WriteFile(path, []byte(patched), 0600)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(lan config): %v", err)
	}
	if cfg.DryRun {
		t.Error("lan config should have dry_run=false (writes enabled)")
	}
	if cfg.Auth.Enabled {
		t.Error("lan config should have auth disabled (open access per owner)")
	}
	if !cfg.Auth.AllowUnauthenticatedWrites {
		t.Error("lan config must set allow_unauthenticated_writes for 0.0.0.0 + writes + no auth")
	}
	if len(cfg.Auth.Users) != 0 {
		t.Errorf("no users expected, got %d", len(cfg.Auth.Users))
	}
	if cfg.Listen != "0.0.0.0:8080" {
		t.Errorf("listen=%q, want 0.0.0.0:8080", cfg.Listen)
	}
}

// TestUnauthenticatedWritesStillRefusedWithoutOptOut confirms the safety default
// is intact: 0.0.0.0 + writes + no auth + NO override flag must fail.
func TestUnauthenticatedWritesStillRefusedWithoutOptOut(t *testing.T) {
	c := Default()
	c.Provider = "real"
	c.Auth.Enabled = false
	c.DryRun = false
	c.Listen = "0.0.0.0:8080"
	if err := c.Validate(); err == nil {
		t.Fatal("expected refusal without allow_unauthenticated_writes")
	}
	// With the explicit opt-out it is permitted.
	c.Auth.AllowUnauthenticatedWrites = true
	if err := c.Validate(); err != nil {
		t.Fatalf("expected validation to pass with explicit opt-out, got: %v", err)
	}
}

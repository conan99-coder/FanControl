package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidateLiveConfig loads the shipped writes-enabled localhost config
// (deploy/fanctrl.live.yaml): dry_run false, localhost bind, auth off (valid
// because the bind is localhost), real provider, no users.
func TestValidateLiveConfig(t *testing.T) {
	// Locate the repo root: walk up from the test file to find go.mod.
	root, err := findRepoRoot()
	if err != nil {
		t.Skipf("repo root not found: %v", err)
	}
	live := filepath.Join(root, "deploy", "fanctrl.live.yaml")
	raw, err := os.ReadFile(live)
	if err != nil {
		t.Fatalf("read live config: %v", err)
	}

	// Temporarily point password_path at an existing temp file so the loader's
	// path-presence checks don't fail (we only validate structure/safety here).
	tmpDir := t.TempDir()
	pwFile := filepath.Join(tmpDir, "bmc_pw")
	if err := os.WriteFile(pwFile, []byte("placeholder"), 0600); err != nil {
		t.Fatal(err)
	}
	patched := strings.ReplaceAll(string(raw), "/etc/fanctrl/bmc_password", filepath.ToSlash(pwFile))
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(patched), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load(live config) failed: %v", err)
	}
	if cfg.DryRun {
		t.Error("live config must have dry_run=false (writes enabled)")
	}
	if cfg.ReadOnly {
		t.Error("read_only should be false")
	}
	if !strings.HasPrefix(cfg.Listen, "127.0.0.1") {
		t.Errorf("listen=%q, want localhost (write-capable auth-off requires it)", cfg.Listen)
	}
	if cfg.Provider != "real" {
		t.Errorf("provider=%q, want real", cfg.Provider)
	}
	if cfg.BMC.URL == "" || cfg.BMC.PasswordPath == "" {
		t.Error("bmc.url and bmc.password_path must be set")
	}
	if cfg.Auth.Enabled {
		t.Errorf("auth.enabled=%v, want false (localhost-only; reverse proxy)", cfg.Auth.Enabled)
	}
	if len(cfg.Auth.Users) != 0 {
		t.Errorf("no users should be configured (skipped), got %d", len(cfg.Auth.Users))
	}
}

// findRepoRoot walks up from the current working dir to the dir containing go.mod.
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}

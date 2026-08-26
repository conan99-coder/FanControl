package config

import (
	"testing"
)

func TestDefaults(t *testing.T) {
	c := Default()
	if c.Listen != "0.0.0.0:8080" {
		t.Errorf("listen = %q", c.Listen)
	}
	if !c.DryRun {
		t.Error("dry_run should default to true (safety)")
	}
	if c.Thresholds.GPUTempHard <= c.Thresholds.GPUTempWarn {
		t.Error("gpu hard should exceed warn")
	}
	if len(c.Layout.Widgets) == 0 {
		t.Error("default layout should have widgets")
	}
}

func TestValidateSafety(t *testing.T) {
	// Exposing non-localhost with auth disabled, write-capable, and real provider
	// must fail.
	c := Default()
	c.Provider = "real"
	c.Auth.Enabled = false
	c.DryRun = false // write-capable
	c.Listen = "0.0.0.0:8080"
	if err := c.Validate(); err == nil {
		t.Fatal("expected refusal to bind non-localhost with auth disabled and writes enabled")
	}
}

func TestValidateDryRunAllowsLAN(t *testing.T) {
	// In dry-run the control service no-ops writes, so a non-localhost bind with
	// auth disabled is permitted (read-only on the LAN).
	c := Default()
	c.Provider = "real"
	c.Auth.Enabled = false
	c.DryRun = true
	c.Listen = "0.0.0.0:8080"
	if err := c.Validate(); err != nil {
		t.Fatalf("expected dry-run non-localhost bind to be allowed, got: %v", err)
	}
}

func TestValidateAuthNoUsers(t *testing.T) {
	c := Default()
	c.Auth.Enabled = true
	c.Auth.Users = nil
	if err := c.Validate(); err == nil {
		t.Fatal("expected error when auth enabled with no users")
	}
}

func TestValidateThresholds(t *testing.T) {
	c := Default()
	c.Thresholds.GPUTempWarn = 90
	c.Thresholds.GPUTempHard = 85
	if err := c.Validate(); err == nil {
		t.Fatal("expected error when hard < warn")
	}
}

func TestResolveSecretEnv(t *testing.T) {
	t.Setenv("FANCTRL_TEST_SECRET", "s3cret")
	s, err := ResolveSecret("env:FANCTRL_TEST_SECRET")
	if err != nil {
		t.Fatalf("ResolveSecret env error: %v", err)
	}
	if s != "s3cret" {
		t.Errorf("got %q, want %q", s, "s3cret")
	}
}

func TestResolveSecretFile(t *testing.T) {
	// nonexistent file -> error (but not panic)
	_, err := ResolveSecret("/no/such/file")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

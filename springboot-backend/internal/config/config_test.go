package config

import (
	"strings"
	"testing"
)

func TestLoadDefaultsToLoopbackAndRequiresMetricsToken(t *testing.T) {
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("JWT_SECRET", strings.Repeat("j", 32))
	t.Setenv("BOOTSTRAP_PASSWORD", "")
	t.Setenv("METRICS_TOKEN", "")
	if _, err := Load(); err == nil {
		t.Fatal("missing metrics token was accepted")
	}
	t.Setenv("METRICS_TOKEN", strings.Repeat("m", 32))
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Address != "127.0.0.1:6365" {
		t.Fatalf("default address=%q", cfg.Address)
	}
}

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileReturnsEmpty(t *testing.T) {
	t.Setenv("INARI_CONFIG_DIR", t.TempDir())
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.CurrentContext != "" || len(cfg.Contexts) != 0 {
		t.Fatalf("expected empty config, got %+v", cfg)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("INARI_CONFIG_DIR", dir)
	cfg := &Config{}
	cfg.SetContext("dev", Context{Server: "https://api.example.dev", Tenant: "acme"})
	cfg.CurrentContext = "dev"
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, ConfigFileName))
	if err != nil {
		t.Fatalf("config file not written: %v", err)
	}
	if perm := info.Mode().Perm(); perm != fileMode {
		t.Fatalf("config file mode = %o, want %o", perm, fileMode)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	name, ctx, err := got.Current()
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if name != "dev" || ctx.Server != "https://api.example.dev" || ctx.Tenant != "acme" {
		t.Fatalf("round trip mismatch: %q %+v", name, ctx)
	}
}

func TestCurrentWithoutContextErrors(t *testing.T) {
	cfg := &Config{}
	if _, _, err := cfg.Current(); err == nil {
		t.Fatal("expected error when no current context set")
	}
	cfg.CurrentContext = "ghost"
	if _, _, err := cfg.Current(); err == nil {
		t.Fatal("expected error for unknown current context")
	}
}

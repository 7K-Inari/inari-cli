package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/7K-Inari/inari-cli/internal/config"
)

func TestRootHelpRenders(t *testing.T) {
	out := &bytes.Buffer{}
	root := NewRootCmd("1.2.3", "abc", "today", out, &bytes.Buffer{})
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), "inari is the command-line interface") {
		t.Errorf("root help missing long description: %q", out.String())
	}
}

func TestVersionCommand(t *testing.T) {
	out := &bytes.Buffer{}
	root := NewRootCmd("1.2.3", "deadbeef", "2026-08-20", out, &bytes.Buffer{})
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "1.2.3") || !strings.Contains(got, "deadbeef") {
		t.Fatalf("version output = %q", got)
	}
}

func TestResolveContextHonorsServerEnv(t *testing.T) {
	t.Setenv("INARI_CONFIG_DIR", t.TempDir())
	cfg := &config.Config{}
	cfg.SetContext("default", config.Context{Server: "https://from-config.example"})
	cfg.CurrentContext = "default"
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INARI_SERVER", "https://from-env.example")

	opts := &GlobalOptions{}
	_, ctx, err := opts.resolveContext()
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Server != "https://from-env.example" {
		t.Errorf("server = %q, want env override", ctx.Server)
	}

	opts = &GlobalOptions{ServerFlag: "https://from-flag.example"}
	_, ctx, err = opts.resolveContext()
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Server != "https://from-flag.example" {
		t.Errorf("server = %q, want flag to beat env", ctx.Server)
	}
}

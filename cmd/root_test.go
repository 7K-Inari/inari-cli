package cmd

import (
	"bytes"
	"strings"
	"testing"
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

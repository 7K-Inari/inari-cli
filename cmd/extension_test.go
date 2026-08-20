package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtensionInitBackend(t *testing.T) {
	dir := t.TempDir()
	out := &bytes.Buffer{}
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"extension", "init", "my-plugin", "--type", "backend", "--dir", dir, "--module", "github.com/acme/my-plugin"})
	if err := root.Execute(); err != nil {
		t.Fatalf("extension init error = %v", err)
	}

	for _, f := range []string{"go.mod", "main.go", "README.md"} {
		if _, err := os.Stat(filepath.Join(dir, "my-plugin", f)); err != nil {
			t.Errorf("expected %s: %v", f, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "my-plugin", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "module github.com/acme/my-plugin") {
		t.Errorf("go.mod not templated: %s", data)
	}

	root = NewRootCmd("dev", "none", "now", &bytes.Buffer{}, &bytes.Buffer{})
	root.SetArgs([]string{"extension", "init", "my-plugin", "--type", "backend", "--dir", dir})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error when target directory exists")
	}
}

func TestExtensionInitUIPascalCase(t *testing.T) {
	dir := t.TempDir()
	out := &bytes.Buffer{}
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"extension", "init", "status-cards", "--type", "ui", "--dir", dir})
	if err := root.Execute(); err != nil {
		t.Fatalf("extension init error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "status-cards", "index.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "StatusCardsPage") {
		t.Errorf("index.tsx not templated with PascalCase: %s", data)
	}
}

func TestExtensionInitValidation(t *testing.T) {
	dir := t.TempDir()
	out := &bytes.Buffer{}
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"extension", "init", "Bad Name", "--type", "backend", "--dir", dir})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "invalid name") {
		t.Fatalf("expected name validation error, got %v", err)
	}

	root = NewRootCmd("dev", "none", "now", &bytes.Buffer{}, &bytes.Buffer{})
	root.SetArgs([]string{"extension", "init", "ok-name", "--type", "banana", "--dir", dir})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "--type") {
		t.Fatalf("expected type validation error, got %v", err)
	}
}

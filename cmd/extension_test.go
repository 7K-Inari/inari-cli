package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	manifest, err := os.ReadFile(filepath.Join(dir, "my-plugin", "extension.yaml"))
	if err != nil || !strings.Contains(string(manifest), "backend: [inari.plugin.v1]") {
		t.Fatalf("extension.yaml contract = %s, %v", manifest, err)
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
	manifest, err := os.ReadFile(filepath.Join(dir, "status-cards", "extension.yaml"))
	if err != nil || !strings.Contains(string(manifest), "hostApi: inari.ui.v1") || !strings.Contains(string(manifest), `sdk: ">=0.1.8 <2.0.0"`) {
		t.Fatalf("UI extension contract = %s, %v", manifest, err)
	}
}

func TestExtensionInitContractWarnings(t *testing.T) {
	for _, tc := range []struct {
		name        string
		kind        string
		response    string
		status      int
		wantWarning string
		skipWarning bool
	}{
		{"backend compatible", "backend", `{"pluginContracts":["inari.plugin.v1"],"uiHostApis":["inari.ui.v1"],"serverVersion":"4.1.0"}`, http.StatusOK, "", true},
		{"backend incompatible", "backend", `{"pluginContracts":["inari.plugin.v2"],"uiHostApis":["inari.ui.v1"],"serverVersion":"4.1.0"}`, http.StatusOK, "inari.plugin.v1", false},
		{"UI incompatible", "ui", `{"pluginContracts":["inari.plugin.v1"],"uiHostApis":["inari.ui.v2"],"serverVersion":"4.1.0"}`, http.StatusOK, "inari.ui.v1", false},
		{"unavailable", "backend", `{"detail":"missing"}`, http.StatusNotFound, "unavailable", false},
		{"malformed", "ui", `{not json`, http.StatusOK, "unavailable", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" {
					t.Error("contract check unexpectedly used auth")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.response))
			}))
			defer srv.Close()
			t.Setenv("INARI_SERVER", srv.URL)
			dir := t.TempDir()
			out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
			root := NewRootCmd("dev", "none", "now", out, errOut)
			root.SetArgs([]string{"extension", "init", "contract-test", "--type", tc.kind, "--dir", dir})
			if err := root.Execute(); err != nil {
				t.Fatalf("scaffold failed: %v", err)
			}
			warning := errOut.String()
			if tc.skipWarning && warning != "" {
				t.Fatalf("unexpected warning: %q", warning)
			}
			if !tc.skipWarning && !strings.Contains(warning, tc.wantWarning) {
				t.Fatalf("warning = %q, want substring %q", warning, tc.wantWarning)
			}
			if _, err := os.Stat(filepath.Join(dir, "contract-test", "extension.yaml")); err != nil {
				t.Fatalf("extension.yaml missing: %v", err)
			}
		})
	}
}

func TestExtensionInitContractNetworkFailureAndNoServerAreNonFatal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server := srv.URL
	srv.Close()
	t.Setenv("INARI_SERVER", server)
	errOut := &bytes.Buffer{}
	root := NewRootCmd("dev", "none", "now", &bytes.Buffer{}, errOut)
	root.SetArgs([]string{"extension", "init", "offline-test", "--type", "backend", "--dir", t.TempDir()})
	if err := root.Execute(); err != nil || !strings.Contains(errOut.String(), "unavailable") {
		t.Fatalf("network failure = %v, warning %q", err, errOut.String())
	}

	t.Setenv("INARI_SERVER", "")
	t.Setenv("INARI_CONFIG_DIR", t.TempDir())
	errOut = &bytes.Buffer{}
	root = NewRootCmd("dev", "none", "now", &bytes.Buffer{}, errOut)
	root.SetArgs([]string{"extension", "init", "no-server-test", "--type", "ui", "--dir", t.TempDir()})
	if err := root.Execute(); err != nil || errOut.String() != "" {
		t.Fatalf("no-server scaffold = %v, warning %q", err, errOut.String())
	}
}

func TestExtensionInitContractTimeoutIsNonFatal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(3 * time.Second)
		_, _ = w.Write([]byte(`{"pluginContracts":["inari.plugin.v1"],"uiHostApis":["inari.ui.v1"],"serverVersion":"dev"}`))
	}))
	defer srv.Close()
	t.Setenv("INARI_SERVER", srv.URL)
	root := NewRootCmd("dev", "none", "now", &bytes.Buffer{}, &bytes.Buffer{})
	root.SetArgs([]string{"extension", "init", "timeout-test", "--type", "backend", "--dir", t.TempDir()})
	start := time.Now()
	if err := root.Execute(); err != nil {
		t.Fatalf("timeout blocked scaffold: %v", err)
	}
	if time.Since(start) >= 3*time.Second {
		t.Fatal("metadata check was not bounded below the server delay")
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

package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHirobaInitFullAppOffline(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("INARI_SERVER", "")
	t.Setenv("INARI_CONFIG_DIR", filepath.Join(dir, "empty-config"))
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	root := NewRootCmd("dev", "none", "now", out, errOut)
	root.SetArgs([]string{"hiroba", "init", "community-app", "--type", "app", "--dir", dir, "--with-plugin", "--with-ui", "--with-chart", "--with-observability", "--with-runtime-smoke"})
	if err := root.Execute(); err != nil {
		t.Fatalf("hiroba init error = %v", err)
	}
	target := filepath.Join(dir, "community-app")
	for _, name := range []string{
		".hiroba.yaml", "README.md", "CONTRIBUTING.md", "AGENTS.md", "renovate.json",
		"packages/community-app/package.yaml", "packages/community-app/values.schema.json",
		"templates/community-app/template.yaml", "templates/community-app/skeleton/README.md",
		"extension/extension.go", "ui/package.json", "charts/community-app/Chart.yaml",
		"observability/observability.yaml", "observability/dashboards/overview.json",
		"observability/rules/app.yaml", "observability/tests/rules/app.test.yaml",
		"packages/observability-pack/rgd.yaml", "tests/smoke/suite.yaml",
		"tests/smoke/workflowtemplate.yaml", ".github/workflows/runtime-smoke.yml",
	} {
		if _, err := os.Stat(filepath.Join(target, name)); err != nil {
			t.Errorf("expected %s: %v", name, err)
		}
	}
	caller, err := os.ReadFile(filepath.Join(target, ".github/workflows/validate.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(caller), "@f572c5a244e27e4a77589f2730066749c7b51c6b # v1.0.0") {
		t.Errorf("workflow pin is not the immutable production release: %s", caller)
	}
	if strings.Contains(out.String(), "warning") || strings.Contains(errOut.String(), "warning") {
		t.Errorf("offline scaffold unexpectedly warned: stdout=%q stderr=%q", out, errOut)
	}
	if !strings.Contains(out.String(), "Scaffolded Hiroba app") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestHirobaInitChartlessComponents(t *testing.T) {
	dir := t.TempDir()
	root := NewRootCmd("dev", "none", "now", &bytes.Buffer{}, &bytes.Buffer{})
	root.SetArgs([]string{"hiroba", "init", "chartless", "--type", "template", "--dir", dir, "--with-observability", "--with-runtime-smoke"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "chartless")
	if _, err := os.Stat(filepath.Join(target, "charts")); !os.IsNotExist(err) {
		t.Errorf("chartless scaffold created charts/: %v", err)
	}
	for _, name := range []string{"packages/observability-pack/rgd.yaml", "tests/smoke/fixtures/app/deployment.yaml", "tests/smoke/fixtures/app/service.yaml"} {
		if _, err := os.Stat(filepath.Join(target, name)); err != nil {
			t.Errorf("expected %s: %v", name, err)
		}
	}
}

func TestHirobaInitContractWarningDoesNotBlock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/meta/contracts" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("unexpected authorization header")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"pluginContracts": []string{"inari.plugin.v2"},
			"uiHostApis":      []string{"inari.ui.v1"},
			"serverVersion":   "2.0.0",
		})
	}))
	defer server.Close()
	dir := t.TempDir()
	errOut := &bytes.Buffer{}
	root := NewRootCmd("dev", "none", "now", &bytes.Buffer{}, errOut)
	root.SetArgs([]string{"hiroba", "init", "warning-app", "--dir", dir, "--with-plugin", "--server", server.URL})
	if err := root.Execute(); err != nil {
		t.Fatalf("incompatible server blocked scaffold: %v", err)
	}
	if !strings.Contains(errOut.String(), "inari.plugin.v1") || !strings.Contains(errOut.String(), "continuing") {
		t.Fatalf("expected incompatibility warning, got %q", errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "warning-app", "extension.yaml")); err != nil {
		t.Fatalf("scaffold missing after warning: %v", err)
	}
}

func TestHirobaInitValidation(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "name", args: []string{"hiroba", "init", "Bad-Name"}, want: "invalid name"},
		{name: "type", args: []string{"hiroba", "init", "valid-name", "--type", "banana"}, want: "invalid type"},
		{name: "module", args: []string{"hiroba", "init", "valid-name", "--module", "bad module"}, want: "invalid Go module"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := NewRootCmd("dev", "none", "now", &bytes.Buffer{}, &bytes.Buffer{})
			root.SetArgs(test.args)
			if err := root.Execute(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q error, got %v", test.want, err)
			}
		})
	}

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "existing"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := NewRootCmd("dev", "none", "now", &bytes.Buffer{}, &bytes.Buffer{})
	root.SetArgs([]string{"hiroba", "init", "existing", "--dir", dir})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected existing-directory error, got %v", err)
	}
}

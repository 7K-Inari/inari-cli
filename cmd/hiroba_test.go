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

	"github.com/7K-Inari/inari-cli/internal/config"
)

func TestHirobaInitFullAppOffline(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("INARI_SERVER", "")
	t.Setenv("INARI_CONFIG_DIR", filepath.Join(dir, "empty-config"))
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	root := NewRootCmd("dev", "none", "now", out, errOut)
	root.SetArgs([]string{"hiroba", "init", "community-app", "--type", "app", "--archetype", "non-stack", "--dir", dir, "--with-plugin", "--with-ui", "--with-chart", "--with-observability", "--with-runtime-smoke"})
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
	if !strings.Contains(string(caller), "@522c3530c94336249928f5a65a5a9f98c46bf540 # v1.1.0") {
		t.Errorf("workflow pin is not the immutable production release: %s", caller)
	}
	if strings.Contains(out.String(), "warning") || strings.Contains(errOut.String(), "warning") {
		t.Errorf("offline scaffold unexpectedly warned: stdout=%q stderr=%q", out, errOut)
	}
	if !strings.Contains(out.String(), "Scaffolded Hiroba app") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestHirobaInitRequiresArchetype(t *testing.T) {
	root := NewRootCmd("dev", "none", "now", &bytes.Buffer{}, &bytes.Buffer{})
	root.SetArgs([]string{"hiroba", "init", "classified", "--dir", t.TempDir()})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "archetype") {
		t.Fatalf("missing archetype error = %v, want explicit classification", err)
	}
}

func TestHirobaInitStackArchetypeIncludesLifecycle(t *testing.T) {
	dir := t.TempDir()
	root := NewRootCmd("dev", "none", "now", &bytes.Buffer{}, &bytes.Buffer{})
	root.SetArgs([]string{"hiroba", "init", "stack-app", "--type", "app", "--archetype", "stack", "--with-chart", "--dir", dir})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "stack-app")
	for _, name := range []string{
		"tests/stack/suite.yaml", "tests/stack/install.sh", "tests/stack/upgrade.sh",
		".github/workflows/stack-test.yml",
	} {
		if _, err := os.Stat(filepath.Join(target, name)); err != nil {
			t.Errorf("expected stack lifecycle %s: %v", name, err)
		}
	}
	config, err := os.ReadFile(filepath.Join(target, ".hiroba.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"schema: inari.hiroba.app.v2", "archetype: stack", "stackTest:"} {
		if !strings.Contains(string(config), expected) {
			t.Errorf("stack config missing %q", expected)
		}
	}
	if _, err := os.Stat(filepath.Join(target, "tests/smoke")); !os.IsNotExist(err) {
		t.Errorf("stack archetype silently added runtime smoke: %v", err)
	}
}

func TestHirobaInitChartlessComponents(t *testing.T) {
	dir := t.TempDir()
	root := NewRootCmd("dev", "none", "now", &bytes.Buffer{}, &bytes.Buffer{})
	root.SetArgs([]string{"hiroba", "init", "chartless", "--type", "template", "--archetype", "non-stack", "--dir", dir, "--with-observability", "--with-runtime-smoke"})
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
	root.SetArgs([]string{"hiroba", "init", "warning-app", "--dir", dir, "--archetype", "non-stack", "--with-plugin", "--server", server.URL})
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

func TestEffectiveHirobaServerUsesNamedContext(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("INARI_CONFIG_DIR", configDir)
	t.Setenv("INARI_SERVER", "")
	cfg := &config.Config{CurrentContext: "current"}
	cfg.SetContext("current", config.Context{Server: "https://current.example.com"})
	cfg.SetContext("alternate", config.Context{Server: "https://alternate.example.com"})
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	opts := &GlobalOptions{ContextFlag: "alternate"}
	if got := effectiveHirobaServer(opts); got != "https://alternate.example.com" {
		t.Fatalf("effectiveHirobaServer() = %q, want alternate context server", got)
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
		{name: "empty type", args: []string{"hiroba", "init", "valid-name", "--type", ""}, want: "invalid type"},
		{name: "module", args: []string{"hiroba", "init", "valid-name", "--module", "bad module"}, want: "invalid Go module"},
		{name: "empty module", args: []string{"hiroba", "init", "valid-name", "--module", ""}, want: "invalid Go module"},
		{name: "whitespace module", args: []string{"hiroba", "init", "valid-name", "--module", " \t"}, want: "invalid Go module"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := NewRootCmd("dev", "none", "now", &bytes.Buffer{}, &bytes.Buffer{})
			args := append(append([]string{}, test.args...), "--archetype", "non-stack", "--dir", t.TempDir())
			root.SetArgs(args)
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
	root.SetArgs([]string{"hiroba", "init", "existing", "--archetype", "non-stack", "--dir", dir})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected existing-directory error, got %v", err)
	}
}

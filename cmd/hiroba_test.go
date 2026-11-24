package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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
	if !strings.Contains(string(caller), "@f092eab6bb07de64a2d6f2e35ac4c65fca794f76 # v2.0.3") {
		t.Errorf("workflow pin is not the immutable production release: %s", caller)
	}
	if strings.Contains(out.String(), "warning") || strings.Contains(errOut.String(), "warning") {
		t.Errorf("offline scaffold unexpectedly warned: stdout=%q stderr=%q", out, errOut)
	}
	if !strings.Contains(out.String(), "Scaffolded Hiroba app") {
		t.Errorf("unexpected output: %s", out)
	}
}

func stackScaffoldBindingScript(t *testing.T) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", ".github", "workflows", "stack-scaffold.yml"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(contents), "\n")
	bindingStep, uploadStep := -1, -1
	for index, line := range lines {
		if strings.TrimSpace(line) == "- name: Bind generator and generated fixture identities" {
			bindingStep = index
		}
		if uploadStep == -1 && strings.Contains(line, "- uses: actions/upload-artifact@") {
			uploadStep = index
		}
	}
	if bindingStep == -1 {
		t.Fatal("stack scaffold workflow is missing the generator identity binding step")
	}
	if uploadStep == -1 || bindingStep > uploadStep {
		t.Fatalf("identity binding step must precede artifact upload (binding=%d upload=%d)", bindingStep, uploadStep)
	}
	always, runBlock := false, -1
	for index := bindingStep + 1; index < uploadStep; index++ {
		trimmed := strings.TrimSpace(lines[index])
		if trimmed == "if: always()" {
			always = true
		}
		if trimmed == "run: |" {
			runBlock = index
			break
		}
	}
	if !always {
		t.Fatal("identity binding step must run with if:always()")
	}
	if runBlock == -1 {
		t.Fatal("identity binding step has no inline run block")
	}
	header := strings.Join(lines[bindingStep:runBlock], "\n")
	if !strings.Contains(header, "GENERATOR_SOURCE_SHA: ${{ github.sha }}") {
		t.Fatal("identity binding step must receive the tested generator merge SHA from github.sha")
	}
	runIndent := leadingSpaceCount(lines[runBlock])
	contentIndent := -1
	scriptLines := []string{}
	for index := runBlock + 1; index < uploadStep; index++ {
		line := lines[index]
		if strings.TrimSpace(line) == "" {
			scriptLines = append(scriptLines, "")
			continue
		}
		indent := leadingSpaceCount(line)
		if indent <= runIndent {
			break
		}
		if contentIndent == -1 {
			contentIndent = indent
		}
		if indent < contentIndent {
			t.Fatalf("malformed inline binding script indentation at workflow line %d", index+1)
		}
		scriptLines = append(scriptLines, line[contentIndent:])
	}
	script := strings.Join(scriptLines, "\n")
	if strings.TrimSpace(script) == "" {
		t.Fatal("identity binding inline run block is empty")
	}
	return script + "\n"
}

func leadingSpaceCount(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

func newStackBindingFixture(t *testing.T) (string, string, string) {
	t.Helper()
	runnerTemp := t.TempDir()
	fixture := filepath.Join(runnerTemp, "qa-kro")
	if err := os.MkdirAll(filepath.Join(fixture, "evidence"), 0o755); err != nil {
		t.Fatal(err)
	}
	runStackBindingGit(t, fixture, "init", "-q")
	runStackBindingGit(t, fixture, "config", "user.name", "Generated QA")
	runStackBindingGit(t, fixture, "config", "user.email", "generated-qa@example.invalid")
	if err := os.WriteFile(filepath.Join(fixture, "fixture.txt"), []byte("generated fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runStackBindingGit(t, fixture, "add", "--", "fixture.txt")
	runStackBindingGit(t, fixture, "commit", "-qm", "generated chartless KRO fixture")
	generatedSha := runStackBindingGit(t, fixture, "rev-parse", "HEAD")
	return runnerTemp, filepath.Join(fixture, "evidence", "stack-lifecycle.json"), generatedSha
}

func runStackBindingGit(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func stackBindingEvidence(sourceSha string) map[string]any {
	return map[string]any{
		"schema":             "inari.lane-result.v1",
		"lane":               "stack-lifecycle",
		"status":             "passed",
		"sourceSha":          sourceSha,
		"workflowSha":        strings.Repeat("b", 40),
		"runId":              "123",
		"runAttempt":         "1",
		"covers":             []any{"packages/observability-pack"},
		"phases":             []any{map[string]any{"phase": "install", "converged": true}},
		"clusterCreateCount": 1,
		"clusterDeleteCount": 1,
		"freshState":         true,
		"cleanedUp":          true,
		"functionalE2E":      false,
		"deployedReadiness":  "not-tested",
		"unrelated":          map[string]any{"nested": []any{"preserve", 42}},
	}
}

func writeStackBindingEvidence(t *testing.T, path string, evidence map[string]any) []byte {
	t.Helper()
	contents, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	contents = append(contents, '\n')
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatal(err)
	}
	return contents
}

func readStackBindingEvidence(t *testing.T, path string) map[string]any {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var evidence map[string]any
	if err := json.Unmarshal(contents, &evidence); err != nil {
		t.Fatal(err)
	}
	return evidence
}

func executeStackBindingScript(script, runnerTemp, generatorSha string) ([]byte, error) {
	command := exec.Command("bash", "-c", script)
	command.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"RUNNER_TEMP=" + runnerTemp,
		"GENERATOR_SOURCE_SHA=" + generatorSha,
	}
	return command.CombinedOutput()
}

func assertNoBindingTemporaryFiles(t *testing.T, evidencePath string) {
	t.Helper()
	leftovers, err := filepath.Glob(evidencePath + ".binding.*")
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("identity binding left temporary files: %v", leftovers)
	}
}

func TestStackScaffoldBindingScriptPreservesBothIdentities(t *testing.T) {
	script := stackScaffoldBindingScript(t)
	runnerTemp, evidencePath, generatedSha := newStackBindingFixture(t)
	writeStackBindingEvidence(t, evidencePath, stackBindingEvidence(generatedSha))
	original := readStackBindingEvidence(t, evidencePath)
	if _, exists := original["generatedSourceSha"]; exists {
		t.Fatal("test fixture unexpectedly starts with generatedSourceSha")
	}
	delete(original, "sourceSha")
	generatorSha := strings.Repeat("a", 40)
	output, err := executeStackBindingScript(script, runnerTemp, generatorSha)
	if err != nil {
		t.Fatalf("identity binding failed: %v\n%s", err, output)
	}
	got := readStackBindingEvidence(t, evidencePath)
	if got["sourceSha"] != generatorSha {
		t.Errorf("sourceSha = %v, want generator merge SHA %s", got["sourceSha"], generatorSha)
	}
	if got["generatedSourceSha"] != generatedSha {
		t.Errorf("generatedSourceSha = %v, want generated fixture HEAD %s", got["generatedSourceSha"], generatedSha)
	}
	delete(got, "sourceSha")
	delete(got, "generatedSourceSha")
	if !reflect.DeepEqual(got, original) {
		t.Errorf("identity binding changed unrelated evidence:\n got: %#v\nwant: %#v", got, original)
	}
	assertNoBindingTemporaryFiles(t, evidencePath)
}

func TestStackScaffoldBindingScriptRejectsInvalidEvidence(t *testing.T) {
	script := stackScaffoldBindingScript(t)
	for _, test := range []struct {
		name          string
		generatorSha  string
		writeEvidence bool
		mutate        func(map[string]any)
	}{
		{name: "missing evidence", generatorSha: strings.Repeat("a", 40), mutate: func(map[string]any) {}},
		{name: "missing sourceSha", generatorSha: strings.Repeat("a", 40), writeEvidence: true, mutate: func(evidence map[string]any) { delete(evidence, "sourceSha") }},
		{name: "mismatched original source", generatorSha: strings.Repeat("a", 40), writeEvidence: true, mutate: func(evidence map[string]any) { evidence["sourceSha"] = strings.Repeat("c", 40) }},
		{name: "invalid original source", generatorSha: strings.Repeat("a", 40), writeEvidence: true, mutate: func(evidence map[string]any) { evidence["sourceSha"] = "not-a-sha" }},
		{name: "invalid generator source", generatorSha: "not-a-sha", writeEvidence: true, mutate: func(map[string]any) {}},
	} {
		t.Run(test.name, func(t *testing.T) {
			runnerTemp, evidencePath, generatedSha := newStackBindingFixture(t)
			evidence := stackBindingEvidence(generatedSha)
			test.mutate(evidence)
			var before []byte
			if test.writeEvidence {
				before = writeStackBindingEvidence(t, evidencePath, evidence)
			}
			output, err := executeStackBindingScript(script, runnerTemp, test.generatorSha)
			if err == nil {
				t.Fatalf("invalid evidence unexpectedly succeeded:\n%s", output)
			}
			if test.writeEvidence {
				after, readErr := os.ReadFile(evidencePath)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if !bytes.Equal(after, before) {
					t.Fatalf("failed identity binding rewrote evidence:\n before: %s\n  after: %s", before, after)
				}
			} else if _, statErr := os.Stat(evidencePath); !os.IsNotExist(statErr) {
				t.Fatalf("missing evidence was created by failed binding: %v", statErr)
			}
			assertNoBindingTemporaryFiles(t, evidencePath)
		})
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

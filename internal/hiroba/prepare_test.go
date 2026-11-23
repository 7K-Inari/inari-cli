package hiroba

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExternalMatrixDocumentsExplicitTimeout(t *testing.T) {
	data, err := os.ReadFile("../../CONTRIBUTING.md")
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(data), "-timeout=30m"); count < 2 {
		t.Fatalf("external matrix commands must include an explicit 30m timeout; found %d", count)
	}
}

// TestVerifyWorkflowMatrix checks every generated tree against an external candidate or exact production checkout.
func TestVerifyWorkflowMatrix(t *testing.T) {
	if os.Getenv("HIROBA_VERIFY_MATRIX") != "1" {
		t.Skip("set HIROBA_VERIFY_MATRIX=1 to verify against a workflows checkout")
	}
	workflows := os.Getenv("HIROBA_WORKFLOWS_DIR")
	if workflows == "" {
		t.Fatal("HIROBA_WORKFLOWS_DIR must name the workflows checkout")
	}
	if os.Getenv("HIROBA_REQUIRE_PRODUCTION_PIN") == "1" {
		command := exec.Command("git", "-C", workflows, "rev-parse", "HEAD")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("resolve workflows HEAD: %v\n%s", err, output)
		}
		if got := strings.TrimSpace(string(output)); got != WorkflowSHA {
			t.Fatalf("production verification requires %s, got %s", WorkflowSHA, got)
		}
	}
	python := filepath.Join(workflows, ".venv", "bin", "python")
	for _, kind := range []string{"app", "extension", "catalog", "template"} {
		for _, archetype := range []string{"non-stack", "stack"} {
			for mask := 0; mask < 32; mask++ {
				t.Run(fmt.Sprintf("%s/%s/%02d", kind, archetype, mask), func(t *testing.T) {
					opts := matrixOptions(kind, mask, archetype)
					fixture := filepath.Join(t.TempDir(), opts.Name)
					if err := Scaffold(fixture, opts); err != nil {
						t.Fatal(err)
					}
					if output, err := exec.Command("git", "init", "-q", fixture).CombinedOutput(); err != nil {
						t.Fatalf("initialize fixture: %v\n%s", err, output)
					}
					commands := [][]string{
						{filepath.Join(workflows, "scripts", "check-workflow-pins.py"), "--root", fixture, "--require-rendered"},
						{filepath.Join(workflows, "scripts", "validate-repository.py"), "--root", fixture},
						{filepath.Join(workflows, "scripts", "build-indexes.py"), "--root", fixture, "--check"},
						{filepath.Join(workflows, "scripts", "sync-release-please-config.py"), "--root", fixture, "--check"},
						{filepath.Join(workflows, "scripts", "discover-components.py"), "--root", fixture, "--all"},
						{filepath.Join(workflows, "scripts", "stack-test-config.py"), "--root", fixture},
						{filepath.Join(workflows, "scripts", "prepare-release-plan.py"), "--root", fixture, "--source-sha", strings.Repeat("a", 40), "--rehearsal"},
					}
					if opts.WithObservability {
						commands = append(commands, []string{filepath.Join(workflows, "scripts", "render-observability.py"), "--root", fixture, "--check"})
					}
					for _, args := range commands {
						command := exec.Command(python, args...)
						if output, err := command.CombinedOutput(); err != nil {
							t.Fatalf("%s: %v\n%s", filepath.Base(args[0]), err, output)
						}
					}
				})
			}
		}
	}
}

// TestPrepareCanonicalMatrix regenerates embedded generated outputs with the
// canonical workflows scripts. It is intentionally manual and never runs in CI.
func TestPrepareCanonicalMatrix(t *testing.T) {
	if os.Getenv("HIROBA_PREPARE_MATRIX") != "1" {
		t.Skip("set HIROBA_PREPARE_MATRIX=1 to regenerate canonical matrix outputs")
	}
	workflows := os.Getenv("HIROBA_WORKFLOWS_DIR")
	if workflows == "" {
		t.Fatal("HIROBA_WORKFLOWS_DIR must name the reviewed workflows checkout")
	}
	python := filepath.Join(workflows, ".venv", "bin", "python")
	for _, kind := range []string{"app", "extension", "catalog", "template"} {
		for _, archetype := range []string{"non-stack", "stack"} {
			for mask := 0; mask < 32; mask++ {
				opts := matrixOptions(kind, mask, archetype)
				fixture := filepath.Join(t.TempDir(), opts.Name)
				if err := Scaffold(fixture, opts); err != nil {
					t.Fatal(err)
				}
				for _, script := range []string{"build-indexes.py", "sync-release-please-config.py"} {
					command := exec.Command(python, filepath.Join(workflows, "scripts", script), "--root", fixture)
					if output, err := command.CombinedOutput(); err != nil {
						t.Fatalf("%s mask %d %s: %v\n%s", kind, mask, script, err, output)
					}
				}
				if opts.WithObservability {
					command := exec.Command(python, filepath.Join(workflows, "scripts", "render-observability.py"), "--root", fixture)
					if output, err := command.CombinedOutput(); err != nil {
						t.Fatalf("%s mask %d observability render: %v\n%s", kind, mask, err, output)
					}
					canonicalOutputs := []string{"packages/observability-pack/rgd.yaml"}
					if opts.Type == "app" || opts.Type == "catalog" {
						canonicalOutputs = append(canonicalOutputs, "packages/matrix-app/observability.yaml")
					}
					if opts.WithChart {
						canonicalOutputs = append(canonicalOutputs,
							"charts/matrix-app/templates/observability/dashboards.yaml",
							"charts/matrix-app/templates/observability/rules.yaml",
							"charts/matrix-app/inari/observability.yaml",
						)
					}
					for _, outputPath := range canonicalOutputs {
						data, err := os.ReadFile(filepath.Join(fixture, outputPath))
						if err != nil {
							t.Fatal(err)
						}
						text := strings.ReplaceAll(string(data), opts.Name, "__HIROBA_NAME__")
						text = strings.ReplaceAll(text, pascalName(opts.Name), "__HIROBA_NAME_PASCAL__")
						var templatePath string
						switch outputPath {
						case "packages/observability-pack/rgd.yaml":
							templatePath = "templates/components/observability/pack/rgd.yaml"
						case "packages/matrix-app/observability.yaml":
							templatePath = "templates/components/observability/manifest/observability.yaml"
						default:
							templatePath = "templates/components/observability/chart/" + strings.TrimPrefix(outputPath, "charts/matrix-app/")
						}
						if err := os.WriteFile(templatePath, []byte(text), 0o644); err != nil {
							t.Fatal(err)
						}
					}
					command = exec.Command(python, filepath.Join(workflows, "scripts", "render-observability.py"), "--root", fixture, "--check")
					if output, err := command.CombinedOutput(); err != nil {
						t.Fatalf("%s mask %d observability drift: %v\n%s", kind, mask, err, output)
					}
				}
				matrixDir := filepath.Join("templates", "matrix", matrixKey(opts))
				if err := os.MkdirAll(matrixDir, 0o755); err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"catalog.yaml", "templates.yaml", "release-please-config.json", ".release-please-manifest.json"} {
					data, err := os.ReadFile(filepath.Join(fixture, name))
					if err != nil {
						t.Fatal(err)
					}
					text := string(data)
					text = strings.ReplaceAll(text, opts.Name, "__HIROBA_NAME__")
					if err := os.WriteFile(filepath.Join(matrixDir, name), []byte(text), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				fmt.Printf("prepared %s\n", matrixKey(opts))
			}
		}
	}
}

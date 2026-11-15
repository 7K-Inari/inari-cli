package hiroba

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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
		for mask := 0; mask < 32; mask++ {
			opts := matrixOptions(kind, mask)
			fixture := filepath.Join(t.TempDir(), opts.Name)
			if err := scaffoldWithPin(fixture, opts, candidateWorkflowSHA, "v1.1.0"); err != nil {
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
					if outputPath == "packages/observability-pack/rgd.yaml" {
						templatePath = "templates/components/observability/pack/rgd.yaml"
					} else {
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

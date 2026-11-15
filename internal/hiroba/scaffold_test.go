package hiroba

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const candidateWorkflowSHA = "ae4be82b95cee92eecd8d87ba55204dea750c7e1"

type goldenFile struct {
	Mode    fs.FileMode `json:"mode"`
	Content string      `json:"content"`
}

type goldenTree map[string]goldenFile

func matrixOptions(kind string, mask int) Options {
	return Options{
		Name:              "matrix-app",
		Type:              kind,
		WithPlugin:        mask&1 != 0,
		WithUI:            mask&2 != 0,
		WithChart:         mask&4 != 0,
		WithObservability: mask&8 != 0,
		WithRuntimeSmoke:  mask&16 != 0,
	}
}

func readTree(t *testing.T, root string) goldenTree {
	t.Helper()
	result := goldenTree{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		result[filepath.ToSlash(rel)] = goldenFile{Mode: info.Mode().Perm(), Content: string(content)}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func goldenPath(kind string, mask int) string {
	return filepath.Join("testdata", "golden", fmt.Sprintf("%s-%02d.json", kind, mask))
}

func TestScaffoldFullMatrixGolden(t *testing.T) {
	for _, kind := range []string{"app", "extension", "catalog", "template"} {
		for mask := 0; mask < 32; mask++ {
			t.Run(fmt.Sprintf("%s/%02d", kind, mask), func(t *testing.T) {
				opts := matrixOptions(kind, mask)
				target := filepath.Join(t.TempDir(), opts.Name)
				if err := scaffoldWithPin(target, opts, candidateWorkflowSHA, "v1.1.0"); err != nil {
					t.Fatalf("valid option combination failed: %v", err)
				}
				actual := readTree(t, target)
				golden := goldenPath(kind, mask)
				if os.Getenv("HIROBA_UPDATE_GOLDEN") == "1" {
					data, err := json.MarshalIndent(actual, "", "  ")
					if err != nil {
						t.Fatal(err)
					}
					data = append(data, '\n')
					if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(golden, data, 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
				data, err := os.ReadFile(golden)
				if err != nil {
					t.Fatalf("read golden: %v", err)
				}
				var expected goldenTree
				if err := json.Unmarshal(data, &expected); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(actual, expected) {
					keys := make([]string, 0, len(actual))
					for key := range actual {
						keys = append(keys, key)
					}
					sort.Strings(keys)
					t.Fatalf("generated tree differs from golden; files: %s", strings.Join(keys, ", "))
				}
			})
		}
	}
}

func TestFullMatrixContentContracts(t *testing.T) {
	for _, kind := range []string{"app", "extension", "catalog", "template"} {
		for mask := 0; mask < 32; mask++ {
			opts := matrixOptions(kind, mask)
			target := filepath.Join(t.TempDir(), opts.Name)
			if err := scaffoldWithPin(target, opts, candidateWorkflowSHA, "v1.1.0"); err != nil {
				t.Fatalf("%s mask %d: %v", kind, mask, err)
			}
			tree := readTree(t, target)
			for path, file := range tree {
				if strings.Contains(file.Content, "__WORKFLOWS_V1_SHA__") || strings.Contains(file.Content, "__HIROBA_NAME__") {
					t.Errorf("%s mask %d %s contains an unresolved placeholder", kind, mask, path)
				}
				if strings.Contains(file.Content, "@v1\n") || strings.Contains(file.Content, "@main") {
					t.Errorf("%s mask %d %s contains a mutable workflow ref", kind, mask, path)
				}
			}
			if opts.WithObservability && !opts.WithChart {
				if _, ok := tree["packages/observability-pack/rgd.yaml"]; !ok {
					t.Errorf("%s mask %d omitted chartless observability pack", kind, mask)
				}
				for path := range tree {
					if strings.HasPrefix(path, "charts/") {
						t.Errorf("%s mask %d emitted phantom chart %s", kind, mask, path)
					}
				}
			}
			if opts.WithRuntimeSmoke && !opts.WithChart {
				suite := tree["tests/smoke/suite.yaml"].Content
				for _, expected := range []string{"kind: manifests", "fixtureOnly: true", "executableProfiles: [ci]", "id: matrix-app-smoke"} {
					if !strings.Contains(suite, expected) {
						t.Errorf("%s mask %d suite missing %q", kind, mask, expected)
					}
				}
				if _, ok := tree["tests/smoke/fixtures/app/deployment.yaml"]; !ok {
					t.Errorf("%s mask %d omitted chartless smoke fixture", kind, mask)
				}
			}
			if opts.WithRuntimeSmoke && opts.WithChart {
				suite := tree["tests/smoke/suite.yaml"].Content
				if !strings.Contains(suite, "kind: helm") || !strings.Contains(suite, "component: charts/matrix-app") {
					t.Errorf("%s mask %d did not select helm adapter", kind, mask)
				}
			}
			if !opts.WithPlugin && kind != "extension" {
				for path := range tree {
					if strings.HasPrefix(path, "extension/") {
						t.Errorf("%s mask %d unexpectedly added plugin %s", kind, mask, path)
					}
				}
			}
		}
	}
}

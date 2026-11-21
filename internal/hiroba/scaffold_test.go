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

	"gopkg.in/yaml.v3"
)

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
				if err := Scaffold(target, opts); err != nil {
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
			if err := Scaffold(target, opts); err != nil {
				t.Fatalf("%s mask %d: %v", kind, mask, err)
			}
			tree := readTree(t, target)
			assertReleaseContract(t, opts, tree)
			for path, file := range tree {
				if strings.Contains(file.Content, "__WORKFLOWS_V1_SHA__") || strings.Contains(file.Content, "__HIROBA_NAME__") {
					t.Errorf("%s mask %d %s contains an unresolved placeholder", kind, mask, path)
				}
				if strings.Contains(file.Content, "@v1\n") || strings.Contains(file.Content, "@main") {
					t.Errorf("%s mask %d %s contains a mutable workflow ref", kind, mask, path)
				}
			}
			if opts.WithObservability {
				if kind == "app" || kind == "catalog" {
					if _, ok := tree["packages/matrix-app/observability.yaml"]; !ok {
						t.Errorf("%s mask %d omitted full package observability metadata", kind, mask)
					}
				}
				if kind == "app" || kind == "template" {
					if _, ok := tree["templates/matrix-app/observability/observability.yaml"]; !ok {
						t.Errorf("%s mask %d omitted template observability metadata", kind, mask)
					}
					manifest := tree["templates/matrix-app/template.yaml"].Content
					if !strings.Contains(manifest, "manifest: observability/observability.yaml") {
						t.Errorf("%s mask %d omitted template observability declaration", kind, mask)
					}
				}
			}
			if !opts.WithObservability && (kind == "app" || kind == "catalog") {
				for _, path := range []string{"packages/matrix-app/package.yaml", "catalog.yaml"} {
					if strings.Contains(tree[path].Content, "observability-pack") {
						t.Errorf("%s mask %d references an unselected observability pack in %s", kind, mask, path)
					}
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
			if opts.WithChart {
				for _, path := range []string{"ci/ct.yaml", "ci/chart_schema.yaml", "ci/lintconf.yaml"} {
					if _, ok := tree[path]; !ok {
						t.Errorf("%s mask %d omitted chart test config %s", kind, mask, path)
					}
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

func assertReleaseContract(t *testing.T, opts Options, tree goldenTree) {
	t.Helper()
	var config struct {
		ReleaseComponents map[string]string `yaml:"releaseComponents"`
		Compatibility     *struct {
			Matrix string `yaml:"matrix"`
		} `yaml:"compatibility"`
	}
	if err := yaml.Unmarshal([]byte(tree[".hiroba.yaml"].Content), &config); err != nil {
		t.Fatalf("parse generated app config: %v", err)
	}
	expected := map[string]string{}
	if opts.Type == "app" || opts.Type == "catalog" {
		expected["packages/"+opts.Name] = "package-" + opts.Name
	}
	if opts.Type == "app" || opts.Type == "template" {
		expected["templates/"+opts.Name] = "template-" + opts.Name
	}
	if opts.WithChart {
		expected["charts/"+opts.Name] = "chart-" + opts.Name
	}
	if opts.WithObservability {
		expected["packages/observability-pack"] = "package-observability-pack"
	}
	if opts.WithRuntimeSmoke && !opts.WithChart {
		expected["tests/smoke"] = "smoke-" + opts.Name + "-smoke"
	}
	if !reflect.DeepEqual(config.ReleaseComponents, expected) && (len(expected) > 0 || len(config.ReleaseComponents) > 0) {
		t.Errorf("%s mask %d releaseComponents = %#v, want %#v", opts.Type, optionMask(opts), config.ReleaseComponents, expected)
	}

	var releasePlease struct {
		Packages map[string]struct {
			Component string `json:"component"`
		} `json:"packages"`
	}
	if err := json.Unmarshal([]byte(tree["release-please-config.json"].Content), &releasePlease); err != nil {
		t.Fatalf("parse release-please config: %v", err)
	}
	for path, identity := range expected {
		component, ok := releasePlease.Packages[path]
		if !ok {
			t.Errorf("%s mask %d release-please omitted %s", opts.Type, optionMask(opts), path)
			continue
		}
		if component.Component != identity {
			t.Errorf("%s mask %d release-please component for %s = %q, want %q", opts.Type, optionMask(opts), path, component.Component, identity)
		}
	}

	effectivePlugin := opts.WithPlugin || opts.Type == "extension"
	if effectivePlugin || opts.WithUI {
		if config.Compatibility == nil || config.Compatibility.Matrix != "ci/compatibility-matrix.json" {
			t.Errorf("%s mask %d omitted compatibility declaration for independent extension components", opts.Type, optionMask(opts))
		}
		if _, ok := tree["ci/compatibility-matrix.json"]; !ok {
			t.Errorf("%s mask %d omitted compatibility matrix", opts.Type, optionMask(opts))
		}
	} else {
		if config.Compatibility != nil {
			t.Errorf("%s mask %d unexpectedly declared compatibility", opts.Type, optionMask(opts))
		}
		if _, ok := tree["ci/compatibility-matrix.json"]; ok {
			t.Errorf("%s mask %d unexpectedly emitted compatibility matrix", opts.Type, optionMask(opts))
		}
	}
	if opts.WithChart {
		repair := tree[".github/workflows/chart-repair.yml"].Content
		for _, expected := range []string{`TAG="chart-$CHART-v$VERSION"`, `TAG="$CHART-v$VERSION"`} {
			if !strings.Contains(repair, expected) {
				t.Errorf("%s mask %d chart repair missing %q", opts.Type, optionMask(opts), expected)
			}
		}
	}
}

func TestNormalizeOptionsRejectsReservedObservabilityPack(t *testing.T) {
	_, err := normalizeOptions(Options{Name: "observability-pack", Type: "catalog", WithObservability: true})
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("reserved observability-pack name error = %v", err)
	}
}

func TestAddTreeRejectsDuplicateDestination(t *testing.T) {
	files := map[string]fileSpec{}
	if err := addTree(files, "templates/common", ""); err != nil {
		t.Fatal(err)
	}
	if err := addTree(files, "templates/common", ""); err == nil || !strings.Contains(err.Error(), "duplicate generated path") {
		t.Fatalf("duplicate tree error = %v", err)
	}
}

func TestNormalizeOptionsModulePaths(t *testing.T) {
	for _, module := range []string{"", "github.com/7K-Hiroba/example", "example.com/foo/bar", "example.com/foo/v2"} {
		t.Run("valid_"+module, func(t *testing.T) {
			if _, err := normalizeOptions(Options{Name: "valid-name", Type: "extension", Module: module}); err != nil {
				t.Fatalf("valid module %q rejected: %v", module, err)
			}
		})
	}
	for _, module := range []string{
		"bad module",
		"example.com/foo/./bar",
		"example.com/.foo/bar",
		"example.com/foo/bar.",
		"example.com/foo//bar",
		"example.com/foo/../bar",
		"example.com/foo~1/bar",
		"example.com/CON/bar",
	} {
		t.Run("invalid_"+module, func(t *testing.T) {
			if _, err := normalizeOptions(Options{Name: "valid-name", Type: "extension", Module: module}); err == nil {
				t.Fatalf("invalid module %q accepted", module)
			}
		})
	}
}

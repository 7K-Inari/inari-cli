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

func matrixOptions(kind string, mask int, archetype ...string) Options {
	selectedArchetype := "non-stack"
	if len(archetype) > 0 {
		selectedArchetype = archetype[0]
	}
	return Options{
		Name:              "matrix-app",
		Type:              kind,
		Archetype:         selectedArchetype,
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

func goldenPath(kind string, mask int, archetype ...string) string {
	key := fmt.Sprintf("%s-%02d", kind, mask)
	if len(archetype) > 0 && archetype[0] == "stack" {
		key = fmt.Sprintf("%s-stack-%02d", kind, mask)
	}
	return filepath.Join("testdata", "golden", key+".json")
}

func TestScaffoldFullMatrixGolden(t *testing.T) {
	for _, kind := range []string{"app", "extension", "catalog", "template"} {
		for _, archetype := range []string{"non-stack", "stack"} {
			for mask := 0; mask < 32; mask++ {
				t.Run(fmt.Sprintf("%s/%s/%02d", kind, archetype, mask), func(t *testing.T) {
					opts := matrixOptions(kind, mask, archetype)
					target := filepath.Join(t.TempDir(), opts.Name)
					if err := Scaffold(target, opts); err != nil {
						t.Fatalf("valid option combination failed: %v", err)
					}
					actual := readTree(t, target)
					golden := goldenPath(kind, mask, archetype)
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
}

func TestFullMatrixContentContracts(t *testing.T) {
	for _, kind := range []string{"app", "extension", "catalog", "template"} {
		for _, archetype := range []string{"non-stack", "stack"} {
			for mask := 0; mask < 32; mask++ {
				opts := matrixOptions(kind, mask, archetype)
				target := filepath.Join(t.TempDir(), opts.Name)
				if err := Scaffold(target, opts); err != nil {
					t.Fatalf("%s mask %d: %v", kind, mask, err)
				}
				tree := readTree(t, target)
				assertReleaseContract(t, opts, tree)
				if opts.Archetype == "stack" {
					for _, path := range []string{"tests/stack/suite.yaml", "tests/stack/install.sh", "tests/stack/upgrade.sh", ".github/workflows/stack-test.yml"} {
						if _, ok := tree[path]; !ok {
							t.Errorf("%s mask %d omitted stack lifecycle %s", kind, optionMask(opts), path)
						}
					}
					if !strings.Contains(tree[".hiroba.yaml"].Content, "archetype: stack") || !strings.Contains(tree[".hiroba.yaml"].Content, "stackTest:") {
						t.Errorf("%s mask %d omitted mandatory stackTest declaration", kind, optionMask(opts))
					}
				} else {
					for path := range tree {
						if strings.HasPrefix(path, "tests/stack/") || path == ".github/workflows/stack-test.yml" {
							t.Errorf("%s mask %d emitted non-stack lifecycle %s", kind, optionMask(opts), path)
						}
					}
					if !strings.Contains(tree[".hiroba.yaml"].Content, "archetype: non-stack") || strings.Contains(tree[".hiroba.yaml"].Content, "stackTest:") {
						t.Errorf("%s mask %d did not explicitly disable stack lifecycle", kind, optionMask(opts))
					}
				}
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
}

func TestStackCallerDocumentsMergeTreeEvidence(t *testing.T) {
	target := filepath.Join(t.TempDir(), "matrix-app")
	if err := Scaffold(target, Options{Name: "matrix-app", Type: "app", Archetype: "stack"}); err != nil {
		t.Fatal(err)
	}
	caller := readTree(t, target)[".github/workflows/stack-test.yml"].Content
	if !strings.Contains(caller, "merge tree") || !strings.Contains(caller, "associated with the head") {
		t.Fatalf("stack caller does not distinguish PR-head checks from merge-tree evidence")
	}
}

func TestStackLifecycleVariantsProveDesiredRevisionConvergence(t *testing.T) {
	for _, test := range []struct {
		name     string
		opts     Options
		expected []string
	}{
		{
			name: "kro",
			opts: Options{Name: "matrix-app", Type: "catalog", Archetype: "stack", WithObservability: true},
			expected: []string{
				`folder = next(resource for resource in data["spec"]["resources"] if resource["id"] == "folder")`,
				`folder["template"]["metadata"].setdefault("annotations", {})["inari.dev/ci-revision"] = phase`,
				`test "$revision" = "$HIROBA_STACK_PHASE"`,
			},
		},
		{
			name: "manifests",
			opts: Options{Name: "matrix-app", Type: "template", Archetype: "stack"},
			expected: []string{
				`data["spec"]["template"]["metadata"].setdefault("annotations", {})["inari.dev/ci-revision"] = phase`,
				`kubectl rollout status deployment/matrix-app-fixture -n default --timeout=120s`,
				`.spec.template.metadata.annotations.inari\.dev/ci-revision`,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			target := filepath.Join(t.TempDir(), test.opts.Name)
			if err := Scaffold(target, test.opts); err != nil {
				t.Fatal(err)
			}
			tree := readTree(t, target)
			for _, phase := range []string{"install", "upgrade"} {
				content := tree["tests/stack/"+phase+".sh"].Content
				for _, expected := range test.expected {
					if !strings.Contains(content, expected) {
						t.Errorf("%s phase is missing convergence assertion %q", phase, expected)
					}
				}
				if strings.Contains(content, `data["metadata"].setdefault`) {
					t.Errorf("%s phase still changes only top-level metadata", phase)
				}
			}
		})
	}
}

func TestBoundedResourceNameMatchesWorkflowContract(t *testing.T) {
	name := "a" + strings.Repeat("b", 61) + "c"
	if got := boundedResourceName(name, 53); got != "abbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb-00bdc0e0" {
		t.Fatalf("bounded release name = %q", got)
	}
	if got := boundedResourceName("matrix-app", 53); got != "matrix-app" {
		t.Fatalf("short name changed to %q", got)
	}
	other := "a" + strings.Repeat("b", 61) + "d"
	if boundedResourceName(name, 53) == boundedResourceName(other, 53) {
		t.Fatal("distinct long names received the same bounded identity")
	}
}

func TestScaffoldMaxNameUsesBoundedRuntimeIdentifiers(t *testing.T) {
	name := "a" + strings.Repeat("b", 61) + "c"
	t.Run("helm", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), name)
		opts := Options{Name: name, Archetype: "non-stack", Type: "catalog", WithChart: true, WithRuntimeSmoke: true}
		if err := Scaffold(target, opts); err != nil {
			t.Fatal(err)
		}
		tree := readTree(t, target)
		releaseName := boundedResourceName(name, 53)
		if !strings.Contains(tree["tests/smoke/suite.yaml"].Content, "name: "+releaseName) {
			t.Error("Helm readiness does not use the bounded release name")
		}
		if !strings.Contains(tree["packages/"+name+"/tests/run.sh"].Content, "helm template "+releaseName) {
			t.Error("package test does not use the bounded release name")
		}
		if !strings.Contains(tree[".hiroba.yaml"].Content, "app: "+name) || !strings.Contains(tree["tests/smoke/suite.yaml"].Content, "id: "+name) {
			t.Error("bounded runtime names changed the permanent app or publication identity")
		}
	})

	t.Run("manifests", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), name)
		opts := Options{Name: name, Archetype: "non-stack", Type: "template", WithRuntimeSmoke: true}
		if err := Scaffold(target, opts); err != nil {
			t.Fatal(err)
		}
		tree := readTree(t, target)
		fixtureName := boundedResourceName(name, 55) + "-fixture"
		smokeLabel := boundedResourceName(name, 57) + "-smoke"
		if len(fixtureName) != 63 || len(smokeLabel) != 63 {
			t.Fatalf("unexpected boundary lengths: fixture=%d label=%d", len(fixtureName), len(smokeLabel))
		}
		for path, content := range map[string]string{
			"tests/smoke/fixtures/app/deployment.yaml": fixtureName,
			"tests/smoke/fixtures/app/service.yaml":    fixtureName,
			"tests/smoke/suite.yaml":                   fixtureName,
			"tests/smoke/workflowtemplate.yaml":        smokeLabel,
		} {
			if !strings.Contains(tree[path].Content, content) {
				t.Errorf("%s does not contain bounded identity %q", path, content)
			}
		}
		if !strings.Contains(tree[".hiroba.yaml"].Content, "app: "+name) || !strings.Contains(tree["tests/smoke/suite.yaml"].Content, "id: "+name+"-smoke") {
			t.Error("bounded runtime names changed the permanent app or publication identity")
		}
	})
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
		for _, expected := range []string{"discover-components.py", `(.releaseComponent // .name)`, `TAG="$release_component-v$VERSION"`} {
			if !strings.Contains(repair, expected) {
				t.Errorf("%s mask %d chart repair missing %q", opts.Type, optionMask(opts), expected)
			}
		}
		if strings.Contains(repair, `TAG="chart-$CHART`) || strings.Contains(repair, `TAG="$CHART`) {
			t.Errorf("%s mask %d chart repair still guesses tag identities", opts.Type, optionMask(opts))
		}
	}
}

func TestScaffoldRefusesExistingBrokenSymlink(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "matrix-app")
	if err := os.Symlink(filepath.Join(parent, "missing"), target); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	if err := Scaffold(target, Options{Name: "matrix-app", Type: "app", Archetype: "non-stack"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing symlink error = %v, want already exists", err)
	}
	if _, err := os.Lstat(target); err != nil {
		t.Fatalf("existing symlink was removed or replaced: %v", err)
	}
}

func TestScaffoldConcurrentSameTarget(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "matrix-app")
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			results <- Scaffold(target, Options{Name: "matrix-app", Type: "app", Archetype: "non-stack"})
		}()
	}
	close(start)
	successes := 0
	for range 2 {
		if err := <-results; err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent scaffold successes = %d, want exactly one", successes)
	}
	if _, err := os.Stat(filepath.Join(target, "README.md")); err != nil {
		t.Fatalf("winning scaffold is incomplete: %v", err)
	}
	temporary, err := filepath.Glob(filepath.Join(parent, ".matrix-app.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(temporary) != 0 {
		t.Fatalf("temporary directories leaked: %v", temporary)
	}
}

func TestNormalizeOptionsRejectsReservedObservabilityPack(t *testing.T) {
	_, err := normalizeOptions(Options{Name: "observability-pack", Type: "catalog", Archetype: "non-stack", WithObservability: true})
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
			if _, err := normalizeOptions(Options{Name: "valid-name", Type: "extension", Archetype: "non-stack", Module: module}); err != nil {
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
			if _, err := normalizeOptions(Options{Name: "valid-name", Type: "extension", Archetype: "non-stack", Module: module}); err == nil {
				t.Fatalf("invalid module %q accepted", module)
			}
		})
	}
}

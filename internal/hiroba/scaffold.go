package hiroba

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	WorkflowSHA     = "f572c5a244e27e4a77589f2730066749c7b51c6b"
	WorkflowVersion = "v1.0.0"
)

var (
	appNamePattern = regexp.MustCompile(`^[a-z](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	modulePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~/-]*$`)
	shaPattern     = regexp.MustCompile(`^[0-9a-f]{40}$`)
	versionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
	placeholderRe  = regexp.MustCompile(`__(?:HIROBA|WORKFLOWS)_[A-Z0-9_]*__`)
)

//go:embed all:templates
var templatesFS embed.FS

type Options struct {
	Name              string
	Type              string
	Module            string
	WithPlugin        bool
	WithUI            bool
	WithChart         bool
	WithObservability bool
	WithRuntimeSmoke  bool
}

type fileSpec struct {
	source string
	mode   fs.FileMode
}

// Scaffold creates a new Hiroba application repository.
func Scaffold(destDir string, opts Options) error {
	return scaffoldWithPin(destDir, opts, WorkflowSHA, WorkflowVersion)
}

func normalizeOptions(opts Options) (Options, error) {
	if opts.Type == "" {
		opts.Type = "app"
	}
	if !appNamePattern.MatchString(opts.Name) {
		return opts, fmt.Errorf("invalid name %q: use a registry-compatible lowercase DNS label of at most 63 characters", opts.Name)
	}
	switch opts.Type {
	case "app", "extension", "catalog", "template":
	default:
		return opts, fmt.Errorf("invalid type %q: want app|extension|catalog|template", opts.Type)
	}
	if opts.Module == "" {
		opts.Module = "github.com/7K-Hiroba/" + opts.Name
	}
	if !modulePattern.MatchString(opts.Module) || strings.HasSuffix(opts.Module, "/") || strings.Contains(opts.Module, "//") || strings.Contains(opts.Module, "..") {
		return opts, fmt.Errorf("invalid Go module path %q", opts.Module)
	}
	return opts, nil
}

func optionMask(opts Options) int {
	mask := 0
	for index, selected := range []bool{opts.WithPlugin, opts.WithUI, opts.WithChart, opts.WithObservability, opts.WithRuntimeSmoke} {
		if selected {
			mask |= 1 << index
		}
	}
	return mask
}

func matrixKey(opts Options) string {
	return fmt.Sprintf("%s-%02d", opts.Type, optionMask(opts))
}

func addTree(files map[string]fileSpec, root, destination string) error {
	return fs.WalkDir(templatesFS, root, func(source string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(source, root), "/")
		rel = strings.TrimSuffix(rel, ".template")
		rel = strings.ReplaceAll(rel, "workflows-scaffold", "__HIROBA_NAME__")
		mode := fs.FileMode(0o644)
		if strings.HasSuffix(rel, ".sh") {
			mode = 0o755
		}
		files[path.Join(destination, rel)] = fileSpec{source: source, mode: mode}
		return nil
	})
}

func addFile(files map[string]fileSpec, source, destination string) {
	files[destination] = fileSpec{source: source, mode: 0o644}
}

func addWorkflow(files map[string]fileSpec, name string) {
	addFile(files, "templates/workflows/"+name+".yml.template", ".github/workflows/"+name+".yml")
}

func selectedFiles(opts Options) (map[string]fileSpec, error) {
	files := map[string]fileSpec{}
	if err := addTree(files, "templates/common", ""); err != nil {
		return nil, err
	}
	effectivePlugin := opts.WithPlugin || opts.Type == "extension"
	packageVariant := "chart"
	if !opts.WithChart {
		packageVariant = "no-chart"
	}
	if opts.Type == "app" || opts.Type == "catalog" {
		if err := addTree(files, "templates/components/package/chart", "packages/"+opts.Name); err != nil {
			return nil, err
		}
		if packageVariant == "no-chart" {
			if err := addTree(files, "templates/components/package/no-chart", "packages/"+opts.Name); err != nil {
				return nil, err
			}
		}
	}
	if opts.Type == "app" || opts.Type == "template" {
		if err := addTree(files, "templates/components/template", "templates/"+opts.Name); err != nil {
			return nil, err
		}
	}
	if effectivePlugin {
		if err := addTree(files, "templates/components/plugin", "extension"); err != nil {
			return nil, err
		}
	}
	if opts.WithUI {
		if err := addTree(files, "templates/components/ui", "ui"); err != nil {
			return nil, err
		}
	}
	if effectivePlugin || opts.WithUI {
		addFile(files, "templates/components/extension.yaml", "extension.yaml")
	}
	if opts.WithChart {
		chartVariant := "nonplugin"
		if effectivePlugin {
			chartVariant = "plugin"
		}
		if err := addTree(files, "templates/components/chart/"+chartVariant, "charts/"+opts.Name); err != nil {
			return nil, err
		}
		if err := addTree(files, "templates/components/chart/config", "ci"); err != nil {
			return nil, err
		}
	}
	if opts.WithObservability {
		if err := addTree(files, "templates/components/observability/canonical", "observability"); err != nil {
			return nil, err
		}
		if err := addTree(files, "templates/components/observability/pack", "packages/observability-pack"); err != nil {
			return nil, err
		}
		if opts.WithChart {
			if err := addTree(files, "templates/components/observability/chart", "charts/"+opts.Name); err != nil {
				return nil, err
			}
		}
	}
	if opts.WithRuntimeSmoke {
		smokeVariant := "manifests"
		if opts.WithChart {
			smokeVariant = "helm/nonplugin"
			if effectivePlugin {
				smokeVariant = "helm/plugin"
			}
		}
		if err := addTree(files, "templates/components/smoke/"+smokeVariant, "tests/smoke"); err != nil {
			return nil, err
		}
	}

	for _, name := range []string{"validate", "release-please", "release", "edge"} {
		addWorkflow(files, name)
	}
	if opts.Type == "app" || opts.Type == "catalog" || opts.WithObservability {
		addWorkflow(files, "test-packages")
	}
	if effectivePlugin || opts.WithUI {
		addWorkflow(files, "extension-ci")
	}
	if effectivePlugin && opts.WithUI {
		if err := addTree(files, "templates/components/compat", ""); err != nil {
			return nil, err
		}
		addWorkflow(files, "compat-pr")
		addWorkflow(files, "compat-nightly")
	}
	if opts.WithChart {
		addWorkflow(files, "chart-ci")
		addWorkflow(files, "chart-repair")
	}
	if opts.WithObservability {
		addWorkflow(files, "observability")
	}
	if opts.WithRuntimeSmoke {
		addWorkflow(files, "runtime-smoke")
	}

	matrixRoot := "templates/matrix/" + matrixKey(opts)
	if _, err := fs.Stat(templatesFS, matrixRoot); err == nil {
		if err := addTree(files, matrixRoot, ""); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return files, nil
}

func pascalName(name string) string {
	parts := strings.Split(name, "-")
	for index, part := range parts {
		if part != "" {
			parts[index] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, "")
}

func dynamicReplacements(opts Options) map[string]string {
	effectivePlugin := opts.WithPlugin || opts.Type == "extension"
	components := ""
	if effectivePlugin {
		components += "  extension: {path: extension, version: 0.1.0}\n"
	}
	if opts.WithUI {
		components += "  ui: {path: ui, version: 0.1.0}\n"
	}
	observability := ""
	if opts.WithObservability {
		observability = "observability:\n  path: observability\n"
		if opts.WithChart {
			observability += "  chart: charts/" + opts.Name + "\n"
		}
		observability += "  package: packages/observability-pack\n  alloyConfig: observability/tests/alloy/config.river\n"
	}
	smoke := ""
	if opts.WithRuntimeSmoke {
		smoke = "smoke:\n  path: tests/smoke\n"
		if opts.WithChart {
			smoke += "  targetComponent: charts/" + opts.Name + "\n"
		}
	}
	compatibility := ""
	if effectivePlugin && opts.WithUI {
		compatibility = "compatibility:\n  matrix: ci/compatibility-matrix.json\n"
	}
	kinds := "[backend]"
	if !effectivePlugin && opts.WithUI {
		kinds = "[ui]"
	} else if effectivePlugin && opts.WithUI {
		kinds = "[backend, ui]"
	}
	chartDependencies := "dependencies: []\n"
	if opts.WithObservability {
		chartDependencies = "dependencies:\n  - packages/observability-pack/tests/deps/prometheusrule-crd.yaml\n"
	}
	extensionJobs := ""
	if effectivePlugin {
		extensionJobs += "  go:\n    uses: 7K-Hiroba/workflows/.github/workflows/ci-go-extension.yml@__WORKFLOWS_V1_SHA__ # __WORKFLOWS_VERSION__\n    with: {root: .}\n"
	}
	if opts.WithUI {
		extensionJobs += "  ui:\n    uses: 7K-Hiroba/workflows/.github/workflows/ci-ui-extension.yml@__WORKFLOWS_V1_SHA__ # __WORKFLOWS_VERSION__\n    with: {root: .}\n"
	}
	return map[string]string{
		"__HIROBA_COMPONENTS__":            components,
		"__HIROBA_OBSERVABILITY__":         observability,
		"__HIROBA_SMOKE__":                 smoke,
		"__HIROBA_COMPATIBILITY__":         compatibility,
		"__HIROBA_EXTENSION_KINDS__":       kinds,
		"__HIROBA_CHART_DEPENDENCIES__":    chartDependencies,
		"__HIROBA_OBSERVABILITY_ENABLED__": fmt.Sprintf("%t", opts.WithObservability),
		"__HIROBA_EXTENSION_JOBS__":        extensionJobs,
	}
}

func resolveDestinations(files map[string]fileSpec, name string) (map[string]fileSpec, error) {
	resolved := make(map[string]fileSpec, len(files))
	for destination, spec := range files {
		actual := strings.ReplaceAll(destination, "__HIROBA_NAME__", name)
		if _, exists := resolved[actual]; exists {
			return nil, fmt.Errorf("duplicate generated path %q", actual)
		}
		resolved[actual] = spec
	}
	return resolved, nil
}

func renderFiles(files map[string]fileSpec, opts Options, workflowSHA, workflowVersion string) (map[string][]byte, error) {
	if !shaPattern.MatchString(workflowSHA) || !versionPattern.MatchString(workflowVersion) {
		return nil, fmt.Errorf("workflow pin must be a full lowercase commit SHA and SemVer tag")
	}
	dynamic := dynamicReplacements(opts)
	baseReplacements := map[string]string{
		"__HIROBA_NAME__":        opts.Name,
		"__HIROBA_NAME_PASCAL__": pascalName(opts.Name),
		"__HIROBA_MODULE__":      opts.Module,
		"__WORKFLOWS_V1_SHA__":   workflowSHA,
		"__WORKFLOWS_VERSION__":  workflowVersion,
	}
	paths := make([]string, 0, len(files))
	for destination := range files {
		paths = append(paths, destination)
	}
	sort.Strings(paths)
	rendered := map[string][]byte{}
	for _, destination := range paths {
		if destination == "" || strings.HasPrefix(destination, "/") || strings.Contains(destination, "../") {
			return nil, fmt.Errorf("unsafe generated path %q", destination)
		}
		raw, err := templatesFS.ReadFile(files[destination].source)
		if err != nil {
			return nil, err
		}
		text := string(raw)
		for token, value := range dynamic {
			text = strings.ReplaceAll(text, token, value)
		}
		for token, value := range baseReplacements {
			text = strings.ReplaceAll(text, token, value)
		}
		if match := placeholderRe.FindString(text); match != "" {
			return nil, fmt.Errorf("unresolved placeholder %s in %s", match, destination)
		}
		rendered[destination] = []byte(text)
	}
	return rendered, nil
}

func scaffoldWithPin(destDir string, opts Options, workflowSHA, workflowVersion string) error {
	opts, err := normalizeOptions(opts)
	if err != nil {
		return err
	}
	target, err := filepath.Abs(destDir)
	if err != nil {
		return err
	}
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("directory %s already exists", destDir)
	} else if !os.IsNotExist(err) {
		return err
	}
	files, err := selectedFiles(opts)
	if err != nil {
		return err
	}
	files, err = resolveDestinations(files, opts.Name)
	if err != nil {
		return err
	}
	rendered, err := renderFiles(files, opts, workflowSHA, workflowVersion)
	if err != nil {
		return err
	}
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	temp, err := os.MkdirTemp(parent, "."+filepath.Base(target)+".tmp-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(temp) }()
	for destination, content := range rendered {
		mode := files[destination].mode
		output := filepath.Join(temp, filepath.FromSlash(destination))
		if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(output, content, mode); err != nil {
			return err
		}
	}
	if err := os.Rename(temp, target); err != nil {
		return err
	}
	return nil
}

package prompt

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/7K-Inari/inari-cli/internal/schema"
)

type fakePrompter struct {
	answers map[string]any
}

func (f fakePrompter) Ask(field schema.Field) (any, bool, error) {
	v, ok := f.answers[field.Key()]
	return v, ok, nil
}

func TestCollectBuildsSpecFromAnswers(t *testing.T) {
	fields := []schema.Field{
		{Path: []string{"size"}, Type: "string", Required: true},
		{Path: []string{"engine", "version"}, Type: "string", Required: true},
		{Path: []string{"replicas"}, Type: "integer"},
	}
	p := fakePrompter{answers: map[string]any{"size": "large", "engine.version": "16"}}
	spec, err := Collect(p, fields)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"size": "large", "engine": map[string]any{"version": "16"}}
	if !reflect.DeepEqual(spec, want) {
		t.Fatalf("spec = %v, want %v", spec, want)
	}
}

func TestLoadFileAndApplySets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "values.yaml")
	if err := os.WriteFile(path, []byte("size: small\nengine:\n  version: \"15\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplySets(spec, []string{"size=large", "engine.version=16", "ha=true", "replicas=3"}); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"size":     "large",
		"engine":   map[string]any{"version": 16},
		"ha":       true,
		"replicas": 3,
	}
	if !reflect.DeepEqual(spec, want) {
		t.Fatalf("spec = %#v, want %#v", spec, want)
	}
	if err := ApplySets(spec, []string{"bad"}); err == nil {
		t.Fatal("expected error for malformed --set")
	}
}

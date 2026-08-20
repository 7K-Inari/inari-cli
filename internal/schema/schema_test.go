package schema

import (
	"reflect"
	"testing"
)

func fixtureSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"size": map[string]any{
				"type":        "string",
				"description": "Instance size",
				"enum":        []any{"small", "large"},
				"default":     "small",
			},
			"engine": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"version": map[string]any{"type": "string", "description": "Engine version"},
				},
				"required": []any{"version"},
			},
			"replicas": map[string]any{"type": "integer"},
		},
		"required": []any{"size", "engine"},
	}
}

func TestWalkOrdersRequiredFirst(t *testing.T) {
	fields, err := Walk(fixtureSchema())
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, f := range fields {
		keys = append(keys, f.Key())
	}
	want := []string{"engine.version", "size", "replicas"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("keys = %v, want %v", keys, want)
	}

	byKey := map[string]Field{}
	for _, f := range fields {
		byKey[f.Key()] = f
	}
	if !byKey["size"].Required || !byKey["engine.version"].Required || byKey["replicas"].Required {
		t.Fatalf("required flags wrong: %+v", byKey)
	}
	if got := byKey["size"].Enum; !reflect.DeepEqual(got, []string{"small", "large"}) {
		t.Fatalf("enum = %v", got)
	}
	if byKey["size"].Default != "small" {
		t.Fatalf("default = %v", byKey["size"].Default)
	}
}

func TestWalkRejectsNonObject(t *testing.T) {
	if _, err := Walk("nope"); err == nil {
		t.Fatal("expected error for non-object schema")
	}
	if fields, err := Walk(nil); err != nil || fields != nil {
		t.Fatalf("nil schema should give nil fields, got %v, %v", fields, err)
	}
}

func TestValidateReportsMissingRequired(t *testing.T) {
	fields, err := Walk(fixtureSchema())
	if err != nil {
		t.Fatal(err)
	}
	missing := Validate(fields, map[string]any{"size": "large"})
	if !reflect.DeepEqual(missing, []string{"engine.version"}) {
		t.Fatalf("missing = %v", missing)
	}
	full := map[string]any{"size": "large", "engine": map[string]any{"version": "16"}}
	if m := Validate(fields, full); len(m) != 0 {
		t.Fatalf("missing = %v, want none", m)
	}
}

func TestSetNestedPath(t *testing.T) {
	spec := map[string]any{}
	Set(spec, "engine.version", "16")
	Set(spec, "size", "small")
	want := map[string]any{"engine": map[string]any{"version": "16"}, "size": "small"}
	if !reflect.DeepEqual(spec, want) {
		t.Fatalf("spec = %v, want %v", spec, want)
	}
}

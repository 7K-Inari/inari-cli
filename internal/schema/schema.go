package schema

import (
	"fmt"
	"sort"
	"strings"
)

// Field is a single promptable leaf in an OpenAPI v3 schema.
type Field struct {
	Path        []string
	Type        string
	Description string
	Required    bool
	Default     any
	Enum        []string
}

func (f Field) Key() string { return strings.Join(f.Path, ".") }

// Walk flattens an OpenAPI v3 schema (as decoded JSON/YAML maps) into an
// ordered list of promptable leaf fields. Required fields come first, then
// optional fields, both in stable (sorted) order. Nested objects recurse;
// arrays and free-form maps are treated as opaque values (entered as YAML).
func Walk(schema any) ([]Field, error) {
	if schema == nil {
		return nil, nil
	}
	root, ok := schema.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("schema is not an object")
	}
	var required, optional []Field
	if err := walk(root, nil, false, &required, &optional); err != nil {
		return nil, err
	}
	return append(required, optional...), nil
}

func walk(node map[string]any, path []string, required bool, requiredOut, optionalOut *[]Field) error {
	props, _ := node["properties"].(map[string]any)
	reqSet := map[string]bool{}
	if req, ok := node["required"].([]any); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				reqSet[s] = true
			}
		}
	}
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		raw, ok := props[name].(map[string]any)
		if !ok {
			continue
		}
		childPath := append(append([]string{}, path...), name)
		isReq := required && reqSet[name] || len(path) == 0 && reqSet[name]
		typ, _ := raw["type"].(string)
		if typ == "object" && raw["properties"] != nil {
			if err := walk(raw, childPath, isReq, requiredOut, optionalOut); err != nil {
				return err
			}
			continue
		}
		desc, _ := raw["description"].(string)
		f := Field{
			Path:        childPath,
			Type:        typ,
			Description: desc,
			Required:    isReq,
			Default:     raw["default"],
		}
		if enum, ok := raw["enum"].([]any); ok {
			for _, e := range enum {
				f.Enum = append(f.Enum, fmt.Sprintf("%v", e))
			}
		}
		if isReq {
			*requiredOut = append(*requiredOut, f)
		} else {
			*optionalOut = append(*optionalOut, f)
		}
	}
	return nil
}

// Validate checks that every required field is present and non-empty in spec.
func Validate(fields []Field, spec map[string]any) []string {
	var missing []string
	for _, f := range fields {
		if !f.Required {
			continue
		}
		v, ok := lookup(spec, f.Path)
		if !ok || v == nil || v == "" {
			missing = append(missing, f.Key())
		}
	}
	return missing
}

func lookup(spec map[string]any, path []string) (any, bool) {
	cur := any(spec)
	for _, p := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[p]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// Set assigns value at the dotted path inside spec, creating intermediate maps.
func Set(spec map[string]any, dottedPath string, value any) {
	parts := strings.Split(dottedPath, ".")
	cur := spec
	for i, p := range parts {
		if i == len(parts)-1 {
			cur[p] = value
			return
		}
		next, ok := cur[p].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[p] = next
		}
		cur = next
	}
}

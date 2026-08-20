package prompt

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"gopkg.in/yaml.v3"

	"github.com/7K-Inari/inari-cli/internal/schema"
)

// Prompter asks the user for one field value.
type Prompter interface {
	Ask(f schema.Field) (any, bool, error)
}

// SurveyPrompter implements Prompter with interactive terminal prompts.
type SurveyPrompter struct{}

func (SurveyPrompter) Ask(f schema.Field) (any, bool, error) {
	label := f.Key()
	if f.Description != "" {
		label += " (" + f.Description + ")"
	}
	if !f.Required {
		label += " [optional, empty to skip]"
	}

	if len(f.Enum) > 0 {
		var ans string
		q := &survey.Select{Message: label, Options: f.Enum}
		if f.Default != nil {
			q.Default = fmt.Sprintf("%v", f.Default)
		}
		if err := survey.AskOne(q, &ans); err != nil {
			return nil, false, err
		}
		return ans, true, nil
	}

	switch f.Type {
	case "boolean":
		ans := false
		q := &survey.Confirm{Message: label}
		if f.Default != nil {
			q.Default, _ = f.Default.(bool)
		}
		if err := survey.AskOne(q, &ans); err != nil {
			return nil, false, err
		}
		return ans, true, nil
	default:
		var ans string
		q := &survey.Input{Message: label}
		if f.Default != nil {
			q.Default = fmt.Sprintf("%v", f.Default)
		}
		if err := survey.AskOne(q, &ans); err != nil {
			return nil, false, err
		}
		if strings.TrimSpace(ans) == "" && !f.Required {
			return nil, false, nil
		}
		return coerce(ans, f.Type), true, nil
	}
}

// Collect prompts for every field and builds the spec map.
func Collect(p Prompter, fields []schema.Field) (map[string]any, error) {
	spec := map[string]any{}
	for _, f := range fields {
		v, ok, err := p.Ask(f)
		if err != nil {
			return nil, err
		}
		if ok {
			schema.Set(spec, f.Key(), v)
		}
	}
	return spec, nil
}

// LoadFile reads a YAML/JSON values file into a spec map.
func LoadFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading values file: %w", err)
	}
	spec := map[string]any{}
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parsing values file %s: %w", path, err)
	}
	return spec, nil
}

// ApplySets applies key=value --set overrides (dot-separated paths) onto spec.
// Values are YAML-parsed so numbers/booleans keep their type.
func ApplySets(spec map[string]any, sets []string) error {
	for _, s := range sets {
		k, v, ok := strings.Cut(s, "=")
		if !ok || k == "" {
			return fmt.Errorf("invalid --set %q, want key=value", s)
		}
		var parsed any
		if err := yaml.Unmarshal([]byte(v), &parsed); err != nil {
			return fmt.Errorf("parsing --set %q value: %w", s, err)
		}
		schema.Set(spec, k, parsed)
	}
	return nil
}

func coerce(s string, typ string) any {
	switch typ {
	case "integer":
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n
		}
	case "number":
		if n, err := strconv.ParseFloat(s, 64); err == nil {
			return n
		}
	}
	return s
}

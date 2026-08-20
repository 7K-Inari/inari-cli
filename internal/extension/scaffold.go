package extension

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed templates
var templatesFS embed.FS

type Params struct {
	Name   string
	Kind   string // backend | ui
	Module string
	// Pascal is Name converted to PascalCase for source identifiers.
	Pascal string
}

// Scaffold renders the template tree for kind into destDir.
func Scaffold(destDir string, p Params) error {
	root := path.Join("templates", p.Kind)
	return fs.WalkDir(templatesFS, root, func(fpath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(fpath, root), "/")
		target := filepath.Join(destDir, filepath.FromSlash(strings.TrimSuffix(rel, ".tmpl")))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		raw, err := templatesFS.ReadFile(fpath)
		if err != nil {
			return err
		}
		tpl, err := template.New(rel).Parse(string(raw))
		if err != nil {
			return fmt.Errorf("parsing template %s: %w", rel, err)
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := tpl.Execute(f, p); err != nil {
			return fmt.Errorf("rendering %s: %w", rel, err)
		}
		return nil
	})
}

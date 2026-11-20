package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/7K-Inari/inari-cli/internal/api"
	"github.com/7K-Inari/inari-cli/internal/extension"
)

var nameRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func newExtensionCmd(opts *GlobalOptions) *cobra.Command {
	c := &cobra.Command{
		Use:   "extension",
		Short: "Scaffold and manage platform extensions",
	}
	c.AddCommand(newExtensionInitCmd(opts))
	return c
}

func newExtensionInitCmd(opts *GlobalOptions) *cobra.Command {
	var kind, dir, module string
	c := &cobra.Command{
		Use:   "init NAME",
		Short: "Scaffold a backend or UI extension from templates",
		Long: `Scaffold a new Inari extension.

  backend  Go gRPC plugin implementing the inari-plugin-sdk contract; the
           control plane proxies its endpoints at /api/extensions/<name>/*
  ui       React/TypeScript Module Federation remote registered against the
           typed blueprint slots of inari-ui-plugin-sdk`,
		Args: cobra.ExactArgs(1),
		Example: `  inari extension init my-argocd-actions --type backend
  inari extension init my-status-cards --type ui --dir ./extensions`,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if !nameRe.MatchString(name) {
				return fmt.Errorf("invalid name %q: use lowercase letters, digits and dashes, starting with a letter", name)
			}
			if kind != "backend" && kind != "ui" {
				return fmt.Errorf("--type must be backend or ui")
			}
			if module == "" {
				module = "github.com/example/" + name
			}
			target := filepath.Join(dir, name)
			if _, err := os.Stat(target); err == nil {
				return fmt.Errorf("directory %s already exists", target)
			}
			if err := extension.Scaffold(target, extension.Params{
				Name: name, Kind: kind, Module: module, Pascal: pascalCase(name),
				BackendContract: extension.BackendContract, UIHostAPI: extension.UIHostAPI, UISDKRange: extension.UISDKRange,
			}); err != nil {
				return err
			}
			if warning := extensionContractWarning(cmd, opts, kind); warning != "" {
				fmt.Fprintf(opts.ErrOut, "Warning: %s\n", warning)
			}
			fmt.Fprintf(opts.Out, "Scaffolded %s extension %q in %s\n", kind, name, target)
			return nil
		},
	}
	c.Flags().StringVar(&kind, "type", "", "Extension type: backend|ui (required)")
	c.Flags().StringVar(&dir, "dir", ".", "Parent directory for the new extension")
	c.Flags().StringVar(&module, "module", "", "Go module path (backend; default github.com/example/<name>)")
	_ = c.MarkFlagRequired("type")
	return c
}

// extensionContractWarning is intentionally best-effort: metadata discovery is
// unavailable on old servers and offline scaffolds must still succeed.
func extensionContractWarning(cmd *cobra.Command, opts *GlobalOptions, kind string) string {
	server := strings.TrimSpace(opts.ServerFlag)
	if server == "" {
		server = strings.TrimSpace(os.Getenv("INARI_SERVER"))
	}
	if server == "" {
		_, cc, err := opts.resolveContext()
		if err != nil {
			return ""
		}
		server = strings.TrimSpace(cc.Server)
	}
	if server == "" {
		return ""
	}
	metadata, err := api.GetContractMetadata(cmd.Context(), server)
	if err != nil {
		return fmt.Sprintf("extension contract check unavailable: %v", err)
	}
	if metadata == nil {
		return ""
	}
	declared := extension.BackendContract
	var supported []string
	if kind == "ui" {
		declared = extension.UIHostAPI
		if metadata.UiHostApis != nil {
			supported = *metadata.UiHostApis
		}
	} else if metadata.PluginContracts != nil {
		supported = *metadata.PluginContracts
	}
	if !extension.ContractIntersection(declared, supported) {
		return fmt.Sprintf("generated extension declares %s, but server advertises [%s]", declared, strings.Join(supported, ", "))
	}
	return ""
}

func pascalCase(name string) string {
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' })
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}

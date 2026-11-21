package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/7K-Inari/inari-cli/internal/hiroba"
)

func newHirobaCmd(opts *GlobalOptions) *cobra.Command {
	command := &cobra.Command{
		Use:   "hiroba",
		Short: "Scaffold Hiroba community applications",
	}
	command.AddCommand(newHirobaInitCmd(opts))
	return command
}

func effectiveHirobaServer(opts *GlobalOptions) string {
	if server := strings.TrimSpace(opts.ServerFlag); server != "" {
		return server
	}
	if server := strings.TrimSpace(os.Getenv("INARI_SERVER")); server != "" {
		return server
	}
	_, current, err := opts.resolveContext()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(current.Server)
}

func newHirobaInitCmd(opts *GlobalOptions) *cobra.Command {
	var options hiroba.Options
	var dir string
	command := &cobra.Command{
		Use:   "init NAME",
		Short: "Scaffold a Hiroba community application repository",
		Args:  cobra.ExactArgs(1),
		Example: `  inari hiroba init my-service
  inari hiroba init my-service --type app --with-plugin --with-ui --with-chart --with-observability --with-runtime-smoke`,
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Name = args[0]
			if cmd.Flags().Changed("type") && options.Type == "" {
				return fmt.Errorf("invalid type %q: want app|extension|catalog|template", options.Type)
			}
			if cmd.Flags().Changed("module") && strings.TrimSpace(options.Module) == "" {
				return fmt.Errorf("invalid Go module path %q: path must not be empty", options.Module)
			}
			target := filepath.Join(dir, options.Name)
			if err := hiroba.Scaffold(target, options); err != nil {
				return err
			}
			if server := effectiveHirobaServer(opts); server != "" {
				hiroba.WarnOnContractMismatch(cmd.Context(), server, options, opts.ErrOut)
			}
			fmt.Fprintf(opts.Out, "Scaffolded Hiroba %s %q in %s\n", options.Type, options.Name, target)
			return nil
		},
	}
	command.Flags().StringVar(&options.Type, "type", "app", "Repository type: app|extension|catalog|template")
	command.Flags().StringVar(&dir, "dir", ".", "Parent directory for the new repository")
	command.Flags().StringVar(&options.Module, "module", "", "Go module path (default github.com/7K-Hiroba/<name>)")
	command.Flags().BoolVar(&options.WithPlugin, "with-plugin", false, "Include a backend plugin component")
	command.Flags().BoolVar(&options.WithUI, "with-ui", false, "Include a UI plugin component")
	command.Flags().BoolVar(&options.WithChart, "with-chart", false, "Include a first-party Helm chart")
	command.Flags().BoolVar(&options.WithObservability, "with-observability", false, "Include canonical observability and the companion pack")
	command.Flags().BoolVar(&options.WithRuntimeSmoke, "with-runtime-smoke", false, "Include a signed Argo runtime smoke suite")
	return command
}

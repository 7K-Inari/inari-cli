package cmd

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/7K-Inari/inari-cli/internal/config"
)

type GlobalOptions struct {
	Out    io.Writer
	ErrOut io.Writer

	ContextFlag string
	ServerFlag  string
	Output      string
	Verbose     bool
}

func NewRootCmd(version, commit, date string, out, errOut io.Writer) *cobra.Command {
	opts := &GlobalOptions{Out: out, ErrOut: errOut}

	root := &cobra.Command{
		Use:   "inari",
		Short: "The Inari platform CLI",
		Long: `inari is the command-line interface for the Inari Internal Developer Platform.

Core flows:
  login       Authenticate via OIDC device flow against your platform Keycloak
  cluster     Register Kubernetes clusters and print the agent install manifest
  catalog     Browse the tenant/cluster-filtered service catalog
  deploy      Deploy a catalog item (interactive wizard or --file/--set for CI)
  resources   Inspect resource instances across clusters with health/status
  extension   Scaffold a backend or UI extension

Configuration lives in ~/.config/inari/ with kubectl-style contexts
(per-context control-plane server and tenant).`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.SetOut(out)
	root.SetErr(errOut)

	root.PersistentFlags().StringVar(&opts.ContextFlag, "context", "", "Use a named context instead of the current one")
	root.PersistentFlags().StringVar(&opts.ServerFlag, "server", "", "Control-plane base URL (overrides context; env INARI_SERVER)")
	root.PersistentFlags().StringVarP(&opts.Output, "output", "o", "table", "Output format: table|json|yaml")
	root.PersistentFlags().BoolVarP(&opts.Verbose, "verbose", "v", false, "Verbose logging to stderr")

	root.AddCommand(newVersionCmd(version, commit, date, out))
	root.AddCommand(newLoginCmd(opts))
	root.AddCommand(newLogoutCmd(opts))
	root.AddCommand(newContextCmd(opts))
	root.AddCommand(newClusterCmd(opts))
	root.AddCommand(newCatalogCmd(opts))
	root.AddCommand(newDeployCmd(opts))
	root.AddCommand(newResourcesCmd(opts))
	root.AddCommand(newExtensionCmd(opts))

	return root
}

func newVersionCmd(version, commit, date string, out io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the inari CLI version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintf(out, "inari %s (commit %s, built %s)\n", version, commit, date)
		},
	}
}

func (o *GlobalOptions) resolveContext() (string, config.Context, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", config.Context{}, err
	}
	name := o.ContextFlag
	if name == "" {
		var ctx config.Context
		name, ctx, err = cfg.Current()
		if err != nil {
			return "", config.Context{}, err
		}
		if o.ServerFlag != "" {
			ctx.Server = o.ServerFlag
		}
		return name, ctx, nil
	}
	ctx, ok := cfg.Contexts[name]
	if !ok {
		return "", config.Context{}, fmt.Errorf("context %q not found", name)
	}
	if o.ServerFlag != "" {
		ctx.Server = o.ServerFlag
	}
	return name, ctx, nil
}

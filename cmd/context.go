package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/7K-Inari/inari-cli/internal/config"
)

func newContextCmd(opts *GlobalOptions) *cobra.Command {
	c := &cobra.Command{
		Use:   "context",
		Short: "Manage config contexts (server/issuer/tenant per context)",
		Long: `Manage kubectl-style contexts stored in ~/.config/inari/config.yaml.

A context pairs a control-plane server with a Keycloak issuer and a tenant.
'inari login' creates or updates a context and makes it current.`,
	}
	c.AddCommand(newContextListCmd(opts), newContextUseCmd(opts), newContextSetCmd(opts))
	return c
}

func newContextListCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List configured contexts",
		Example: "  inari context list",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if len(cfg.Contexts) == 0 {
				fmt.Fprintln(opts.Out, "No contexts configured. Run 'inari login' to create one.")
				return nil
			}
			for name, ctx := range cfg.Contexts {
				marker := "  "
				if name == cfg.CurrentContext {
					marker = "* "
				}
				fmt.Fprintf(opts.Out, "%s%s\tserver=%s", marker, name, ctx.Server)
				if ctx.Tenant != "" {
					fmt.Fprintf(opts.Out, "\ttenant=%s", ctx.Tenant)
				}
				fmt.Fprintln(opts.Out)
			}
			return nil
		},
	}
}

func newContextUseCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "use NAME",
		Short:   "Switch the current context",
		Args:    cobra.ExactArgs(1),
		Example: "  inari context use prod",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if _, ok := cfg.Contexts[args[0]]; !ok {
				return fmt.Errorf("context %q not found", args[0])
			}
			cfg.CurrentContext = args[0]
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Fprintf(opts.Out, "Current context is now %q.\n", args[0])
			return nil
		},
	}
}

func newContextSetCmd(opts *GlobalOptions) *cobra.Command {
	var server, issuer, tenant string
	c := &cobra.Command{
		Use:   "set NAME",
		Short: "Create or update a context without logging in",
		Args:  cobra.ExactArgs(1),
		Example: `  inari context set prod --server https://api.inari.example.com --tenant acme`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			ctx := cfg.Contexts[args[0]]
			if server != "" {
				ctx.Server = server
			}
			if issuer != "" {
				ctx.Issuer = issuer
			}
			if tenant != "" {
				ctx.Tenant = tenant
			}
			if ctx.Server == "" {
				return fmt.Errorf("context %q has no server; pass --server", args[0])
			}
			cfg.SetContext(args[0], ctx)
			if cfg.CurrentContext == "" {
				cfg.CurrentContext = args[0]
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Fprintf(opts.Out, "Context %q saved.\n", args[0])
			return nil
		},
	}
	c.Flags().StringVar(&server, "server", "", "Control-plane base URL")
	c.Flags().StringVar(&issuer, "issuer", "", "Keycloak issuer URL")
	c.Flags().StringVar(&tenant, "tenant", "", "Tenant slug")
	return c
}

package cmd

import (
	"fmt"
	"strings"

	"github.com/7K-Inari/inari-api/gen/go/oas"
	"github.com/spf13/cobra"
)

func newClusterCmd(opts *GlobalOptions) *cobra.Command {
	c := &cobra.Command{
		Use:   "cluster",
		Short: "Register and inspect tenant Kubernetes clusters",
	}
	c.AddCommand(newClusterRegisterCmd(opts), newClusterListCmd(opts))
	return c
}

func newClusterRegisterCmd(opts *GlobalOptions) *cobra.Command {
	var labels []string
	c := &cobra.Command{
		Use:   "register NAME",
		Short: "Register a cluster and print the agent install manifest",
		Long: `Register a Kubernetes cluster with the Inari control plane and print the
agent install manifest. The manifest embeds a one-time, TTL'd registration
token; the agent exchanges it for a per-cluster OIDC client on first connect.

The manifest is printed to stdout by default so it can be piped directly:

  inari cluster register prod-eu | kubectl apply -f -`,
		Args: cobra.ExactArgs(1),
		Example: `  inari cluster register prod-eu
  inari cluster register prod-eu --label env=prod --label region=eu-west-1 > prod-eu-agent.yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, cc, err := opts.resolveContext()
			if err != nil {
				return err
			}
			if err := requireTenant(cc); err != nil {
				return err
			}
			client, err := newAPIClient(cmd, opts, cc)
			if err != nil {
				return err
			}

			body := oas.CreateClusterJSONRequestBody{Name: args[0]}
			if len(labels) > 0 {
				m := map[string]string{}
				for _, l := range labels {
					k, v, ok := strings.Cut(l, "=")
					if !ok || k == "" {
						return fmt.Errorf("invalid --label %q, want key=value", l)
					}
					m[k] = v
				}
				body.Labels = &m
			}

			rsp, err := client.OAS.CreateClusterWithResponse(cmd.Context(), cc.Tenant, body)
			if err != nil {
				return err
			}
			if rsp.JSON200 == nil {
				return apiError(rsp.Status(), rsp.ApplicationproblemJSONDefault)
			}
			cluster := rsp.JSON200.Cluster
			fmt.Fprintf(opts.ErrOut, "Cluster %q registered (id %s, state %s).\n", cluster.Name, cluster.Id, cluster.State)

			manifest, err := client.OAS.RenderInstallManifestWithResponse(cmd.Context(), cc.Tenant, cluster.Id)
			if err != nil {
				return err
			}
			if manifest.JSON200 == nil {
				return apiError(manifest.Status(), manifest.ApplicationproblemJSONDefault)
			}
			if _, err := opts.Out.Write(*manifest.JSON200); err != nil {
				return err
			}
			fmt.Fprintln(opts.ErrOut, "\nThe manifest embeds a one-time registration token — treat it as a secret.")
			return nil
		},
	}
	c.Flags().StringArrayVar(&labels, "label", nil, "Cluster label key=value (repeatable)")
	return c
}

func newClusterListCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List registered clusters with connection health",
		Example: "  inari cluster list",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, cc, err := opts.resolveContext()
			if err != nil {
				return err
			}
			client, err := newAPIClient(cmd, opts, cc)
			if err != nil {
				return err
			}
			rsp, err := client.OAS.ListClustersWithResponse(cmd.Context(), cc.Tenant)
			if err != nil {
				return err
			}
			if rsp.JSON200 == nil {
				return apiError(rsp.Status(), rsp.ApplicationproblemJSONDefault)
			}
			clusters := rsp.JSON200.Clusters
			if clusters == nil {
				clusters = &[]oas.Cluster{}
			}
			return renderClusters(opts, *clusters)
		},
	}
}

func renderClusters(opts *GlobalOptions, clusters []oas.Cluster) error {
	if opts.Output == "json" || opts.Output == "yaml" {
		return printStructured(opts, clusters)
	}
	tw := newTable(opts.Out)
	fmt.Fprintln(tw, "NAME\tID\tSTATE\tK8S VERSION\tLAST SEEN")
	for _, c := range clusters {
		lastSeen := ""
		if c.LastSeenAt != nil {
			lastSeen = c.LastSeenAt.Format("2006-01-02 15:04")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", c.Name, c.Id, c.State, deref(c.KubernetesVersion), lastSeen)
	}
	return tw.Flush()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

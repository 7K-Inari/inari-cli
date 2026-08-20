package cmd

import (
	"fmt"

	"github.com/7K-Inari/inari-api/gen/go/oas"
	"github.com/spf13/cobra"
)

func newResourcesCmd(opts *GlobalOptions) *cobra.Command {
	c := &cobra.Command{
		Use:   "resources",
		Short: "Inspect resource instances across clusters",
		Long: `List and inspect resource instances (things deployed from the catalog)
across all clusters in the tenant, with health and sync status streamed back
by the in-cluster agents.`,
	}
	c.AddCommand(newResourcesListCmd(opts), newResourcesGetCmd(opts))
	return c
}

func newResourcesListCmd(opts *GlobalOptions) *cobra.Command {
	var cluster, item, health, ownerTeam string
	c := &cobra.Command{
		Use:     "list",
		Short:   "List resource instances with health/status",
		Example: "  inari resources list\n  inari resources list --cluster clu-1 --health Degraded",
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
			params := &oas.ListInstancesParams{}
			if cluster != "" {
				params.Cluster = &cluster
			}
			if item != "" {
				params.Item = &item
			}
			if health != "" {
				params.Health = &health
			}
			if ownerTeam != "" {
				params.OwnerTeam = &ownerTeam
			}
			rsp, err := client.OAS.ListInstancesWithResponse(cmd.Context(), cc.Tenant, params)
			if err != nil {
				return err
			}
			if rsp.JSON200 == nil {
				return apiError(rsp.Status(), rsp.ApplicationproblemJSONDefault)
			}
			instances := rsp.JSON200.Instances
			if instances == nil {
				instances = &[]oas.InstanceView{}
			}
			if opts.Output == "json" || opts.Output == "yaml" {
				return printStructured(opts, *instances)
			}
			tw := newTable(opts.Out)
			fmt.Fprintln(tw, "ID\tITEM\tCLUSTER\tHEALTH\tSTATE\tVERSION\tUPDATE")
			for _, in := range *instances {
				update := ""
				if in.NewVersionAvailable && in.LatestVersion != nil {
					update = "-> " + *in.LatestVersion
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					in.Id, in.CatalogItemId, in.ClusterId, in.Health, in.State, in.Version, update)
			}
			return tw.Flush()
		},
	}
	c.Flags().StringVar(&cluster, "cluster", "", "Filter by cluster ID")
	c.Flags().StringVar(&item, "item", "", "Filter by catalog item")
	c.Flags().StringVar(&health, "health", "", "Filter by health (Healthy|Degraded|...)")
	c.Flags().StringVar(&ownerTeam, "owner-team", "", "Filter by owning team")
	return c
}

func newResourcesGetCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "get ID",
		Short:   "Show details of a single resource instance",
		Args:    cobra.ExactArgs(1),
		Example: "  inari resources get inst-1\n  inari resources get inst-1 -o yaml",
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
			rsp, err := client.OAS.GetInstanceWithResponse(cmd.Context(), cc.Tenant, args[0])
			if err != nil {
				return err
			}
			if rsp.JSON200 == nil {
				return apiError(rsp.Status(), rsp.ApplicationproblemJSONDefault)
			}
			in := rsp.JSON200.Instance
			if opts.Output == "json" || opts.Output == "yaml" {
				return printStructured(opts, in)
			}
			fmt.Fprintf(opts.Out, "ID:           %s\n", in.Id)
			fmt.Fprintf(opts.Out, "Item:         %s@%s\n", in.CatalogItemId, in.Version)
			fmt.Fprintf(opts.Out, "Cluster:      %s\n", in.ClusterId)
			fmt.Fprintf(opts.Out, "Health:       %s\n", in.Health)
			fmt.Fprintf(opts.Out, "State:        %s\n", in.State)
			if in.SyncState != nil {
				fmt.Fprintf(opts.Out, "Sync:         %s\n", *in.SyncState)
			}
			if in.StatusMessage != nil {
				fmt.Fprintf(opts.Out, "Status:       %s\n", *in.StatusMessage)
			}
			fmt.Fprintf(opts.Out, "Owner team:   %s\n", in.OwnerTeam)
			fmt.Fprintf(opts.Out, "Resource:     %s/%s", in.ResourceRef.Kind, in.ResourceRef.Name)
			if in.ResourceRef.Namespace != nil {
				fmt.Fprintf(opts.Out, " (ns %s)", *in.ResourceRef.Namespace)
			}
			fmt.Fprintln(opts.Out)
			if in.NewVersionAvailable && in.LatestVersion != nil {
				fmt.Fprintf(opts.Out, "Update:       %s available\n", *in.LatestVersion)
			}
			if in.PrUrl != nil {
				fmt.Fprintf(opts.Out, "Pull request: %s\n", *in.PrUrl)
			}
			return nil
		},
	}
}

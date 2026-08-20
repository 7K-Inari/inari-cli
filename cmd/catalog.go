package cmd

import (
	"fmt"
	"strings"

	"github.com/7K-Inari/inari-api/gen/go/oas"
	"github.com/spf13/cobra"
)

func newCatalogCmd(opts *GlobalOptions) *cobra.Command {
	c := &cobra.Command{
		Use:   "catalog",
		Short: "Browse the tenant service catalog",
		Long: `Browse catalog items visible to your tenant. Items come from capabilities
discovered in your clusters and from curated packages. Use --cluster to
intersect with what a specific cluster can actually run.`,
	}
	c.AddCommand(newCatalogListCmd(opts), newCatalogDescribeCmd(opts))
	return c
}

func newCatalogListCmd(opts *GlobalOptions) *cobra.Command {
	var cluster string
	c := &cobra.Command{
		Use:     "list",
		Short:   "List catalog items visible to the tenant (optionally per cluster)",
		Example: "  inari catalog list\n  inari catalog list --cluster clu-1",
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
			params := &oas.ListCatalogParams{}
			if cluster != "" {
				params.Cluster = &cluster
			}
			rsp, err := client.OAS.ListCatalogWithResponse(cmd.Context(), cc.Tenant, params)
			if err != nil {
				return err
			}
			if rsp.JSON200 == nil {
				return apiError(rsp.Status(), rsp.ApplicationproblemJSONDefault)
			}
			items := rsp.JSON200.Items
			if items == nil {
				items = &[]oas.ItemView{}
			}
			if opts.Output == "json" || opts.Output == "yaml" {
				return printStructured(opts, *items)
			}
			tw := newTable(opts.Out)
			fmt.Fprintln(tw, "NAME\tDISPLAY NAME\tSOURCE\tVERSIONS\tAPPROVAL")
			for _, it := range *items {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
					it.Name, it.DisplayName, it.Source, catalogVersions(it), it.ApprovalPolicy)
			}
			return tw.Flush()
		},
	}
	c.Flags().StringVar(&cluster, "cluster", "", "Cluster ID; intersects discovered capabilities")
	return c
}

func newCatalogDescribeCmd(opts *GlobalOptions) *cobra.Command {
	var cluster string
	c := &cobra.Command{
		Use:   "describe ITEM",
		Short: "Show details, versions and schema of a catalog item",
		Long: `Show a catalog item's details including available versions, approval
policy, and (with -o json|yaml) the full OpenAPI v3 schema and UI hints used
by 'inari deploy' to generate prompts.`,
		Args:    cobra.ExactArgs(1),
		Example: "  inari catalog describe postgres-aws\n  inari catalog describe postgres-aws -o yaml",
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
			params := &oas.GetCatalogItemParams{}
			if cluster != "" {
				params.Cluster = &cluster
			}
			rsp, err := client.OAS.GetCatalogItemWithResponse(cmd.Context(), cc.Tenant, args[0], params)
			if err != nil {
				return err
			}
			if rsp.JSON200 == nil {
				return apiError(rsp.Status(), rsp.ApplicationproblemJSONDefault)
			}
			it := rsp.JSON200.Item
			if opts.Output == "json" || opts.Output == "yaml" {
				return printStructured(opts, it)
			}
			fmt.Fprintf(opts.Out, "Name:        %s\n", it.Name)
			fmt.Fprintf(opts.Out, "Display:     %s\n", it.DisplayName)
			fmt.Fprintf(opts.Out, "ID:          %s\n", it.Id)
			fmt.Fprintf(opts.Out, "Source:      %s\n", it.Source)
			fmt.Fprintf(opts.Out, "Approval:    %s\n", it.ApprovalPolicy)
			if it.PinnedVersion != nil {
				fmt.Fprintf(opts.Out, "Pinned:      %s\n", *it.PinnedVersion)
			}
			if it.Description != "" {
				fmt.Fprintf(opts.Out, "Description: %s\n", it.Description)
			}
			if it.Versions != nil && len(*it.Versions) > 0 {
				fmt.Fprintln(opts.Out, "Versions:")
				for _, v := range *it.Versions {
					fmt.Fprintf(opts.Out, "  %s (channel %s)\n", v.Version, v.Channel)
				}
			}
			return nil
		},
	}
	c.Flags().StringVar(&cluster, "cluster", "", "Cluster ID; intersects discovered capabilities")
	return c
}

func catalogVersions(it oas.ItemView) string {
	if it.Versions == nil {
		return ""
	}
	var vs []string
	for _, v := range *it.Versions {
		vs = append(vs, v.Version)
	}
	return strings.Join(vs, ", ")
}

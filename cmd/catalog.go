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
			fmt.Fprintln(tw, "NAME\tDISPLAY NAME\tID\tSOURCE\tTRUST\tVERSIONS\tAPPROVAL")
			for _, it := range *items {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					it.Name, it.DisplayName, it.Id, catalogSource(it), catalogTrust(it), catalogVersions(it), it.ApprovalPolicy)
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
		Example: "  inari catalog describe core/postgres-aws\n  inari catalog describe core/postgres-aws -o yaml",
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
			itemID, err := resolveCatalogItemID(cmd.Context(), client, cc.Tenant, cluster, args[0])
			if err != nil {
				return err
			}
			it, err := getCatalogItem(cmd.Context(), client, cc.Tenant, itemID, cluster)
			if err != nil {
				return err
			}
			if opts.Output == "json" || opts.Output == "yaml" {
				return printStructured(opts, it)
			}
			fmt.Fprintf(opts.Out, "Name:        %s\n", it.Name)
			fmt.Fprintf(opts.Out, "Display:     %s\n", it.DisplayName)
			fmt.Fprintf(opts.Out, "ID:          %s\n", it.Id)
			fmt.Fprintf(opts.Out, "Source:      %s\n", catalogSource(*it))
			if it.SourceId != nil && *it.SourceId != "" {
				fmt.Fprintf(opts.Out, "Kind:        %s\n", it.Source)
			}
			fmt.Fprintf(opts.Out, "Trust:       %s\n", catalogTrust(*it))
			if it.Unsigned != nil && *it.Unsigned {
				fmt.Fprintln(opts.Out, "Unsigned:    development only")
			}
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
					if v.ArtifactDigest != nil && *v.ArtifactDigest != "" {
						fmt.Fprintf(opts.Out, "    Artifact digest: %s\n", *v.ArtifactDigest)
					}
					if v.ChartDigest != nil && *v.ChartDigest != "" {
						fmt.Fprintf(opts.Out, "    Chart digest:    %s\n", *v.ChartDigest)
					}
				}
			}
			return nil
		},
	}
	c.Flags().StringVar(&cluster, "cluster", "", "Cluster ID; intersects discovered capabilities")
	return c
}

func catalogSource(it oas.ItemView) string {
	if it.SourceId != nil && *it.SourceId != "" {
		return *it.SourceId
	}
	return it.Source
}

func catalogTrust(it oas.ItemView) string {
	if it.TrustTier != nil && *it.TrustTier != "" {
		return *it.TrustTier
	}
	return "-"
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

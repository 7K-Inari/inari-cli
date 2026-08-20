package cmd

import (
	"fmt"
	"strings"

	"github.com/7K-Inari/inari-api/gen/go/oas"
	"github.com/spf13/cobra"

	"github.com/7K-Inari/inari-cli/internal/prompt"
	"github.com/7K-Inari/inari-cli/internal/schema"
)

func newDeployCmd(opts *GlobalOptions) *cobra.Command {
	var (
		clusterID string
		version   string
		channel   string
		name      string
		namespace string
		ownerTeam string
		file      string
		sets      []string
		dryRun    bool
	)
	c := &cobra.Command{
		Use:   "deploy ITEM",
		Short: "Deploy a catalog item to a cluster",
		Long: `Deploy a catalog item into one of your clusters.

With --file and/or --set the deploy runs non-interactively (CI-friendly):

  inari deploy postgres-aws --cluster clu-1 --file values.yaml --set size=large

Without them, an interactive wizard walks the item's OpenAPI v3 schema and
prompts for each field (required fields first).

Use --dry-run to evaluate request-time policies without deploying; the
server's decision, including the reason and remediation when denied, is
printed.`,
		Args: cobra.ExactArgs(1),
		Example: `  inari deploy postgres-aws --cluster clu-1
  inari deploy postgres-aws --cluster clu-1 --file values.yaml --set replicas=3
  inari deploy postgres-aws --cluster clu-1 --file values.yaml --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, cc, err := opts.resolveContext()
			if err != nil {
				return err
			}
			if err := requireTenant(cc); err != nil {
				return err
			}
			if clusterID == "" {
				return fmt.Errorf("--cluster is required (see 'inari cluster list')")
			}
			client, err := newAPIClient(cmd, opts, cc)
			if err != nil {
				return err
			}
			ctx := cmd.Context()

			itemRsp, err := client.OAS.GetCatalogItemWithResponse(ctx, cc.Tenant, args[0], &oas.GetCatalogItemParams{Cluster: &clusterID})
			if err != nil {
				return err
			}
			if itemRsp.JSON200 == nil {
				return apiError(itemRsp.Status(), itemRsp.ApplicationproblemJSONDefault)
			}
			item := itemRsp.JSON200.Item
			rawSchema := pickSchema(item, version)
			if rawSchema == nil && version != "" {
				return fmt.Errorf("version %q not found for item %q (see 'inari catalog describe %s')", version, args[0], args[0])
			}
			fields, err := schema.Walk(rawSchema)
			if err != nil {
				return fmt.Errorf("reading item schema: %w", err)
			}

			var spec map[string]any
			switch {
			case file != "" || len(sets) > 0:
				spec = map[string]any{}
				if file != "" {
					spec, err = prompt.LoadFile(file)
					if err != nil {
						return err
					}
				}
				if err := prompt.ApplySets(spec, sets); err != nil {
					return err
				}
				if missing := schema.Validate(fields, spec); len(missing) > 0 {
					return fmt.Errorf("missing required values: %s (provide via --file or --set)", strings.Join(missing, ", "))
				}
			default:
				if len(fields) == 0 {
					return fmt.Errorf("item %q has no promptable schema; use --file/--set for non-interactive deploy", args[0])
				}
				fmt.Fprintf(opts.ErrOut, "Starting interactive deploy of %q (ctrl-c to abort)\n", args[0])
				spec, err = prompt.Collect(prompt.SurveyPrompter{}, fields)
				if err != nil {
					return err
				}
				if missing := schema.Validate(fields, spec); len(missing) > 0 {
					return fmt.Errorf("missing required values: %s", strings.Join(missing, ", "))
				}
			}

			if dryRun {
				evalBody := oas.EvaluateInputBody{
					ClusterId: clusterID,
					ItemId:    args[0],
					Spec:      spec,
					Version:   version,
				}
				rsp, err := client.OAS.EvaluatePoliciesWithResponse(ctx, cc.Tenant, evalBody)
				if err != nil {
					return err
				}
				if rsp.JSON200 == nil {
					return apiError(rsp.Status(), rsp.ApplicationproblemJSONDefault)
				}
				return printDecision(opts, rsp.JSON200.Decision)
			}

			deployBody := oas.DeployCatalogItemJSONRequestBody{
				ClusterId: clusterID,
				ItemId:    args[0],
				Spec:      spec,
			}
			if version != "" {
				deployBody.Version = &version
			}
			if channel != "" {
				deployBody.Channel = &channel
			}
			if name != "" {
				deployBody.Name = &name
			}
			if namespace != "" {
				deployBody.Namespace = &namespace
			}
			if ownerTeam != "" {
				deployBody.OwnerTeam = &ownerTeam
			}
			rsp, err := client.OAS.DeployCatalogItemWithResponse(ctx, cc.Tenant, deployBody)
			if err != nil {
				return err
			}
			if rsp.JSON200 == nil {
				return apiError(rsp.Status(), rsp.ApplicationproblemJSONDefault)
			}
			d := rsp.JSON200.Deploy
			fmt.Fprintf(opts.Out, "Deploy accepted: instance %s (status %s, version %s)\n", d.InstanceID, d.Status, d.Version)
			if d.CommitSHA != "" {
				fmt.Fprintf(opts.Out, "Commit: %s\n", d.CommitSHA)
			}
			if d.PRURL != "" {
				fmt.Fprintf(opts.Out, "Pull request: %s\n", d.PRURL)
			}
			if d.ApprovalID != "" {
				fmt.Fprintf(opts.Out, "Approval required: %s\n", d.ApprovalID)
			}
			return nil
		},
	}
	c.Flags().StringVar(&clusterID, "cluster", "", "Target cluster ID (required)")
	c.Flags().StringVar(&version, "version", "", "Catalog item version (default: tenant pin or latest stable)")
	c.Flags().StringVar(&channel, "channel", "", "Version channel (stable|incubating)")
	c.Flags().StringVar(&name, "name", "", "Instance name (DNS-1123 label)")
	c.Flags().StringVar(&namespace, "namespace", "", "Target namespace")
	c.Flags().StringVar(&ownerTeam, "owner-team", "", "Owning team")
	c.Flags().StringVarP(&file, "file", "f", "", "YAML values file (non-interactive)")
	c.Flags().StringArrayVar(&sets, "set", nil, "Set a value key=value, dot paths supported (repeatable)")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "Evaluate request-time policies without deploying")
	return c
}

func printDecision(opts *GlobalOptions, d oas.PolicyDecision) error {
	if opts.Output == "json" || opts.Output == "yaml" {
		return printStructured(opts, d)
	}
	if d.Allow {
		fmt.Fprintln(opts.Out, "Policy decision: ALLOWED")
	} else {
		fmt.Fprintln(opts.Out, "Policy decision: DENIED")
	}
	printViolations := func(label string, vs *[]oas.PolicyViolation) {
		if vs == nil {
			return
		}
		for _, v := range *vs {
			msg := v.Reason
			if v.Rule != "" {
				msg = v.Rule + ": " + msg
			}
			if v.Remediation != "" {
				msg += " (remediation: " + v.Remediation + ")"
			}
			fmt.Fprintf(opts.Out, "%s: %s\n", label, msg)
		}
	}
	printViolations("Violation", d.Violations)
	printViolations("Warning", d.Warnings)
	return nil
}

func pickSchema(item oas.ItemView, version string) any {
	derefSchema := func(s *interface{}) any {
		if s == nil {
			return nil
		}
		return *s
	}
	if item.Versions == nil {
		return nil
	}
	if version != "" {
		for _, v := range *item.Versions {
			if v.Version == version {
				return derefSchema(v.Schema)
			}
		}
		return nil
	}
	for _, v := range *item.Versions {
		if v.Channel == "stable" {
			return derefSchema(v.Schema)
		}
	}
	return derefSchema((*item.Versions)[0].Schema)
}

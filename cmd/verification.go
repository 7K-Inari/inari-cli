package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/7K-Inari/inari-api/gen/go/oas"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newVerificationCmd(opts *GlobalOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "verification", Short: "Configure and inspect signed-suite environment verification"}
	cmd.AddCommand(
		newVerificationStatusCmd(opts), newVerificationPreviewCmd(opts), newVerificationSetCmd(opts),
		newVerificationRunCmd(opts), newVerificationRunsCmd(opts), newVerificationRunStatusCmd(opts),
		newVerificationPromotionPreviewCmd(opts),
	)
	return cmd
}

func newVerificationStatusCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{Use: "status INSTANCE", Short: "Show current verification policy, gate, and recent runs", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
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
		rsp, err := client.OAS.GetInstanceVerificationWithResponse(cmd.Context(), cc.Tenant, args[0])
		if err != nil {
			return err
		}
		if rsp.JSON200 == nil {
			return apiError(rsp.Status(), rsp.ApplicationproblemJSONDefault)
		}
		if opts.Output == "json" || opts.Output == "yaml" {
			return printStructured(opts, rsp.JSON200)
		}
		fmt.Fprintf(opts.Out, "Render available: %t\n", rsp.JSON200.Gate.RenderAvailable)
		if rsp.JSON200.Gate.Reason != nil {
			fmt.Fprintf(opts.Out, "Gate:           %s\n", *rsp.JSON200.Gate.Reason)
		}
		if rsp.JSON200.Policy == nil {
			fmt.Fprintln(opts.Out, "Policy:         none")
			return nil
		}
		p := rsp.JSON200.Policy
		fmt.Fprintf(opts.Out, "Policy:         revision %d (enabled=%t)\n", p.PolicyRevision, p.Enabled)
		fmt.Fprintf(opts.Out, "Suite:          %s (%s)\n", p.Binding.SuiteDigest, p.Binding.Profile)
		return nil
	}}
}

func loadVerificationPolicy(path string) (*oas.VerificationPolicy, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var document map[string]any
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("decode policy YAML: %w", err)
	}
	canonical, err := json.Marshal(document)
	if err != nil {
		return nil, err
	}
	var policy oas.VerificationPolicy
	if err := json.Unmarshal(canonical, &policy); err != nil {
		return nil, fmt.Errorf("decode verification policy: %w", err)
	}
	return &policy, nil
}

func previewVerification(cmd *cobra.Command, opts *GlobalOptions, instanceID string, policy *oas.VerificationPolicy) (*oas.PolicyPreview, error) {
	_, cc, err := opts.resolveContext()
	if err != nil {
		return nil, err
	}
	if err := requireTenant(cc); err != nil {
		return nil, err
	}
	client, err := newAPIClient(cmd, opts, cc)
	if err != nil {
		return nil, err
	}
	rsp, err := client.OAS.PreviewInstanceVerificationWithResponse(cmd.Context(), cc.Tenant, instanceID, *policy)
	if err != nil {
		return nil, err
	}
	if rsp.JSON200 == nil {
		return nil, apiError(rsp.Status(), rsp.ApplicationproblemJSONDefault)
	}
	return rsp.JSON200, nil
}

func printPolicyPreview(opts *GlobalOptions, preview *oas.PolicyPreview) error {
	if opts.Output == "json" || opts.Output == "yaml" {
		return printStructured(opts, preview)
	}
	fmt.Fprintf(opts.Out, "Render available: %t\n", preview.Gate.RenderAvailable)
	fmt.Fprintf(opts.Out, "Side effects:     %d\n", preview.SideEffectsPerformed)
	fmt.Fprintf(opts.Out, "Suite:            %s (%s)\n", preview.Policy.Binding.SuiteDigest, preview.Policy.Binding.Profile)
	if preview.Warnings != nil {
		for _, warning := range *preview.Warnings {
			fmt.Fprintf(opts.Out, "Warning:          %s\n", warning)
		}
	}
	return nil
}

func newVerificationPreviewCmd(opts *GlobalOptions) *cobra.Command {
	var file string
	cmd := &cobra.Command{Use: "preview INSTANCE", Short: "Preview a policy with zero side effects", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		policy, err := loadVerificationPolicy(file)
		if err != nil {
			return err
		}
		preview, err := previewVerification(cmd, opts, args[0], policy)
		if err != nil {
			return err
		}
		return printPolicyPreview(opts, preview)
	}}
	cmd.Flags().StringVarP(&file, "file", "f", "", "Verification policy YAML or JSON file")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func newVerificationSetCmd(opts *GlobalOptions) *cobra.Command {
	var file string
	var dryRun bool
	cmd := &cobra.Command{Use: "set INSTANCE", Short: "Preview then save a verification policy revision", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		policy, err := loadVerificationPolicy(file)
		if err != nil {
			return err
		}
		preview, err := previewVerification(cmd, opts, args[0], policy)
		if err != nil {
			return err
		}
		if dryRun {
			return printPolicyPreview(opts, preview)
		}
		_, cc, err := opts.resolveContext()
		if err != nil {
			return err
		}
		client, err := newAPIClient(cmd, opts, cc)
		if err != nil {
			return err
		}
		rsp, err := client.OAS.PutInstanceVerificationWithResponse(cmd.Context(), cc.Tenant, args[0], *policy)
		if err != nil {
			return err
		}
		if rsp.JSON200 == nil {
			return apiError(rsp.Status(), rsp.ApplicationproblemJSONDefault)
		}
		if opts.Output == "json" || opts.Output == "yaml" {
			return printStructured(opts, rsp.JSON200)
		}
		fmt.Fprintf(opts.Out, "Verification policy revision %d saved; no Workflow was rendered.\n", rsp.JSON200.PolicyRevision)
		return nil
	}}
	cmd.Flags().StringVarP(&file, "file", "f", "", "Verification policy YAML or JSON file")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Call preview only; never save the policy")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func newVerificationRunCmd(opts *GlobalOptions) *cobra.Command {
	var key string
	var dryRun bool
	cmd := &cobra.Command{Use: "run INSTANCE", Short: "Dry-run or request a manual exact verification run", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if key == "" {
			key = uuid.NewString()
		}
		_, cc, err := opts.resolveContext()
		if err != nil {
			return err
		}
		client, err := newAPIClient(cmd, opts, cc)
		if err != nil {
			return err
		}
		rsp, err := client.OAS.CreateInstanceVerificationRunWithResponse(cmd.Context(), cc.Tenant, args[0], oas.RunInputBody{IdempotencyKey: key, DryRun: &dryRun})
		if err != nil {
			return err
		}
		if rsp.JSON200 == nil {
			return apiError(rsp.Status(), rsp.ApplicationproblemJSONDefault)
		}
		if opts.Output == "json" || opts.Output == "yaml" {
			return printStructured(opts, rsp.JSON200)
		}
		if rsp.JSON200.Preview != nil {
			return printPolicyPreview(opts, rsp.JSON200.Preview)
		}
		if rsp.JSON200.Run != nil {
			printRun(opts, rsp.JSON200.Run)
		}
		return nil
	}}
	cmd.Flags().StringVar(&key, "key", "", "Manual run idempotency key (generated when empty)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview only; do not request a run")
	return cmd
}

func newVerificationRunsCmd(opts *GlobalOptions) *cobra.Command {
	var limit int64
	cmd := &cobra.Command{Use: "runs INSTANCE", Short: "List verification run history", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		_, cc, err := opts.resolveContext()
		if err != nil {
			return err
		}
		client, err := newAPIClient(cmd, opts, cc)
		if err != nil {
			return err
		}
		rsp, err := client.OAS.ListInstanceVerificationRunsWithResponse(cmd.Context(), cc.Tenant, args[0], &oas.ListInstanceVerificationRunsParams{Limit: &limit})
		if err != nil {
			return err
		}
		if rsp.JSON200 == nil {
			return apiError(rsp.Status(), rsp.ApplicationproblemJSONDefault)
		}
		if opts.Output == "json" || opts.Output == "yaml" {
			return printStructured(opts, rsp.JSON200.Runs)
		}
		tw := newTable(opts.Out)
		fmt.Fprintln(tw, "RUN\tSTATUS\tPROFILE\tSUITE DIGEST\tATTEMPT")
		if rsp.JSON200.Runs != nil {
			for _, run := range *rsp.JSON200.Runs {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\n", run.RunId, run.Status, run.Profile, run.SuiteDigest, run.LatestAttempt)
			}
		}
		return tw.Flush()
	}}
	cmd.Flags().Int64Var(&limit, "limit", 50, "Maximum runs to return")
	return cmd
}

func printRun(opts *GlobalOptions, run *oas.Run) {
	fmt.Fprintf(opts.Out, "Run:         %s (attempt %d)\n", run.RunId, run.LatestAttempt)
	fmt.Fprintf(opts.Out, "Status:      %s\n", run.Status)
	fmt.Fprintf(opts.Out, "Revision:    %s\n", run.AppRevision)
	fmt.Fprintf(opts.Out, "Suite:       %s (%s)\n", run.SuiteDigest, run.Profile)
	if run.Links == nil {
		fmt.Fprintln(opts.Out, "Links:       none")
	} else {
		fmt.Fprintf(opts.Out, "Workflow:    %s\nLogs:        %s\nArtifacts:   %s\n", run.Links.Workflow, run.Links.Logs, run.Links.Artifacts)
	}
}

func newVerificationRunStatusCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{Use: "run-status INSTANCE RUN", Short: "Show run evidence and eligible log/artifact links", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		_, cc, err := opts.resolveContext()
		if err != nil {
			return err
		}
		client, err := newAPIClient(cmd, opts, cc)
		if err != nil {
			return err
		}
		rsp, err := client.OAS.GetInstanceVerificationRunWithResponse(cmd.Context(), cc.Tenant, args[0], args[1])
		if err != nil {
			return err
		}
		if rsp.JSON200 == nil {
			return apiError(rsp.Status(), rsp.ApplicationproblemJSONDefault)
		}
		if opts.Output == "json" || opts.Output == "yaml" {
			return printStructured(opts, rsp.JSON200)
		}
		printRun(opts, rsp.JSON200)
		return nil
	}}
}

func newVerificationPromotionPreviewCmd(opts *GlobalOptions) *cobra.Command {
	var version, digest string
	cmd := &cobra.Command{Use: "promotion-preview INSTANCE", Short: "Evaluate exact promotion evidence without deploying", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		_, cc, err := opts.resolveContext()
		if err != nil {
			return err
		}
		client, err := newAPIClient(cmd, opts, cc)
		if err != nil {
			return err
		}
		rsp, err := client.OAS.PreviewInstancePromotionWithResponse(cmd.Context(), cc.Tenant, args[0], oas.PromotionInputBody{ToVersion: version, ToDigest: digest})
		if err != nil {
			return err
		}
		if rsp.JSON200 == nil {
			return apiError(rsp.Status(), rsp.ApplicationproblemJSONDefault)
		}
		if opts.Output == "json" || opts.Output == "yaml" {
			return printStructured(opts, rsp.JSON200)
		}
		fmt.Fprintf(opts.Out, "Promotion allowed: %t\n", rsp.JSON200.Decision.Allowed)
		if rsp.JSON200.Decision.ReasonCode != nil {
			fmt.Fprintf(opts.Out, "Reason:            %s\n", *rsp.JSON200.Decision.ReasonCode)
		}
		return nil
	}}
	cmd.Flags().StringVar(&version, "version", "", "Exact target app version")
	cmd.Flags().StringVar(&digest, "digest", "", "Exact target app artifact digest")
	_ = cmd.MarkFlagRequired("version")
	_ = cmd.MarkFlagRequired("digest")
	return cmd
}

package cmd

import (
	"context"
	"fmt"

	"github.com/7K-Inari/inari-api/gen/go/oas"
	"github.com/7K-Inari/inari-cli/internal/api"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

type deploymentPlanFlags struct {
	mode        string
	targetID    string
	folder      string
	datasource  string
	evaluation  string
	packItemID  string
	packVersion string
	packDigest  string
}

func addDeploymentPlanFlags(cmd *cobra.Command, flags *deploymentPlanFlags) {
	cmd.Flags().StringVar(&flags.mode, "observability", "skip", "Observability companion mode: skip|provision")
	cmd.Flags().StringVar(&flags.targetID, "observability-target", "", "Entitled monitoring resource-target ID")
	cmd.Flags().StringVar(&flags.folder, "observability-folder", "", "Provider-allowed Grafana folder")
	cmd.Flags().StringVar(&flags.datasource, "observability-datasource", "", "Provider-allowed datasource name")
	cmd.Flags().StringVar(&flags.evaluation, "observability-evaluation", "", "Target evaluation: central|local")
	cmd.Flags().StringVar(&flags.packItemID, "observability-pack", "", "Exact compatible pack item ID")
	cmd.Flags().StringVar(&flags.packVersion, "observability-pack-version", "", "Exact compatible pack version")
	cmd.Flags().StringVar(&flags.packDigest, "observability-pack-digest", "", "Exact compatible pack sha256 digest")
}

func observabilityFromFlags(flags deploymentPlanFlags) (oas.ObservabilityInput, error) {
	if flags.mode == "" {
		flags.mode = "skip"
	}
	if flags.mode != "skip" && flags.mode != "provision" {
		return oas.ObservabilityInput{}, fmt.Errorf("--observability must be skip or provision")
	}
	out := oas.ObservabilityInput{Mode: flags.mode}
	if flags.mode == "skip" {
		return out, nil
	}
	if flags.packItemID != "" || flags.packVersion != "" || flags.packDigest != "" {
		if flags.packItemID == "" || flags.packVersion == "" || flags.packDigest == "" {
			return out, fmt.Errorf("pack item, version, and digest must be supplied together")
		}
		out.Pack = &oas.PackPin{ItemId: &flags.packItemID, Version: flags.packVersion, Digest: flags.packDigest, RenderContract: "inari.observability-pack/v2"}
	}
	selection := map[string]any{}
	if flags.folder != "" {
		selection["folder"] = flags.folder
	}
	if flags.datasource != "" {
		selection["datasource"] = flags.datasource
	}
	if flags.evaluation != "" {
		selection["evaluation"] = flags.evaluation
	}
	if flags.targetID != "" {
		out.Target = &oas.TargetIntent{Id: flags.targetID, Selection: selection}
	}
	return out, nil
}

func planPreflight(ctx context.Context, client *api.Client, tenant string, request oas.PlanRequest) (*oas.Preflight, error) {
	rsp, err := client.OAS.PreflightDeploymentPlanWithResponse(ctx, tenant, request)
	if err != nil {
		return nil, err
	}
	if rsp.JSON200 == nil {
		return nil, apiError(rsp.Status(), rsp.ApplicationproblemJSONDefault)
	}
	return &rsp.JSON200.Preflight, nil
}

func resolveDeploymentTarget(preflight *oas.Preflight, clusterID string) (oas.Target, error) {
	if preflight.DeploymentTargets != nil {
		for _, target := range *preflight.DeploymentTargets {
			if target.ResourceRef.Id == clusterID {
				return target, nil
			}
		}
	}
	return oas.Target{}, fmt.Errorf("cluster %q is not an entitled deployment target", clusterID)
}

func normalizeObservability(flags deploymentPlanFlags, initial *oas.Preflight, request *oas.PlanRequest) error {
	observability, err := observabilityFromFlags(flags)
	if err != nil {
		return err
	}
	if observability.Mode == "skip" {
		request.Observability = observability
		return nil
	}
	if initial.Plan.Capability == nil {
		return fmt.Errorf("this exact app version has no signed observability metadata")
	}
	if observability.Target == nil && initial.Plan.Observability.Target != nil {
		observability.Target = initial.Plan.Observability.Target
	}
	if observability.Target == nil {
		return fmt.Errorf("--observability-target is required when no policy default exists")
	}
	if observability.Target.Revision == 0 && initial.MonitoringTargets != nil {
		for _, target := range *initial.MonitoringTargets {
			if target.Id == observability.Target.Id {
				observability.Target.Revision = target.Revision
				break
			}
		}
	}
	if observability.Pack == nil && initial.Plan.Observability.Pack != nil {
		observability.Pack = initial.Plan.Observability.Pack
	}
	if observability.Pack == nil && initial.CompatiblePacks != nil && len(*initial.CompatiblePacks) > 0 {
		observability.Pack = &(*initial.CompatiblePacks)[0]
	}
	if observability.Pack == nil {
		return fmt.Errorf("no compatible observability pack is available")
	}
	request.Observability = observability
	return nil
}

func completePlanPreflight(ctx context.Context, client *api.Client, tenant string, request oas.PlanRequest, flags deploymentPlanFlags) (*oas.Preflight, error) {
	if request.IdempotencyKey == "" {
		request.IdempotencyKey = uuid.NewString()
	}
	initial, err := planPreflight(ctx, client, tenant, request)
	if err != nil {
		return nil, err
	}
	if request.Deployment.TargetId != "" && request.Deployment.TargetRevision == 0 {
		target, err := resolveDeploymentTarget(initial, request.Deployment.TargetId)
		if err != nil {
			return nil, err
		}
		request.Deployment.TargetId = target.Id
		request.Deployment.TargetRevision = target.Revision
	}
	if err := normalizeObservability(flags, initial, &request); err != nil {
		return nil, err
	}
	if request.Observability.Mode == "provision" && (request.Deployment.OwnerTeam == nil || *request.Deployment.OwnerTeam == "") && initial.Plan.Deployment.OwnerTeam != nil {
		request.Deployment.OwnerTeam = initial.Plan.Deployment.OwnerTeam
	}
	if request.Observability.Mode == "provision" && (request.Deployment.OwnerTeam == nil || *request.Deployment.OwnerTeam == "") {
		return nil, fmt.Errorf("--owner-team is required with --observability=provision")
	}
	return planPreflight(ctx, client, tenant, request)
}

func submitPlan(ctx context.Context, client *api.Client, tenant string, preflight *oas.Preflight, idempotencyKey string) (*oas.Plan, error) {
	request := oas.PlanRequest{
		Operation: preflight.Plan.Operation, Source: &preflight.Plan.Source, Deployment: preflight.Plan.Deployment,
		Observability: preflight.Plan.Observability, PlanFingerprint: &preflight.Plan.PlanFingerprint,
		IdempotencyKey: idempotencyKey,
	}
	if preflight.Plan.Operation == "provisionExisting" && preflight.Plan.AppInstanceId != nil {
		request.InstanceId = preflight.Plan.AppInstanceId
	}
	rsp, err := client.OAS.SubmitDeploymentPlanWithResponse(ctx, tenant, request)
	if err != nil {
		return nil, err
	}
	if rsp.JSON200 == nil {
		return nil, apiError(rsp.Status(), rsp.ApplicationproblemJSONDefault)
	}
	return &rsp.JSON200.Plan, nil
}

func printPlan(opts *GlobalOptions, plan *oas.Plan) error {
	if opts.Output == "json" || opts.Output == "yaml" {
		return printStructured(opts, plan)
	}
	appItemID := ""
	if plan.App.ItemId != nil {
		appItemID = *plan.App.ItemId
	}
	fmt.Fprintf(opts.Out, "Deployment plan: %s (status %s, app %s@%s)\n", plan.Id, plan.State, appItemID, plan.App.Version)
	fmt.Fprintf(opts.Out, "Fingerprint: %s\n", plan.PlanFingerprint)
	if plan.AppInstanceId != nil {
		fmt.Fprintf(opts.Out, "Instance: %s\n", *plan.AppInstanceId)
	}
	if plan.CommitSha != nil {
		fmt.Fprintf(opts.Out, "Commit: %s\n", *plan.CommitSha)
	}
	if plan.PrUrl != nil {
		fmt.Fprintf(opts.Out, "Pull request: %s\n", *plan.PrUrl)
	}
	if plan.ApprovalId != nil {
		fmt.Fprintf(opts.Out, "Approval required: %s\n", *plan.ApprovalId)
	}
	if plan.ScaffoldRunId != nil {
		fmt.Fprintf(opts.Out, "Scaffold run: %s\n", *plan.ScaffoldRunId)
	}
	if plan.Companion != nil {
		fmt.Fprintf(opts.Out, "Companion: %s (state %s, generation %d)\n", plan.Companion.Id, plan.Companion.State, plan.Companion.Generation)
		if plan.Companion.Links != nil {
			for _, link := range *plan.Companion.Links {
				fmt.Fprintf(opts.Out, "%s: %s\n", link.Title, link.Url)
			}
		}
	}
	return nil
}

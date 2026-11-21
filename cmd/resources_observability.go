package cmd

import (
	"fmt"

	"github.com/7K-Inari/inari-api/gen/go/oas"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

func newResourcesObservabilityCmd(opts *GlobalOptions) *cobra.Command {
	cmd := &cobra.Command{Use: "observability", Short: "Provision and inspect existing-instance observability"}
	cmd.AddCommand(newExistingObservabilityPreflightCmd(opts), newExistingObservabilityProvisionCmd(opts), newExistingObservabilityStatusCmd(opts))
	return cmd
}

func existingObservabilityRequest(instanceID string) oas.PlanRequest {
	return oas.PlanRequest{Operation: "provisionExisting", InstanceId: &instanceID,
		Deployment: oas.DeploymentInput{TargetId: "", TargetRevision: 0, Spec: map[string]any{}}, Observability: oas.ObservabilityInput{Mode: "provision"}}
}

func newExistingObservabilityPreflightCmd(opts *GlobalOptions) *cobra.Command {
	var flags deploymentPlanFlags
	cmd := &cobra.Command{Use: "preflight INSTANCE", Short: "Preview exact companion pins and entitled targets", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
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
		request := existingObservabilityRequest(args[0])
		preflight, err := completePlanPreflight(cmd.Context(), client, cc.Tenant, request, flags)
		if err != nil {
			return err
		}
		return printPlan(opts, &preflight.Plan)
	}}
	addDeploymentPlanFlags(cmd, &flags, "provision")
	return cmd
}

func newExistingObservabilityProvisionCmd(opts *GlobalOptions) *cobra.Command {
	var flags deploymentPlanFlags
	cmd := &cobra.Command{Use: "provision INSTANCE", Short: "Provision observability from the original exact app pin", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
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
		request := existingObservabilityRequest(args[0])
		request.IdempotencyKey = uuid.NewString()
		preflight, err := completePlanPreflight(cmd.Context(), client, cc.Tenant, request, flags)
		if err != nil {
			return err
		}
		plan, err := submitPlan(cmd.Context(), client, cc.Tenant, preflight, request.IdempotencyKey)
		if err != nil {
			return err
		}
		return printPlan(opts, plan)
	}}
	addDeploymentPlanFlags(cmd, &flags, "provision")
	return cmd
}

func newExistingObservabilityStatusCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{Use: "status INSTANCE", Short: "Show companion readiness and verified links", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
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
		rsp, err := client.OAS.GetInstanceObservabilityWithResponse(cmd.Context(), cc.Tenant, args[0])
		if err != nil {
			return err
		}
		if rsp.JSON200 == nil {
			return apiError(rsp.Status(), rsp.ApplicationproblemJSONDefault)
		}
		status := rsp.JSON200.Observability
		if opts.Output == "json" || opts.Output == "yaml" {
			return printStructured(opts, status)
		}
		fmt.Fprintf(opts.Out, "Mode: %s\nAvailable: %t\n", status.Mode, status.Available)
		if status.Reason != nil {
			fmt.Fprintf(opts.Out, "Reason: %s\n", *status.Reason)
		}
		if status.Companion != nil {
			fmt.Fprintf(opts.Out, "Companion: %s\nState: %s\nGeneration: %d\nTarget: %s revision %d\n", status.Companion.Id, status.Companion.State, status.Companion.Generation, status.Companion.TargetId, status.Companion.TargetRevision)
			if status.Companion.Links != nil {
				for _, link := range *status.Companion.Links {
					fmt.Fprintf(opts.Out, "%s: %s\n", link.Title, link.Url)
				}
			}
		}
		return nil
	}}
}

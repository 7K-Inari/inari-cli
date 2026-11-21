package cmd

import (
	"fmt"

	"github.com/7K-Inari/inari-api/gen/go/oas"
	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/7K-Inari/inari-cli/internal/prompt"
)

type sourceDeployFlags struct {
	clusterID, name, namespace, ownerTeam, file string
	sets                                        []string
	observability                               deploymentPlanFlags
}

func addSourceDeployFlags(cmd *cobra.Command, flags *sourceDeployFlags, clusterRequired bool) {
	cmd.Flags().StringVar(&flags.clusterID, "cluster", "", "Target cluster ID")
	if clusterRequired {
		_ = cmd.MarkFlagRequired("cluster")
	}
	cmd.Flags().StringVar(&flags.name, "name", "", "Instance or component name")
	cmd.Flags().StringVar(&flags.namespace, "namespace", "", "Target namespace")
	cmd.Flags().StringVar(&flags.ownerTeam, "owner-team", "", "Owning team for folder access")
	cmd.Flags().StringVarP(&flags.file, "file", "f", "", "YAML values file")
	cmd.Flags().StringArrayVar(&flags.sets, "set", nil, "Set a value key=value (repeatable)")
	addDeploymentPlanFlags(cmd, &flags.observability, "skip")
}

func sourceSpec(flags sourceDeployFlags) (map[string]any, error) {
	spec := map[string]any{}
	if flags.file != "" {
		loaded, err := prompt.LoadFile(flags.file)
		if err != nil {
			return nil, err
		}
		spec = loaded
	}
	if err := prompt.ApplySets(spec, flags.sets); err != nil {
		return nil, err
	}
	return spec, nil
}

func submitSourcePlan(cmd *cobra.Command, opts *GlobalOptions, request oas.PlanRequest, flags deploymentPlanFlags) error {
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
	idempotencyKey := uuid.NewString()
	request.IdempotencyKey = idempotencyKey
	preflight, err := completePlanPreflight(cmd.Context(), client, cc.Tenant, request, flags)
	if err != nil {
		return err
	}
	plan, err := submitPlan(cmd.Context(), client, cc.Tenant, preflight, idempotencyKey)
	if err != nil {
		return err
	}
	return printPlan(opts, plan)
}

func newDeployChartCmd(opts *GlobalOptions) *cobra.Command {
	var sourceID, version, digest, admissionItemID, admissionVersion string
	var flags sourceDeployFlags
	cmd := &cobra.Command{
		Use: "chart REF", Short: "Deploy an exact-digest direct OCI Helm chart", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if sourceID == "" || version == "" || digest == "" {
				return fmt.Errorf("--source, --version, and --digest are required")
			}
			ref := args[0]
			spec, err := sourceSpec(flags)
			if err != nil {
				return err
			}
			source := oas.Source{Kind: "chart", SourceId: &sourceID, Ref: &ref, Version: &version, Digest: &digest}
			if admissionItemID != "" {
				source.AdmissionItemId = &admissionItemID
				source.AdmissionVersion = &admissionVersion
			}
			request := oas.PlanRequest{Operation: "deploy", Source: &source, Deployment: oas.DeploymentInput{TargetId: flags.clusterID, Spec: spec}}
			if flags.name != "" {
				request.Deployment.Name = &flags.name
			}
			if flags.namespace != "" {
				request.Deployment.Namespace = &flags.namespace
			}
			if flags.ownerTeam != "" {
				request.Deployment.OwnerTeam = &flags.ownerTeam
			}
			return submitSourcePlan(cmd, opts, request, flags.observability)
		},
	}
	cmd.Flags().StringVar(&sourceID, "source", "", "Configured trusted source ID")
	cmd.Flags().StringVar(&version, "version", "", "Exact chart version")
	cmd.Flags().StringVar(&digest, "digest", "", "Exact chart sha256 digest")
	cmd.Flags().StringVar(&admissionItemID, "admission-item", "", "Signed-index item ID when chart identity is ambiguous")
	cmd.Flags().StringVar(&admissionVersion, "admission-version", "", "Signed-index item version when ambiguous")
	addSourceDeployFlags(cmd, &flags, true)
	return cmd
}

func newDeployTemplateCmd(opts *GlobalOptions) *cobra.Command {
	var version string
	var flags sourceDeployFlags
	cmd := &cobra.Command{
		Use: "template ITEM", Short: "Deploy a deployment-capable component template", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := sourceSpec(flags)
			if err != nil {
				return err
			}
			itemID := args[0]
			source := oas.Source{Kind: "template", ItemId: &itemID}
			if version != "" {
				source.Version = &version
			}
			if flags.name != "" {
				source.DisplayName = &flags.name
			}
			request := oas.PlanRequest{Operation: "deploy", Source: &source, Deployment: oas.DeploymentInput{TargetId: flags.clusterID, Spec: spec}}
			if flags.name != "" {
				request.Deployment.Name = &flags.name
				request.Deployment.DisplayName = &flags.name
			}
			if flags.namespace != "" {
				request.Deployment.Namespace = &flags.namespace
			}
			if flags.ownerTeam != "" {
				request.Deployment.OwnerTeam = &flags.ownerTeam
			}
			return submitSourcePlan(cmd, opts, request, flags.observability)
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "Exact template version")
	addSourceDeployFlags(cmd, &flags, false)
	return cmd
}

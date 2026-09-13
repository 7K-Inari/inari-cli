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
	c.AddCommand(newClusterRegisterCmd(opts), newClusterListCmd(opts), newClusterKubeconfigCmd(opts))
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

func newClusterKubeconfigCmd(opts *GlobalOptions) *cobra.Command {
	var server, grantType string
	var gateway bool
	c := &cobra.Command{
		Use:   "kubeconfig CLUSTER_ID",
		Short: "Print a kubelogin exec-credential kubeconfig for a cluster",
		Long: `Print a kubeconfig that authenticates to a tenant cluster with your
Keycloak identity via kubelogin (kubectl oidc-login). The output contains no
secrets — only the OIDC issuer, the tenant's public client ID, and exec-plugin
arguments.

Direct mode (default) targets the cluster API server you name with --server;
use it when your machine can reach the tenant API server. For private
(pull-only) clusters, --gateway targets the control plane's impersonating
proxy instead (plan §5.4): kubectl talks to the hub, which forwards over the
agent stream with impersonation headers, so the same cluster RBAC applies.

Pipe it into a file and point kubectl at it:

  inari cluster kubeconfig clu-1 --server https://api.prod:6443 > ~/.kube/prod-1
  kubectl --kubeconfig ~/.kube/prod-1 get ns`,
		Args: cobra.ExactArgs(1),
		Example: `  inari cluster kubeconfig clu-1 --server https://api.prod:6443
  inari cluster kubeconfig clu-1 --server https://api.prod:6443 --grant-type device-code
  inari cluster kubeconfig clu-1 --gateway   # private cluster (pull-only agent)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if grantType != "authcode" && grantType != "device-code" {
				return fmt.Errorf("invalid --grant-type %q, want authcode or device-code", grantType)
			}
			if !gateway && server == "" {
				return fmt.Errorf("--server is required in direct mode (the control plane never learns tenant API endpoints); use --gateway for private clusters")
			}
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
			clusterID := args[0]
			rsp, err := client.OAS.GetClusterAccessInfoWithResponse(cmd.Context(), cc.Tenant, clusterID)
			if err != nil {
				return err
			}
			if rsp.JSON200 == nil {
				return apiError(rsp.Status(), rsp.ApplicationproblemJSONDefault)
			}
			ai := &rsp.JSON200.AccessInfo
			apiServer := server
			if gateway {
				apiServer = strings.TrimSuffix(cc.Server, "/") + "/api/v1/tenants/" + cc.Tenant + "/clusters/" + clusterID + "/proxy"
			}
			_, err = opts.Out.Write([]byte(renderKubeconfig(clusterID, cc.Tenant, apiServer, ai, grantType)))
			return err
		},
	}
	c.Flags().StringVar(&server, "server", "", "Tenant cluster API server URL (required in direct mode)")
	c.Flags().BoolVar(&gateway, "gateway", false, "Use the control plane's impersonating proxy (private clusters)")
	c.Flags().StringVar(&grantType, "grant-type", "authcode", "kubelogin grant: authcode (browser) or device-code (headless)")
	return c
}

// renderKubeconfig builds a secret-free exec-credential kubeconfig (plan
// §5.4): identity comes from kubelogin at exec time, never from the file.
func renderKubeconfig(clusterID, tenant, apiServer string, ai *oas.ClusterAccessInfo, grantType string) string {
	name := tenant + "-" + clusterID
	var b strings.Builder
	b.WriteString("apiVersion: v1\nkind: Config\n")
	b.WriteString("clusters:\n- name: " + name + "\n  cluster:\n    server: " + apiServer + "\n")
	b.WriteString("users:\n- name: " + name + "\n  user:\n    exec:\n")
	b.WriteString("      apiVersion: client.authentication.k8s.io/v1\n")
	b.WriteString("      command: kubectl\n      args:\n")
	for _, a := range []string{
		"oidc-login", "get-token",
		"--oidc-issuer-url=" + ai.IssuerUrl,
		"--oidc-client-id=" + ai.KubectlClientId,
		"--oidc-extra-scope=organization",
		"--grant-type=" + grantType,
	} {
		b.WriteString("      - " + a + "\n")
	}
	b.WriteString("      provideClusterInfo: true\n")
	b.WriteString("contexts:\n- name: " + name + "\n  context:\n    cluster: " + name + "\n    user: " + name + "\n")
	b.WriteString("current-context: " + name + "\n")
	return b.String()
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

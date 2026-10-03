package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/7K-Inari/inari-api/gen/go/oas"
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func newClusterConnectCmd(opts *GlobalOptions) *cobra.Command {
	var server, grantType, kubeconfigPath, org string
	var setCurrent bool
	c := &cobra.Command{
		Use:   "connect CLUSTER_ID",
		Short: "Merge a secret-free kubeconfig for a cluster into your kubeconfig",
		Long: `Fetch the server-rendered, secret-free kubeconfig for a cluster and merge
it into your kubeconfig (aws eks update-kubeconfig style). Existing contexts
are preserved; same-named entries are updated in place, so re-running connect
refreshes the entry.

Gateway mode (the default) targets the control plane's impersonating proxy and
works for private (pull-only) clusters. Passing --server additionally adds a
<name>-direct context for the tenant API server (Rancher ACE dual-context
pattern), and the gateway context stays the current context unless
--set-current-context=false.

kubectl authentication happens at exec time via kubelogin — no secrets are
ever written to the file.`,
		Args: cobra.ExactArgs(1),
		Example: `  inari cluster connect clu-1
  inari cluster connect clu-1 --org acme
  inari cluster connect clu-1 --server https://api.prod:6443
  inari cluster connect clu-1 --kubeconfig ~/.kube/prod --set-current-context=false`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if grantType != "authcode" && grantType != "device-code" {
				return fmt.Errorf("invalid --grant-type %q, want authcode or device-code", grantType)
			}
			clusterID := args[0]
			if !clusterIDRe.MatchString(clusterID) {
				return fmt.Errorf("invalid cluster ID %q", clusterID)
			}
			_, cc, err := opts.resolveContext()
			if err != nil {
				return err
			}
			if org != "" {
				cc.Tenant = org
			}
			if err := requireTenant(cc); err != nil {
				return err
			}
			client, err := newAPIClient(cmd, opts, cc)
			if err != nil {
				return err
			}

			aiRsp, err := client.OAS.GetClusterAccessInfoWithResponse(cmd.Context(), cc.Tenant, clusterID)
			if err != nil {
				return err
			}
			if aiRsp.JSON200 == nil {
				return apiError(aiRsp.Status(), aiRsp.ApplicationproblemJSONDefault)
			}
			ai := &aiRsp.JSON200.AccessInfo
			if ai.KubectlAccessEnabled != nil && !*ai.KubectlAccessEnabled {
				return fmt.Errorf("kubectl access is disabled by the control plane (kubectl_access.enabled=false); ask your platform admin to enable it")
			}
			mode := oas.GetClusterKubeconfigParamsModeGateway
			if ai.TunnelAvailable != nil && !*ai.TunnelAvailable {
				reason := ""
				if ai.TunnelUnavailableReason != nil {
					reason = " (" + *ai.TunnelUnavailableReason + ")"
				}
				fmt.Fprintf(opts.ErrOut, "Warning: no tunnel-agent session is live for cluster %s%s; the gateway context will not work until the agent connects.\n", clusterID, reason)
			}
			params := &oas.GetClusterKubeconfigParams{
				Mode:      &mode,
				GrantType: (*oas.GetClusterKubeconfigParamsGrantType)(&grantType),
			}
			if server != "" {
				params.Server = &server
			}
			// Use the raw response: the generated WithResponse parser tries to
			// yaml.Unmarshal the kubeconfig document into a string and fails.
			kcRsp, err := client.OAS.GetClusterKubeconfig(cmd.Context(), cc.Tenant, clusterID, params)
			if err != nil {
				return err
			}
			defer func() { _ = kcRsp.Body.Close() }()
			rendered, err := io.ReadAll(kcRsp.Body)
			if err != nil {
				return err
			}
			if kcRsp.StatusCode != http.StatusOK {
				var model oas.ErrorModel
				if jsonErr := json.Unmarshal(rendered, &model); jsonErr != nil {
					model = oas.ErrorModel{}
				}
				return apiError(kcRsp.Status, &model)
			}

			if _, err := exec.LookPath("kubelogin"); err != nil {
				fmt.Fprintln(opts.ErrOut, "Warning: kubelogin not found in PATH; kubectl will fail to authenticate until it is installed.")
				fmt.Fprintln(opts.ErrOut, "  Install: https://github.com/int128/kubelogin#setup (or: kubectl krew install oidc-login)")
			}

			if kubeconfigPath == "" {
				kubeconfigPath = clientcmd.NewDefaultClientConfigLoadingRules().GetDefaultFilename()
			}
			name, err := mergeKubeconfig(rendered, kubeconfigPath, setCurrent)
			if err != nil {
				return err
			}
			fmt.Fprintf(opts.ErrOut, "Merged context %q into %s.\n", name, kubeconfigPath)
			if setCurrent {
				fmt.Fprintf(opts.ErrOut, "Switched to context %q.\n", name)
			}
			return nil
		},
	}
	c.Flags().StringVar(&org, "org", "", "Tenant slug override (default: tenant from the current context)")
	c.Flags().StringVar(&server, "server", "", "Tenant cluster API server URL; adds a <name>-direct context alongside the gateway context")
	c.Flags().StringVar(&kubeconfigPath, "kubeconfig", "", "Kubeconfig file to merge into (default: $KUBECONFIG or ~/.kube/config)")
	c.Flags().BoolVar(&setCurrent, "set-current-context", true, "Set the current context to the gateway context")
	c.Flags().StringVar(&grantType, "grant-type", "authcode", "kubelogin grant: authcode (browser) or device-code (headless)")
	return c
}

// mergeKubeconfig merges the rendered kubeconfig into the file at path,
// replacing same-named clusters/users/contexts and preserving everything else.
// It returns the rendered config's current context name.
func mergeKubeconfig(rendered []byte, path string, setCurrent bool) (string, error) {
	newCfg, err := clientcmd.Load(rendered)
	if err != nil {
		return "", fmt.Errorf("parse server-rendered kubeconfig: %w", err)
	}
	if len(newCfg.Contexts) == 0 {
		return "", fmt.Errorf("server-rendered kubeconfig contains no contexts")
	}
	if setCurrent && newCfg.CurrentContext == "" {
		// Never blank the user's existing current-context; fall back to the
		// single rendered context or fail loudly.
		if len(newCfg.Contexts) == 1 {
			for name := range newCfg.Contexts {
				newCfg.CurrentContext = name
			}
		} else {
			return "", fmt.Errorf("server-rendered kubeconfig has no current-context")
		}
	}

	var existing *clientcmdapi.Config
	loaded, err := clientcmd.LoadFromFile(path)
	switch {
	case err == nil:
		existing = loaded
	case errors.Is(err, os.ErrNotExist):
		existing = clientcmdapi.NewConfig()
		if dir := filepath.Dir(path); dir != "" {
			if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
				return "", mkErr
			}
		}
	default:
		return "", fmt.Errorf("load kubeconfig %s: %w", path, err)
	}

	for name, c := range newCfg.Clusters {
		existing.Clusters[name] = c
	}
	for name, a := range newCfg.AuthInfos {
		existing.AuthInfos[name] = a
	}
	for name, ctx := range newCfg.Contexts {
		existing.Contexts[name] = ctx
	}
	if setCurrent {
		existing.CurrentContext = newCfg.CurrentContext
	}
	if err := clientcmd.WriteToFile(*existing, path); err != nil {
		return "", fmt.Errorf("write kubeconfig %s: %w", path, err)
	}
	return newCfg.CurrentContext, nil
}

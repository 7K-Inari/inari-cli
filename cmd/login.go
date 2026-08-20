package cmd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/7K-Inari/inari-cli/internal/auth"
	"github.com/7K-Inari/inari-cli/internal/config"
)

const defaultIssuer = "https://keycloak.inari.dev/realms/inari"

func newLoginCmd(opts *GlobalOptions) *cobra.Command {
	var (
		issuer      string
		tenant      string
		contextName string
		timeoutSec  int
	)
	c := &cobra.Command{
		Use:   "login",
		Short: "Authenticate to the Inari platform via OIDC device flow",
		Long: `Authenticate to the Inari control plane using the OAuth2/OIDC device
authorization flow against the platform Keycloak 'inari' realm.

A user code and verification URL are printed; complete the login in a
browser and the CLI polls until the tokens are issued. Tokens (including the
refresh token) are cached per context under ~/.config/inari/tokens/ with
0600 permissions and are refreshed automatically on expiry.`,
		Example: `  inari login
  inari login --server https://api.inari.example.com --tenant acme
  inari login --context-name prod`,
		RunE: func(cmd *cobra.Command, args []string) error {
			server := opts.ServerFlag
			if server == "" {
				server = os.Getenv("INARI_SERVER")
			}
			if server == "" {
				server = config.DefaultServer
			}
			if issuer == "" {
				issuer = os.Getenv("INARI_ISSUER")
			}
			if issuer == "" {
				issuer = defaultIssuer
			}

			flow := &auth.DeviceFlow{
				Issuer:   issuer,
				ClientID: auth.DefaultClientID,
				Scopes:   []string{"openid", "organization", "profile", "email"},
			}
			ctx := cmd.Context()

			da, err := flow.Start(ctx)
			if err != nil {
				return err
			}
			fmt.Fprintf(opts.Out, "Open %s and enter code %s\n", da.VerificationURI, da.UserCode)
			if da.VerificationURIComplete != "" {
				fmt.Fprintf(opts.Out, "Or open: %s\n", da.VerificationURIComplete)
			}
			fmt.Fprintln(opts.Out, "Waiting for login to complete...")

			pollCtx := ctx
			if timeoutSec > 0 {
				var cancel context.CancelFunc
				pollCtx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
				defer cancel()
			}
			tok, err := flow.Poll(pollCtx, da)
			if err != nil {
				return err
			}

			if tenant == "" {
				tenant = organizationClaim(tok.AccessToken)
			}

			cfg, err := config.Load()
			if err != nil {
				return err
			}
			cfg.SetContext(contextName, config.Context{Server: server, Issuer: issuer, Tenant: tenant})
			cfg.CurrentContext = contextName
			if err := cfg.Save(); err != nil {
				return err
			}
			cache, err := auth.NewCache()
			if err != nil {
				return err
			}
			if err := cache.Save(contextName, tok); err != nil {
				return err
			}

			fmt.Fprintf(opts.Out, "Logged in. Context %q (server %s", contextName, server)
			if tenant != "" {
				fmt.Fprintf(opts.Out, ", tenant %s", tenant)
			}
			fmt.Fprintln(opts.Out, ") is now current.")
			return nil
		},
	}
	c.Flags().StringVar(&issuer, "issuer", "", "Keycloak issuer URL (default "+defaultIssuer+"; env INARI_ISSUER)")
	c.Flags().StringVar(&tenant, "tenant", "", "Tenant slug (default: organization claim from token)")
	c.Flags().StringVar(&contextName, "context-name", "default", "Name of the config context to create/update")
	c.Flags().IntVar(&timeoutSec, "timeout", 0, "Abort login after N seconds (0 = device code expiry)")
	return c
}

func newLogoutCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove cached credentials for the current context",
		Example: `  inari logout
  inari logout --context prod`,
		RunE: func(cmd *cobra.Command, args []string) error {
			name, _, err := opts.resolveContext()
			if err != nil {
				return err
			}
			cache, err := auth.NewCache()
			if err != nil {
				return err
			}
			if err := cache.Delete(name); err != nil {
				return err
			}
			fmt.Fprintf(opts.Out, "Removed cached credentials for context %q.\n", name)
			return nil
		},
	}
}

// organizationClaim decodes the JWT payload (unverified — display/UX only) and
// returns the Keycloak organization claim, if present.
func organizationClaim(accessToken string) string {
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Organization any `json:"organization"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	switch v := claims.Organization.(type) {
	case string:
		return v
	case map[string]any:
		if id, ok := v["id"].(string); ok {
			return id
		}
		if name, ok := v["name"].(string); ok {
			return name
		}
	case []any:
		if len(v) > 0 {
			if s, ok := v[0].(string); ok {
				return s
			}
		}
	}
	return ""
}

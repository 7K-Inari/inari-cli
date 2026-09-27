package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/7K-Inari/inari-cli/internal/auth"
	"github.com/7K-Inari/inari-cli/internal/gitconn"
)

// gitPollInterval is the connect wait-loop interval (overridden in tests).
var gitPollInterval = 3 * time.Second

func newGitCmd(opts *GlobalOptions) *cobra.Command {
	c := &cobra.Command{
		Use:   "git",
		Short: "Connect your personal git provider account (per-user Git social login)",
		Long: `Manage your personal git provider connections (GitHub first).

'connect' starts the server's OAuth flow: the CLI prints a consent URL to
open in your browser and waits until the control plane reports the
connection. Provider tokens are stored server-side only — the CLI never
sees or persists them.`,
	}
	c.AddCommand(
		newGitConnectCmd(opts),
		newGitListCmd(opts),
		newGitStatusCmd(opts),
		newGitDisconnectCmd(opts),
	)
	return c
}

// newGitConnClient resolves the context and builds an authenticated gitconn
// client using the cached session token (refreshed transparently).
func newGitConnClient(cmd *cobra.Command, opts *GlobalOptions) (*gitconn.Client, error) {
	name, cc, err := opts.resolveContext()
	if err != nil {
		return nil, err
	}
	if err := requireTenant(cc); err != nil {
		return nil, err
	}
	tok, err := auth.SessionToken(cmd.Context(), name, cc)
	if err != nil {
		return nil, err
	}
	return &gitconn.Client{Base: cc.Server, Tenant: cc.Tenant, Token: tok.AccessToken}, nil
}

func newGitConnectCmd(opts *GlobalOptions) *cobra.Command {
	var (
		apiBase   string
		timeout   int
		noBrowser bool
	)
	c := &cobra.Command{
		Use:   "connect PROVIDER",
		Short: "Link your personal git provider account via browser OAuth",
		Long: `Start the OAuth flow for a git provider (currently: github).

The server returns a consent URL which is opened in your browser (and always
printed, for headless use). After you authorize, the CLI polls the control
plane until the connection appears, the timeout passes, or you cancel with
Ctrl-C. If the provider is already connected, the command reports the existing
connection and exits without starting a new flow.`,
		Args: cobra.ExactArgs(1),
		Example: `  inari git connect github
  inari git connect github --no-browser          # headless: print URL only
  inari git connect github --api-base https://ghe.example.com/api/v3
  inari git connect github --timeout 120`,
		RunE: func(cmd *cobra.Command, args []string) error {
			provider := args[0]
			client, err := newGitConnClient(cmd, opts)
			if err != nil {
				return err
			}
			ctx := cmd.Context()

			conns, err := client.List(ctx)
			if err != nil {
				return err
			}
			if existing := gitconn.Find(conns, provider); existing != nil {
				fmt.Fprintf(opts.Out, "Already connected to %s as %s (since %s). Use 'inari git disconnect %s' first to re-connect.\n",
					provider, existing.ProviderLogin, existing.CreatedAt.Format("2006-01-02"), provider)
				return nil
			}

			consentURL, err := client.Authorize(ctx, provider, apiBase)
			if err != nil {
				return err
			}
			fmt.Fprintf(opts.Out, "Open this URL to connect %s:\n%s\n", provider, consentURL)
			if !noBrowser {
				openBrowser(opts.ErrOut, consentURL)
			}
			fmt.Fprintln(opts.Out, "Waiting for the connection to complete...")

			pollCtx := ctx
			if timeout > 0 {
				var cancel context.CancelFunc
				pollCtx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
				defer cancel()
			}
			client.PollInterval = gitPollInterval
			conn, err := client.WaitForConnection(pollCtx, provider)
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					return fmt.Errorf("timed out waiting for the %s connection; if you completed the browser login, run 'inari git status %s'", provider, provider)
				}
				if errors.Is(err, context.Canceled) {
					return fmt.Errorf("cancelled; no connection was created unless you completed the browser step — check 'inari git list'")
				}
				return err
			}
			if opts.Output == "json" || opts.Output == "yaml" {
				return printStructured(opts, conn)
			}
			fmt.Fprintf(opts.Out, "Connected to %s as %s (scopes: %s).\n", conn.Provider, conn.ProviderLogin, conn.Scopes)
			return nil
		},
	}
	c.Flags().StringVar(&apiBase, "api-base", "", "Self-hosted/GitHub Enterprise API base (must be allowlisted server-side)")
	c.Flags().IntVar(&timeout, "timeout", 300, "Abort waiting after N seconds (0 = no timeout)")
	c.Flags().BoolVar(&noBrowser, "no-browser", false, "Do not open a browser; print the consent URL only")
	return c
}

func newGitListCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List your git provider connections (metadata only)",
		Example: "  inari git list\n  inari git list -o json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newGitConnClient(cmd, opts)
			if err != nil {
				return err
			}
			conns, err := client.List(cmd.Context())
			if err != nil {
				return err
			}
			if opts.Output == "json" || opts.Output == "yaml" {
				return printStructured(opts, conns)
			}
			tw := newTable(opts.Out)
			fmt.Fprintln(tw, "PROVIDER\tLOGIN\tSCOPES\tCONNECTED\tLAST USED")
			for _, cn := range conns {
				lastUsed := ""
				if cn.LastUsedAt != nil {
					lastUsed = cn.LastUsedAt.Format("2006-01-02 15:04")
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", cn.Provider, cn.ProviderLogin, cn.Scopes, cn.CreatedAt.Format("2006-01-02"), lastUsed)
			}
			return tw.Flush()
		},
	}
}

func newGitStatusCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "status PROVIDER",
		Short: "Show whether a git provider is connected (exit 1 if not)",
		Example: `  inari git status github
  inari git status github && git clone https://github.com/acme/repo`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			provider := args[0]
			client, err := newGitConnClient(cmd, opts)
			if err != nil {
				return err
			}
			conns, err := client.List(cmd.Context())
			if err != nil {
				return err
			}
			conn := gitconn.Find(conns, provider)
			if conn == nil {
				fmt.Fprintf(opts.Out, "%s: not connected (run 'inari git connect %s')\n", provider, provider)
				return fmt.Errorf("%s not connected", provider)
			}
			if opts.Output == "json" || opts.Output == "yaml" {
				return printStructured(opts, conn)
			}
			fmt.Fprintf(opts.Out, "%s: connected as %s (scopes: %s)\n", conn.Provider, conn.ProviderLogin, conn.Scopes)
			return nil
		},
	}
}

func newGitDisconnectCmd(opts *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "disconnect PROVIDER",
		Short:   "Revoke the provider grant and delete the connection",
		Example: "  inari git disconnect github",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			provider := args[0]
			client, err := newGitConnClient(cmd, opts)
			if err != nil {
				return err
			}
			if err := client.Disconnect(cmd.Context(), provider); err != nil {
				return err
			}
			fmt.Fprintf(opts.Out, "Disconnected %s; the provider grant was revoked server-side.\n", provider)
			return nil
		},
	}
}

// openBrowser best-effort opens url in the system browser; failures are
// non-fatal since the URL is always printed.
func openBrowser(errOut io.Writer, url string) {
	if f, ok := errOut.(*os.File); ok {
		if st, err := f.Stat(); err != nil || st.Mode()&os.ModeCharDevice == 0 {
			return
		}
	}
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{url}
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		name, args = "xdg-open", []string{url}
	}
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err == nil {
		go func() { _ = cmd.Wait() }()
	}
}

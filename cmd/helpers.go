package cmd

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/7K-Inari/inari-api/gen/go/oas"

	"github.com/7K-Inari/inari-cli/internal/api"
	"github.com/7K-Inari/inari-cli/internal/config"
	"github.com/7K-Inari/inari-cli/internal/output"
)

func newAPIClient(cmd *cobra.Command, opts *GlobalOptions, cc config.Context) (*api.Client, error) {
	name, _, err := opts.resolveContext()
	if err != nil {
		return nil, err
	}
	return api.New(cmd.Context(), name, cc)
}

func apiError(status string, model *oas.ErrorModel) error {
	return api.Error(status, model)
}

func requireTenant(cc config.Context) error {
	if cc.Tenant == "" {
		return fmt.Errorf("no tenant in context; run 'inari login --tenant <slug>'")
	}
	return nil
}

func newTable(w interface{ Write([]byte) (int, error) }) *tabwriter.Writer {
	return output.NewTable(w)
}

func printStructured(opts *GlobalOptions, v any) error {
	return output.Structured(opts.Out, opts.Output, v)
}

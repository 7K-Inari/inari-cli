package cmd

import (
	"context"
	"fmt"
	"strings"
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

// resolveCatalogItemID maps a bare catalog item name (as shown by
// 'inari catalog list') to its fully-qualified ID ("<source>:<name>").
// Catalog API paths take the ID, but users naturally type the name — an
// argument that already contains ':' is assumed to be an ID and passed
// through. Fails when no item matches or the name is ambiguous.
func resolveCatalogItemID(ctx context.Context, client *api.Client, tenant, cluster, arg string) (string, error) {
	if strings.Contains(arg, ":") {
		return arg, nil
	}
	params := &oas.ListCatalogParams{}
	if cluster != "" {
		params.Cluster = &cluster
	}
	rsp, err := client.OAS.ListCatalogWithResponse(ctx, tenant, params)
	if err != nil {
		return "", err
	}
	if rsp.JSON200 == nil {
		return "", apiError(rsp.Status(), rsp.ApplicationproblemJSONDefault)
	}
	var matches []string
	if rsp.JSON200.Items != nil {
		for _, it := range *rsp.JSON200.Items {
			if it.Name == arg {
				matches = append(matches, it.Id)
			}
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("catalog item %q not found (see 'inari catalog list')", arg)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("catalog item name %q is ambiguous across sources (%s); use the full ID", arg, strings.Join(matches, ", "))
	}
}

func newTable(w interface{ Write([]byte) (int, error) }) *tabwriter.Writer {
	return output.NewTable(w)
}

func printStructured(opts *GlobalOptions, v any) error {
	return output.Structured(opts.Out, opts.Output, v)
}

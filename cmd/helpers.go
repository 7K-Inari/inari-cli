package cmd

import (
	"context"
	"fmt"
	"regexp"
	"sort"
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

var catalogIdentitySegment = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func canonicalCatalogIDParts(id string) (source, name string, ok bool, err error) {
	if !strings.Contains(id, "/") {
		return "", "", false, nil
	}
	parts := strings.Split(id, "/")
	if len(parts) != 2 || len(parts[0]) == 0 || len(parts[0]) > 40 || len(parts[1]) == 0 || len(parts[1]) > 63 ||
		!catalogIdentitySegment.MatchString(parts[0]) || !catalogIdentitySegment.MatchString(parts[1]) {
		return "", "", false, fmt.Errorf("invalid catalog item ID %q: trusted-source identity must be <source>/<package> with lowercase DNS-style segments", id)
	}
	return parts[0], parts[1], true, nil
}

func internalCatalogItemID(id string) bool {
	return strings.HasPrefix(id, "component:") || strings.HasPrefix(id, "discovered:")
}

// resolveCatalogItemID accepts canonical <source>/<package> identity directly.
// A bare package name is resolved only when it identifies exactly one item;
// duplicate names require an explicit source. Legacy external colon IDs are
// rejected, while internal component:/discovered: identities remain valid.
func resolveCatalogItemID(ctx context.Context, client *api.Client, tenant, cluster, arg string) (string, error) {
	if internalCatalogItemID(arg) {
		return arg, nil
	}
	if strings.Contains(arg, ":") {
		return "", fmt.Errorf("legacy catalog item ID %q is not supported; use canonical <source>/<package> identity", arg)
	}
	if _, _, canonical, err := canonicalCatalogIDParts(arg); err != nil {
		return "", err
	} else if canonical {
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
	sort.Strings(matches)
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("catalog item %q not found (see 'inari catalog list')", arg)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("catalog item name %q is ambiguous across sources (%s); use canonical <source>/<package> identity", arg, strings.Join(matches, ", "))
	}
}

func getCatalogItem(ctx context.Context, client *api.Client, tenant, itemID, cluster string) (*oas.ItemView, error) {
	if internalCatalogItemID(itemID) {
		params := &oas.GetCatalogItemParams{}
		if cluster != "" {
			params.Cluster = &cluster
		}
		rsp, err := client.OAS.GetCatalogItemWithResponse(ctx, tenant, itemID, params)
		if err != nil {
			return nil, err
		}
		if rsp.JSON200 == nil {
			return nil, apiError(rsp.Status(), rsp.ApplicationproblemJSONDefault)
		}
		return &rsp.JSON200.Item, nil
	}
	source, name, canonical, err := canonicalCatalogIDParts(itemID)
	if err != nil {
		return nil, err
	}
	if !canonical {
		return nil, fmt.Errorf("catalog item ID %q is not source-qualified", itemID)
	}
	params := &oas.GetSourceCatalogItemParams{}
	if cluster != "" {
		params.Cluster = &cluster
	}
	rsp, err := client.OAS.GetSourceCatalogItemWithResponse(ctx, tenant, source, name, params)
	if err != nil {
		return nil, err
	}
	if rsp.JSON200 == nil {
		return nil, apiError(rsp.Status(), rsp.ApplicationproblemJSONDefault)
	}
	return &rsp.JSON200.Item, nil
}

func newTable(w interface{ Write([]byte) (int, error) }) *tabwriter.Writer {
	return output.NewTable(w)
}

func printStructured(opts *GlobalOptions, v any) error {
	return output.Structured(opts.Out, opts.Output, v)
}

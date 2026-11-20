package hiroba

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/7K-Inari/inari-api/gen/go/oas"
)

type ContractSet struct {
	PluginContracts []string `json:"pluginContracts"`
	UIHostAPIs      []string `json:"uiHostApis"`
	ServerVersion   string   `json:"serverVersion"`
}

func fetchContracts(ctx context.Context, server string) (ContractSet, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	client, err := oas.NewClientWithResponses(server)
	if err != nil {
		return ContractSet{}, err
	}
	response, err := client.GetMetaContractsWithResponse(ctx)
	if err != nil {
		return ContractSet{}, err
	}
	if response.JSON200 == nil {
		status := "unknown response"
		if response.HTTPResponse != nil {
			status = response.HTTPResponse.Status
		}
		return ContractSet{}, fmt.Errorf("meta/contracts returned %s", status)
	}
	metadata := response.JSON200
	contracts := ContractSet{ServerVersion: metadata.ServerVersion}
	if metadata.PluginContracts != nil {
		contracts.PluginContracts = *metadata.PluginContracts
	}
	if metadata.UiHostApis != nil {
		contracts.UIHostAPIs = *metadata.UiHostApis
	}
	if err := validateContracts(contracts); err != nil {
		return ContractSet{}, err
	}
	return contracts, nil
}

func validateContracts(contracts ContractSet) error {
	if strings.TrimSpace(contracts.ServerVersion) == "" {
		return fmt.Errorf("serverVersion is required")
	}
	for name, values := range map[string][]string{"pluginContracts": contracts.PluginContracts, "uiHostApis": contracts.UIHostAPIs} {
		if len(values) == 0 {
			return fmt.Errorf("%s must be non-empty", name)
		}
		if !sort.StringsAreSorted(values) {
			return fmt.Errorf("%s must be sorted", name)
		}
		for index, value := range values {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("%s contains an empty contract", name)
			}
			if index > 0 && values[index-1] == value {
				return fmt.Errorf("%s contains duplicate contract %q", name, value)
			}
		}
	}
	return nil
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

// WarnOnContractMismatch performs the optional, nonblocking contract check.
func WarnOnContractMismatch(ctx context.Context, server string, opts Options, out io.Writer) {
	backend := opts.WithPlugin || opts.Type == "extension"
	if !backend && !opts.WithUI {
		return
	}
	contracts, err := fetchContracts(ctx, server)
	if err != nil {
		fmt.Fprintf(out, "warning: contract check unavailable: %v; continuing\n", err)
		return
	}
	warnings := []string{}
	if backend && !contains(contracts.PluginContracts, "inari.plugin.v1") {
		warnings = append(warnings, fmt.Sprintf("backend requires inari.plugin.v1 but server %s advertises %s", contracts.ServerVersion, strings.Join(contracts.PluginContracts, ",")))
	}
	if opts.WithUI && !contains(contracts.UIHostAPIs, "inari.ui.v1") {
		warnings = append(warnings, fmt.Sprintf("UI requires inari.ui.v1 but server %s advertises %s", contracts.ServerVersion, strings.Join(contracts.UIHostAPIs, ",")))
	}
	if len(warnings) > 0 {
		fmt.Fprintf(out, "warning: incompatible Inari contracts: %s; continuing\n", strings.Join(warnings, "; "))
	}
}

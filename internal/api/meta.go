package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/7K-Inari/inari-api/gen/go/oas"
)

const contractMetadataTimeout = 2 * time.Second

// GetContractMetadata reads tenant-free server contract support without any
// authentication or interactive login flow. An empty server returns nil.
func GetContractMetadata(ctx context.Context, server string) (*oas.ContractMetadata, error) {
	if strings.TrimSpace(server) == "" {
		return nil, nil
	}
	httpClient := &http.Client{Timeout: contractMetadataTimeout}
	client, err := oas.NewClientWithResponses(server, oas.WithHTTPClient(httpClient))
	if err != nil {
		return nil, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, contractMetadataTimeout)
	defer cancel()
	resp, err := client.GetMetaContractsWithResponse(requestCtx)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return nil, fmt.Errorf("GET /api/v1/meta/contracts returned %s", resp.Status())
	}
	metadata := resp.JSON200
	if metadata.PluginContracts == nil || len(*metadata.PluginContracts) == 0 ||
		metadata.UiHostApis == nil || len(*metadata.UiHostApis) == 0 || strings.TrimSpace(metadata.ServerVersion) == "" {
		return nil, fmt.Errorf("GET /api/v1/meta/contracts returned an incomplete contract response")
	}
	return metadata, nil
}

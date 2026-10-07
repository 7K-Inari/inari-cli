package cmd

import (
	"testing"

	"github.com/7K-Inari/inari-api/contract/clusterids"
)

// TestClusterIDCharsetContract validates the CLI's cluster-ID validator
// against the canonical IDs inari-server issues, pinned in the inari-api
// contract testdata. Regression guard: the CLI charset previously
// rejected every server-issued cluster:<uuid> ID.
func TestClusterIDCharsetContract(t *testing.T) {
	for _, id := range clusterids.ValidIDs {
		if !clusterIDRe.MatchString(id) {
			t.Errorf("clusterIDRe rejects server-issued/contract ID %q", id)
		}
	}
	for _, id := range clusterids.InvalidIDs {
		if clusterIDRe.MatchString(id) {
			t.Errorf("clusterIDRe accepts contract-invalid ID %q", id)
		}
	}
}

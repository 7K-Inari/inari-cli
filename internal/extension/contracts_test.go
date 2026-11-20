package extension

import "testing"

func TestContractIntersection(t *testing.T) {
	if !ContractIntersection(BackendContract, []string{"inari.plugin.v2", BackendContract}) {
		t.Fatal("expected shared contract")
	}
	if ContractIntersection(BackendContract, []string{"inari.plugin.v2"}) {
		t.Fatal("unexpected cross-major intersection")
	}
	if ContractIntersection(BackendContract, nil) {
		t.Fatal("empty support set must not intersect")
	}
}

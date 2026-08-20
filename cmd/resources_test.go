package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func instanceFixture() map[string]any {
	return map[string]any{
		"id": "inst-1", "catalogItemId": "postgres-aws", "clusterId": "clu-1",
		"orgId": "acme", "ownerTeam": "platform", "health": "Healthy", "state": "Ready",
		"version": "1.2.0", "latestVersion": "1.3.0", "newVersionAvailable": true,
		"generation": 3, "managementMode": "adopt", "syncState": "Synced",
		"resourceRef": map[string]any{"kind": "PostgreSQL", "name": "orders-db", "namespace": "data"},
		"spec": map[string]any{"size": "large"},
		"createdAt": "2026-08-01T00:00:00Z", "updatedAt": "2026-08-10T00:00:00Z",
	}
}

func resourcesFakeServer(t *testing.T, assertQuery func(r *http.Request)) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tenants/acme/instances", func(w http.ResponseWriter, r *http.Request) {
		if assertQuery != nil {
			assertQuery(r)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"instances": []map[string]any{instanceFixture()}})
	})
	mux.HandleFunc("/api/v1/tenants/acme/instances/inst-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"instance": instanceFixture()})
	})
	return httptest.NewServer(mux)
}

func TestResourcesListWithFilters(t *testing.T) {
	srv := resourcesFakeServer(t, func(r *http.Request) {
		q := r.URL.Query()
		if q.Get("cluster") != "clu-1" || q.Get("health") != "Healthy" {
			t.Errorf("query = %q", r.URL.RawQuery)
		}
	})
	defer srv.Close()

	out := setupAuthedContext(t, srv.URL)
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"resources", "list", "--cluster", "clu-1", "--health", "Healthy"})
	if err := root.Execute(); err != nil {
		t.Fatalf("resources list error = %v", err)
	}
	got := out.String()
	for _, want := range []string{"inst-1", "postgres-aws", "Healthy", "Ready", "1.2.0", "-> 1.3.0"} {
		if !strings.Contains(got, want) {
			t.Errorf("resources list missing %q: %q", want, got)
		}
	}
}

func TestResourcesGet(t *testing.T) {
	srv := resourcesFakeServer(t, nil)
	defer srv.Close()

	out := setupAuthedContext(t, srv.URL)
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"resources", "get", "inst-1"})
	if err := root.Execute(); err != nil {
		t.Fatalf("resources get error = %v", err)
	}
	got := out.String()
	for _, want := range []string{"postgres-aws@1.2.0", "Healthy", "Synced", "PostgreSQL/orders-db", "ns data", "1.3.0 available"} {
		if !strings.Contains(got, want) {
			t.Errorf("resources get missing %q: %q", want, got)
		}
	}
}

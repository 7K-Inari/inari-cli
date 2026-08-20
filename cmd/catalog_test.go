package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func catalogItemFixture() map[string]any {
	return map[string]any{
		"id":             "item-1",
		"name":           "postgres-aws",
		"displayName":    "PostgreSQL on AWS",
		"description":    "Managed PostgreSQL via Crossplane",
		"source":         "curated",
		"approvalPolicy": "auto",
		"createdAt":      "2026-08-01T00:00:00Z",
		"versions": []map[string]any{
			{"id": "v1", "itemId": "item-1", "version": "1.2.0", "channel": "stable"},
			{"id": "v2", "itemId": "item-1", "version": "1.3.0-rc.1", "channel": "incubating"},
		},
	}
}

func catalogFakeServer(t *testing.T, assertQuery func(r *http.Request)) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tenants/acme/catalog", func(w http.ResponseWriter, r *http.Request) {
		if assertQuery != nil {
			assertQuery(r)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{catalogItemFixture()}})
	})
	mux.HandleFunc("/api/v1/tenants/acme/catalog/postgres-aws", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"item": catalogItemFixture()})
	})
	return httptest.NewServer(mux)
}

func TestCatalogListTable(t *testing.T) {
	srv := catalogFakeServer(t, func(r *http.Request) {
		if r.URL.Query().Get("cluster") != "clu-1" {
			t.Errorf("expected cluster=clu-1 query, got %q", r.URL.RawQuery)
		}
	})
	defer srv.Close()

	out := setupAuthedContext(t, srv.URL)
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"catalog", "list", "--cluster", "clu-1"})
	if err := root.Execute(); err != nil {
		t.Fatalf("catalog list error = %v", err)
	}
	got := out.String()
	for _, want := range []string{"postgres-aws", "PostgreSQL on AWS", "curated", "1.2.0", "auto"} {
		if !strings.Contains(got, want) {
			t.Errorf("catalog list missing %q: %q", want, got)
		}
	}
}

func TestCatalogDescribe(t *testing.T) {
	srv := catalogFakeServer(t, nil)
	defer srv.Close()

	out := setupAuthedContext(t, srv.URL)
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"catalog", "describe", "postgres-aws"})
	if err := root.Execute(); err != nil {
		t.Fatalf("catalog describe error = %v", err)
	}
	got := out.String()
	for _, want := range []string{"postgres-aws", "Managed PostgreSQL via Crossplane", "1.2.0 (channel stable)", "1.3.0-rc.1 (channel incubating)"} {
		if !strings.Contains(got, want) {
			t.Errorf("describe output missing %q: %q", want, got)
		}
	}
}

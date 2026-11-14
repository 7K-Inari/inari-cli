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
		"id":             "core/postgres-aws",
		"name":           "postgres-aws",
		"displayName":    "PostgreSQL on AWS",
		"description":    "Managed PostgreSQL via Crossplane",
		"source":         "curated",
		"sourceId":       "core",
		"trustTier":      "reviewed",
		"approvalPolicy": "auto",
		"createdAt":      "2026-08-01T00:00:00Z",
		"versions": []map[string]any{
			{"id": "v1", "itemId": "core/postgres-aws", "version": "1.2.0", "channel": "stable", "artifactDigest": "sha256:52ccaee6d4dd272e54bfccda77738b42e1edf0e4a20c27e23f0b6c15d01aef79"},
			{"id": "v2", "itemId": "core/postgres-aws", "version": "1.3.0-rc.1", "channel": "incubating"},
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
	mux.HandleFunc("/api/v1/tenants/acme/catalog/core/postgres-aws", func(w http.ResponseWriter, r *http.Request) {
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
	for _, want := range []string{"postgres-aws", "PostgreSQL on AWS", "core", "reviewed", "1.2.0", "auto"} {
		if !strings.Contains(got, want) {
			t.Errorf("catalog list missing %q: %q", want, got)
		}
	}
}

func TestCatalogDescribeCanonicalID(t *testing.T) {
	srv := catalogFakeServer(t, func(r *http.Request) {
		t.Errorf("canonical ID must not require a bare-name list lookup: %s", r.URL)
	})
	defer srv.Close()

	out := setupAuthedContext(t, srv.URL)
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"catalog", "describe", "core/postgres-aws"})
	if err := root.Execute(); err != nil {
		t.Fatalf("catalog describe canonical ID error = %v", err)
	}
	if !strings.Contains(out.String(), "ID:          core/postgres-aws") {
		t.Errorf("canonical describe output = %q", out.String())
	}
}

func TestCatalogDescribeRejectsLegacyID(t *testing.T) {
	out := setupAuthedContext(t, "http://127.0.0.1:1")
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"catalog", "describe", "curated:postgres-aws"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "legacy") || !strings.Contains(err.Error(), "<source>/<package>") {
		t.Fatalf("expected legacy-ID rejection with canonical example, got %v", err)
	}
}

func TestCatalogDescribeRejectsMalformedCanonicalIDs(t *testing.T) {
	for _, id := range []string{"core/team/postgres-aws", "Core/postgres-aws", "core//postgres-aws", "/postgres-aws"} {
		out := setupAuthedContext(t, "http://127.0.0.1:1")
		root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
		root.SetArgs([]string{"catalog", "describe", id})
		err := root.Execute()
		if err == nil || !strings.Contains(err.Error(), "<source>/<package>") {
			t.Errorf("ID %q: expected canonical identity error, got %v", id, err)
		}
	}
}

func TestCatalogDescribeUnsignedDevelopmentOnly(t *testing.T) {
	item := catalogItemFixture()
	item["id"] = "community/postgres-aws"
	item["sourceId"] = "community"
	item["trustTier"] = "experimental"
	item["unsigned"] = true
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tenants/acme/catalog/community/postgres-aws", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"item": item})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	out := setupAuthedContext(t, srv.URL)
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"catalog", "describe", "community/postgres-aws"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"community", "experimental", "Unsigned:    development only"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("unsigned describe missing %q: %s", want, out.String())
		}
	}
}

func TestCatalogDescribeDuplicateNameRequiresSource(t *testing.T) {
	core := catalogItemFixture()
	community := catalogItemFixture()
	community["id"] = "community/postgres-aws"
	community["sourceId"] = "community"
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tenants/acme/catalog", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{core, community}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	out := setupAuthedContext(t, srv.URL)
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"catalog", "describe", "postgres-aws"})
	err := root.Execute()
	if err == nil {
		t.Fatal("duplicate bare name unexpectedly resolved")
	}
	for _, want := range []string{"ambiguous", "community/postgres-aws", "core/postgres-aws", "<source>/<package>"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("duplicate-name error missing %q: %v", want, err)
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
	for _, want := range []string{"postgres-aws", "Managed PostgreSQL via Crossplane", "core", "reviewed", "1.2.0 (channel stable)", "sha256:52ccaee6d4dd272e54bfccda77738b42e1edf0e4a20c27e23f0b6c15d01aef79", "1.3.0-rc.1 (channel incubating)"} {
		if !strings.Contains(got, want) {
			t.Errorf("describe output missing %q: %q", want, got)
		}
	}
}

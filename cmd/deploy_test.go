package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/7K-Inari/inari-cli/internal/auth"
	"github.com/7K-Inari/inari-cli/internal/config"
)

func deployFakeServer(t *testing.T, onDeploy func(body map[string]any)) *httptest.Server {
	t.Helper()
	item := catalogItemFixture()
	item["versions"] = []map[string]any{{
		"id": "v1", "itemId": "item-1", "version": "1.2.0", "channel": "stable",
		"schema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"size":     map[string]any{"type": "string", "enum": []any{"small", "large"}},
				"replicas": map[string]any{"type": "integer"},
			},
			"required": []any{"size"},
		},
	}}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tenants/acme/catalog/postgres-aws", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"item": item})
	})
	mux.HandleFunc("/api/v1/tenants/acme/deploys", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode deploy body: %v", err)
		}
		if onDeploy != nil {
			onDeploy(body)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"deploy": map[string]any{
				"InstanceID": "inst-1", "Status": "Provisioning", "Version": "1.2.0",
				"CommitSHA": "abc123", "PRURL": "https://git.example/pr/7", "ApprovalID": "",
			},
		})
	})
	mux.HandleFunc("/api/v1/tenants/acme/policies/evaluate", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"decision": map[string]any{"allow": true, "violations": []any{}, "warnings": []any{}},
		})
	})
	return httptest.NewServer(mux)
}

func TestDeployNonInteractiveWithFileAndSet(t *testing.T) {
	dir := t.TempDir()
	values := filepath.Join(dir, "values.yaml")
	if err := os.WriteFile(values, []byte("size: small\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var gotBody map[string]any
	srv := deployFakeServer(t, func(b map[string]any) { gotBody = b })
	defer srv.Close()

	out := setupAuthedContext(t, srv.URL)
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"deploy", "postgres-aws", "--cluster", "clu-1", "--file", values, "--set", "replicas=3"})
	if err := root.Execute(); err != nil {
		t.Fatalf("deploy error = %v", err)
	}
	if !strings.Contains(out.String(), "inst-1") || !strings.Contains(out.String(), "https://git.example/pr/7") {
		t.Errorf("deploy output = %q", out.String())
	}
	if gotBody["clusterId"] != "clu-1" || gotBody["itemId"] != "postgres-aws" {
		t.Fatalf("deploy body = %v", gotBody)
	}
	spec, ok := gotBody["spec"].(map[string]any)
	if !ok {
		t.Fatalf("spec missing in %v", gotBody)
	}
	if spec["size"] != "small" {
		t.Errorf("spec.size = %v", spec["size"])
	}
	if spec["replicas"] != float64(3) {
		t.Errorf("spec.replicas = %v (%T)", spec["replicas"], spec["replicas"])
	}
}

func TestDeployMissingRequiredValueFails(t *testing.T) {
	srv := deployFakeServer(t, nil)
	defer srv.Close()

	out := setupAuthedContext(t, srv.URL)
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"deploy", "postgres-aws", "--cluster", "clu-1", "--set", "replicas=3"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "missing required values: size") {
		t.Fatalf("expected missing-required error, got %v", err)
	}
}

func TestDeployRequiresClusterFlag(t *testing.T) {
	srv := deployFakeServer(t, nil)
	defer srv.Close()

	out := setupAuthedContext(t, srv.URL)
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"deploy", "postgres-aws", "--set", "size=small"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "--cluster is required") {
		t.Fatalf("expected --cluster error, got %v", err)
	}
}

func TestDeployDryRunEvaluatesPolicy(t *testing.T) {
	var deployed bool
	srv := deployFakeServer(t, func(b map[string]any) { deployed = true })
	defer srv.Close()

	out := setupAuthedContext(t, srv.URL)
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"deploy", "postgres-aws", "--cluster", "clu-1", "--set", "size=large", "--dry-run", "-o", "json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("dry-run error = %v", err)
	}
	if deployed {
		t.Fatal("dry-run must not call the deploy endpoint")
	}
	if !strings.Contains(out.String(), `"allow": true`) {
		t.Errorf("dry-run output = %q", out.String())
	}
}

func TestDeployDryRunDefaultOutputRendersDecision(t *testing.T) {
	srv := deployFakeServer(t, nil)
	defer srv.Close()

	out := setupAuthedContext(t, srv.URL)
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"deploy", "postgres-aws", "--cluster", "clu-1", "--set", "size=large", "--dry-run"})
	if err := root.Execute(); err != nil {
		t.Fatalf("dry-run with default table output must not error, got %v", err)
	}
	if !strings.Contains(strings.ToLower(out.String()), "allow") {
		t.Errorf("dry-run table output = %q", out.String())
	}
}

func TestDeployUnknownVersionFails(t *testing.T) {
	var deployed bool
	srv := deployFakeServer(t, func(b map[string]any) { deployed = true })
	defer srv.Close()

	out := setupAuthedContext(t, srv.URL)
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"deploy", "postgres-aws", "--cluster", "clu-1", "--version", "9.9.9", "--set", "size=large"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), `version "9.9.9"`) {
		t.Fatalf("expected unknown-version error, got %v", err)
	}
	if deployed {
		t.Fatal("no deploy call must be made for an unknown version")
	}
}

func TestCatalogListRejectsUnknownOutputFormat(t *testing.T) {
	srv := deployFakeServer(t, nil)
	defer srv.Close()

	out := setupAuthedContext(t, srv.URL)
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"-o", "bogus", "catalog", "list"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), `unknown output format "bogus"`) {
		t.Fatalf("expected unknown-format error, got %v", err)
	}
}

func TestCatalogListRequiresTenant(t *testing.T) {
	srv := deployFakeServer(t, nil)
	defer srv.Close()

	out := setupAuthedContext(t, srv.URL)
	t.Setenv("INARI_CONFIG_DIR", t.TempDir())
	cfg := &config.Config{}
	cfg.SetContext("default", config.Context{Server: srv.URL, Issuer: srv.URL})
	cfg.CurrentContext = "default"
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	cache, err := auth.NewCache()
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Save("default", &auth.Token{AccessToken: "tok", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"catalog", "list"})
	err = root.Execute()
	if err == nil || !strings.Contains(err.Error(), "no tenant") {
		t.Fatalf("expected no-tenant error, got %v", err)
	}
}

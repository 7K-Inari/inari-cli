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

	"github.com/7K-Inari/inari-api/gen/go/oas"
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
	mux.HandleFunc("/api/v1/tenants/acme/catalog", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{item}})
	})
	mux.HandleFunc("/api/v1/tenants/acme/catalog/core/postgres-aws", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"item": item})
	})
	mux.HandleFunc("/api/v1/tenants/acme/deployment-plans/preflight", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		source := body["source"].(map[string]any)
		deployment := body["deployment"].(map[string]any)
		version := source["version"]
		if version == nil {
			version = "1.2.0"
		}
		plan := map[string]any{
			"id": "dp-1", "orgId": "acme", "orgSlug": "acme", "operation": "deploy", "source": source, "deployment": deployment,
			"app":           map[string]any{"itemId": source["itemId"], "version": version, "digest": "sha256:abc"},
			"observability": map[string]any{"mode": "none"}, "planFingerprint": "sha256:plan", "state": "preflight",
			"gate":      map[string]any{"companionRendering": false, "deployedVerification": false},
			"createdAt": "2026-11-15T21:42:00Z", "updatedAt": "2026-11-15T21:42:00Z",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"preflight": map[string]any{
				"plan": plan, "schema": item["versions"].([]map[string]any)[0]["schema"], "compatiblePacks": []any{}, "monitoringTargets": []any{}, "providers": []any{},
				"deploymentTargets": []map[string]any{{"id": "rt-clu-1", "orgId": "acme", "name": "test", "resourceRef": map[string]any{"kind": "cluster", "id": "clu-1"}, "capabilities": []string{"deployment.cluster"}, "config": map[string]any{}, "enabled": true, "isDefault": false, "revision": 1, "binding": map[string]any{}, "readiness": map[string]any{"state": "ready"}, "createdAt": "2026-11-15T21:42:00Z", "updatedAt": "2026-11-15T21:42:00Z"}},
			},
		})
	})
	mux.HandleFunc("/api/v1/tenants/acme/deployment-plans", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode deploy body: %v", err)
		}
		if onDeploy != nil {
			onDeploy(body)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"plan": map[string]any{
				"id": "dp-1", "orgId": "acme", "orgSlug": "acme", "operation": "deploy", "source": body["source"], "deployment": body["deployment"],
				"app":           map[string]any{"itemId": "core/postgres-aws", "version": "1.2.0", "digest": "sha256:abc"},
				"observability": map[string]any{"mode": "none"}, "planFingerprint": "sha256:plan", "state": "submitted",
				"gate": map[string]any{"companionRendering": false, "deployedVerification": false}, "appInstanceId": "inst-1",
				"commitSha": "abc123", "prUrl": "https://git.example/pr/7", "createdAt": "2026-11-15T21:42:00Z", "updatedAt": "2026-11-15T21:42:00Z",
			},
		})
	})
	mux.HandleFunc("/api/v1/tenants/acme/policies/evaluate", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
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
	root.SetArgs([]string{"deploy", "core/postgres-aws", "--cluster", "clu-1", "--file", values, "--set", "replicas=3"})
	if err := root.Execute(); err != nil {
		t.Fatalf("deploy error = %v", err)
	}
	if !strings.Contains(out.String(), "inst-1") || !strings.Contains(out.String(), "https://git.example/pr/7") {
		t.Errorf("deploy output = %q", out.String())
	}
	source := gotBody["source"].(map[string]any)
	deployment := gotBody["deployment"].(map[string]any)
	if deployment["targetId"] != "rt-clu-1" || source["itemId"] != "core/postgres-aws" {
		t.Fatalf("deploy body = %v", gotBody)
	}
	spec, ok := deployment["spec"].(map[string]any)
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

func TestLatestChannelVersionUsesSemVerOrdering(t *testing.T) {
	versions := []oas.CatalogItemVersion{
		{Version: "1.9.0", Channel: "incubating"},
		{Version: "1.10.0", Channel: "incubating"},
		{Version: "1.11.0", Channel: "stable"},
		{Version: "not-semver", Channel: "incubating"},
	}
	got, err := latestChannelVersion(oas.ItemView{Versions: &versions}, "incubating")
	if err != nil || got != "1.10.0" {
		t.Fatalf("latestChannelVersion = %q, %v", got, err)
	}
	if _, err := latestChannelVersion(oas.ItemView{Versions: &versions}, "missing"); err == nil {
		t.Fatal("unknown channel unexpectedly resolved")
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

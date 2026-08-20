package cmd

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/7K-Inari/inari-cli/internal/auth"
	"github.com/7K-Inari/inari-cli/internal/config"
)

// setupAuthedContext writes a config + valid token pointing at srv and returns
// output buffers for a root command.
func setupAuthedContext(t *testing.T, server string) *bytes.Buffer {
	t.Helper()
	t.Setenv("INARI_CONFIG_DIR", t.TempDir())
	cfg := &config.Config{}
	cfg.SetContext("default", config.Context{Server: server, Issuer: server, Tenant: "acme"})
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
	return &bytes.Buffer{}
}

func TestClusterRegisterPrintsManifest(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tenants/acme/clusters", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"cluster": map[string]any{
				"id": "clu-1", "name": "prod-eu", "orgId": "acme", "state": "Pending",
				"createdAt": time.Now().UTC().Format(time.RFC3339),
			},
		})
	})
	manifest := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: inari-agent-bootstrap\n"
	mux.HandleFunc("/api/v1/tenants/acme/clusters/clu-1/install-manifest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if b, err := json.Marshal(base64.StdEncoding.EncodeToString([]byte(manifest))); err == nil {
			_, _ = w.Write(b)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	out := setupAuthedContext(t, srv.URL)
	errOut := &bytes.Buffer{}
	root := NewRootCmd("dev", "none", "now", out, errOut)
	root.SetArgs([]string{"cluster", "register", "prod-eu", "--label", "env=prod"})
	if err := root.Execute(); err != nil {
		t.Fatalf("cluster register error = %v", err)
	}
	if !strings.Contains(out.String(), "inari-agent-bootstrap") {
		t.Errorf("stdout should contain manifest, got %q", out.String())
	}
	if gotBody["name"] != "prod-eu" {
		t.Errorf("request name = %v", gotBody["name"])
	}
	labels, ok := gotBody["labels"].(map[string]any)
	if !ok || labels["env"] != "prod" {
		t.Errorf("request labels = %v", gotBody["labels"])
	}
	if !strings.Contains(errOut.String(), "one-time") {
		t.Errorf("expected one-time token warning on stderr, got %q", errOut.String())
	}
}

func TestClusterListTable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tenants/acme/clusters", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"clusters": []map[string]any{{
				"id": "clu-1", "name": "prod-eu", "orgId": "acme", "state": "Active",
				"kubernetesVersion": "1.34.1", "createdAt": time.Now().UTC().Format(time.RFC3339),
			}},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	out := setupAuthedContext(t, srv.URL)
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"cluster", "list"})
	if err := root.Execute(); err != nil {
		t.Fatalf("cluster list error = %v", err)
	}
	got := out.String()
	for _, want := range []string{"NAME", "prod-eu", "Active", "1.34.1"} {
		if !strings.Contains(got, want) {
			t.Errorf("cluster list output missing %q: %q", want, got)
		}
	}
}

func TestClusterRegisterSurfacesAPIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tenants/acme/clusters", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"title": "Conflict", "detail": "cluster name already exists", "status": 409,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	out := setupAuthedContext(t, srv.URL)
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"cluster", "register", "prod-eu"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "cluster name already exists") {
		t.Fatalf("expected API error surfaced, got %v", err)
	}
}

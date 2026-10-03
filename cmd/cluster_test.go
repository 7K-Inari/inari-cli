package cmd

import (
	"bytes"
	"encoding/base64"
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

func accessInfoServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tenants/acme/clusters/clu-1/access-info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"accessInfo": map[string]any{
				"issuerUrl":       "https://keycloak.example.com/realms/inari",
				"kubectlClientId": "org-acme-kubectl",
				"audience":        "kubernetes",
				"organization":    "acme",
			},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestClusterKubeconfigDirect(t *testing.T) {
	srv := accessInfoServer(t)
	out := setupAuthedContext(t, srv.URL)
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"cluster", "kubeconfig", "clu-1", "--server", "https://api.prod:6443"})
	if err := root.Execute(); err != nil {
		t.Fatalf("cluster kubeconfig error = %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"server: https://api.prod:6443",
		"command: kubectl",
		"oidc-login",
		"get-token",
		"--oidc-issuer-url=https://keycloak.example.com/realms/inari",
		"--oidc-client-id=org-acme-kubectl",
		"--oidc-extra-scope=organization",
		"client.authentication.k8s.io",
		"interactiveMode: Never",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("kubeconfig missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "secret") || strings.Contains(got, "token:") {
		t.Errorf("kubeconfig must carry no secrets:\n%s", got)
	}
}

func TestClusterKubeconfigRequiresServerInDirectMode(t *testing.T) {
	srv := accessInfoServer(t)
	out := setupAuthedContext(t, srv.URL)
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"cluster", "kubeconfig", "clu-1"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "--server") {
		t.Fatalf("want --server required error, got %v", err)
	}
}

func TestClusterKubeconfigDeviceCode(t *testing.T) {
	srv := accessInfoServer(t)
	out := setupAuthedContext(t, srv.URL)
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"cluster", "kubeconfig", "clu-1", "--server", "https://api.prod:6443", "--grant-type", "device-code"})
	if err := root.Execute(); err != nil {
		t.Fatalf("cluster kubeconfig error = %v", err)
	}
	if !strings.Contains(out.String(), "--grant-type=device-code") {
		t.Errorf("device-code grant missing:\n%s", out.String())
	}
}

func TestClusterKubeconfigGateway(t *testing.T) {
	srv := accessInfoServer(t)
	out := setupAuthedContext(t, srv.URL)
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"cluster", "kubeconfig", "clu-1", "--gateway"})
	if err := root.Execute(); err != nil {
		t.Fatalf("cluster kubeconfig --gateway error = %v", err)
	}
	got := out.String()
	if !strings.Contains(got, srv.URL+"/api/v1/tenants/acme/clusters/clu-1/proxy") {
		t.Errorf("gateway server URL missing:\n%s", got)
	}
}

func TestClusterKubeconfigRejectsBadGrantType(t *testing.T) {
	srv := accessInfoServer(t)
	out := setupAuthedContext(t, srv.URL)
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"cluster", "kubeconfig", "clu-1", "--server", "https://api.prod:6443", "--grant-type", "password"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "grant-type") {
		t.Fatalf("want grant-type error, got %v", err)
	}
}

func TestClusterKubeconfigRejectsServerWithGateway(t *testing.T) {
	srv := accessInfoServer(t)
	out := setupAuthedContext(t, srv.URL)
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"cluster", "kubeconfig", "clu-1", "--gateway", "--server", "https://api.prod:6443"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("want mutually-exclusive error, got %v", err)
	}
}

func TestClusterKubeconfigRejectsUnsafeClusterID(t *testing.T) {
	srv := accessInfoServer(t)
	out := setupAuthedContext(t, srv.URL)
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"cluster", "kubeconfig", "bad id\ninjected: true", "--server", "https://api.prod:6443"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "invalid cluster ID") {
		t.Fatalf("want invalid cluster ID error, got %v", err)
	}
}

func TestClusterKubeconfigRejectsIncompleteAccessInfo(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tenants/acme/clusters/clu-1/access-info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"accessInfo": map[string]any{"issuerUrl": "", "kubectlClientId": "org-acme-kubectl"},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	out := setupAuthedContext(t, srv.URL)
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"cluster", "kubeconfig", "clu-1", "--server", "https://api.prod:6443"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "incomplete access info") {
		t.Fatalf("want incomplete access info error, got %v", err)
	}
}

// --- cluster connect ---

const connectKubeconfigYAML = `apiVersion: v1
kind: Config
clusters:
- name: acme-clu-1
  cluster:
    server: https://gateway.example.com/acme/clu-1
users:
- name: acme-clu-1
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1
      command: kubectl
      args: ["oidc-login", "get-token"]
contexts:
- name: acme-clu-1
  context:
    cluster: acme-clu-1
    user: acme-clu-1
current-context: acme-clu-1
`

const connectDirectContextYAML = `- name: acme-clu-1-direct
  context:
    cluster: acme-clu-1-direct
    user: acme-clu-1
`

// connectServer serves access-info (with the given extra fields) and a
// server-rendered kubeconfig that optionally includes a -direct context.
func connectServer(t *testing.T, accessInfo map[string]any, withDirect bool) *httptest.Server {
	t.Helper()
	base := map[string]any{
		"issuerUrl":       "https://keycloak.example.com/realms/inari",
		"kubectlClientId": "org-acme-kubectl",
		"audience":        "kubernetes",
		"organization":    "acme",
	}
	for k, v := range accessInfo {
		base[k] = v
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tenants/acme/clusters/clu-1/access-info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"accessInfo": base})
	})
	mux.HandleFunc("/api/v1/tenants/acme/clusters/clu-1/kubeconfig", func(w http.ResponseWriter, r *http.Request) {
		yaml := connectKubeconfigYAML
		if r.URL.Query().Get("server") != "" || withDirect {
			yaml = strings.Replace(connectKubeconfigYAML, "current-context:",
				connectDirectContextYAML+"current-context:", 1)
			// add the direct cluster entry too
			yaml = strings.Replace(yaml, "users:\n",
				"- name: acme-clu-1-direct\n  cluster:\n    server: "+r.URL.Query().Get("server")+"\nusers:\n", 1)
		}
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write([]byte(yaml))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func runConnect(t *testing.T, srv *httptest.Server, args ...string) (string, string, error) {
	t.Helper()
	out := setupAuthedContext(t, srv.URL)
	errOut := &bytes.Buffer{}
	root := NewRootCmd("dev", "none", "now", out, errOut)
	root.SetArgs(append([]string{"cluster", "connect", "clu-1"}, args...))
	err := root.Execute()
	return out.String(), errOut.String(), err
}

func loadKubeconfigFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read kubeconfig: %v", err)
	}
	return string(b)
}

func TestClusterConnectMergesIntoEmptyKubeconfig(t *testing.T) {
	srv := connectServer(t, map[string]any{"tunnelAvailable": true}, false)
	kubeconfig := filepath.Join(t.TempDir(), "config")
	_, _, err := runConnect(t, srv, "--kubeconfig", kubeconfig)
	if err != nil {
		t.Fatalf("connect error = %v", err)
	}
	got := loadKubeconfigFile(t, kubeconfig)
	if !strings.Contains(got, "acme-clu-1") {
		t.Errorf("merged kubeconfig missing context:\n%s", got)
	}
	if !strings.Contains(got, "current-context: acme-clu-1") {
		t.Errorf("current-context not set:\n%s", got)
	}
}

func TestClusterConnectDualContext(t *testing.T) {
	srv := connectServer(t, map[string]any{"tunnelAvailable": true}, false)
	kubeconfig := filepath.Join(t.TempDir(), "config")
	_, _, err := runConnect(t, srv, "--kubeconfig", kubeconfig, "--server", "https://api.prod:6443")
	if err != nil {
		t.Fatalf("connect error = %v", err)
	}
	got := loadKubeconfigFile(t, kubeconfig)
	for _, want := range []string{"acme-clu-1\n", "acme-clu-1-direct", "current-context: acme-clu-1\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("merged kubeconfig missing %q:\n%s", want, got)
		}
	}
}

func TestClusterConnectNoSetCurrentContext(t *testing.T) {
	srv := connectServer(t, map[string]any{"tunnelAvailable": true}, false)
	kubeconfig := filepath.Join(t.TempDir(), "config")
	existing := `apiVersion: v1
kind: Config
clusters:
- name: other
  cluster:
    server: https://other:6443
users:
- name: other
  user: {}
contexts:
- name: other
  context:
    cluster: other
    user: other
current-context: other
`
	if err := os.WriteFile(kubeconfig, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := runConnect(t, srv, "--kubeconfig", kubeconfig, "--set-current-context=false")
	if err != nil {
		t.Fatalf("connect error = %v", err)
	}
	got := loadKubeconfigFile(t, kubeconfig)
	if !strings.Contains(got, "current-context: other") {
		t.Errorf("current-context should be preserved:\n%s", got)
	}
	if !strings.Contains(got, "acme-clu-1") || !strings.Contains(got, "other") {
		t.Errorf("expected both contexts:\n%s", got)
	}
}

func TestClusterConnectReplacesDuplicateContext(t *testing.T) {
	srv := connectServer(t, map[string]any{"tunnelAvailable": true}, false)
	kubeconfig := filepath.Join(t.TempDir(), "config")
	existing := `apiVersion: v1
kind: Config
clusters:
- name: acme-clu-1
  cluster:
    server: https://stale:6443
users:
- name: acme-clu-1
  user: {}
contexts:
- name: acme-clu-1
  context:
    cluster: acme-clu-1
    user: acme-clu-1
current-context: acme-clu-1
`
	if err := os.WriteFile(kubeconfig, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := runConnect(t, srv, "--kubeconfig", kubeconfig)
	if err != nil {
		t.Fatalf("connect error = %v", err)
	}
	got := loadKubeconfigFile(t, kubeconfig)
	if strings.Contains(got, "stale") {
		t.Errorf("stale entry should be replaced:\n%s", got)
	}
	if n := strings.Count(got, "name: acme-clu-1"); n != 3 { // cluster + user + context
		t.Errorf("expected 3 acme-clu-1 entries, got %d:\n%s", n, got)
	}
}

func TestClusterConnectWarnsWhenTunnelUnavailable(t *testing.T) {
	srv := connectServer(t, map[string]any{
		"tunnelAvailable":         false,
		"tunnelUnavailableReason": "agent not connected",
	}, false)
	kubeconfig := filepath.Join(t.TempDir(), "config")
	_, errOut, err := runConnect(t, srv, "--kubeconfig", kubeconfig)
	if err != nil {
		t.Fatalf("connect error = %v", err)
	}
	if !strings.Contains(errOut, "tunnel") || !strings.Contains(errOut, "agent not connected") {
		t.Errorf("expected tunnel warning on stderr, got %q", errOut)
	}
}

func TestClusterConnectOrgOverride(t *testing.T) {
	var gotPath string
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if strings.HasSuffix(r.URL.Path, "/access-info") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"accessInfo": map[string]any{"tunnelAvailable": true}})
			return
		}
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write([]byte(connectKubeconfigYAML))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	kubeconfig := filepath.Join(t.TempDir(), "config")
	_, _, err := runConnect(t, srv, "--kubeconfig", kubeconfig, "--org", "other-org")
	if err != nil {
		t.Fatalf("connect error = %v", err)
	}
	if !strings.HasPrefix(gotPath, "/api/v1/tenants/other-org/") {
		t.Errorf("expected tenant override in request path, got %q", gotPath)
	}
}

func TestMergeKubeconfigEmptyExistingFile(t *testing.T) {
	kubeconfig := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(kubeconfig, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	name, err := mergeKubeconfig([]byte(connectKubeconfigYAML), kubeconfig, true)
	if err != nil {
		t.Fatalf("merge into empty file: %v", err)
	}
	if name != "acme-clu-1" {
		t.Errorf("name = %q, want acme-clu-1", name)
	}
}

func TestMergeKubeconfigNeverBlanksCurrentContext(t *testing.T) {
	kubeconfig := filepath.Join(t.TempDir(), "config")
	existing := `apiVersion: v1
kind: Config
clusters:
- name: other
  cluster:
    server: https://other:6443
users:
- name: other
  user: {}
contexts:
- name: other
  context:
    cluster: other
    user: other
current-context: other
`
	if err := os.WriteFile(kubeconfig, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	rendered := strings.Replace(connectKubeconfigYAML, "current-context: acme-clu-1\n", "", 1)
	name, err := mergeKubeconfig([]byte(rendered), kubeconfig, true)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if name != "acme-clu-1" {
		t.Errorf("name = %q, want acme-clu-1 (fall back to single rendered context)", name)
	}
	got := loadKubeconfigFile(t, kubeconfig)
	if !strings.Contains(got, "current-context: acme-clu-1") {
		t.Errorf("current-context should fall back to rendered context, not be blanked:\n%s", got)
	}
}

func TestMergeKubeconfigInvalidRendered(t *testing.T) {
	kubeconfig := filepath.Join(t.TempDir(), "config")
	if _, err := mergeKubeconfig([]byte("{{{{not yaml"), kubeconfig, true); err == nil {
		t.Fatal("want parse error for invalid rendered kubeconfig")
	}
	if _, statErr := os.Stat(kubeconfig); !os.IsNotExist(statErr) {
		t.Errorf("kubeconfig should not be created on parse failure")
	}
}

func TestClusterConnectFailsWhenKubectlAccessDisabled(t *testing.T) {
	srv := connectServer(t, map[string]any{"kubectlAccessEnabled": false, "tunnelAvailable": true}, false)
	kubeconfig := filepath.Join(t.TempDir(), "config")
	_, _, err := runConnect(t, srv, "--kubeconfig", kubeconfig)
	if err == nil || !strings.Contains(err.Error(), "kubectl access") {
		t.Fatalf("want kubectl access error, got %v", err)
	}
	if _, statErr := os.Stat(kubeconfig); !os.IsNotExist(statErr) {
		t.Errorf("kubeconfig should not be written on failure")
	}
}

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/7K-Inari/inari-cli/internal/auth"
	"github.com/7K-Inari/inari-cli/internal/config"
)

// setupGitSession writes a config context and a valid cached session token
// pointing at srv, returning the config dir.
func setupGitSession(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("INARI_CONFIG_DIR", dir)
	cfg := &config.Config{}
	cfg.SetContext("default", config.Context{Server: srv.URL, Issuer: "https://kc.example/realms/inari", Tenant: "acme"})
	cfg.CurrentContext = "default"
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	cache, err := auth.NewCache()
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Save("default", &auth.Token{AccessToken: "cli-session", TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	return dir
}

type gitFakeServer struct {
	authorizeCalls *atomic.Int32
	listCalls      *atomic.Int32
	connectAfter   int32 // list call number at which the connection appears; 0 = never
	deleteStatus   int
	authorizeReply func(w http.ResponseWriter)
}

func newGitFakeServer(t *testing.T, f *gitFakeServer) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/tenants/acme/git-connections/github/authorize", func(w http.ResponseWriter, r *http.Request) {
		f.authorizeCalls.Add(1)
		if f.authorizeReply != nil {
			f.authorizeReply(w)
			return
		}
		w.Header().Set("Location", "https://github.com/login/oauth/authorize?client_id=x&state=y")
		w.WriteHeader(http.StatusFound)
	})
	mux.HandleFunc("GET /api/v1/tenants/acme/git-connections", func(w http.ResponseWriter, r *http.Request) {
		n := f.listCalls.Add(1)
		conns := []map[string]any{}
		if f.connectAfter > 0 && n >= f.connectAfter {
			conns = append(conns, map[string]any{
				"id": "gc-1", "orgId": "org-1", "provider": "github",
				"providerLogin": "octocat", "scopes": "repo",
				"createdAt": "2026-09-01T10:00:00Z",
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"connections": conns})
	})
	mux.HandleFunc("DELETE /api/v1/tenants/acme/git-connections/github", func(w http.ResponseWriter, r *http.Request) {
		if f.deleteStatus != 0 && f.deleteStatus != http.StatusNoContent {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(f.deleteStatus)
			_ = json.NewEncoder(w).Encode(map[string]any{"title": "Not Found", "detail": "git connection not found"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return httptest.NewServer(mux)
}

func withFastGitPoll(t *testing.T) {
	t.Helper()
	old := gitPollInterval
	gitPollInterval = time.Millisecond
	t.Cleanup(func() { gitPollInterval = old })
}

func runGit(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out := &bytes.Buffer{}
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestGitConnectHappyPath(t *testing.T) {
	withFastGitPoll(t)
	f := &gitFakeServer{authorizeCalls: &atomic.Int32{}, listCalls: &atomic.Int32{}, connectAfter: 2}
	srv := newGitFakeServer(t, f)
	defer srv.Close()
	setupGitSession(t, srv)

	out, err := runGit(t, "git", "connect", "github", "--no-browser")
	if err != nil {
		t.Fatalf("connect error = %v", err)
	}
	if !strings.Contains(out, "https://github.com/login/oauth/authorize") {
		t.Errorf("output should print the consent URL, got %q", out)
	}
	if !strings.Contains(out, "Connected to github as octocat") {
		t.Errorf("output should confirm the connection, got %q", out)
	}
	if f.authorizeCalls.Load() != 1 {
		t.Errorf("authorize calls = %d", f.authorizeCalls.Load())
	}
}

func TestGitConnectAlreadyConnected(t *testing.T) {
	withFastGitPoll(t)
	f := &gitFakeServer{authorizeCalls: &atomic.Int32{}, listCalls: &atomic.Int32{}, connectAfter: 1}
	srv := newGitFakeServer(t, f)
	defer srv.Close()
	setupGitSession(t, srv)

	out, err := runGit(t, "git", "connect", "github", "--no-browser")
	if err != nil {
		t.Fatalf("connect error = %v", err)
	}
	if !strings.Contains(out, "Already connected to github as octocat") {
		t.Errorf("output = %q", out)
	}
	if f.authorizeCalls.Load() != 0 {
		t.Errorf("authorize must not be called when already connected; calls = %d", f.authorizeCalls.Load())
	}
}

func TestGitConnectTimeout(t *testing.T) {
	withFastGitPoll(t)
	f := &gitFakeServer{authorizeCalls: &atomic.Int32{}, listCalls: &atomic.Int32{}}
	srv := newGitFakeServer(t, f)
	defer srv.Close()
	setupGitSession(t, srv)

	_, err := runGit(t, "git", "connect", "github", "--no-browser", "--timeout", "1")
	if err == nil || !strings.Contains(err.Error(), "timed out") || !strings.Contains(err.Error(), "inari git status github") {
		t.Fatalf("err = %v", err)
	}
}

func TestGitConnectCancel(t *testing.T) {
	withFastGitPoll(t)
	f := &gitFakeServer{authorizeCalls: &atomic.Int32{}, listCalls: &atomic.Int32{}}
	srv := newGitFakeServer(t, f)
	defer srv.Close()
	setupGitSession(t, srv)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	out := &bytes.Buffer{}
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"git", "connect", "github", "--no-browser"})
	err := root.ExecuteContext(ctx)
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("err = %v", err)
	}
}

func TestGitConnectProviderNotEnabled(t *testing.T) {
	withFastGitPoll(t)
	f := &gitFakeServer{
		authorizeCalls: &atomic.Int32{}, listCalls: &atomic.Int32{},
		authorizeReply: func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusNotImplemented)
			_ = json.NewEncoder(w).Encode(map[string]any{"title": "Not Implemented", "detail": "git provider not enabled"})
		},
	}
	srv := newGitFakeServer(t, f)
	defer srv.Close()
	setupGitSession(t, srv)

	_, err := runGit(t, "git", "connect", "github", "--no-browser")
	if err == nil || !strings.Contains(err.Error(), "not enabled on this server") {
		t.Fatalf("err = %v", err)
	}
}

func TestGitConnectUnknownProvider(t *testing.T) {
	withFastGitPoll(t)
	f := &gitFakeServer{
		authorizeCalls: &atomic.Int32{}, listCalls: &atomic.Int32{},
		authorizeReply: func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"title": "Not Found", "detail": "unknown git provider"})
		},
	}
	srv := newGitFakeServer(t, f)
	defer srv.Close()
	setupGitSession(t, srv)

	_, err := runGit(t, "git", "connect", "gitlab", "--no-browser")
	if err == nil || !strings.Contains(err.Error(), `unknown git provider "gitlab"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestGitDisconnect(t *testing.T) {
	f := &gitFakeServer{authorizeCalls: &atomic.Int32{}, listCalls: &atomic.Int32{}}
	srv := newGitFakeServer(t, f)
	defer srv.Close()
	setupGitSession(t, srv)

	out, err := runGit(t, "git", "disconnect", "github")
	if err != nil {
		t.Fatalf("disconnect error = %v", err)
	}
	if !strings.Contains(out, "Disconnected github") {
		t.Errorf("output = %q", out)
	}
}

func TestGitDisconnectNotFound(t *testing.T) {
	f := &gitFakeServer{authorizeCalls: &atomic.Int32{}, listCalls: &atomic.Int32{}, deleteStatus: http.StatusNotFound}
	srv := newGitFakeServer(t, f)
	defer srv.Close()
	setupGitSession(t, srv)

	_, err := runGit(t, "git", "disconnect", "github")
	if err == nil || !strings.Contains(err.Error(), `no "github" connection`) {
		t.Fatalf("err = %v", err)
	}
}

func TestGitListTableAndStatus(t *testing.T) {
	f := &gitFakeServer{authorizeCalls: &atomic.Int32{}, listCalls: &atomic.Int32{}, connectAfter: 1}
	srv := newGitFakeServer(t, f)
	defer srv.Close()
	setupGitSession(t, srv)

	out, err := runGit(t, "git", "list")
	if err != nil {
		t.Fatalf("list error = %v", err)
	}
	if !strings.Contains(out, "PROVIDER") || !strings.Contains(out, "octocat") {
		t.Errorf("list output = %q", out)
	}

	out, err = runGit(t, "git", "list", "-o", "json")
	if err != nil {
		t.Fatalf("list -o json error = %v", err)
	}
	if !strings.Contains(out, `"providerLogin": "octocat"`) {
		t.Errorf("json output = %q", out)
	}

	out, err = runGit(t, "git", "status", "github")
	if err != nil {
		t.Fatalf("status error = %v", err)
	}
	if !strings.Contains(out, "connected as octocat") {
		t.Errorf("status output = %q", out)
	}
}

func TestGitStatusNotConnected(t *testing.T) {
	f := &gitFakeServer{authorizeCalls: &atomic.Int32{}, listCalls: &atomic.Int32{}}
	srv := newGitFakeServer(t, f)
	defer srv.Close()
	setupGitSession(t, srv)

	_, err := runGit(t, "git", "status", "github")
	if err == nil || !strings.Contains(err.Error(), "github not connected") {
		t.Fatalf("err = %v", err)
	}
}

func TestGitConnectRequiresTenant(t *testing.T) {
	f := &gitFakeServer{authorizeCalls: &atomic.Int32{}, listCalls: &atomic.Int32{}}
	srv := newGitFakeServer(t, f)
	defer srv.Close()
	dir := t.TempDir()
	t.Setenv("INARI_CONFIG_DIR", dir)
	cfg := &config.Config{}
	cfg.SetContext("default", config.Context{Server: srv.URL})
	cfg.CurrentContext = "default"
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	_, err := runGit(t, "git", "connect", "github", "--no-browser")
	if err == nil || !strings.Contains(err.Error(), "no tenant in context") {
		t.Fatalf("err = %v", err)
	}
}

func TestGitConnectNeverPersistsProviderTokens(t *testing.T) {
	withFastGitPoll(t)
	f := &gitFakeServer{authorizeCalls: &atomic.Int32{}, listCalls: &atomic.Int32{}, connectAfter: 1}
	srv := newGitFakeServer(t, f)
	defer srv.Close()
	dir := setupGitSession(t, srv)

	if _, err := runGit(t, "git", "connect", "github", "--no-browser"); err != nil {
		t.Fatalf("connect error = %v", err)
	}

	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			rel, _ := filepath.Rel(dir, path)
			files = append(files, rel)
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			lower := strings.ToLower(string(data))
			for _, bad := range []string{"provider_token", "github_token", "gho_", "ghp_", "providerlogin"} {
				if strings.Contains(lower, bad) {
					t.Errorf("file %s contains provider material marker %q", rel, bad)
				}
			}
			if strings.HasSuffix(rel, ".json") {
				if info.Mode().Perm() != 0o600 {
					t.Errorf("file %s mode = %o, want 0600", rel, info.Mode().Perm())
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("config dir files = %v; want exactly config.yaml and tokens/default.json", files)
	}
}

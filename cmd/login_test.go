package cmd

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/7K-Inari/inari-cli/internal/auth"
	"github.com/7K-Inari/inari-cli/internal/config"
)

func fakeDeviceFlowServer(t *testing.T, accessToken string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/protocol/openid-connect/auth/device", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "dc",
			"user_code":        "WXYZ-1234",
			"verification_uri": "https://kc.example/device",
			"expires_in":       60,
		})
	})
	mux.HandleFunc("/protocol/openid-connect/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  accessToken,
			"refresh_token": "rt",
			"token_type":    "Bearer",
			"expires_in":    300,
		})
	})
	return httptest.NewServer(mux)
}

func unsignedJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func TestLoginSavesContextAndToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("INARI_CONFIG_DIR", dir)
	token := unsignedJWT(t, map[string]any{"organization": "acme"})
	kc := fakeDeviceFlowServer(t, token)
	defer kc.Close()

	out := &bytes.Buffer{}
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"login", "--issuer", kc.URL, "--server", "https://api.example.dev"})
	if err := root.Execute(); err != nil {
		t.Fatalf("login error = %v", err)
	}
	if !strings.Contains(out.String(), "WXYZ-1234") {
		t.Errorf("login output should show the user code, got %q", out.String())
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	name, ctx, err := cfg.Current()
	if err != nil {
		t.Fatal(err)
	}
	if name != "default" || ctx.Server != "https://api.example.dev" {
		t.Fatalf("context = %q %+v", name, ctx)
	}
	if ctx.Tenant != "acme" {
		t.Fatalf("tenant = %q, want acme (from organization claim)", ctx.Tenant)
	}

	cache, err := auth.NewCache()
	if err != nil {
		t.Fatal(err)
	}
	tok, err := cache.Load("default")
	if err != nil || tok == nil {
		t.Fatalf("cached token = %v, %v", tok, err)
	}
	if tok.RefreshToken != "rt" {
		t.Fatalf("cached refresh token = %q", tok.RefreshToken)
	}
}

func TestLogoutRemovesCachedToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("INARI_CONFIG_DIR", dir)

	cfg := &config.Config{}
	cfg.SetContext("default", config.Context{Server: "https://api.example.dev"})
	cfg.CurrentContext = "default"
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	cache, err := auth.NewCache()
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Save("default", &auth.Token{AccessToken: "a"}); err != nil {
		t.Fatal(err)
	}

	out := &bytes.Buffer{}
	root := NewRootCmd("dev", "none", "now", out, &bytes.Buffer{})
	root.SetArgs([]string{"logout"})
	if err := root.Execute(); err != nil {
		t.Fatalf("logout error = %v", err)
	}
	if tok, err := cache.Load("default"); err != nil || tok != nil {
		t.Fatalf("token after logout = %v, %v; want nil, nil", tok, err)
	}
}

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/7K-Inari/inari-api/gen/go/oas"

	"github.com/7K-Inari/inari-cli/internal/auth"
	"github.com/7K-Inari/inari-cli/internal/config"
)

func TestNewRequiresLogin(t *testing.T) {
	t.Setenv("INARI_CONFIG_DIR", t.TempDir())
	_, err := New(t.Context(), "default", config.Context{Server: "https://x"})
	if err == nil {
		t.Fatal("expected error when no token cached")
	}
}

func TestNewRefreshesExpiredToken(t *testing.T) {
	t.Setenv("INARI_CONFIG_DIR", t.TempDir())

	var sawRefresh bool
	mux := http.NewServeMux()
	mux.HandleFunc("/protocol/openid-connect/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		if r.Form.Get("grant_type") != "refresh_token" {
			t.Errorf("grant_type = %q", r.Form.Get("grant_type"))
		}
		sawRefresh = true
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "fresh-token",
			"token_type":   "Bearer",
			"expires_in":   300,
		})
	})
	var sawAuth string
	mux.HandleFunc("/api/v1/tenants/acme/clusters", func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"clusters": []any{}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cache, err := auth.NewCache()
	if err != nil {
		t.Fatal(err)
	}
	expired := &auth.Token{AccessToken: "old", RefreshToken: "rt", ExpiresAt: time.Now().Add(-time.Hour)}
	if err := cache.Save("default", expired); err != nil {
		t.Fatal(err)
	}

	c, err := New(t.Context(), "default", config.Context{Server: srv.URL, Issuer: srv.URL, Tenant: "acme"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if !sawRefresh {
		t.Fatal("expected a refresh_token grant")
	}
	rsp, err := c.OAS.ListClustersWithResponse(t.Context(), "acme")
	if err != nil {
		t.Fatalf("ListClusters() error = %v", err)
	}
	if rsp.StatusCode() != http.StatusOK {
		t.Fatalf("status = %d", rsp.StatusCode())
	}
	if sawAuth != "Bearer fresh-token" {
		t.Fatalf("Authorization = %q, want fresh token", sawAuth)
	}
}

func TestErrorFormatsProblem(t *testing.T) {
	title := "Policy denied"
	detail := "image registry not allowed"
	msg := "only registries.example.com images are permitted"
	loc := "body.spec.image"
	err := Error("422 Unprocessable Entity", &oas.ErrorModel{
		Title:  &title,
		Detail: &detail,
		Errors: &[]oas.ErrorDetail{{Location: &loc, Message: &msg}},
	})
	want := "Policy denied: image registry not allowed\n  - body.spec.image: only registries.example.com images are permitted"
	if err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
	}
	if err := Error("500 Internal Server Error", nil); err == nil {
		t.Fatal("expected non-nil error for nil model")
	}
}

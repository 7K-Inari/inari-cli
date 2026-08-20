package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeKeycloak implements the device-authorization and token endpoints.
type fakeKeycloak struct {
	t *testing.T
	// approveAfter controls how many token polls return authorization_pending
	// before issuing tokens.
	approveAfter int32
	polls        int32
}

func (fk *fakeKeycloak) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/protocol/openid-connect/auth/device", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			fk.t.Errorf("ParseForm: %v", err)
		}
		if got := r.Form.Get("client_id"); got != DefaultClientID {
			fk.t.Errorf("client_id = %q, want %q", got, DefaultClientID)
		}
		if scope := r.Form.Get("scope"); !strings.Contains(scope, "organization") {
			fk.t.Errorf("scope = %q, want it to contain 'organization'", scope)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DeviceAuth{
			DeviceCode:      "dev-code",
			UserCode:        "ABCD-EFGH",
			VerificationURI: "https://kc.example/device",
			ExpiresIn:       600,
			Interval:        1,
		})
	})
	mux.HandleFunc("/protocol/openid-connect/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			fk.t.Errorf("ParseForm: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Form.Get("grant_type") {
		case "urn:ietf:params:oauth:grant-type:device_code":
			n := atomic.AddInt32(&fk.polls, 1)
			if n <= fk.approveAfter {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(tokenError{Error: "authorization_pending"})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "access-123",
				"refresh_token": "refresh-456",
				"token_type":    "Bearer",
				"expires_in":    300,
			})
		case "refresh_token":
			if r.Form.Get("refresh_token") != "refresh-456" {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(tokenError{Error: "invalid_grant"})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"access_token": "access-789",
				"token_type":   "Bearer",
				"expires_in":   300,
			})
		default:
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(tokenError{Error: "unsupported_grant_type"})
		}
	})
	return mux
}

func TestDeviceFlowHappyPath(t *testing.T) {
	fk := &fakeKeycloak{t: t, approveAfter: 2}
	srv := httptest.NewServer(fk.handler())
	defer srv.Close()

	flow := &DeviceFlow{
		Issuer:       srv.URL,
		ClientID:     DefaultClientID,
		Scopes:       []string{"openid", "organization"},
		PollInterval: time.Millisecond,
	}
	da, err := flow.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if da.UserCode != "ABCD-EFGH" {
		t.Fatalf("UserCode = %q", da.UserCode)
	}
	tok, err := flow.Poll(context.Background(), da)
	if err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if tok.AccessToken != "access-123" || tok.RefreshToken != "refresh-456" {
		t.Fatalf("unexpected token: %+v", tok)
	}
	if !tok.Valid() {
		t.Fatal("token should be valid")
	}
	if atomic.LoadInt32(&fk.polls) != 3 {
		t.Fatalf("polls = %d, want 3 (2 pending + 1 success)", fk.polls)
	}
}

func TestRefreshKeepsOldRefreshTokenWhenNotRotated(t *testing.T) {
	fk := &fakeKeycloak{t: t}
	srv := httptest.NewServer(fk.handler())
	defer srv.Close()

	flow := &DeviceFlow{Issuer: srv.URL, ClientID: DefaultClientID}
	tok, err := flow.Refresh(context.Background(), "refresh-456")
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if tok.AccessToken != "access-789" {
		t.Fatalf("AccessToken = %q", tok.AccessToken)
	}
	if tok.RefreshToken != "refresh-456" {
		t.Fatalf("RefreshToken = %q, want original kept", tok.RefreshToken)
	}
}

func TestCacheRoundTripAndDelete(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("INARI_CONFIG_DIR", dir)
	cache, err := NewCache()
	if err != nil {
		t.Fatal(err)
	}
	if tok, err := cache.Load("dev"); err != nil || tok != nil {
		t.Fatalf("Load(missing) = %v, %v; want nil, nil", tok, err)
	}
	want := &Token{AccessToken: "a", RefreshToken: "r", ExpiresAt: time.Now().Add(time.Hour).Truncate(time.Second)}
	if err := cache.Save("dev", want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := cache.Load("dev")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken || !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if err := cache.Delete("dev"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := cache.Delete("dev"); err != nil {
		t.Fatalf("Delete() again should be idempotent: %v", err)
	}
	if tok, err := cache.Load("dev"); err != nil || tok != nil {
		t.Fatalf("Load(after delete) = %v, %v; want nil, nil", tok, err)
	}
}

func TestTokenValiditySkew(t *testing.T) {
	tok := Token{AccessToken: "a", ExpiresAt: time.Now().Add(10 * time.Second)}
	if tok.Valid() {
		t.Fatal("token inside 30s skew should be invalid")
	}
	tok.ExpiresAt = time.Now().Add(time.Hour)
	if !tok.Valid() {
		t.Fatal("token should be valid")
	}
}

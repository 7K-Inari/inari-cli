package gitconn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Client{Base: srv.URL, Tenant: "acme", Token: "tok"}, srv
}

func TestListReturnsConnections(t *testing.T) {
	var gotAuth, gotPath string
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"connections": []map[string]any{{
				"id":            "gc-1",
				"orgId":         "org-1",
				"provider":      "github",
				"providerLogin": "octocat",
				"scopes":        "repo",
				"createdAt":     "2026-09-01T10:00:00Z",
			}},
		})
	}))
	conns, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(conns) != 1 || conns[0].ProviderLogin != "octocat" {
		t.Fatalf("connections = %+v", conns)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotPath != "/api/v1/tenants/acme/git-connections" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestListProblemError(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{"title": "Forbidden", "detail": "not a member of this organization"})
	}))
	_, err := c.List(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not a member") {
		t.Fatalf("err = %v", err)
	}
}

func TestAuthorizeReturnsConsentURLWithoutFollowingRedirect(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/git-connections/github/authorize") {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Location", "https://github.com/login/oauth/authorize?client_id=x&state=y")
		w.WriteHeader(http.StatusFound)
	}))
	url, err := c.Authorize(context.Background(), "github", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(url, "https://github.com/login/oauth/authorize") {
		t.Fatalf("url = %q", url)
	}
}

func TestAuthorizeSendsAPIBase(t *testing.T) {
	var body map[string]string
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Location", "https://ghe.example/login/oauth/authorize?state=z")
		w.WriteHeader(http.StatusFound)
	}))
	if _, err := c.Authorize(context.Background(), "github", "https://ghe.example/api/v3"); err != nil {
		t.Fatal(err)
	}
	if body["apiBase"] != "https://ghe.example/api/v3" {
		t.Fatalf("body = %v", body)
	}
}

func TestAuthorizeErrorMapping(t *testing.T) {
	cases := []struct {
		status  int
		detail  string
		wantSub string
	}{
		{http.StatusNotFound, "unknown git provider", "unknown git provider"},
		{http.StatusNotImplemented, "git provider not enabled", "not enabled on this server"},
		{http.StatusUnprocessableEntity, "apiBase not allowlisted", "apiBase not allowlisted"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(map[string]any{"title": "err", "detail": tc.detail})
			}))
			_, err := c.Authorize(context.Background(), "github", "")
			if err == nil || !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("status %d: err = %v, want substring %q", tc.status, err, tc.wantSub)
			}
		})
	}
}

func TestDisconnect(t *testing.T) {
	var gotMethod string
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	if err := c.Disconnect(context.Background(), "github"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("method = %s", gotMethod)
	}
}

func TestDisconnectNotFound(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"title": "Not Found", "detail": "git connection not found"})
	}))
	err := c.Disconnect(context.Background(), "github")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestWaitForConnectionPollsUntilConnected(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		conns := []map[string]any{}
		if n >= 2 {
			conns = append(conns, map[string]any{"id": "gc-1", "provider": "github", "providerLogin": "octocat", "createdAt": "2026-09-01T10:00:00Z"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"connections": conns})
	}))
	c.PollInterval = time.Millisecond
	conn, err := c.WaitForConnection(context.Background(), "github")
	if err != nil {
		t.Fatal(err)
	}
	if conn.ProviderLogin != "octocat" {
		t.Fatalf("conn = %+v", conn)
	}
}

func TestWaitForConnectionCancel(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"connections": []any{}})
	}))
	c.PollInterval = 50 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err := c.WaitForConnection(ctx, "github")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

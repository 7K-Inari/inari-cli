// Package gitconn is a minimal REST client for the server's per-user git
// connections API (W4). These endpoints are not yet part of the generated
// inari-api OpenAPI client, so they are called directly with the cached
// Keycloak session token. Provider OAuth tokens are never returned by the
// server and never stored by the CLI.
package gitconn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrNotFound marks a missing git connection (DELETE of an unknown provider).
var ErrNotFound = errors.New("git connection not found")

// Connection is the metadata-only view of a user's git provider connection.
type Connection struct {
	ID            string     `json:"id"`
	OrgID         string     `json:"orgId"`
	Provider      string     `json:"provider"`
	ProviderLogin string     `json:"providerLogin"`
	Scopes        string     `json:"scopes"`
	APIBase       string     `json:"apiBase,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	LastUsedAt    *time.Time `json:"lastUsedAt,omitempty"`
}

// Client calls the user git connections endpoints for one tenant.
type Client struct {
	Base   string
	Tenant string
	Token  string

	HTTPClient *http.Client
	// PollInterval overrides the wait-loop interval (tests).
	PollInterval time.Duration
}

func (c *Client) http() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func (c *Client) url(parts ...string) string {
	return strings.TrimSuffix(c.Base, "/") + "/api/v1/tenants/" + c.Tenant + "/git-connections" + strings.Join(parts, "")
}

type problem struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

// decodeError surfaces a problem+json body (or the bare status) as an error.
func decodeError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	var p problem
	if err := json.Unmarshal(body, &p); err == nil && (p.Title != "" || p.Detail != "") {
		msg := p.Title
		if p.Detail != "" {
			msg = p.Detail
		}
		return fmt.Errorf("%s: %s", resp.Status, msg)
	}
	if s := strings.TrimSpace(string(body)); s != "" {
		return fmt.Errorf("%s: %s", resp.Status, s)
	}
	return fmt.Errorf("request failed: %s", resp.Status)
}

func (c *Client) newRequest(ctx context.Context, method, url string, body any) (*http.Request, error) {
	var rdr io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// List returns the caller's own git connections (metadata only).
func (c *Client) List(ctx context.Context) ([]Connection, error) {
	req, err := c.newRequest(ctx, http.MethodGet, c.url(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing git connections: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, decodeError(resp)
	}
	var out struct {
		Connections []Connection `json:"connections"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding git connections: %w", err)
	}
	return out.Connections, nil
}

// Authorize starts the OAuth flow and returns the provider consent URL. The
// server's 302 is not followed; the CLI hands the URL to the user's browser.
func (c *Client) Authorize(ctx context.Context, provider, apiBase string) (string, error) {
	body := map[string]string{}
	if apiBase != "" {
		body["apiBase"] = apiBase
	}
	req, err := c.newRequest(ctx, http.MethodPost, c.url("/", provider, "/authorize"), body)
	if err != nil {
		return "", err
	}
	hc := c.http()
	noFollow := &http.Client{
		Transport: hc.Transport,
		Jar:       hc.Jar,
		Timeout:   hc.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := noFollow.Do(req)
	if err != nil {
		return "", fmt.Errorf("starting git OAuth flow: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect:
		loc := resp.Header.Get("Location")
		if loc == "" {
			return "", fmt.Errorf("server redirected (%s) without a Location header", resp.Status)
		}
		return loc, nil
	case http.StatusNotFound:
		return "", fmt.Errorf("unknown git provider %q (the server currently supports: github)", provider)
	case http.StatusNotImplemented:
		return "", fmt.Errorf("git connections are not enabled on this server; contact your platform admin")
	case http.StatusUnprocessableEntity:
		return "", fmt.Errorf("invalid authorize input: %w", decodeError(resp))
	default:
		return "", decodeError(resp)
	}
}

// Disconnect revokes the provider grant and deletes the connection server-side.
func (c *Client) Disconnect(ctx context.Context, provider string) error {
	req, err := c.newRequest(ctx, http.MethodDelete, c.url("/", provider), nil)
	if err != nil {
		return err
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return fmt.Errorf("disconnecting %s: %w", provider, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w: no %q connection", ErrNotFound, provider)
	}
	return decodeError(resp)
}

// WaitForConnection polls List until a connection for provider exists, the
// context is cancelled, or its deadline passes.
func (c *Client) WaitForConnection(ctx context.Context, provider string) (*Connection, error) {
	interval := c.PollInterval
	if interval <= 0 {
		interval = 3 * time.Second
	}
	for {
		conns, err := c.List(ctx)
		if err != nil {
			return nil, err
		}
		for _, cn := range conns {
			if cn.Provider == provider {
				conn := cn
				return &conn, nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// Find returns the connection for provider, or nil.
func Find(conns []Connection, provider string) *Connection {
	for _, cn := range conns {
		if cn.Provider == provider {
			conn := cn
			return &conn
		}
	}
	return nil
}

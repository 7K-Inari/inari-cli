package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const DefaultClientID = "inari-cli"

type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func (t Token) Valid() bool {
	return t.AccessToken != "" && time.Now().Before(t.ExpiresAt.Add(-30*time.Second))
}

type DeviceAuth struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete,omitempty"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval,omitempty"`
}

type DeviceFlow struct {
	Issuer     string
	ClientID   string
	Scopes     []string
	HTTPClient *http.Client
	// PollInterval overrides the server-provided interval (tests).
	PollInterval time.Duration
}

func (f *DeviceFlow) endpoints() (deviceURL, tokenURL string) {
	base := strings.TrimSuffix(f.Issuer, "/") + "/protocol/openid-connect"
	return base + "/auth/device", base + "/token"
}

func (f *DeviceFlow) http() *http.Client {
	if f.HTTPClient != nil {
		return f.HTTPClient
	}
	return http.DefaultClient
}

func (f *DeviceFlow) Start(ctx context.Context) (*DeviceAuth, error) {
	deviceURL, _ := f.endpoints()
	scope := strings.Join(f.Scopes, " ")
	if scope == "" {
		scope = "openid"
	}
	form := url.Values{
		"client_id": {f.ClientID},
		"scope":     {scope},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := f.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("device authorization request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device authorization failed (%s): %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var da DeviceAuth
	if err := json.Unmarshal(body, &da); err != nil {
		return nil, fmt.Errorf("decoding device authorization response: %w", err)
	}
	if da.DeviceCode == "" || da.UserCode == "" {
		return nil, fmt.Errorf("device authorization response missing codes: %s", strings.TrimSpace(string(body)))
	}
	return &da, nil
}

type tokenError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

// Poll exchanges the device code for tokens, honoring the OAuth2 device-flow
// slow_down / authorization_pending signaling, until the user completes login,
// the code expires, or ctx is cancelled.
func (f *DeviceFlow) Poll(ctx context.Context, da *DeviceAuth) (*Token, error) {
	_, tokenURL := f.endpoints()
	interval := time.Duration(da.Interval) * time.Second
	if f.PollInterval > 0 {
		interval = f.PollInterval
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(da.ExpiresIn) * time.Second)

	for {
		if !deadline.IsZero() && time.Now().After(deadline) {
			return nil, fmt.Errorf("device code expired; run 'inari login' again")
		}
		form := url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code": {da.DeviceCode},
			"client_id":   {f.ClientID},
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := f.http().Do(req)
		if err != nil {
			return nil, fmt.Errorf("token poll: %w", err)
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == http.StatusOK {
			var raw struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
				TokenType    string `json:"token_type"`
				ExpiresIn    int    `json:"expires_in"`
			}
			if err := json.Unmarshal(body, &raw); err != nil {
				return nil, fmt.Errorf("decoding token response: %w", err)
			}
			if raw.AccessToken == "" {
				return nil, fmt.Errorf("token response missing access_token")
			}
			return &Token{
				AccessToken:  raw.AccessToken,
				RefreshToken: raw.RefreshToken,
				TokenType:    raw.TokenType,
				ExpiresAt:    time.Now().Add(time.Duration(raw.ExpiresIn) * time.Second),
			}, nil
		}

		var te tokenError
		if err := json.Unmarshal(body, &te); err != nil {
			return nil, fmt.Errorf("token poll failed (%s): %s", resp.Status, strings.TrimSpace(string(body)))
		}
		switch te.Error {
		case "authorization_pending":
		case "slow_down":
			interval += 5 * time.Second
		case "expired_token":
			return nil, fmt.Errorf("device code expired; run 'inari login' again")
		case "access_denied":
			return nil, fmt.Errorf("login denied by user")
		default:
			return nil, fmt.Errorf("token poll failed: %s (%s)", te.Error, te.ErrorDescription)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// Refresh exchanges a refresh token for a fresh token pair.
func (f *DeviceFlow) Refresh(ctx context.Context, refreshToken string) (*Token, error) {
	_, tokenURL := f.endpoints()
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {f.ClientID},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := f.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refresh failed (%s): %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var raw struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decoding refresh response: %w", err)
	}
	rt := raw.RefreshToken
	if rt == "" {
		rt = refreshToken
	}
	return &Token{
		AccessToken:  raw.AccessToken,
		RefreshToken: rt,
		TokenType:    raw.TokenType,
		ExpiresAt:    time.Now().Add(time.Duration(raw.ExpiresIn) * time.Second),
	}, nil
}

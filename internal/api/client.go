package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/7K-Inari/inari-api/gen/go/oas"

	"github.com/7K-Inari/inari-cli/internal/auth"
	"github.com/7K-Inari/inari-cli/internal/config"
)

// Client wraps the generated inari-api client with bearer auth from the
// token cache, refreshing expired tokens transparently.
type Client struct {
	OAS    *oas.ClientWithResponses
	Tenant string
}

// New builds an authenticated client for the given config context.
func New(ctx context.Context, contextName string, cc config.Context) (*Client, error) {
	cache, err := auth.NewCache()
	if err != nil {
		return nil, err
	}
	tok, err := cache.Load(contextName)
	if err != nil {
		return nil, err
	}
	if tok == nil {
		return nil, fmt.Errorf("not logged in for context %q; run 'inari login'", contextName)
	}
	if !tok.Valid() {
		if tok.RefreshToken == "" {
			return nil, fmt.Errorf("session expired and no refresh token is cached; run 'inari login'")
		}
		issuer := cc.Issuer
		if issuer == "" {
			return nil, fmt.Errorf("context %q has no issuer configured; run 'inari login' again", contextName)
		}
		flow := &auth.DeviceFlow{Issuer: issuer, ClientID: auth.DefaultClientID}
		tok, err = flow.Refresh(ctx, tok.RefreshToken)
		if err != nil {
			return nil, fmt.Errorf("refreshing session: %w (run 'inari login')", err)
		}
		if err := cache.Save(contextName, tok); err != nil {
			return nil, err
		}
	}

	raw, err := oas.NewClientWithResponses(
		cc.Server,
		oas.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
			return nil
		}),
	)
	if err != nil {
		return nil, err
	}
	return &Client{OAS: raw, Tenant: cc.Tenant}, nil
}

// Error surfaces a problem+json response body as a Go error.
func Error(status string, model *oas.ErrorModel) error {
	if model == nil {
		return fmt.Errorf("request failed: %s", status)
	}
	msg := status
	if model.Title != nil {
		msg = *model.Title
	}
	if model.Detail != nil {
		msg += ": " + *model.Detail
	}
	if model.Errors != nil {
		for _, e := range *model.Errors {
			if e.Message != nil {
				loc := ""
				if e.Location != nil {
					loc = *e.Location + ": "
				}
				msg += "\n  - " + loc + *e.Message
			}
		}
	}
	return fmt.Errorf("%s", msg)
}

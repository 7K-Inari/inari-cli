package auth

import (
	"context"
	"fmt"

	"github.com/7K-Inari/inari-cli/internal/config"
)

// SessionToken loads the cached token for a context, refreshing it via the
// issuer's refresh grant when expired. The refreshed pair is written back to
// the cache (0600) so subsequent callers reuse it.
func SessionToken(ctx context.Context, contextName string, cc config.Context) (*Token, error) {
	cache, err := NewCache()
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
	if tok.Valid() {
		return tok, nil
	}
	if tok.RefreshToken == "" {
		return nil, fmt.Errorf("session expired and no refresh token is cached; run 'inari login'")
	}
	if cc.Issuer == "" {
		return nil, fmt.Errorf("context %q has no issuer configured; run 'inari login' again", contextName)
	}
	flow := &DeviceFlow{Issuer: cc.Issuer, ClientID: DefaultClientID}
	tok, err = flow.Refresh(ctx, tok.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("refreshing session: %w (run 'inari login')", err)
	}
	if err := cache.Save(contextName, tok); err != nil {
		return nil, err
	}
	return tok, nil
}

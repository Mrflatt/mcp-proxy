package auth

import (
	"context"
	"net/http"
	"sync"

	"github.com/Mrflatt/mcp-proxy/config"
	"golang.org/x/oauth2"
	"google.golang.org/api/idtoken"
	"google.golang.org/api/impersonate"
)

type googleIDTokenHandler struct {
	cfg *config.AuthConfig
	mu  sync.Mutex
	ts  oauth2.TokenSource
}

func newGoogleIDToken(cfg *config.AuthConfig) *googleIDTokenHandler {
	return &googleIDTokenHandler{cfg: cfg}
}

// TokenSource returns the cached token source, creating it on first call or
// after an Authorize-triggered reset. The source itself handles token refresh.
func (h *googleIDTokenHandler) TokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ts != nil {
		return h.ts, nil
	}
	var (
		ts  oauth2.TokenSource
		err error
	)
	if h.cfg.ServiceAccount != "" {
		ts, err = impersonate.IDTokenSource(ctx, impersonate.IDTokenConfig{
			TargetPrincipal: h.cfg.ServiceAccount,
			Audience:        h.cfg.Audience,
			IncludeEmail:    h.cfg.IncludeEmail,
		})
	} else {
		ts, err = idtoken.NewTokenSource(ctx, h.cfg.Audience)
	}
	if err != nil {
		return nil, err // not cached — caller can retry after fixing credentials
	}
	h.ts = ts
	return ts, nil
}

// Authorize is called by the transport on 401/403. It invalidates the cached
// token source so the next request re-reads ADC credentials from disk.
func (h *googleIDTokenHandler) Authorize(_ context.Context, _ *http.Request, resp *http.Response) error {
	resp.Body.Close()
	h.mu.Lock()
	h.ts = nil
	h.mu.Unlock()
	return nil
}

package auth

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/Mrflatt/mcp-proxy/config"
	"golang.org/x/oauth2"
)

type bearerHandler struct {
	token string
}

func newBearer(cfg *config.AuthConfig) (*bearerHandler, error) {
	tok := cfg.Token
	if tok == "" && cfg.TokenEnv != "" {
		tok = os.Getenv(cfg.TokenEnv)
	}
	if tok == "" {
		return nil, fmt.Errorf("auth/bearer: no token configured")
	}
	return &bearerHandler{token: tok}, nil
}

func (h *bearerHandler) TokenSource(_ context.Context) (oauth2.TokenSource, error) {
	return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: h.token}), nil
}

func (h *bearerHandler) Authorize(_ context.Context, _ *http.Request, _ *http.Response) error {
	return nil
}

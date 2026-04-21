package auth

import (
	"fmt"

	"github.com/Mrflatt/mcp-proxy/config"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
)

func New(cfg *config.AuthConfig) (sdkauth.OAuthHandler, error) {
	if cfg == nil {
		return nil, nil
	}
	switch cfg.Type {
	case "bearer":
		return newBearer(cfg)
	case "google-idtoken":
		return newGoogleIDToken(cfg), nil
	case "google-access-token":
		return newGoogleAccessToken(cfg), nil
	default:
		return nil, fmt.Errorf("auth: unknown type %q", cfg.Type)
	}
}

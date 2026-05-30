package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/Mrflatt/mcp-proxy/config"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connector holds lazy connection state for a single upstream server.
// Errors are never cached — every failed connect is retried on the next call.
type connector struct {
	name    string
	cfg     config.ServerConfig
	handler sdkauth.OAuthHandler

	mu   sync.Mutex
	sess *mcp.ClientSession
}

// get returns the live session, dialing if not yet connected or after a reset.
func (c *connector) get(ctx context.Context) (*mcp.ClientSession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sess != nil {
		return c.sess, nil
	}
	sess, err := dial(ctx, c.cfg, c.handler)
	if err != nil {
		// If the dial failed due to an auth error, reset the token source so
		// the next attempt re-reads ADC credentials from disk (handles RAPT
		// expiry after gcloud auth application-default login).
		if shouldReset(err) {
			if r, ok := c.handler.(Resetter); ok {
				r.Reset()
			}
		}
		return nil, fmt.Errorf("server %q: %w", c.name, err)
	}
	c.sess = sess
	return sess, nil
}

// Resetter is implemented by auth handlers that support explicit cache
// invalidation (e.g. clearing a stale token source after RAPT expiry).
type Resetter interface {
	Reset()
}

// reset clears the cached session so the next get() re-dials.
// It also resets the auth handler's cached token source so fresh ADC
// credentials are picked up — this handles RAPT expiry and
// `gcloud auth application-default login` refreshes.
func (c *connector) reset() {
	c.mu.Lock()
	c.sess = nil
	c.mu.Unlock()
	if r, ok := c.handler.(Resetter); ok {
		r.Reset()
	}
}

// shouldReset reports whether an error indicates the upstream session is
// broken and the connector should be reset. This covers connection drops,
// stale sessions, transport-level rejections, and auth failures (401/403)
// such as RAPT token expiry.
func shouldReset(err error) bool {
	if errors.Is(err, mcp.ErrConnectionClosed) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "Bad Request") ||
		strings.Contains(msg, "rejected by transport") ||
		strings.Contains(msg, "client is closing") ||
		strings.Contains(msg, "401") ||
		strings.Contains(msg, "403") ||
		strings.Contains(msg, "Unauthorized") ||
		strings.Contains(msg, "Forbidden") ||
		strings.Contains(msg, "invalid_rapt") ||
		strings.Contains(msg, "invalid_grant")
}

// dial creates the transport from cfg and returns a connected MCP session.
func dial(ctx context.Context, cfg config.ServerConfig, handler sdkauth.OAuthHandler) (*mcp.ClientSession, error) {
	var transport mcp.Transport
	if cfg.URL != "" {
		t := &mcp.StreamableClientTransport{Endpoint: cfg.URL}
		if handler != nil {
			t.OAuthHandler = handler
		}
		if len(cfg.Headers) > 0 {
			t.HTTPClient = &http.Client{
				Transport: &headerTransport{base: http.DefaultTransport, headers: cfg.Headers},
			}
		}
		transport = t
	} else if cfg.Command != "" {
		cmd := exec.Command(cfg.Command, cfg.Args...)
		if len(cfg.Env) > 0 {
			cmd.Env = mergeEnv(cfg.Env)
		}
		transport = &mcp.CommandTransport{Command: cmd}
	} else {
		return nil, fmt.Errorf("must set url or command")
	}

	var opts *mcp.ClientOptions
	if cfg.Keepalive != "" {
		d, err := time.ParseDuration(cfg.Keepalive)
		if err != nil {
			return nil, fmt.Errorf("invalid keepalive %q: %w", cfg.Keepalive, err)
		}
		opts = &mcp.ClientOptions{KeepAlive: d}
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "mcp-proxy-client", Version: "0.1.0"}, opts)
	return client.Connect(ctx, transport, nil)
}

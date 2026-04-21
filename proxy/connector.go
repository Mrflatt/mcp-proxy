package proxy

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
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
		return nil, fmt.Errorf("server %q: %w", c.name, err)
	}
	c.sess = sess
	return sess, nil
}

// reset clears the cached session so the next get() re-dials.
func (c *connector) reset() {
	c.mu.Lock()
	c.sess = nil
	c.mu.Unlock()
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

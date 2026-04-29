package proxy

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/Mrflatt/mcp-proxy/config"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// toolRoute maps a discover-side tool name back to the server and original tool name.
type toolRoute struct {
	serverName string
	toolName   string
}

type Proxy struct {
	connectors  map[string]*connector
	fullyDirect map[string]bool            // servers where ALL tools are direct
	directTools map[string]map[string]bool // server → set of tool names exposed directly
	noPrefix    map[string]config.NoPrefix  // per-server noPrefix config
	exclude     map[string]map[string]bool // server → set of excluded tool names

	// routes maps the tool name as exposed by mcp_discover/mcp_search to its
	// upstream server and original tool name. Updated on every collectTools call.
	routes   map[string]toolRoute
	routesMu sync.RWMutex

	server *mcp.Server
}

func New() *Proxy {
	return &Proxy{
		connectors:  make(map[string]*connector),
		fullyDirect: make(map[string]bool),
		directTools: make(map[string]map[string]bool),
		noPrefix:    make(map[string]config.NoPrefix),
		exclude:     make(map[string]map[string]bool),
		routes:      make(map[string]toolRoute),
		server:      mcp.NewServer(&mcp.Implementation{Name: "mcp-proxy", Version: "0.1.0"}, nil),
	}
}

func (p *Proxy) Server() *mcp.Server { return p.server }

func (p *Proxy) Connect(_ context.Context, cfg *config.Config, handlers map[string]sdkauth.OAuthHandler) {
	for name, sc := range cfg.Servers {
		if len(sc.ExcludeTools) > 0 {
			set := make(map[string]bool, len(sc.ExcludeTools))
			for _, t := range sc.ExcludeTools {
				set[t] = true
			}
			p.exclude[name] = set
		}
		if sc.NoPrefix.Enabled() {
			p.noPrefix[name] = sc.NoPrefix
		}
		if sc.DirectTools.All {
			p.fullyDirect[name] = true
		} else if len(sc.DirectTools.Names) > 0 {
			set := make(map[string]bool, len(sc.DirectTools.Names))
			for _, t := range sc.DirectTools.Names {
				set[t] = true
			}
			p.directTools[name] = set
		}
		p.connectors[name] = &connector{
			name:    name,
			cfg:     sc,
			handler: handlers[name],
		}
		slog.Info("registered upstream server", "server", name)
	}
}

// ConnectEager dials all servers configured with eager:true in parallel.
// Errors are logged but don't prevent startup — the connector will retry lazily.
func (p *Proxy) ConnectEager(ctx context.Context) {
	var wg sync.WaitGroup
	for name, c := range p.connectors {
		if !c.cfg.Eager {
			continue
		}
		wg.Go(func() {
			if _, err := c.get(ctx); err != nil {
				slog.Warn("eager connect failed", "server", name, "error", err)
			} else {
				slog.Info("eager connect ok", "server", name)
			}
		})
	}
	wg.Wait()
}
func (p *Proxy) proxyConnectors() map[string]*connector {
	out := make(map[string]*connector, len(p.connectors))
	for name, c := range p.connectors {
		if !p.fullyDirect[name] {
			out[name] = c
		}
	}
	return out
}

// isDirectTool reports whether a tool is registered directly and should be
// excluded from discover/call.
func (p *Proxy) isDirectTool(serverName, toolName string) bool {
	return p.fullyDirect[serverName] || p.directTools[serverName][toolName]
}

// discoverName returns the tool name as exposed to the LLM for a given server + tool.
func (p *Proxy) discoverName(serverName, toolName string) string {
	if np, ok := p.noPrefix[serverName]; ok && np.Includes(toolName) {
		return toolName
	}
	return serverName + "-" + toolName
}

// isExcluded reports whether tool is excluded for the given server.
func (p *Proxy) isExcluded(serverName, toolName string) bool {
	return p.exclude[serverName][toolName]
}

// expandEnv replaces ${VAR} references with values from the process environment.
func expandEnv(s string) string {
	return os.Expand(s, func(key string) string {
		return strings.TrimSpace(os.Getenv(key))
	})
}

// mergeEnv returns os.Environ() extended with the provided key=value pairs,
// with ${VAR} interpolation applied to values.
func mergeEnv(extra map[string]string) []string {
	env := os.Environ()
	for k, v := range extra {
		env = append(env, k+"="+expandEnv(v))
	}
	return env
}

type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for k, v := range t.headers {
		req.Header.Set(k, expandEnv(v))
	}
	return t.base.RoundTrip(req)
}

package proxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpInput struct {
	Server    string         `json:"server,omitempty"    jsonschema:"Server name to list its tools."`
	Search    string         `json:"search,omitempty"    jsonschema:"Search term to filter tools by server or tool name."`
	Tool      string         `json:"tool,omitempty"      jsonschema:"Tool name to call (as returned by mcp). Provide arguments to pass to the tool."`
	Arguments map[string]any `json:"arguments,omitempty" jsonschema:"Arguments for the tool call."`
}

type serverInfo struct {
	ToolCount int `json:"toolCount"`
}

type serverListing struct {
	Servers map[string]serverInfo `json:"servers"`
}

type toolListing struct {
	Tools  []toolSummary     `json:"tools"`
	Errors map[string]string `json:"errors,omitempty"`
}

type toolSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema,omitempty"`
}

func (p *Proxy) RegisterDiscover() {
	mcp.AddTool(p.server, &mcp.Tool{
		Name: "mcp",
		Description: `MCP server gateway.

mcp({}) lists all servers with tool counts.
mcp({server:"name"}) lists tools for a specific server.
mcp({search:"query"}) searches tools by name across all servers.
mcp({tool:"server_tool",arguments:{...}}) calls a tool.`,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input mcpInput) (*mcp.CallToolResult, any, error) {
		if input.Tool != "" {
			return p.callTool(ctx, input.Tool, input.Arguments)
		}
		if input.Server == "" && input.Search == "" {
			return p.listServers(ctx)
		}
		return p.listTools(ctx, input.Server, input.Search)
	})
}

// listServers returns all proxy-mode servers with their tool counts.
func (p *Proxy) listServers(ctx context.Context) (*mcp.CallToolResult, any, error) {
	type countResult struct {
		name  string
		count int
		err   error
	}

	conns := p.proxyConnectors()
	results := make(chan countResult, len(conns))

	var wg sync.WaitGroup
	for serverName, conn := range conns {
		wg.Go(func() {
			sess, err := conn.get(ctx)
			if err != nil {
				results <- countResult{name: serverName, err: err}
				return
			}
			var count int
			for tool, err := range sess.Tools(ctx, nil) {
				if err != nil {
					if errors.Is(err, mcp.ErrConnectionClosed) {
						conn.reset()
					}
					results <- countResult{name: serverName, err: err}
					return
				}
				if !p.isExcluded(serverName, tool.Name) && !p.isDirectTool(serverName, tool.Name) {
					count++
				}
			}
			results <- countResult{name: serverName, count: count}
		})
	}

	go func() { wg.Wait(); close(results) }()

	listing := serverListing{Servers: make(map[string]serverInfo)}
	for r := range results {
		if r.err != nil {
			slog.Warn("server unavailable", "server", r.name, "error", r.err)
			continue
		}
		listing.Servers[r.name] = serverInfo{ToolCount: r.count}
	}
	return nil, listing, nil
}

// listTools returns tools, optionally filtered by server and/or search query.
func (p *Proxy) listTools(ctx context.Context, serverName, query string) (*mcp.CallToolResult, any, error) {
	words := strings.Fields(strings.ToLower(query))

	type serverResult struct {
		name   string
		tools  []toolSummary
		routes map[string]toolRoute
		err    error
	}

	conns := p.proxyConnectors()
	if serverName != "" {
		c, ok := conns[serverName]
		if !ok {
			return nil, nil, nil
		}
		conns = map[string]*connector{serverName: c}
	}

	results := make(chan serverResult, len(conns))

	var wg sync.WaitGroup
	for serverName, conn := range conns {
		wg.Go(func() {
			sess, err := conn.get(ctx)
			if err != nil {
				results <- serverResult{name: serverName, err: err}
				return
			}
			var matched []toolSummary
			routes := make(map[string]toolRoute)
			for tool, err := range sess.Tools(ctx, nil) {
				if err != nil {
					if errors.Is(err, mcp.ErrConnectionClosed) {
						conn.reset()
					}
					results <- serverResult{name: serverName, err: err}
					return
				}
				if p.isExcluded(serverName, tool.Name) || p.isDirectTool(serverName, tool.Name) {
					continue
				}
				discName := p.discoverName(serverName, tool.Name)
				routes[discName] = toolRoute{serverName: serverName, toolName: tool.Name}
				if len(words) > 0 && !matchesQuery(words, strings.ToLower(serverName), strings.ToLower(tool.Name)) {
					continue
				}
				matched = append(matched, toolSummary{
					Name:        discName,
					Description: tool.Description,
					InputSchema: tool.InputSchema,
				})
			}
			results <- serverResult{name: serverName, tools: matched, routes: routes}
		})
	}

	go func() { wg.Wait(); close(results) }()

	var tools []toolSummary
	errs := make(map[string]string)
	newRoutes := make(map[string]toolRoute, len(p.routes))

	for r := range results {
		if r.err != nil {
			slog.Warn("server unavailable", "server", r.name, "error", r.err)
			errs[r.name] = r.err.Error()
			continue
		}
		tools = append(tools, r.tools...)
		for k, v := range r.routes {
			newRoutes[k] = v
		}
	}

	p.routesMu.Lock()
	p.routes = newRoutes
	p.routesMu.Unlock()

	if tools == nil {
		tools = []toolSummary{}
	}
	var errsOut map[string]string
	if len(errs) > 0 {
		errsOut = errs
	}
	return nil, toolListing{Tools: tools, Errors: errsOut}, nil
}

// norm replaces hyphens with underscores for fuzzy matching.
func norm(s string) string { return strings.ReplaceAll(s, "-", "_") }

// matchesQuery matches a tool against a set of words using two-phase logic:
//  1. Words that match the server name are satisfied globally for that server.
//  2. Any remaining unmatched words are OR'd against the tool name — the tool
//     is included if at least one remaining word appears in the tool name.
func matchesQuery(words []string, serverName, toolName string) bool {
	sn := norm(serverName)
	tn := norm(toolName)
	var remaining []string
	for _, w := range words {
		if !strings.Contains(sn, norm(w)) {
			remaining = append(remaining, w)
		}
	}
	if len(remaining) == 0 {
		return true
	}
	for _, w := range remaining {
		if strings.Contains(tn, norm(w)) {
			return true
		}
	}
	return false
}

// callTool dispatches a tool call to the correct upstream server.
func (p *Proxy) callTool(ctx context.Context, discoverName string, arguments map[string]any) (*mcp.CallToolResult, any, error) {
	conn, toolName, err := p.resolveCall(discoverName)
	if err != nil {
		return nil, nil, err
	}
	sess, err := conn.get(ctx)
	if err != nil {
		return nil, nil, err
	}
	result, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name:      toolName,
		Arguments: arguments,
	})
	if errors.Is(err, mcp.ErrConnectionClosed) {
		conn.reset()
	}
	if err != nil {
		return nil, nil, err
	}
	return result, nil, nil
}

// resolveCall returns the connector and original tool name for a discover-side
// tool name. It first checks the routes map, then falls back to splitting on "_".
func (p *Proxy) resolveCall(discoverName string) (*connector, string, error) {
	p.routesMu.RLock()
	route, ok := p.routes[discoverName]
	p.routesMu.RUnlock()

	if ok {
		conn, exists := p.proxyConnectors()[route.serverName]
		if exists {
			return conn, route.toolName, nil
		}
	}

	serverName, toolName, found := strings.Cut(discoverName, "_")
	if !found {
		return nil, "", fmt.Errorf("tool %q not found — call mcp({}) first", discoverName)
	}
	conn, exists := p.proxyConnectors()[serverName]
	if !exists {
		return nil, "", fmt.Errorf("unknown server %q (from tool %q)", serverName, discoverName)
	}
	return conn, toolName, nil
}

package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/Mrflatt/mcp-proxy/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterDirect registers tools from the named servers directly on the local
// MCP server. If no names are given, all servers are registered using their
// DirectTools config. Called with explicit names by the --direct flag (all tools).
func (p *Proxy) RegisterDirect(ctx context.Context, serverNames ...string) error {
	// Build a synthetic DirectTools{All: true} for the --direct flag path.
	allDirect := config.DirectTools{}
	_ = allDirect.UnmarshalJSON([]byte("true"))

	if len(serverNames) == 0 {
		for name := range p.connectors {
			serverNames = append(serverNames, name)
		}
	}
	for _, serverName := range serverNames {
		conn, ok := p.connectors[serverName]
		if !ok {
			return fmt.Errorf("direct: unknown server %q", serverName)
		}
		dt := conn.cfg.DirectTools
		if !dt.Enabled() {
			dt = allDirect // --direct flag: expose everything
		}

		sess, err := conn.get(ctx)
		if err != nil {
			return fmt.Errorf("direct: connect %q: %w", serverName, err)
		}
		lr, err := sess.ListTools(ctx, nil)
		if err != nil {
			return fmt.Errorf("direct: list tools for %q: %w", serverName, err)
		}
		for _, t := range lr.Tools {
			if p.isExcluded(serverName, t.Name) || !dt.Includes(t.Name) {
				continue
			}
			toolName := p.discoverName(serverName, t.Name)
			slog.Info("registering direct tool", "name", toolName)

			schema := t.InputSchema
			if schema == nil {
				schema = json.RawMessage(`{"type":"object","properties":{}}`)
			}

			tn := t.Name
			c := conn

			mcp.AddTool(p.server, &mcp.Tool{
				Name:        toolName,
				Description: t.Description,
				InputSchema: schema,
			}, func(ctx context.Context, _ *mcp.CallToolRequest, args map[string]any) (*mcp.CallToolResult, any, error) {
				sess, err := c.get(ctx)
				if err != nil {
					return nil, nil, err
				}
				result, err := sess.CallTool(ctx, &mcp.CallToolParams{
					Name:      tn,
					Arguments: args,
				})
				if errors.Is(err, mcp.ErrConnectionClosed) {
					c.reset()
				}
				if err != nil {
					return nil, nil, err
				}
				return result, nil, nil
			})
		}
		if dt.All {
			p.fullyDirect[serverName] = true
		}
	}
	return nil
}

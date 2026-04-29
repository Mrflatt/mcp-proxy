# AGENTS.md

## Commands

```bash
go build ./...                        # build all packages
go vet ./...                          # vet all packages
go test ./...                         # run all tests
go install ./cmd/mcp-proxy            # install binary to $GOPATH/bin
```

No Makefile or CI config yet. Always run `go build ./...` and `go vet ./...` after any change.

## Project structure

```
cmd/mcp-proxy/main.go     entrypoint: flag parsing, wiring
config/config.go          Config/ServerConfig/AuthConfig structs + JSON loader
proxy/
  proxy.go                Proxy struct: upstream client pool, env expansion, header transport
  discover.go             mcp_discover + mcp_search tool handlers
  call.go                 mcp_call tool handler
  direct.go               direct mode: upstream tools re-registered as servername-toolname
auth/
  auth.go                 factory: New(cfg) → auth.OAuthHandler
  bearer.go               static bearer token
  google_idtoken.go       Google OIDC ID token (ADC + optional SA impersonation)
  google_access_token.go  Google OAuth2 access token (ADC + optional SA impersonation)
```

## Stack

- Go 1.26.2
- `github.com/modelcontextprotocol/go-sdk` — MCP client + server (stdio + streamable HTTP)
- `golang.org/x/oauth2` + `google.golang.org/api` — Google auth and SA impersonation

## Code style

Use `mcp.AddTool` (generic) for all tool registrations — not `server.AddTool` (the raw method). Input types are named structs with `json` and `jsonschema` tags; output is `any` when there is no structured schema.

```go
// Good
type callInput struct {
    Tool      string         `json:"tool"      jsonschema:"Tool in server-toolname format"`
    Arguments map[string]any `json:"arguments,omitempty" jsonschema:"Tool arguments"`
}

mcp.AddTool(p.server, &mcp.Tool{Name: "mcp_call", Description: "..."}, 
    func(ctx context.Context, _ *mcp.CallToolRequest, input callInput) (*mcp.CallToolResult, any, error) {
        ...
    })

// Bad — raw JSON schema, raw ToolHandler signature
p.server.AddTool(&mcp.Tool{..., InputSchema: json.RawMessage(`{"type":"object",...}`)},
    func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) { ... })
```

Use `sync.OnceValues` (not `sync.Once` + manual fields) for lazy one-time initialisation that returns a value and error.

```go
// Good
h.init = sync.OnceValues(func() (oauth2.TokenSource, error) { ... })

// Bad
var once sync.Once
var ts oauth2.TokenSource
var err error
once.Do(func() { ts, err = ... })
```

All logging goes to stderr via `log/slog`. Never write to stdout — it is the MCP wire.

Tool names in proxy mode: `servername-toolname` (hyphen separator). The fallback resolver matches known server name prefixes, so hyphenated server names work correctly.

Env values in config support `${VAR}` interpolation via `os.Expand`.

## Key behaviours

**Connection**: lazy — servers are connected on first use, not at startup. Errors surface at call time. Failed connects are retried on the next call (errors are never cached). Session drops (`mcp.ErrConnectionClosed`) reset the connector so the next call re-dials.

**Auth token sources**: created lazily and cached on success; errors are not cached. `Authorize()` (called by the transport on 401/403) invalidates the cached token source so the next request re-reads ADC credentials from disk — this handles RAPT failures and `gcloud auth application-default login` refreshes without restarting the proxy.

**Proxy mode (default):** exposes `mcp_discover`, `mcp_search`, `mcp_call`. Only sessions not marked direct are included.

**Direct mode:** tools registered as `servername-toolname` (or just `toolname` with `noPrefix`). Activated by `--direct` flag (all servers) or `"direct": true` per server in config. Direct mode connects eagerly at registration time to list tools, but handlers reconnect on session drop.

**`noPrefix`:** per-server flag to omit the server name prefix. Only safe when tool names are unique across all servers. `mcp_call` uses a routes map (refreshed on each discover/search call) to resolve unprefixed names; falls back to server-name prefix matching for prefixed names.

**`excludeTools`:** per-server list of upstream tool names to hide. Applied in discover, search, and direct registration.

## Boundaries

- Never log to stdout.
- Never commit directly to `main`.
- Never edit `go.sum` manually.
- `go build ./...` and `go vet ./...` must pass clean before any commit.
- Do not add dependencies without discussion — the dep tree is already large due to `google.golang.org/api`.

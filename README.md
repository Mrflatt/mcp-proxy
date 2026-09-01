# mcp-proxy

A single-binary MCP proxy that connects AI clients (Claude, Copilot, Cursor, etc.) to one or more upstream MCP servers, with built-in authentication support for Google Cloud and static bearer tokens.

Instead of flooding the LLM context with dozens of tools from multiple servers, mcp-proxy exposes a single `mcp` meta-tool so the model can discover and invoke upstream tools on demand. A direct mode is also available when full tool visibility is preferred.

## Installation

```bash
go install github.com/Mrflatt/mcp-proxy/cmd/mcp-proxy@latest
```

Requires Go 1.24+.

## Quick start

1. Create `~/.config/mcp-proxy/config.json`:

```json
{
  "servers": {
    "my-api": {
      "url": "https://my-api.example.com/mcp",
      "auth": {
        "type": "google-idtoken",
        "audience": "https://my-api.example.com"
      }
    }
  }
}
```

2. Add mcp-proxy to your client config:

**Claude Desktop** (`~/Library/Application Support/Claude/claude_desktop_config.json`):
```json
{
  "mcpServers": {
    "proxy": {
      "command": "mcp-proxy"
    }
  }
}
```

**VS Code Copilot** (`.vscode/mcp.json` or run `MCP: Open User Configuration`):
```json
{
  "servers": {
    "proxy": {
      "command": "mcp-proxy"
    }
  }
}
```

**Claude Code** — run once to register:
```bash
claude mcp add --transport stdio proxy -- mcp-proxy
```
Or edit `~/.claude.json` directly:
```json
{
  "mcpServers": {
    "proxy": {
      "type": "stdio",
      "command": "mcp-proxy",
      "args": []
    }
  }
}
```

**GitHub Copilot CLI** (`~/.copilot/mcp-config.json`):
```json
{
  "mcpServers": {
    "proxy": {
      "type": "stdio",
      "command": "mcp-proxy",
      "args": []
    }
  }
}
```

## How it works

### Proxy mode (default)

mcp-proxy exposes a single `mcp` tool to the AI client:

| Call | Description |
|------|-------------|
| `mcp({})` | List all servers with tool counts |
| `mcp({ server: "name" })` | List tools for a specific server |
| `mcp({ search: "query" })` | Search tools by server or tool name |
| `mcp({ tool: "server-toolname", arguments: {} })` | Call a tool |

Tool names use the `server-toolname` convention, so `my-api-list_users` identifies the `list_users` tool on the `my-api` server.

Tool metadata is persisted in a JSON cache at the platform's user cache directory (`~/.cache/mcp-proxy/tools.json` on Linux). When cached metadata is available, `mcp({})` and searches return it immediately while a background refresh updates memory and the file. Cache entries older than five minutes are refreshed; a cold cache still fetches metadata from the upstream server. Override the location with `--cache`, or pass an empty value to disable caching.

### Direct mode

When you want the LLM to see all upstream tools natively, use `--direct`:

```bash
mcp-proxy --direct
```

Each upstream tool is registered directly as `servername-toolname`. You can also enable this per server in config with `"directTools": true`, or for specific tools with `"directTools": ["tool1", "tool2"]`.

## Configuration

Default config location: `~/.config/mcp-proxy/config.json`  
Override with: `mcp-proxy --config /path/to/config.json`

### Full config reference

```json
{
  "servers": {
    "http-server": {
      "url": "https://api.example.com/mcp",
      "headers": {
        "X-Custom-Header": "value"
      },
      "auth": {
        "type": "bearer",
        "tokenEnv": "MY_API_TOKEN"
      },
      "eager": true,
      "keepalive": "30s",
      "excludeTools": ["internal_tool", "debug_tool"],
      "includeTools": ["public_tool_1", "public_tool_2"],
      "directTools": false
    },

    "stdio-server": {
      "command": "my-mcp-server",
      "args": ["--flag"],
      "env": {
        "SERVER_USERNAME": "${SERVER_USERNAME}",
        "SERVER_PASSWORD": "${SERVER_PASSWORD}"
      }
    }
  }
}
```

### Server options

| Field | Type | Description |
|-------|------|-------------|
| `url` | string | HTTP upstream endpoint (streamable MCP) |
| `headers` | object | Extra HTTP headers added to every request. Values support `${VAR}` interpolation |
| `command` | string | Command to run as a stdio MCP server |
| `args` | array | Arguments for the stdio command |
| `env` | object | Extra environment variables for the subprocess. Values support `${VAR}` interpolation |
| `auth` | object | Authentication config (see below) |
| `eager` | bool | Connect at startup instead of on first use |
| `keepalive` | string | Ping interval to keep the connection alive (e.g. `"30s"`) |
| `directTools` | `true` or `["tool1","tool2"]` | Expose all or specific tools directly, bypassing the meta-tool layer |
| `noPrefix` | `true` or `["tool1","tool2"]` | Omit the server name prefix from tool names (`toolname` instead of `server-toolname`). `true` = all tools, array = only those tools. Only safe when unprefixed names are unique across all servers |
| `includeTools` | array | Whitelist of tool names to expose. When set, all other tools are hidden. Mutually exclusive with `excludeTools` |
| `excludeTools` | array | Upstream tool names to hide from the LLM |

### Authentication

#### No auth (default)

Omit the `auth` field entirely.

#### Static bearer token

```json
{
  "type": "bearer",
  "tokenEnv": "MY_API_TOKEN"
}
```

Use `token` for a literal value or `tokenEnv` to read from an environment variable.

#### Google ID token (OIDC)

```json
{
  "type": "google-idtoken",
  "audience": "https://api.example.com"
}
```

Uses Application Default Credentials. Run `gcloud auth application-default login` to set up ADC locally.

Add `"includeEmail": true` to include the service account email in the token claim (required by some IAP-protected endpoints).

#### Google access token (OAuth2)

```json
{
  "type": "google-access-token",
  "scopes": ["https://www.googleapis.com/auth/cloud-platform"]
}
```

#### Service account impersonation

Add `serviceAccount` to either Google type to impersonate a service account before fetching the token:

```json
{
  "type": "google-idtoken",
  "audience": "https://api.example.com",
  "serviceAccount": "proxy-sa@my-project.iam.gserviceaccount.com",
  "includeEmail": true
}
```

```json
{
  "type": "google-access-token",
  "scopes": ["https://www.googleapis.com/auth/cloud-platform"],
  "serviceAccount": "proxy-sa@my-project.iam.gserviceaccount.com"
}
```

Your ADC principal needs the `roles/iam.serviceAccountTokenCreator` role on the target service account.

## Mixing proxy and direct mode

You can expose some servers directly while keeping others behind the meta-tool layer. `directTools` accepts `true` for all tools or a list of specific tool names:

```json
{
  "servers": {
    "always-needed": {
      "url": "https://core.example.com/mcp",
      "directTools": true
    },
    "partially-direct": {
      "url": "https://other.example.com/mcp",
      "directTools": ["ping", "list_interfaces"]
    },
    "large-api": {
      "url": "https://large.example.com/mcp"
    }
  }
}
```

`always-needed` tools appear directly in the client's tool list as `always-needed-toolname`. `partially-direct` exposes only `ping` and `list_interfaces` directly — its other tools are still discoverable via `mcp({ server: "partially-direct" })`. `large-api` tools are only visible after calling `mcp({})` or `mcp({ search: "..." })`.

> [!NOTE]
> `mcp({})` and `mcp({ search })` only list tools not exposed via `directTools`.

> [!NOTE]
> `noPrefix: true` is safe only when tool names are unique across all servers. If two servers expose a tool with the same name, the last one wins.

## CLI flags

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | `~/.config/mcp-proxy/config.json` | Path to config file |
| `--cache` | platform user cache directory | Path to persistent tool metadata cache; empty disables caching |
| `--direct` | `false` | Expose all upstream tools directly (overrides per-server config) |
| `--version` | | Print version and exit |
| `--update` | | Self-update to latest version via `go install` and exit |

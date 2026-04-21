# mcp-proxy

A single-binary MCP proxy that connects AI clients (Claude, Copilot, Cursor, etc.) to one or more upstream MCP servers, with built-in authentication support for Google Cloud and static bearer tokens.

Instead of flooding the LLM context with dozens of tools from multiple servers, mcp-proxy exposes three lightweight meta-tools — `mcp_discover`, `mcp_search`, and `mcp_call` — so the model can find and invoke what it needs on demand. A direct mode is also available when full tool visibility is preferred.

## Installation

```bash
go install github.com/Mrflatt/mcp-proxy/cmd/mcp-proxy@latest
```

Requires Go 1.21+.

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

2. Add mcp-proxy to your client config (Claude Desktop example):

```json
{
  "mcpServers": {
    "proxy": {
      "command": "mcp-proxy"
    }
  }
}
```

The LLM will call `mcp_discover` to see what tools are available, then use `mcp_call` to invoke them.

## How it works

### Proxy mode (default)

mcp-proxy exposes three tools to the AI client:

| Tool | Description |
|------|-------------|
| `mcp_discover` | Lists all tools across all upstream servers |
| `mcp_search` | Filters tools by name or description |
| `mcp_call` | Calls a tool by its `server__toolname` identifier |

Tool names follow the `server__toolname` convention (double underscore), so `my-api__list_users` unambiguously identifies the `list_users` tool on the `my-api` server.

### Direct mode

When you want the LLM to see all upstream tools natively, use `--direct`:

```bash
mcp-proxy --direct
```

Each upstream tool is registered directly as `servername_toolname`. You can also enable this per server in config with `"directTools": true`, or for specific tools with `"directTools": ["tool1", "tool2"]`.

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
      "excludeTools": ["internal_tool", "debug_tool"],
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
| `headers` | object | Extra HTTP headers added to every request |
| `command` | string | Command to run as a stdio MCP server |
| `args` | array | Arguments for the stdio command |
| `env` | object | Extra environment variables for the subprocess. Values support `${VAR}` interpolation from the current environment |
| `auth` | object | Authentication config (see below) |
| `directTools` | `true` or `["tool1","tool2"]` | Expose all or specific tools directly, bypassing the meta-tool layer |
| `noPrefix` | bool | Omit the server name prefix from tool names (`toolname` instead of `server_toolname`). Only safe when tool names are unique across all servers |
| `excludeTools` | array | Tool names to hide from the LLM |

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
  "serviceAccount": "proxy-sa@my-project.iam.gserviceaccount.com"
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

`always-needed` tools appear directly in the client's tool list as `always-needed_toolname`. `partially-direct` exposes only `ping` and `list_interfaces` directly — its other tools are still reachable via `mcp_discover`. `large-api` tools are only visible after calling `mcp_discover` or `mcp_search`.

> [!NOTE]
> `mcp_discover` and `mcp_search` only list tools not exposed via `directTools`.

> [!NOTE]
> `noPrefix: true` is safe only when tool names are unique across all servers. If two servers expose a tool with the same name, the last one to appear in `mcp_discover` wins.

## CLI flags

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | `~/.config/mcp-proxy/config.json` | Path to config file |
| `--direct` | `false` | Expose all upstream tools directly (overrides per-server config) |

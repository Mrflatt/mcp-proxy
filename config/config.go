package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	Servers map[string]ServerConfig `json:"servers"`
}

// DirectTools controls which tools are exposed directly on the proxy server.
// It unmarshal from either a JSON boolean or a JSON array of tool name strings.
type DirectTools struct {
	All   bool
	Names []string
}

func (d *DirectTools) Enabled() bool { return d.All || len(d.Names) > 0 }
func (d *DirectTools) Includes(name string) bool {
	if d.All {
		return true
	}
	for _, n := range d.Names {
		if n == name {
			return true
		}
	}
	return false
}

func (d *DirectTools) UnmarshalJSON(data []byte) error {
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		d.All = b
		return nil
	}
	var names []string
	if err := json.Unmarshal(data, &names); err != nil {
		return fmt.Errorf("directTools must be a boolean or array of strings: %w", err)
	}
	d.Names = names
	return nil
}

// NoPrefix controls which tools omit the server name prefix.
// It unmarshals from either a JSON boolean or a JSON array of tool name strings.
type NoPrefix struct {
	All   bool
	Names []string
}

func (n *NoPrefix) Enabled() bool { return n.All || len(n.Names) > 0 }
func (n *NoPrefix) Includes(name string) bool {
	if n.All {
		return true
	}
	for _, s := range n.Names {
		if s == name {
			return true
		}
	}
	return false
}

func (n *NoPrefix) UnmarshalJSON(data []byte) error {
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		n.All = b
		return nil
	}
	var names []string
	if err := json.Unmarshal(data, &names); err != nil {
		return fmt.Errorf("noPrefix must be a boolean or array of strings: %w", err)
	}
	n.Names = names
	return nil
}

type ServerConfig struct {
	// HTTP upstream
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"` // extra HTTP headers
	// Stdio upstream
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"` // extra env vars for subprocess
	// Auth
	Auth *AuthConfig `json:"auth,omitempty"`
	// Eager connects to this server at startup instead of on first use.
	Eager bool `json:"eager,omitempty"`
	// Keepalive is the interval for sending pings to keep the connection alive (e.g. "30s").
	// Zero or omitted disables keepalive.
	Keepalive string `json:"keepalive,omitempty"`
	// DirectTools exposes tools directly on the proxy. true = all tools,
	// ["tool1","tool2"] = only those tools (rest go through discover/call).
	DirectTools DirectTools `json:"directTools,omitempty"`
	// NoPrefix omits the server name prefix from tool names (toolname instead of server-toolname).
	// true = all tools, ["tool1","tool2"] = only those tools.
	// Use only when tool names are unique across all servers.
	NoPrefix NoPrefix `json:"noPrefix,omitempty"`
	// ExcludeTools lists tool names to hide from the LLM.
	ExcludeTools []string `json:"excludeTools,omitempty"`
}

type AuthConfig struct {
	Type           string   `json:"type"`
	Token          string   `json:"token,omitempty"`
	TokenEnv       string   `json:"tokenEnv,omitempty"`
	Audience       string   `json:"audience,omitempty"`
	IncludeEmail   bool     `json:"includeEmail,omitempty"`
	Scopes         []string `json:"scopes,omitempty"`
	ServiceAccount string   `json:"serviceAccount,omitempty"`
}

func Load(path string) (*Config, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("config: cannot find home dir: %w", err)
		}
		path = filepath.Join(home, ".config", "mcp-proxy", "config.json")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: cannot read %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: malformed JSON in %s: %w", path, err)
	}
	return &cfg, nil
}

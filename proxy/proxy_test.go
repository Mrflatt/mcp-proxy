package proxy

import (
	"testing"

	"github.com/Mrflatt/mcp-proxy/config"
)

func newTestProxy(servers map[string]config.ServerConfig) *Proxy {
	p := New()
	p.Connect(nil, &config.Config{Servers: servers}, nil)
	return p
}

func TestDiscoverName(t *testing.T) {
	tests := []struct {
		name       string
		noPrefix   bool
		serverName string
		toolName   string
		want       string
	}{
		{name: "prefixed", serverName: "my-server", toolName: "list_items", want: "my-server-list_items"},
		{name: "no prefix", noPrefix: true, serverName: "my-server", toolName: "list_items", want: "list_items"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestProxy(map[string]config.ServerConfig{
				tt.serverName: {Command: "dummy", NoPrefix: tt.noPrefix},
			})
			if got := p.discoverName(tt.serverName, tt.toolName); got != tt.want {
				t.Errorf("discoverName(%q, %q) = %q, want %q", tt.serverName, tt.toolName, got, tt.want)
			}
		})
	}
}

func TestIsExcluded(t *testing.T) {
	p := newTestProxy(map[string]config.ServerConfig{
		"server-a": {Command: "dummy", ExcludeTools: []string{"internal_tool", "debug"}},
	})

	if !p.isExcluded("server-a", "internal_tool") {
		t.Error("expected internal_tool to be excluded")
	}
	if p.isExcluded("server-a", "list_items") {
		t.Error("expected list_items to not be excluded")
	}
	if p.isExcluded("server-b", "internal_tool") {
		t.Error("exclusion should be per-server")
	}
}

func TestIsDirectTool(t *testing.T) {
	var dtAll config.DirectTools
	_ = dtAll.UnmarshalJSON([]byte("true"))

	var dtList config.DirectTools
	_ = dtList.UnmarshalJSON([]byte(`["list_items"]`))

	p := newTestProxy(map[string]config.ServerConfig{
		"server-a": {Command: "dummy", DirectTools: dtAll},
		"server-b": {Command: "dummy", DirectTools: dtList},
		"server-c": {Command: "dummy"},
	})

	// fullyDirect server — all tools are direct
	if !p.isDirectTool("server-a", "any_tool") {
		t.Error("all server-a tools should be direct")
	}
	// partial direct — only listed tool
	if !p.isDirectTool("server-b", "list_items") {
		t.Error("server-b/list_items should be direct")
	}
	if p.isDirectTool("server-b", "delete_item") {
		t.Error("server-b/delete_item should not be direct")
	}
	// no direct config
	if p.isDirectTool("server-c", "list_items") {
		t.Error("server-c/list_items should not be direct")
	}
}

func TestProxyConnectors(t *testing.T) {
	var dtAll config.DirectTools
	_ = dtAll.UnmarshalJSON([]byte("true"))

	p := newTestProxy(map[string]config.ServerConfig{
		"server-a": {Command: "dummy", DirectTools: dtAll},
		"server-b": {Command: "dummy"},
	})

	// Simulate RegisterDirect marking server-a as fully direct.
	p.fullyDirect["server-a"] = true

	conns := p.proxyConnectors()
	if _, ok := conns["server-a"]; ok {
		t.Error("fully direct server should be excluded from proxyConnectors")
	}
	if _, ok := conns["server-b"]; !ok {
		t.Error("non-direct server should be in proxyConnectors")
	}
}

func TestExpandEnv(t *testing.T) {
	t.Setenv("TEST_VAR", "hello")

	tests := []struct {
		input string
		want  string
	}{
		{input: "${TEST_VAR}", want: "hello"},
		{input: "prefix_${TEST_VAR}_suffix", want: "prefix_hello_suffix"},
		{input: "${UNSET_VAR}", want: ""},
		{input: "no_var", want: "no_var"},
	}
	for _, tt := range tests {
		if got := expandEnv(tt.input); got != tt.want {
			t.Errorf("expandEnv(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestResolveCall(t *testing.T) {
	p := New()
	p.connectors["server-a"] = &connector{name: "server-a", cfg: config.ServerConfig{Command: "dummy"}}

	// Seed routes as mcp() would.
	p.routes["server-a-list_items"] = toolRoute{serverName: "server-a", toolName: "list_items"}

	conn, tool, err := p.resolveCall("server-a-list_items")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool != "list_items" {
		t.Errorf("tool = %q, want %q", tool, "list_items")
	}
	if conn.name != "server-a" {
		t.Errorf("conn.name = %q, want %q", conn.name, "server-a")
	}
}

func TestResolveCallFallback(t *testing.T) {
	p := New()
	p.connectors["server-a"] = &connector{name: "server-a", cfg: config.ServerConfig{Command: "dummy"}}
	// No routes seeded — tests the server-name prefix fallback.

	conn, tool, err := p.resolveCall("server-a-list_items")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool != "list_items" {
		t.Errorf("tool = %q, want %q", tool, "list_items")
	}
	if conn.name != "server-a" {
		t.Errorf("conn.name = %q, want %q", conn.name, "server-a")
	}
}

func TestResolveCallUnknown(t *testing.T) {
	p := New()
	_, _, err := p.resolveCall("no_such_tool")
	if err == nil {
		t.Error("expected error for unknown tool")
	}
}

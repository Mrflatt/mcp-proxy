package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Mrflatt/mcp-proxy/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestToolCacheRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "tools.json")
	identity := "server-identity"
	want := []cachedTool{{
		Name:         "list_items",
		Description:  "List items",
		InputSchema:  map[string]any{"type": "object"},
		OutputSchema: map[string]any{"type": "array"},
	}}

	cache := newToolCache(path)
	cache.setIdentities(map[string]string{"api": identity})
	cache.replace("api", want)
	if err := cache.save(); err != nil {
		t.Fatalf("save cache: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	var disk toolCacheFile
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatalf("decode cache: %v", err)
	}
	if disk.Version != toolCacheVersion {
		t.Errorf("cache version = %d, want %d", disk.Version, toolCacheVersion)
	}

	loaded := newToolCache(path)
	if err := loaded.load(); err != nil {
		t.Fatalf("load cache: %v", err)
	}
	loaded.setIdentities(map[string]string{"api": identity})
	got, ok := loaded.tools("api")
	if !ok {
		t.Fatal("expected cached tools")
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cached tools = %#v, want %#v", got, want)
	}

	loaded.setIdentities(map[string]string{"api": "changed"})
	if _, ok := loaded.tools("api"); ok {
		t.Error("returned tools for a changed server identity")
	}
}

func TestListToolsUsesPersistentCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tools.json")
	sc := config.ServerConfig{URL: "http://127.0.0.1:1/mcp"}

	cache := newToolCache(path)
	cache.setIdentities(map[string]string{"api": serverCacheFingerprint(sc)})
	cache.replace("api", []cachedTool{{
		Name:         "cached_search",
		Description:  "Search from cache",
		InputSchema:  map[string]any{"type": "object"},
		OutputSchema: map[string]any{"type": "array"},
	}})
	if err := cache.save(); err != nil {
		t.Fatalf("save cache: %v", err)
	}

	p := NewWithCache(path)
	p.Connect(t.Context(), &config.Config{Servers: map[string]config.ServerConfig{"api": sc}}, nil)

	_, output, err := p.listTools(t.Context(), "api", "cached")
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	listing, ok := output.(toolListing)
	if !ok {
		t.Fatalf("output type = %T, want toolListing", output)
	}
	if len(listing.Tools) != 1 {
		t.Fatalf("got %d tools, want 1", len(listing.Tools))
	}
	if listing.Tools[0].Name != "api-cached_search" {
		t.Errorf("tool name = %q, want %q", listing.Tools[0].Name, "api-cached_search")
	}
	if listing.Tools[0].Description != "Search from cache" {
		t.Errorf("tool description = %q, want %q", listing.Tools[0].Description, "Search from cache")
	}
	if !reflect.DeepEqual(listing.Tools[0].OutputSchema, map[string]any{"type": "array"}) {
		t.Errorf("output schema = %#v, want array schema", listing.Tools[0].OutputSchema)
	}

	_, output, err = p.listServers(t.Context())
	if err != nil {
		t.Fatalf("list servers: %v", err)
	}
	servers, ok := output.(serverListing)
	if !ok {
		t.Fatalf("server output type = %T, want serverListing", output)
	}
	if got := servers.Servers["api"].ToolCount; got != 1 {
		t.Errorf("cached tool count = %d, want 1", got)
	}
}

func TestToolCacheRefreshesInBackground(t *testing.T) {
	upstream := mcp.NewServer(&mcp.Implementation{Name: "upstream", Version: "1.0.0"}, nil)
	mcp.AddTool(upstream, &mcp.Tool{
		Name:         "fresh_tool",
		Description:  "Fresh tool",
		InputSchema:  map[string]any{"type": "object"},
		OutputSchema: map[string]any{"type": "object"},
	}, func(context.Context, *mcp.CallToolRequest, map[string]any) (*mcp.CallToolResult, any, error) {
		return nil, nil, nil
	})
	upstreamHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return upstream
	}, nil)
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		upstreamHandler.ServeHTTP(w, r)
	}))
	defer httpServer.Close()

	sc := config.ServerConfig{URL: httpServer.URL}
	path := filepath.Join(t.TempDir(), "tools.json")
	cache := newToolCache(path)
	cache.setIdentities(map[string]string{"api": serverCacheFingerprint(sc)})
	cache.replace("api", []cachedTool{{Name: "stale_tool", Description: "Stale tool"}})
	cache.mu.Lock()
	server := cache.servers["api"]
	server.UpdatedAt = time.Now().Add(-toolCacheRefreshInterval - time.Second)
	cache.servers["api"] = server
	cache.mu.Unlock()
	if err := cache.save(); err != nil {
		t.Fatalf("save stale cache: %v", err)
	}

	p := NewWithCache(path)
	p.Connect(t.Context(), &config.Config{Servers: map[string]config.ServerConfig{"api": sc}}, nil)
	defer func() {
		if sess := p.connectors["api"].sess; sess != nil {
			_ = sess.Close()
		}
	}()

	_, output, err := p.listTools(t.Context(), "api", "")
	if err != nil {
		t.Fatalf("list cached tools: %v", err)
	}
	listing := output.(toolListing)
	if len(listing.Tools) != 1 || listing.Tools[0].Name != "api-stale_tool" {
		t.Fatalf("initial tools = %#v, want stale cached tool", listing.Tools)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tools, ok := p.cache.tools("api")
		if ok && len(tools) == 1 && tools[0].Name == "fresh_tool" {
			loaded := newToolCache(path)
			if err := loaded.load(); err == nil {
				loaded.setIdentities(map[string]string{"api": serverCacheFingerprint(sc)})
				fileTools, fileOK := loaded.tools("api")
				if fileOK && len(fileTools) == 1 && fileTools[0].Name == "fresh_tool" {
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	tools, _ := p.cache.tools("api")
	t.Fatalf("background refresh did not update cache, got %#v", tools)
}

package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/Mrflatt/mcp-proxy/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	toolCacheVersion         = 1
	toolCacheRefreshInterval = 5 * time.Minute
	toolCacheRefreshTimeout  = 30 * time.Second
)

type toolCache struct {
	path string

	mu         sync.RWMutex
	servers    map[string]cachedServer
	identities map[string]string
	refreshing map[string]bool
	saveMu     sync.Mutex
}

type toolCacheFile struct {
	Version int                     `json:"version"`
	Servers map[string]cachedServer `json:"servers"`
}

type cachedServer struct {
	Fingerprint string       `json:"fingerprint"`
	Tools       []cachedTool `json:"tools"`
	UpdatedAt   time.Time    `json:"updatedAt"`
}

type cachedTool struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	InputSchema  any    `json:"inputSchema,omitempty"`
	OutputSchema any    `json:"outputSchema,omitempty"`
}

// DefaultCachePath returns the default persistent tool cache path.
func DefaultCachePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cache: cannot find user cache dir: %w", err)
	}
	return filepath.Join(dir, "mcp-proxy", "tools.json"), nil
}

func newToolCache(path string) *toolCache {
	return &toolCache{
		path:       path,
		servers:    make(map[string]cachedServer),
		identities: make(map[string]string),
		refreshing: make(map[string]bool),
	}
}

func (c *toolCache) load() error {
	data, err := os.ReadFile(c.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", c.path, err)
	}

	var disk toolCacheFile
	if err := json.Unmarshal(data, &disk); err != nil {
		return fmt.Errorf("parse %s: %w", c.path, err)
	}
	if disk.Version != toolCacheVersion {
		return fmt.Errorf("unsupported version %d in %s", disk.Version, c.path)
	}
	if disk.Servers == nil {
		disk.Servers = make(map[string]cachedServer)
	}

	c.mu.Lock()
	c.servers = disk.Servers
	c.refreshing = make(map[string]bool)
	c.mu.Unlock()
	return nil
}

func (c *toolCache) setIdentities(identities map[string]string) {
	c.mu.Lock()
	c.identities = maps.Clone(identities)
	for name := range c.servers {
		if _, ok := c.identities[name]; !ok {
			delete(c.servers, name)
		}
	}
	c.mu.Unlock()
}

func (c *toolCache) tools(serverName string) ([]cachedTool, bool) {
	c.mu.RLock()
	server, ok := c.servers[serverName]
	identity := c.identities[serverName]
	c.mu.RUnlock()
	if !ok || identity == "" || server.Fingerprint != identity {
		return nil, false
	}
	return slices.Clone(server.Tools), true
}

func (c *toolCache) beginRefresh(serverName string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	server, ok := c.servers[serverName]
	if !ok || c.identities[serverName] == "" || server.Fingerprint != c.identities[serverName] {
		return false
	}
	if c.refreshing[serverName] {
		return false
	}
	if !server.UpdatedAt.IsZero() && now.Sub(server.UpdatedAt) < toolCacheRefreshInterval {
		return false
	}
	c.refreshing[serverName] = true
	return true
}

func (c *toolCache) endRefresh(serverName string) {
	c.mu.Lock()
	delete(c.refreshing, serverName)
	c.mu.Unlock()
}

func (c *toolCache) replace(serverName string, tools []cachedTool) {
	c.mu.Lock()
	identity := c.identities[serverName]
	if identity == "" {
		c.mu.Unlock()
		return
	}
	if tools == nil {
		tools = []cachedTool{}
	}
	c.servers[serverName] = cachedServer{
		Fingerprint: identity,
		Tools:       slices.Clone(tools),
		UpdatedAt:   time.Now().UTC(),
	}
	c.mu.Unlock()
}

func (c *toolCache) save() error {
	c.saveMu.Lock()
	defer c.saveMu.Unlock()

	c.mu.RLock()
	servers := make(map[string]cachedServer, len(c.servers))
	for name, server := range c.servers {
		server.Tools = slices.Clone(server.Tools)
		servers[name] = server
	}
	c.mu.RUnlock()

	data, err := json.MarshalIndent(toolCacheFile{
		Version: toolCacheVersion,
		Servers: servers,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", c.path, err)
	}

	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create cache directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".tools-*.tmp")
	if err != nil {
		return fmt.Errorf("create cache temporary file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("set permissions on cache temporary file: %w", err)
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("write cache temporary file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync cache temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close cache temporary file: %w", err)
	}
	if err := os.Rename(tmpName, c.path); err != nil {
		return fmt.Errorf("replace cache file: %w", err)
	}
	return nil
}

func cachedToolsFromUpstream(tools []*mcp.Tool) ([]cachedTool, error) {
	cached := make([]cachedTool, 0, len(tools))
	for _, tool := range tools {
		if tool == nil {
			return nil, fmt.Errorf("upstream returned a nil tool")
		}
		inputSchema, err := normalizeSchema(tool.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("encode input schema for %q: %w", tool.Name, err)
		}
		outputSchema, err := normalizeSchema(tool.OutputSchema)
		if err != nil {
			return nil, fmt.Errorf("encode output schema for %q: %w", tool.Name, err)
		}
		cached = append(cached, cachedTool{
			Name:         tool.Name,
			Description:  tool.Description,
			InputSchema:  inputSchema,
			OutputSchema: outputSchema,
		})
	}
	return cached, nil
}

func normalizeSchema(schema any) (any, error) {
	if schema == nil {
		return nil, nil
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	var normalized any
	if err := json.Unmarshal(data, &normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}

func serverCacheFingerprint(sc config.ServerConfig) string {
	identity := struct {
		URL     string             `json:"url,omitempty"`
		Headers map[string]string  `json:"headers,omitempty"`
		Command string             `json:"command,omitempty"`
		Args    []string           `json:"args,omitempty"`
		Env     map[string]string  `json:"env,omitempty"`
		Auth    *config.AuthConfig `json:"auth,omitempty"`
	}{
		URL:     sc.URL,
		Headers: sc.Headers,
		Command: sc.Command,
		Args:    sc.Args,
		Env:     sc.Env,
		Auth:    sc.Auth,
	}
	data, _ := json.Marshal(identity)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

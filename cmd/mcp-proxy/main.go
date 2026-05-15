package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime/debug"

	"github.com/Mrflatt/mcp-proxy/auth"
	"github.com/Mrflatt/mcp-proxy/config"
	"github.com/Mrflatt/mcp-proxy/proxy"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func version() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	v := bi.Main.Version
	for _, s := range bi.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 7 {
			v += " (" + s.Value[:7] + ")"
			break
		}
	}
	return v
}

func selfUpdate() error {
	cmd := exec.Command("go", "install", "github.com/Mrflatt/mcp-proxy/cmd/mcp-proxy@latest")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func main() {
	configPath := flag.String("config", "", "path to config file (default: ~/.config/mcp-proxy/config.json)")
	directAll := flag.Bool("direct", false, "expose all upstream tools directly (overrides per-server config)")
	showVersion := flag.Bool("version", false, "print version and exit")
	update := flag.Bool("update", false, "self-update to latest version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Fprintln(os.Stderr, "mcp-proxy", version())
		return
	}
	if *update {
		fmt.Fprintln(os.Stderr, "updating mcp-proxy to latest...")
		if err := selfUpdate(); err != nil {
			fmt.Fprintln(os.Stderr, "update failed:", err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "updated successfully")
		return
	}

	// All logging to stderr — stdout is the MCP wire.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()

	handlers := make(map[string]sdkauth.OAuthHandler)
	for name, sc := range cfg.Servers {
		h, err := auth.New(sc.Auth)
		if err != nil {
			slog.Error("auth setup failed", "server", name, "error", err)
			os.Exit(1)
		}
		handlers[name] = h
	}

	p := proxy.New()
	p.Connect(ctx, cfg, handlers)
	p.ConnectEager(ctx)

	if *directAll {
		// --direct flag: all tools from all servers exposed directly.
		if err := p.RegisterDirect(ctx); err != nil {
			slog.Error("failed to register direct tools", "error", err)
			os.Exit(1)
		}
	} else {
		// Per-server directTools config.
		var directServers []string
		for name, sc := range cfg.Servers {
			if sc.DirectTools.Enabled() {
				directServers = append(directServers, name)
			}
		}
		if len(directServers) > 0 {
			if err := p.RegisterDirect(ctx, directServers...); err != nil {
				slog.Error("failed to register direct tools", "error", err)
				os.Exit(1)
			}
		}
		p.RegisterDiscover()
	}

	slog.Info("starting mcp-proxy stdio server")
	if err := p.Server().Run(ctx, &mcp.StdioTransport{}); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}

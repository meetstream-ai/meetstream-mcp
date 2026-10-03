// Command meetstream-mcp is the MeetStream MCP server.
//
//	meetstream-mcp          stdio (local clients; reads MEETSTREAM_API_KEY)
//	meetstream-mcp http     Streamable HTTP (hosted at mcp.meetstream.ai)
//
// HTTP environment:
//
//	PORT, HOST                 listen address (default 0.0.0.0:8080)
//	MCP_ALLOWED_HOSTS          comma-separated Host allowlist (optional)
//	MEETSTREAM_API_KEY         single-tenant fallback key (leave unset when public)
//	MEETSTREAM_API_URL         API base (default https://api.meetstream.ai/api/v1)
//	MCP_OAUTH_MODE             off (default) | paste | dashboard
//	MCP_PUBLIC_URL             public origin, e.g. https://mcp.meetstream.ai (OAuth)
//	MCP_OAUTH_SECRET           comma-separated base64 32-byte keys; first seals (OAuth)
//	MEETSTREAM_DASHBOARD_URL   dashboard origin (dashboard mode)
//	MCP_DASHBOARD_GRANT_KEY    base64 32-byte key shared with the dashboard (dashboard mode)
//	DO_NOT_TRACK / MEETSTREAM_TELEMETRY=0   disable anonymous telemetry
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/meetstream-ai/meetstream-mcp/go/internal/meetstream"
	"github.com/meetstream-ai/meetstream-mcp/go/internal/oauth"
	"github.com/meetstream-ai/meetstream-mcp/go/internal/seal"
	"github.com/meetstream-ai/meetstream-mcp/go/internal/server"
	"github.com/meetstream-ai/meetstream-mcp/go/internal/telemetry"
	"github.com/meetstream-ai/meetstream-mcp/go/internal/tools"
)

// Version is reported in serverInfo. Set at build time with -ldflags "-X main.Version=...".
var Version = "0.4.0"

func main() {
	mode := "stdio"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	// stdout is the protocol channel in stdio mode, so all logs go to stderr.
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	var err error
	switch mode {
	case "stdio":
		err = runStdio(logger)
	case "http":
		err = runHTTP(logger)
	case "version", "--version", "-v":
		fmt.Println(Version)
	case "gen-secret":
		fmt.Println(genSecret())
	default:
		err = fmt.Errorf("unknown mode %q (use stdio, http, version or gen-secret)", mode)
	}
	if err != nil {
		logger.Error("fatal", "err", err.Error())
		os.Exit(1)
	}
}

func newMCPServer(transport string, fallbackKey string) (*mcp.Server, *telemetry.Client) {
	tel := telemetry.New(transport)
	return buildServer(tel, fallbackKey, nil), tel
}

func buildServer(tel *telemetry.Client, key string, opts *mcp.ServerOptions) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "meetstream", Version: Version}, opts)
	tools.Register(srv, tools.Config{
		BaseURL:     os.Getenv("MEETSTREAM_API_URL"),
		FallbackKey: key,
		OnCall: func(tool string, ok bool) {
			tel.Track("mcp_tool_called", map[string]any{"tool_name": tool, "ok": ok})
		},
	})
	return srv
}

func runStdio(logger *slog.Logger) error {
	srv, tel := newMCPServer("local", os.Getenv("MEETSTREAM_API_KEY"))
	tel.Track("mcp_server_started", nil)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Fprintln(os.Stderr, "meetstream-mcp ready (stdio). Set MEETSTREAM_API_KEY in this process env.")
	err := srv.Run(ctx, &mcp.StdioTransport{})
	tel.Flush(2 * time.Second)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func runHTTP(logger *slog.Logger) error {
	host := envOr("HOST", "0.0.0.0")
	port := envOr("PORT", "8080")
	fallback := os.Getenv("MEETSTREAM_API_KEY")
	srv, tel := newMCPServer("remote", fallback)

	cfg := server.Config{Host: host, FallbackKey: fallback, Logger: logger}
	// Legacy HTTP+SSE sessions get their own server bound to the session's key.
	// The keepalive ping stops nginx and other proxies from dropping idle streams.
	cfg.SessionServer = func(apiKey string) *mcp.Server {
		if apiKey == "" {
			apiKey = fallback
		}
		return buildServer(tel, apiKey, &mcp.ServerOptions{KeepAlive: 30 * time.Second, KeepAliveFailureThreshold: 3})
	}
	if v := os.Getenv("MCP_ALLOWED_HOSTS"); v != "" {
		cfg.AllowedHosts = strings.Split(v, ",")
	}
	oa, err := oauthFromEnv(logger)
	if err != nil {
		return err
	}
	cfg.OAuth = oa

	hs := &http.Server{
		Addr:              net.JoinHostPort(host, port),
		Handler:           server.New(srv, cfg),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		// get_transcript with wait=true can poll for up to 10 minutes.
		WriteTimeout:   11 * time.Minute,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 64 << 10,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- hs.ListenAndServe() }()
	tel.Track("mcp_server_started", nil)
	logger.Info("meetstream-mcp listening", "addr", "http://"+hs.Addr+"/mcp", "version", Version, "oauth", oauthMode(oa))

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = hs.Shutdown(shutdownCtx)
	tel.Flush(2 * time.Second)
	return nil
}

func oauthMode(o *oauth.Server) string {
	if o == nil {
		return "off"
	}
	return os.Getenv("MCP_OAUTH_MODE")
}

func oauthFromEnv(logger *slog.Logger) (*oauth.Server, error) {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("MCP_OAUTH_MODE")))
	if mode == "" || mode == "off" {
		return nil, nil
	}
	box, err := seal.FromString(os.Getenv("MCP_OAUTH_SECRET"))
	if err != nil {
		return nil, fmt.Errorf("MCP_OAUTH_SECRET: %w (generate one with: meetstream-mcp gen-secret)", err)
	}
	pub := os.Getenv("MCP_PUBLIC_URL")
	if pub == "" {
		return nil, errors.New("MCP_PUBLIC_URL is required when MCP_OAUTH_MODE is set")
	}
	apiURL := os.Getenv("MEETSTREAM_API_URL")
	cfg := oauth.Config{
		PublicURL:  pub,
		Mode:       mode,
		Box:        box,
		Logger:     logger,
		APIKeysURL: envOr("MEETSTREAM_API_KEYS_URL", "https://app.meetstream.ai/api-key"),
		CheckKey: func(ctx context.Context, key string) error {
			// Cheapest authenticated read: one page of the caller's own bots.
			_, err := meetstream.New(key, apiURL).Request(ctx, http.MethodGet, "/bots", nil, nil, url.Values{"limit": {"1"}})
			var ae *meetstream.APIError
			if errors.As(err, &ae) && (ae.Status == 401 || ae.Status == 403) {
				return oauth.ErrBadKey
			}
			return err
		},
	}
	if mode == oauth.ModeDashboard {
		gb, err := seal.FromString(os.Getenv("MCP_DASHBOARD_GRANT_KEY"))
		if err != nil {
			return nil, fmt.Errorf("MCP_DASHBOARD_GRANT_KEY: %w", err)
		}
		cfg.GrantBox = gb
		cfg.DashboardURL = envOr("MEETSTREAM_DASHBOARD_URL", "https://app.meetstream.ai")
	}
	return oauth.New(cfg)
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

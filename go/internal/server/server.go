// Package server is the Streamable HTTP front end of the MeetStream MCP server.
//
// Compared with the Node server it replaces:
//   - The MCP server (tool table, schemas, validators) is built once at startup.
//     Node rebuilt an McpServer and transport for every POST /mcp.
//   - Request logs record the path only. A ?key= query string never reaches a log.
//   - Optional OAuth: clients that cannot send a header can sign in through the
//     browser instead of putting the key in the URL.
//
// Every existing way of connecting keeps working unchanged: Authorization:
// Bearer <ms_ key>, X-MeetStream-Api-Key, ?key= / ?api_key=, and the server-side
// fallback key. Status codes and JSON bodies for 401/404/405/403 match Node.
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/meetstream-ai/meetstream-mcp/go/internal/oauth"
	"github.com/meetstream-ai/meetstream-mcp/go/internal/tools"
)

// Config configures the HTTP server.
type Config struct {
	Host         string        // bind host; "0.0.0.0" and "::" disable localhost DNS-rebinding checks, as in Node
	AllowedHosts []string      // MCP_ALLOWED_HOSTS: if set, every request's Host must be listed
	FallbackKey  string        // MEETSTREAM_API_KEY: single-tenant fallback; leave empty for the public server
	OAuth        *oauth.Server // nil = OAuth off (Node behaviour)
	Logger       *slog.Logger
}

const missingKeyMessage = `Missing MeetStream API key. Send it as "Authorization: Bearer <key>", ` +
	`"X-MeetStream-Api-Key: <key>", or as a ?key=<key> query param on the URL. ` +
	`Create one at https://app.meetstream.ai/api-keys`

// New returns the HTTP handler. srv is the shared MCP server.
func New(srv *mcp.Server, cfg Config) http.Handler {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	streamable := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
		// Same rule as Node's createMcpExpressApp: only a localhost bind gets the
		// automatic loopback check. Behind nginx every request arrives from
		// 127.0.0.1 with the public Host header, which that check would reject.
		DisableLocalhostProtection: !isLocalBind(cfg.Host),
	})

	a := &authn{cfg: cfg}
	mcpHandler := a.middleware(auth.RequireBearerToken(a.verify, &auth.RequireBearerTokenOptions{
		AllowMissingExpiration: true, // raw API keys do not expire; OAuth tokens carry their own expiry
	})(streamable))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]string{"name": "meetstream-mcp", "status": "ok", "mcp_endpoint": "/mcp", "docs": "https://github.com/meetstream-ai/meetstream-mcp"})
	})
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.Handle("POST /mcp", mcpHandler)
	// Clients configured with the bare origin. A keyless POST / stays a plain 404:
	// Claude then moves on to /mcp, where a 401 can start OAuth discovery.
	mux.HandleFunc("POST /{$}", func(w http.ResponseWriter, r *http.Request) {
		if a.rawCredential(r) == "" {
			writeJSON(w, 404, map[string]string{"error": "Not found. The MCP endpoint is /mcp."})
			return
		}
		mcpHandler.ServeHTTP(w, r)
	})
	mux.HandleFunc("GET /mcp", func(w http.ResponseWriter, _ *http.Request) {
		rpcError(w, 405, -32000, "Method not allowed. This server runs stateless - use POST.")
	})
	mux.HandleFunc("DELETE /mcp", func(w http.ResponseWriter, _ *http.Request) {
		rpcError(w, 405, -32000, "Method not allowed.")
	})
	if cfg.OAuth != nil {
		mux.Handle("OPTIONS /mcp", corsMCP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })))
		cfg.OAuth.Routes(mux)
	}

	var h http.Handler = mux
	if cfg.OAuth != nil {
		h = corsFor(h)
	}
	h = hostCheck(cfg.AllowedHosts, h)
	h = recoverer(cfg.Logger, h)
	return accessLog(cfg.Logger, h)
}

func isLocalBind(host string) bool {
	switch host {
	case "127.0.0.1", "localhost", "::1", "":
		return true
	}
	return false
}

// ── authentication ────────────────────────────────────────────────────────

type authn struct{ cfg Config }

// rawCredential returns what the caller sent, in Node's precedence order:
// Bearer, X-MeetStream-Api-Key, ?key, ?api_key. Empty and unexpanded
// "${VAR}" placeholders count as nothing.
func (a *authn) rawCredential(r *http.Request) string {
	if h := r.Header.Get("Authorization"); len(h) >= 7 && strings.EqualFold(h[:7], "bearer ") {
		if v := usable(h[7:]); v != "" {
			return v
		}
	}
	if v := usable(r.Header.Get("X-MeetStream-Api-Key")); v != "" {
		return v
	}
	q := r.URL.Query()
	if v := usable(q.Get("key")); v != "" {
		return v
	}
	return usable(q.Get("api_key"))
}

func usable(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || (strings.HasPrefix(v, "${") && strings.HasSuffix(v, "}")) {
		return ""
	}
	return v
}

// middleware resolves the credential, answers 401s in Node's format, and
// rewrites the request so the SDK's bearer middleware sees a single
// Authorization header.
func (a *authn) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cred := a.rawCredential(r)
		if cred == "" {
			cred = a.cfg.FallbackKey
		}
		if cred == "" {
			a.challenge(w, "")
			rpcError(w, 401, -32001, missingKeyMessage)
			return
		}
		if strings.HasPrefix(cred, oauth.AccessPrefix) || strings.HasPrefix(cred, oauth.RefreshPrefix) {
			if a.cfg.OAuth == nil || strings.HasPrefix(cred, oauth.RefreshPrefix) {
				a.challenge(w, "invalid_token")
				rpcError(w, 401, -32001, "Invalid access token. Reconnect MeetStream in your app to sign in again.")
				return
			}
			if _, _, _, err := a.cfg.OAuth.VerifyAccessToken(cred); err != nil {
				a.challenge(w, "invalid_token")
				rpcError(w, 401, -32001, "Access token expired or invalid. Your app should refresh it automatically; if not, reconnect MeetStream.")
				return
			}
		}
		r = r.Clone(r.Context())
		r.Header.Set("Authorization", "Bearer "+cred)
		r.Header.Del("X-MeetStream-Api-Key")
		next.ServeHTTP(w, r)
	})
}

// verify turns the (already normalised) bearer value into TokenInfo carrying
// the MeetStream API key for the tool handlers.
func (a *authn) verify(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
	ti := &auth.TokenInfo{Scopes: []string{oauth.Scope}, Extra: map[string]any{}}
	if strings.HasPrefix(token, oauth.AccessPrefix) {
		if a.cfg.OAuth == nil {
			return nil, auth.ErrInvalidToken
		}
		key, exp, client, err := a.cfg.OAuth.VerifyAccessToken(token)
		if err != nil {
			return nil, auth.ErrInvalidToken
		}
		ti.Expiration = exp
		ti.Extra[tools.APIKeyExtra] = key
		ti.Extra["oauth_client"] = client
		return ti, nil
	}
	ti.Extra[tools.APIKeyExtra] = token
	return ti, nil
}

// challenge adds WWW-Authenticate so OAuth-capable clients can discover the
// sign-in flow. With OAuth off, 401s look exactly like Node's.
func (a *authn) challenge(w http.ResponseWriter, errCode string) {
	if a.cfg.OAuth == nil {
		return
	}
	v := `Bearer resource_metadata="` + a.cfg.OAuth.ResourceMetadataURL() + `", scope="` + oauth.Scope + `"`
	if errCode != "" {
		v += `, error="` + errCode + `"`
	}
	w.Header().Set("WWW-Authenticate", v)
}

// ── middleware ────────────────────────────────────────────────────────────

// hostCheck mirrors the Node SDK's hostHeaderValidation when MCP_ALLOWED_HOSTS is set.
func hostCheck(allowed []string, next http.Handler) http.Handler {
	if len(allowed) == 0 {
		return next
	}
	set := map[string]bool{}
	for _, h := range allowed {
		if h = strings.TrimSpace(h); h != "" {
			set[strings.ToLower(h)] = true
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "" {
			rpcError(w, 403, -32000, "Missing Host header")
			return
		}
		u, err := url.Parse("http://" + r.Host)
		if err != nil {
			rpcError(w, 403, -32000, "Invalid Host header: "+r.Host)
			return
		}
		hn := strings.ToLower(u.Hostname())
		if strings.Contains(hn, ":") {
			hn = "[" + hn + "]"
		}
		if !set[hn] {
			rpcError(w, 403, -32000, "Invalid Host: "+hn)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// corsFor lets browser-based MCP clients call /mcp once OAuth is on. No cookies
// are used anywhere, so a wildcard origin grants nothing extra.
func corsFor(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mcp" {
			setCORS(w)
		}
		next.ServeHTTP(w, r)
	})
}

func corsMCP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { setCORS(w); next.ServeHTTP(w, r) })
}

func setCORS(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Methods", "POST, GET, DELETE, OPTIONS")
	h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, Mcp-Protocol-Version, Mcp-Session-Id, Last-Event-ID, X-MeetStream-Api-Key")
	h.Set("Access-Control-Expose-Headers", "WWW-Authenticate, Mcp-Session-Id, Mcp-Protocol-Version")
	h.Set("Access-Control-Max-Age", "86400")
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(c int) { s.status = c; s.ResponseWriter.WriteHeader(c) }
func (s *statusWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = 200
	}
	return s.ResponseWriter.Write(b)
}
func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// accessLog records method, path (never the query string), status and timing.
func accessLog(l *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		if xr := r.Header.Get("X-Real-IP"); xr != "" {
			ip = xr
		}
		l.Info("http", "method", r.Method, "path", r.URL.Path, "status", sw.status,
			"ms", time.Since(start).Milliseconds(), "ip", ip, "ua", trim(r.UserAgent(), 120))
	})
}

func recoverer(l *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				l.Error("panic", "path", r.URL.Path, "err", v)
				rpcError(w, 500, -32603, "Internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func trim(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func rpcError(w http.ResponseWriter, status, code int, msg string) {
	writeJSON(w, status, map[string]any{
		"jsonrpc": "2.0",
		"error":   map[string]any{"code": code, "message": msg},
		"id":      nil,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

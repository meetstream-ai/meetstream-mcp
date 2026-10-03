package server

import (
	"net/http"
	"sync/atomic"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/meetstream-ai/meetstream-mcp/go/internal/tools"
)

// legacySSE serves the 2024-11-05 HTTP+SSE transport for clients that predate
// Streamable HTTP (older Cursor, n8n's MCP client, some agent frameworks):
//
//	GET  /sse                    opens the event stream; first event is "endpoint"
//	POST /sse?sessionid=<id>     client → server messages for that stream
//
// The caller authenticates once, on the GET, with any of the usual methods
// (Bearer key or OAuth token, X-MeetStream-Api-Key, ?key=). The session is
// bound to that key for its lifetime. The session ID in the endpoint URL is
// 128 bits from crypto/rand and acts as the session's credential; the key
// itself never appears in the endpoint URL.
type legacySSE struct {
	open, post http.Handler
}

func newLegacySSE(cfg Config, a *authn) *legacySSE {
	max := int64(cfg.MaxSSESessions)
	if max <= 0 {
		max = 500
	}
	var live atomic.Int64
	h := mcp.NewSSEHandler(func(r *http.Request) *mcp.Server {
		key := ""
		if ti := auth.TokenInfoFromContext(r.Context()); ti != nil {
			key, _ = ti.Extra[tools.APIKeyExtra].(string)
		}
		return cfg.SessionServer(key)
	}, &mcp.SSEOptions{DisableLocalhostProtection: !isLocalBind(cfg.Host)})

	open := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if live.Add(1) > max {
			live.Add(-1)
			rpcError(w, http.StatusServiceUnavailable, -32000, "Too many open SSE sessions. Use the Streamable HTTP endpoint /mcp instead.")
			return
		}
		defer live.Add(-1)
		// nginx must not buffer the event stream.
		w.Header().Set("X-Accel-Buffering", "no")
		h.ServeHTTP(w, r)
	})
	return &legacySSE{
		open: a.middleware(auth.RequireBearerToken(a.verify, &auth.RequireBearerTokenOptions{AllowMissingExpiration: true})(open)),
		post: h,
	}
}

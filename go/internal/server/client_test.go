package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type headerRT struct {
	h  map[string]string
	rt http.RoundTripper
}

func (t headerRT) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	for k, v := range t.h {
		r.Header.Set(k, v)
	}
	return t.rt.RoundTrip(r)
}

func clientWith(h map[string]string) *http.Client {
	return &http.Client{Transport: headerRT{h, http.DefaultTransport}}
}

// exercise connects a real MCP client, lists tools and calls one through the
// fake upstream, checking the upstream saw wantKey.
func exercise(t *testing.T, e *env, tr mcp.Transport) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	cs, err := c.Connect(ctx, tr, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer cs.Close()
	lt, err := cs.ListTools(ctx, nil)
	if err != nil || len(lt.Tools) != 19 {
		t.Fatalf("list tools: %v (%d)", err, len(lt.Tools))
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "list_bots", Arguments: map[string]any{}})
	if err != nil || res.IsError {
		t.Fatalf("call: %v %+v", err, res)
	}
	if txt := res.Content[0].(*mcp.TextContent).Text; !strings.Contains(txt, `"bot_id": "b1"`) {
		t.Fatalf("unexpected output %q", txt)
	}
	if e.up.lastKey() != goodKey {
		t.Fatalf("upstream saw %q", e.up.lastKey())
	}
	// A second call on the same session keeps working (SSE stream stays open).
	if res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "webhook_events_guide"}); err != nil || res.IsError {
		t.Fatalf("second call: %v", err)
	}
}

func TestRealClientStreamableHTTP(t *testing.T) {
	e := newEnv(t, false, "")
	exercise(t, e, &mcp.StreamableClientTransport{Endpoint: e.srv.URL + "/mcp", HTTPClient: clientWith(map[string]string{"Authorization": "Bearer " + goodKey})})
	exercise(t, e, &mcp.StreamableClientTransport{Endpoint: e.srv.URL + "/mcp?key=" + goodKey})
}

func TestRealClientLegacySSE(t *testing.T) {
	e := newEnv(t, true, "")
	t.Run("bearer", func(t *testing.T) {
		exercise(t, e, &mcp.SSEClientTransport{Endpoint: e.srv.URL + "/sse", HTTPClient: clientWith(map[string]string{"Authorization": "Bearer " + goodKey})})
	})
	t.Run("header", func(t *testing.T) {
		exercise(t, e, &mcp.SSEClientTransport{Endpoint: e.srv.URL + "/sse", HTTPClient: clientWith(map[string]string{"X-MeetStream-Api-Key": goodKey})})
	})
	t.Run("query key", func(t *testing.T) {
		exercise(t, e, &mcp.SSEClientTransport{Endpoint: e.srv.URL + "/sse?key=" + goodKey})
	})
}

func TestLegacySSERequiresKey(t *testing.T) {
	e := newEnv(t, true, "")
	res, err := http.Get(e.srv.URL + "/sse")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 || !strings.Contains(res.Header.Get("WWW-Authenticate"), "resource_metadata") {
		t.Fatalf("GET /sse without key → %d", res.StatusCode)
	}
	// Posting to a made-up session is rejected.
	pr, _ := http.Post(e.srv.URL+"/sse?sessionid=nope", "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	if pr.StatusCode != 404 {
		t.Fatalf("unknown session → %d", pr.StatusCode)
	}
}

func TestSSEEndpointDoesNotLeakKey(t *testing.T) {
	e := newEnv(t, false, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", e.srv.URL+"/sse?key="+goodKey, nil)
	req.Header.Set("Accept", "text/event-stream")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	buf := make([]byte, 512)
	n, _ := res.Body.Read(buf)
	first := string(buf[:n])
	if !strings.Contains(first, "event: endpoint") || !strings.Contains(first, "sessionid=") || strings.Contains(first, goodKey) {
		t.Fatalf("endpoint event: %q", first)
	}
}

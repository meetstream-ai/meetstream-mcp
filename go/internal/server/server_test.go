package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/meetstream-ai/meetstream-mcp/go/internal/oauth"
	"github.com/meetstream-ai/meetstream-mcp/go/internal/seal"
	"github.com/meetstream-ai/meetstream-mcp/go/internal/tools"
)

const goodKey = "ms_goodkey_for_tests_only"

// upstream is a fake MeetStream API that records which key each call used.
type upstream struct {
	*httptest.Server
	mu   sync.Mutex
	keys []string
}

func newUpstream(t *testing.T) *upstream {
	u := &upstream{}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Token ")
		u.mu.Lock()
		u.keys = append(u.keys, key)
		u.mu.Unlock()
		if key != goodKey {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"detail":"Invalid API key"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"bots":[{"bot_id":"b1"}]}`)
	}))
	t.Cleanup(u.Close)
	return u
}

func (u *upstream) lastKey() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.keys) == 0 {
		return ""
	}
	return u.keys[len(u.keys)-1]
}

type env struct {
	srv *httptest.Server
	up  *upstream
	oa  *oauth.Server
}

func newEnv(t *testing.T, withOAuth bool, fallback string) *env {
	t.Helper()
	up := newUpstream(t)
	m := mcp.NewServer(&mcp.Implementation{Name: "meetstream", Version: "test"}, nil)
	tools.Register(m, tools.Config{BaseURL: up.URL, FallbackKey: fallback})
	e := &env{up: up}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := Config{Host: "0.0.0.0", FallbackKey: fallback, Logger: logger}
	// The public URL must be known before the listener exists, so start unstarted.
	e.srv = httptest.NewUnstartedServer(nil)
	e.srv.Start()
	if withOAuth {
		box, _ := seal.FromString(base64.StdEncoding.EncodeToString(make([]byte, 32)))
		oa, err := oauth.New(oauth.Config{
			PublicURL: e.srv.URL, Mode: oauth.ModePaste, Box: box, Logger: logger,
			CheckKey: func(_ context.Context, k string) error {
				if k != goodKey {
					return oauth.ErrBadKey
				}
				return nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		e.oa = oa
		cfg.OAuth = oa
	}
	e.srv.Config.Handler = New(m, cfg)
	t.Cleanup(e.srv.Close)
	return e
}

func rpc(t *testing.T, base, path string, hdr map[string]string, body string) (*http.Response, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", base+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return res, m
}

const initBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`
const listBody = `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
const callBody = `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_bots","arguments":{}}}`

func TestInfoAndHealth(t *testing.T) {
	e := newEnv(t, false, "")
	for path, want := range map[string]string{"/": `"mcp_endpoint":"/mcp"`, "/health": `{"status":"ok"}`} {
		res, _ := http.Get(e.srv.URL + path)
		b, _ := io.ReadAll(res.Body)
		if res.StatusCode != 200 || !strings.Contains(string(b), want) {
			t.Fatalf("%s: %d %s", path, res.StatusCode, b)
		}
	}
}

func TestMissingKeyMatchesNode(t *testing.T) {
	e := newEnv(t, false, "")
	res, body := rpc(t, e.srv.URL, "/mcp", nil, initBody)
	if res.StatusCode != 401 {
		t.Fatalf("status %d", res.StatusCode)
	}
	if res.Header.Get("WWW-Authenticate") != "" {
		t.Fatal("OAuth off must not advertise WWW-Authenticate")
	}
	errObj := body["error"].(map[string]any)
	if errObj["code"].(float64) != -32001 || !strings.Contains(errObj["message"].(string), "Authorization: Bearer") {
		t.Fatalf("body %v", body)
	}
	// keyless POST / stays 404
	res, body = rpc(t, e.srv.URL, "/", nil, initBody)
	if res.StatusCode != 404 || body["error"] != "Not found. The MCP endpoint is /mcp." {
		t.Fatalf("POST / → %d %v", res.StatusCode, body)
	}
}

func TestLegacyKeyTransports(t *testing.T) {
	e := newEnv(t, false, "")
	cases := []struct {
		name, path string
		hdr        map[string]string
	}{
		{"bearer", "/mcp", map[string]string{"Authorization": "Bearer " + goodKey}},
		{"header", "/mcp", map[string]string{"X-MeetStream-Api-Key": goodKey}},
		{"query key", "/mcp?key=" + goodKey, nil},
		{"query api_key", "/mcp?api_key=" + goodKey, nil},
		{"bare origin", "/?key=" + goodKey, nil},
		{"empty bearer falls through", "/mcp?key=" + goodKey, map[string]string{"Authorization": "Bearer ${MEETSTREAM_API_KEY}"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, body := rpc(t, e.srv.URL, c.path, c.hdr, initBody)
			if res.StatusCode != 200 || body["result"].(map[string]any)["serverInfo"].(map[string]any)["name"] != "meetstream" {
				t.Fatalf("init %d %v", res.StatusCode, body)
			}
			res, body = rpc(t, e.srv.URL, c.path, c.hdr, callBody)
			if res.StatusCode != 200 || body["result"].(map[string]any)["isError"] == true {
				t.Fatalf("call %d %v", res.StatusCode, body)
			}
			if e.up.lastKey() != goodKey {
				t.Fatalf("upstream saw %q", e.up.lastKey())
			}
		})
	}
}

func TestFallbackKey(t *testing.T) {
	e := newEnv(t, false, goodKey)
	res, body := rpc(t, e.srv.URL, "/mcp", nil, callBody)
	if res.StatusCode != 200 || body["result"].(map[string]any)["isError"] == true || e.up.lastKey() != goodKey {
		t.Fatalf("%d %v", res.StatusCode, body)
	}
}

func TestPerRequestKeysAreIsolated(t *testing.T) {
	e := newEnv(t, false, "")
	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := goodKey
			if i%2 == 1 {
				key = "ms_wrong"
			}
			_, body := rpc(t, e.srv.URL, "/mcp", map[string]string{"Authorization": "Bearer " + key}, callBody)
			isErr := body["result"].(map[string]any)["isError"] == true
			if isErr != (key != goodKey) {
				errs <- errors.New("key leaked between concurrent requests")
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func TestToolsList(t *testing.T) {
	e := newEnv(t, false, "")
	_, body := rpc(t, e.srv.URL, "/mcp", map[string]string{"Authorization": "Bearer x"}, listBody)
	ts := body["result"].(map[string]any)["tools"].([]any)
	if len(ts) != 19 {
		t.Fatalf("got %d tools", len(ts))
	}
}

func TestMethodNotAllowed(t *testing.T) {
	e := newEnv(t, false, "")
	res, _ := http.Get(e.srv.URL + "/mcp")
	if res.StatusCode != 405 {
		t.Fatalf("GET /mcp %d", res.StatusCode)
	}
	req, _ := http.NewRequest("DELETE", e.srv.URL+"/mcp", nil)
	res, _ = http.DefaultClient.Do(req)
	if res.StatusCode != 405 {
		t.Fatalf("DELETE /mcp %d", res.StatusCode)
	}
}

func TestAllowedHosts(t *testing.T) {
	up := newUpstream(t)
	m := mcp.NewServer(&mcp.Implementation{Name: "meetstream", Version: "t"}, nil)
	tools.Register(m, tools.Config{BaseURL: up.URL})
	h := New(m, Config{Host: "0.0.0.0", AllowedHosts: []string{"mcp.meetstream.ai"}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "http://evil.example/health", nil))
	if rr.Code != 403 || !strings.Contains(rr.Body.String(), "Invalid Host: evil.example") {
		t.Fatalf("%d %s", rr.Code, rr.Body)
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "http://mcp.meetstream.ai/health", nil))
	if rr.Code != 200 {
		t.Fatalf("%d", rr.Code)
	}
}

func TestAccessLogOmitsQuery(t *testing.T) {
	var buf strings.Builder
	var mu sync.Mutex
	w := writerFunc(func(p []byte) (int, error) { mu.Lock(); defer mu.Unlock(); return buf.Write(p) })
	m := mcp.NewServer(&mcp.Implementation{Name: "meetstream", Version: "t"}, nil)
	tools.Register(m, tools.Config{BaseURL: "http://127.0.0.1:1"})
	h := New(m, Config{Host: "0.0.0.0", Logger: slog.New(slog.NewTextHandler(w, nil))})
	req := httptest.NewRequest("POST", "/mcp?key=ms_secret_in_url", strings.NewReader(initBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	h.ServeHTTP(httptest.NewRecorder(), req)
	mu.Lock()
	defer mu.Unlock()
	if strings.Contains(buf.String(), "ms_secret_in_url") || !strings.Contains(buf.String(), "path=/mcp") {
		t.Fatalf("log: %s", buf.String())
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// ── OAuth end to end ──────────────────────────────────────────────────────

func noRedirect() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func pkce() (verifier, challenge string) {
	verifier = strings.Repeat("v", 20) + "0123456789abcdefghijklmnopqrstuvwxyz"
	s := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(s[:])
}

func postForm(t *testing.T, u string, v url.Values) (int, map[string]any) {
	t.Helper()
	res, err := http.PostForm(u, v)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(res.Body).Decode(&m)
	return res.StatusCode, m
}

func TestOAuthFlow(t *testing.T) {
	e := newEnv(t, true, "")
	base := e.srv.URL
	const redirect = "http://127.0.0.1:33418/callback"

	// 1. Unauthenticated call → 401 with discovery pointer (legacy body kept).
	res, body := rpc(t, base, "/mcp", nil, initBody)
	wa := res.Header.Get("WWW-Authenticate")
	if res.StatusCode != 401 || !strings.Contains(wa, `resource_metadata="`+base+`/.well-known/oauth-protected-resource"`) {
		t.Fatalf("401 challenge: %d %q", res.StatusCode, wa)
	}
	if body["error"].(map[string]any)["code"].(float64) != -32001 {
		t.Fatal("legacy 401 body changed")
	}

	// 2. Discovery.
	var prm, asm map[string]any
	r1, _ := http.Get(base + "/.well-known/oauth-protected-resource/mcp")
	_ = json.NewDecoder(r1.Body).Decode(&prm)
	if prm["resource"] != base+"/mcp" {
		t.Fatalf("prm %v", prm)
	}
	r2, _ := http.Get(base + "/.well-known/oauth-authorization-server")
	_ = json.NewDecoder(r2.Body).Decode(&asm)
	if asm["registration_endpoint"] != base+"/oauth/register" {
		t.Fatalf("asm %v", asm)
	}

	// 3. Dynamic client registration.
	rr, _ := http.Post(base+"/oauth/register", "application/json", strings.NewReader(`{"client_name":"Test Client","redirect_uris":["`+redirect+`"]}`))
	var reg map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&reg)
	clientID, _ := reg["client_id"].(string)
	if rr.StatusCode != 201 || !strings.HasPrefix(clientID, "msc_") {
		t.Fatalf("register %d %v", rr.StatusCode, reg)
	}
	bad, _ := http.Post(base+"/oauth/register", "application/json", strings.NewReader(`{"redirect_uris":["javascript:alert(1)"]}`))
	if bad.StatusCode != 400 {
		t.Fatalf("javascript: redirect accepted")
	}

	// 4. Authorize → paste page.
	verifier, challenge := pkce()
	q := url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {"http://127.0.0.1:51000/callback"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "state": {"st8"}, "resource": {base + "/mcp"}}
	ar, _ := noRedirect().Get(base + "/oauth/authorize?" + q.Encode())
	page, _ := io.ReadAll(ar.Body)
	if ar.StatusCode != 200 || !strings.Contains(string(page), "Test Client") || ar.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatalf("authorize %d", ar.StatusCode)
	}
	reqTok := between(string(page), `name="request" value="`, `"`)

	// 5a. Wrong key re-renders the form.
	sr, _ := noRedirect().PostForm(base+"/oauth/authorize", url.Values{"request": {reqTok}, "api_key": {"ms_wrong"}, "action": {"allow"}})
	if sr.StatusCode != 400 {
		t.Fatalf("bad key → %d", sr.StatusCode)
	}
	// 5b. Good key redirects with a code (loopback port differs from registration: allowed).
	sr, _ = noRedirect().PostForm(base+"/oauth/authorize", url.Values{"request": {reqTok}, "api_key": {goodKey}, "action": {"allow"}})
	loc, _ := url.Parse(sr.Header.Get("Location"))
	code := loc.Query().Get("code")
	if sr.StatusCode != 302 || loc.Host != "127.0.0.1:51000" || loc.Query().Get("state") != "st8" || code == "" {
		t.Fatalf("approve %d %s", sr.StatusCode, loc)
	}
	if strings.Contains(sr.Header.Get("Location"), goodKey) {
		t.Fatal("API key visible in redirect")
	}

	// 6. Token exchange: wrong verifier fails, right one succeeds, reuse fails.
	tokURL := base + "/oauth/token"
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID}, "redirect_uri": {"http://127.0.0.1:51000/callback"}}
	form.Set("code_verifier", strings.Repeat("x", 50))
	if st, m := postForm(t, tokURL, form); st != 400 || m["error"] != "invalid_grant" {
		t.Fatalf("bad verifier %d %v", st, m)
	}
	form.Set("code_verifier", verifier)
	st, tok := postForm(t, tokURL, form)
	at, _ := tok["access_token"].(string)
	rt, _ := tok["refresh_token"].(string)
	if st != 200 || !strings.HasPrefix(at, "mso_at_") || !strings.HasPrefix(rt, "mso_rt_") {
		t.Fatalf("token %d %v", st, tok)
	}
	if strings.Contains(at, goodKey) {
		t.Fatal("API key visible in token")
	}
	if st, _ := postForm(t, tokURL, form); st != 400 {
		t.Fatal("code reuse accepted")
	}

	// 7. Access token works for tool calls and the upstream sees the real key.
	_, body = rpc(t, base, "/mcp", map[string]string{"Authorization": "Bearer " + at}, callBody)
	if body["result"].(map[string]any)["isError"] == true || e.up.lastKey() != goodKey {
		t.Fatalf("call with OAuth token: %v (upstream key %q)", body, e.up.lastKey())
	}

	// 8. Tampered or refresh token at /mcp → 401 invalid_token.
	res, _ = rpc(t, base, "/mcp", map[string]string{"Authorization": "Bearer " + at[:len(at)-3] + "AAA"}, callBody)
	if res.StatusCode != 401 || !strings.Contains(res.Header.Get("WWW-Authenticate"), `error="invalid_token"`) {
		t.Fatalf("tampered → %d %q", res.StatusCode, res.Header.Get("WWW-Authenticate"))
	}
	res, _ = rpc(t, base, "/mcp", map[string]string{"Authorization": "Bearer " + rt}, callBody)
	if res.StatusCode != 401 {
		t.Fatal("refresh token accepted as access token")
	}

	// 9. Refresh rotates and the new token works; another client cannot use it.
	st, tok2 := postForm(t, tokURL, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {rt}, "client_id": {clientID}})
	if st != 200 || tok2["access_token"] == at {
		t.Fatalf("refresh %d %v", st, tok2)
	}
	_, body = rpc(t, base, "/mcp", map[string]string{"Authorization": "Bearer " + tok2["access_token"].(string)}, callBody)
	if body["result"].(map[string]any)["isError"] == true {
		t.Fatal("refreshed token rejected")
	}
	rr, _ = http.Post(base+"/oauth/register", "application/json", strings.NewReader(`{"redirect_uris":["https://other.example/cb"]}`))
	var other map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&other)
	if st, _ := postForm(t, tokURL, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {rt}, "client_id": {other["client_id"].(string)}}); st != 400 {
		t.Fatal("refresh token usable by another client")
	}

	// 10. Legacy keys still work with OAuth on.
	_, body = rpc(t, base, "/mcp?key="+goodKey, nil, callBody)
	if body["result"].(map[string]any)["isError"] == true {
		t.Fatal("legacy ?key= broke with OAuth on")
	}
}

func TestAuthorizeRejectsUnregisteredRedirect(t *testing.T) {
	e := newEnv(t, true, "")
	rr, _ := http.Post(e.srv.URL+"/oauth/register", "application/json", strings.NewReader(`{"redirect_uris":["https://claude.ai/api/mcp/auth_callback"]}`))
	var reg map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&reg)
	_, challenge := pkce()
	q := url.Values{"response_type": {"code"}, "client_id": {reg["client_id"].(string)}, "redirect_uri": {"https://evil.example/cb"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}}
	ar, _ := noRedirect().Get(e.srv.URL + "/oauth/authorize?" + q.Encode())
	if ar.StatusCode != 400 || ar.Header.Get("Location") != "" {
		t.Fatalf("unregistered redirect → %d %s", ar.StatusCode, ar.Header.Get("Location"))
	}
	// Missing PKCE is reported back to the registered redirect.
	q.Set("redirect_uri", "https://claude.ai/api/mcp/auth_callback")
	q.Del("code_challenge")
	ar, _ = noRedirect().Get(e.srv.URL + "/oauth/authorize?" + q.Encode())
	if ar.StatusCode != 302 || !strings.Contains(ar.Header.Get("Location"), "error=invalid_request") {
		t.Fatalf("no PKCE → %d %s", ar.StatusCode, ar.Header.Get("Location"))
	}
}

func between(s, a, b string) string {
	i := strings.Index(s, a)
	if i < 0 {
		return ""
	}
	s = s[i+len(a):]
	return s[:strings.Index(s, b)]
}

var _ = time.Second

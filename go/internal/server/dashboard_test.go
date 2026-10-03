package server

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/meetstream-ai/meetstream-mcp/go/internal/oauth"
	"github.com/meetstream-ai/meetstream-mcp/go/internal/seal"
	"github.com/meetstream-ai/meetstream-mcp/go/internal/tools"
)

// TestDashboardModeFlow plays the dashboard's part (open the consent context,
// mint a key, seal a grant, auto-POST it) and checks the whole OAuth round trip.
func TestDashboardModeFlow(t *testing.T) {
	up := newUpstream(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := mcp.NewServer(&mcp.Implementation{Name: "meetstream", Version: "t"}, nil)
	tools.Register(m, tools.Config{BaseURL: up.URL})
	srv := httptest.NewServer(nil)
	t.Cleanup(srv.Close)
	box, _ := seal.FromString(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	shared, _ := seal.FromString(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	oa, err := oauth.New(oauth.Config{PublicURL: srv.URL, Mode: oauth.ModeDashboard, Box: box, GrantBox: shared, DashboardURL: "https://app.example", Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	srv.Config.Handler = New(m, Config{Host: "0.0.0.0", OAuth: oa, Logger: logger})

	const redirect = "https://claude.ai/api/mcp/auth_callback"
	rr, _ := http.Post(srv.URL+"/oauth/register", "application/json", strings.NewReader(`{"client_name":"Claude","redirect_uris":["`+redirect+`"]}`))
	var reg map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&reg)
	clientID := reg["client_id"].(string)
	verifier, challenge := pkce()

	authorize := func() (request, ctx string) {
		q := url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirect},
			"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "state": {"s1"}}
		ar, _ := noRedirect().Get(srv.URL + "/oauth/authorize?" + q.Encode())
		loc, _ := url.Parse(ar.Header.Get("Location"))
		if ar.StatusCode != 302 || loc.Host != "app.example" || loc.Path != "/oauth/mcp/authorize" {
			t.Fatalf("authorize → %d %s", ar.StatusCode, loc)
		}
		if loc.Query().Has("client_name") || loc.Query().Has("callback") {
			t.Fatal("display data must only travel sealed")
		}
		return loc.Query().Get("request"), loc.Query().Get("ctx")
	}
	request, ctxTok := authorize()

	// Dashboard: open the consent context and check it belongs to this request.
	var cc struct {
		RequestHash  string `json:"request_hash"`
		ClientName   string `json:"client_name"`
		RedirectHost string `json:"redirect_host"`
		Exp          int64  `json:"exp"`
	}
	if err := shared.Open(oauth.ConsentPurpose, ctxTok, &cc); err != nil || cc.ClientName != "Claude" || cc.RedirectHost != "claude.ai" || cc.RequestHash != seal.Hash(request) {
		t.Fatalf("consent ctx %v %+v", err, cc)
	}
	grant := func(req string, key string, exp int64) string {
		g, _ := shared.Seal(oauth.GrantPurpose, map[string]any{"v": 1, "request_hash": seal.Hash(req), "user_id": "u1",
			"api_key": key, "api_key_name": "MCP · Claude", "iat": time.Now().Unix(), "exp": exp, "jti": randJTI()})
		return g
	}
	callback := func(form url.Values) *url.URL {
		res, _ := noRedirect().PostForm(srv.URL+"/oauth/callback", form)
		loc, _ := url.Parse(res.Header.Get("Location"))
		return loc
	}

	// A grant minted for a different request is refused.
	other, _ := authorize()
	if loc := callback(url.Values{"request": {request}, "grant": {grant(other, goodKey, time.Now().Add(time.Minute).Unix())}}); loc.Query().Get("error") != "server_error" {
		t.Fatalf("mismatched grant → %s", loc)
	}
	// Denial goes back to the client as access_denied.
	if loc := callback(url.Values{"request": {request}, "error": {"access_denied"}}); loc.Query().Get("error") != "access_denied" || loc.Query().Get("state") != "s1" {
		t.Fatalf("deny → %s", loc)
	}
	// Approval.
	g := grant(request, goodKey, time.Now().Add(time.Minute).Unix())
	loc := callback(url.Values{"request": {request}, "grant": {g}})
	code := loc.Query().Get("code")
	if loc.Host != "claude.ai" || code == "" || loc.Query().Get("state") != "s1" {
		t.Fatalf("approve → %s", loc)
	}
	// The same grant cannot be replayed.
	if loc := callback(url.Values{"request": {request}, "grant": {g}}); loc.Query().Get("code") != "" {
		t.Fatal("grant replay accepted")
	}

	st, tok := postForm(t, srv.URL+"/oauth/token", url.Values{"grant_type": {"authorization_code"}, "code": {code},
		"client_id": {clientID}, "redirect_uri": {redirect}, "code_verifier": {verifier}})
	if st != 200 {
		t.Fatalf("token %d %v", st, tok)
	}
	_, body := rpc(t, srv.URL, "/mcp", map[string]string{"Authorization": "Bearer " + tok["access_token"].(string)}, callBody)
	if body["result"].(map[string]any)["isError"] == true || up.lastKey() != goodKey {
		t.Fatalf("tool call with dashboard-issued token: %v", body)
	}
}

func randJTI() string { return base64.RawURLEncoding.EncodeToString([]byte(time.Now().String())) }

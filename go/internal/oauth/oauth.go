// Package oauth makes the MCP server its own OAuth 2.1 authorization server, per
// the MCP authorization spec: protected resource metadata (RFC 9728),
// authorization server metadata (RFC 8414), dynamic client registration
// (RFC 7591), authorization code + PKCE S256, and rotating refresh tokens.
//
// Every artifact (client ID, pending request, code, token) is a sealed value, so
// the server keeps no database. An access token carries the user's MeetStream
// API key encrypted inside it; the server decrypts it per request and calls the
// API with it, exactly as if the key had been sent directly.
//
// How the user proves who they are is pluggable:
//   - ModePaste: a page on this server asks for an API key and validates it.
//     Works today with no dashboard or API changes.
//   - ModeDashboard: the user is sent to the MeetStream dashboard, signs in,
//     approves, and the dashboard mints a key and hands it back sealed under a
//     shared secret (see docs/oauth-dashboard-contract.md).
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/meetstream-ai/meetstream-mcp/go/internal/seal"
)

// Modes for the sign-in step.
const (
	ModePaste     = "paste"
	ModeDashboard = "dashboard"
)

// Token prefixes. Legacy MeetStream API keys start with "ms_", so an OAuth
// access token is always distinguishable from a raw key.
const (
	AccessPrefix  = "mso_at_"
	RefreshPrefix = "mso_rt_"
	codePrefix    = "mso_c_"
	clientPrefix  = "msc_"
	requestPrefix = "msr_"

	purposeClient  = "msmcp:client:v1"
	purposeRequest = "msmcp:request:v1"
	purposeCode    = "msmcp:code:v1"
	purposeAccess  = "msmcp:access:v1"
	purposeRefresh = "msmcp:refresh:v1"
	purposeSecret  = "msmcp:client-secret:v1"
	// GrantPurpose is the AAD the dashboard must use when sealing a grant.
	GrantPurpose = "meetstream-mcp-grant-v1"

	Scope = "meetstream"
)

// KeyChecker validates a MeetStream API key (paste mode). It returns
// ErrBadKey when the API rejects the key.
type KeyChecker func(ctx context.Context, apiKey string) error

// ErrBadKey means the API rejected the key.
var ErrBadKey = errors.New("api key rejected")

// Config configures the authorization server.
type Config struct {
	PublicURL    string     // e.g. https://mcp.meetstream.ai (no trailing slash)
	Mode         string     // ModePaste or ModeDashboard
	Box          *seal.Box  // seals codes, tokens, clients
	GrantBox     *seal.Box  // dashboard mode: opens grants sealed by the dashboard
	DashboardURL string     // dashboard mode: e.g. https://app.meetstream.ai
	CheckKey     KeyChecker // paste mode
	APIKeysURL   string     // where users create keys (shown on the paste page)
	AccessTTL    time.Duration
	RefreshTTL   time.Duration
	Logger       *slog.Logger
}

// Server is the authorization server.
type Server struct {
	cfg  Config
	used *usedSet
}

// New validates cfg and returns a Server.
func New(cfg Config) (*Server, error) {
	cfg.PublicURL = strings.TrimRight(cfg.PublicURL, "/")
	if cfg.PublicURL == "" || cfg.Box == nil {
		return nil, errors.New("oauth: PublicURL and Box are required")
	}
	switch cfg.Mode {
	case ModePaste:
		if cfg.CheckKey == nil {
			return nil, errors.New("oauth: paste mode needs CheckKey")
		}
	case ModeDashboard:
		if cfg.GrantBox == nil || cfg.DashboardURL == "" {
			return nil, errors.New("oauth: dashboard mode needs GrantBox and DashboardURL")
		}
	default:
		return nil, fmt.Errorf("oauth: unknown mode %q", cfg.Mode)
	}
	if cfg.AccessTTL == 0 {
		cfg.AccessTTL = time.Hour
	}
	if cfg.RefreshTTL == 0 {
		cfg.RefreshTTL = 90 * 24 * time.Hour
	}
	if cfg.APIKeysURL == "" {
		cfg.APIKeysURL = "https://app.meetstream.ai/api-key"
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Server{cfg: cfg, used: newUsedSet()}, nil
}

// Resource is the protected resource identifier (the MCP endpoint).
func (s *Server) Resource() string { return s.cfg.PublicURL + "/mcp" }

// ResourceMetadataURL is advertised in WWW-Authenticate on 401.
func (s *Server) ResourceMetadataURL() string {
	return s.cfg.PublicURL + "/.well-known/oauth-protected-resource"
}

// Routes registers the OAuth endpoints.
func (s *Server) Routes(mux *http.ServeMux) {
	prm := cors(http.HandlerFunc(s.protectedResourceMetadata))
	asm := cors(http.HandlerFunc(s.authServerMetadata))
	for _, p := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp"} {
		mux.Handle("GET "+p, prm)
		mux.Handle("OPTIONS "+p, prm)
	}
	for _, p := range []string{"/.well-known/oauth-authorization-server", "/.well-known/oauth-authorization-server/mcp", "/.well-known/openid-configuration"} {
		mux.Handle("GET "+p, asm)
		mux.Handle("OPTIONS "+p, asm)
	}
	mux.Handle("POST /oauth/register", cors(http.HandlerFunc(s.register)))
	mux.Handle("OPTIONS /oauth/register", cors(http.HandlerFunc(s.register)))
	mux.HandleFunc("GET /oauth/authorize", s.authorize)
	mux.HandleFunc("POST /oauth/authorize", s.authorizeSubmit)
	mux.HandleFunc("GET /oauth/callback", s.callback)
	mux.HandleFunc("POST /oauth/callback", s.callback)
	mux.Handle("POST /oauth/token", cors(http.HandlerFunc(s.token)))
	mux.Handle("OPTIONS /oauth/token", cors(http.HandlerFunc(s.token)))
	mux.Handle("POST /oauth/revoke", cors(http.HandlerFunc(s.revoke)))
	mux.Handle("OPTIONS /oauth/revoke", cors(http.HandlerFunc(s.revoke)))
}

// ── Discovery ─────────────────────────────────────────────────────────────

func (s *Server) protectedResourceMetadata(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{
		"resource":                 s.Resource(),
		"authorization_servers":    []string{s.cfg.PublicURL},
		"scopes_supported":         []string{Scope},
		"bearer_methods_supported": []string{"header"},
		"resource_name":            "MeetStream",
		"resource_documentation":   "https://docs.meetstream.ai/build-with-ai/meetstream-mcp-server",
	})
}

func (s *Server) authServerMetadata(w http.ResponseWriter, _ *http.Request) {
	u := s.cfg.PublicURL
	writeJSON(w, 200, map[string]any{
		"issuer":                                         u,
		"authorization_endpoint":                         u + "/oauth/authorize",
		"token_endpoint":                                 u + "/oauth/token",
		"registration_endpoint":                          u + "/oauth/register",
		"revocation_endpoint":                            u + "/oauth/revoke",
		"response_types_supported":                       []string{"code"},
		"response_modes_supported":                       []string{"query"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          []string{"none", "client_secret_post", "client_secret_basic"},
		"revocation_endpoint_auth_methods_supported":     []string{"none", "client_secret_post", "client_secret_basic"},
		"scopes_supported":                               []string{Scope},
		"authorization_response_iss_parameter_supported": true,
		"service_documentation":                          "https://docs.meetstream.ai/build-with-ai/meetstream-mcp-server",
	})
}

// ── Dynamic client registration (RFC 7591) ───────────────────────────────

type clientReg struct {
	Name      string   `json:"n,omitempty"`
	Redirects []string `json:"r"`
	Auth      string   `json:"a"` // none | client_secret_post | client_secret_basic
	Iat       int64    `json:"i"`
}

var schemeRe = regexp.MustCompile(`^[a-z][a-z0-9+.\-]*$`)

func validRedirect(raw string) error {
	if len(raw) > 2000 {
		return errors.New("redirect_uri too long")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return fmt.Errorf("redirect_uri %q is not an absolute URI", raw)
	}
	if u.Fragment != "" {
		return fmt.Errorf("redirect_uri %q must not contain a fragment", raw)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		if u.Host == "" {
			return fmt.Errorf("redirect_uri %q has no host", raw)
		}
	case "http":
		if !isLoopback(u.Hostname()) {
			return fmt.Errorf("redirect_uri %q: http is only allowed for localhost", raw)
		}
	case "javascript", "data", "file", "vbscript", "blob", "about":
		return fmt.Errorf("redirect_uri scheme %q is not allowed", u.Scheme)
	default:
		if !schemeRe.MatchString(strings.ToLower(u.Scheme)) {
			return fmt.Errorf("redirect_uri %q has an invalid scheme", raw)
		}
	}
	return nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var req struct {
		RedirectURIs            []string `json:"redirect_uris"`
		ClientName              string   `json:"client_name"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
		GrantTypes              []string `json:"grant_types"`
		ResponseTypes           []string `json:"response_types"`
		Scope                   string   `json:"scope"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		oauthError(w, 400, "invalid_client_metadata", "body must be a JSON object")
		return
	}
	if len(req.RedirectURIs) == 0 || len(req.RedirectURIs) > 10 {
		oauthError(w, 400, "invalid_redirect_uri", "provide between 1 and 10 redirect_uris")
		return
	}
	for _, ru := range req.RedirectURIs {
		if err := validRedirect(ru); err != nil {
			oauthError(w, 400, "invalid_redirect_uri", err.Error())
			return
		}
	}
	for _, g := range req.GrantTypes {
		if g != "authorization_code" && g != "refresh_token" {
			oauthError(w, 400, "invalid_client_metadata", "unsupported grant_type "+g)
			return
		}
	}
	auth := req.TokenEndpointAuthMethod
	switch auth {
	case "":
		auth = "none"
	case "none", "client_secret_post", "client_secret_basic":
	default:
		oauthError(w, 400, "invalid_client_metadata", "unsupported token_endpoint_auth_method "+auth)
		return
	}
	name := strings.TrimSpace(req.ClientName)
	if len([]rune(name)) > 100 {
		name = string([]rune(name)[:100])
	}
	now := time.Now().Unix()
	sealed, err := s.cfg.Box.Seal(purposeClient, clientReg{Name: name, Redirects: req.RedirectURIs, Auth: auth, Iat: now})
	if err != nil {
		oauthError(w, 500, "server_error", "could not register client")
		return
	}
	clientID := clientPrefix + sealed
	resp := map[string]any{
		"client_id":                  clientID,
		"client_id_issued_at":        now,
		"redirect_uris":              req.RedirectURIs,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": auth,
		"scope":                      Scope,
	}
	if name != "" {
		resp["client_name"] = name
	}
	if auth != "none" {
		resp["client_secret"] = s.cfg.Box.MAC(purposeSecret, clientID)
		resp["client_secret_expires_at"] = 0
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) openClient(clientID string) (*clientReg, error) {
	if !strings.HasPrefix(clientID, clientPrefix) {
		return nil, seal.ErrInvalid
	}
	var c clientReg
	if err := s.cfg.Box.Open(purposeClient, strings.TrimPrefix(clientID, clientPrefix), &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// matchRedirect returns the registered URI that requested matches. Loopback
// http URIs match on any port (RFC 8252 §7.3), everything else exactly.
func matchRedirect(c *clientReg, requested string) (string, bool) {
	if requested == "" {
		if len(c.Redirects) == 1 {
			return c.Redirects[0], true
		}
		return "", false
	}
	for _, reg := range c.Redirects {
		if reg == requested {
			return requested, true
		}
		ru, err1 := url.Parse(reg)
		qu, err2 := url.Parse(requested)
		if err1 == nil && err2 == nil && ru.Scheme == "http" && qu.Scheme == "http" &&
			isLoopback(ru.Hostname()) && ru.Hostname() == qu.Hostname() &&
			ru.Path == qu.Path && ru.RawQuery == qu.RawQuery {
			return requested, true
		}
	}
	return "", false
}

// ── Authorization ─────────────────────────────────────────────────────────

// authReq is the pending authorization request, sealed and carried through
// the sign-in step.
type authReq struct {
	ClientID  string `json:"c"`
	Redirect  string `json:"r"`
	Challenge string `json:"cc"`
	State     string `json:"s,omitempty"`
	Scope     string `json:"sc,omitempty"`
	Name      string `json:"n,omitempty"`
	Exp       int64  `json:"e"`
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	c, err := s.openClient(q.Get("client_id"))
	if err != nil {
		s.page(w, 400, pageData{Title: "Unknown application", Error: "This sign-in link has an unknown or expired client ID. Remove the MeetStream connector in your app and add it again."})
		return
	}
	redirect, ok := matchRedirect(c, q.Get("redirect_uri"))
	if !ok {
		s.page(w, 400, pageData{Title: "Redirect not allowed", Error: "The app asked to return to an address it did not register. Remove the MeetStream connector in your app and add it again."})
		return
	}
	fail := func(code, desc string) { redirectError(w, r, redirect, code, desc, q.Get("state"), s.cfg.PublicURL) }
	if q.Get("response_type") != "code" {
		fail("unsupported_response_type", "only response_type=code is supported")
		return
	}
	challenge := q.Get("code_challenge")
	if challenge == "" || q.Get("code_challenge_method") != "S256" {
		fail("invalid_request", "PKCE with code_challenge_method=S256 is required")
		return
	}
	if len(challenge) < 43 || len(challenge) > 128 {
		fail("invalid_request", "code_challenge has the wrong length")
		return
	}
	if res := q.Get("resource"); res != "" && !s.matchesResource(res) {
		fail("invalid_target", "unknown resource "+res)
		return
	}
	ar := authReq{
		ClientID: q.Get("client_id"), Redirect: redirect, Challenge: challenge,
		State: q.Get("state"), Scope: Scope, Name: c.Name,
		Exp: time.Now().Add(15 * time.Minute).Unix(),
	}
	sealed, err := s.cfg.Box.Seal(purposeRequest, ar)
	if err != nil {
		fail("server_error", "could not start sign-in")
		return
	}
	reqToken := requestPrefix + sealed

	if s.cfg.Mode == ModeDashboard {
		// What the consent screen shows (app name, where it sends the user back)
		// is sealed with the key shared with the dashboard, so a crafted link
		// cannot make the screen claim to be a different app.
		ctxTok, err := s.cfg.GrantBox.Seal(ConsentPurpose, consentContext{
			V: 1, RequestHash: seal.Hash(reqToken), ClientName: displayName(c.Name),
			RedirectURI: redirect, RedirectHost: redirectHost(redirect), Exp: ar.Exp,
		})
		if err != nil {
			fail("server_error", "could not start sign-in")
			return
		}
		v := url.Values{}
		v.Set("request", reqToken)
		v.Set("ctx", ctxTok)
		http.Redirect(w, r, strings.TrimRight(s.cfg.DashboardURL, "/")+"/oauth/mcp/authorize?"+v.Encode(), http.StatusFound)
		return
	}
	s.page(w, 200, pageData{
		Title: "Connect " + displayName(c.Name) + " to MeetStream", Client: displayName(c.Name),
		RedirectHost: redirectHost(redirect), Request: reqToken, APIKeysURL: s.cfg.APIKeysURL, Form: true,
	})
}

func (s *Server) matchesResource(res string) bool {
	res = strings.TrimRight(res, "/")
	return res == s.Resource() || res == s.cfg.PublicURL
}

func (s *Server) openRequest(tok string) (*authReq, error) {
	if !strings.HasPrefix(tok, requestPrefix) {
		return nil, seal.ErrInvalid
	}
	var ar authReq
	if err := s.cfg.Box.Open(purposeRequest, strings.TrimPrefix(tok, requestPrefix), &ar); err != nil {
		return nil, err
	}
	if time.Now().Unix() > ar.Exp {
		return nil, errors.New("expired")
	}
	return &ar, nil
}

// authorizeSubmit handles the paste-mode form.
func (s *Server) authorizeSubmit(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Mode != ModePaste {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.page(w, 400, pageData{Title: "Something went wrong", Error: "Could not read the form."})
		return
	}
	reqToken := r.PostForm.Get("request")
	ar, err := s.openRequest(reqToken)
	if err != nil {
		s.page(w, 400, pageData{Title: "This sign-in has expired", Error: "Start the connection again from your app."})
		return
	}
	if r.PostForm.Get("action") == "deny" {
		redirectError(w, r, ar.Redirect, "access_denied", "the user cancelled", ar.State, s.cfg.PublicURL)
		return
	}
	key := strings.TrimSpace(r.PostForm.Get("api_key"))
	again := pageData{
		Title: "Connect " + displayName(ar.Name) + " to MeetStream", Client: displayName(ar.Name),
		RedirectHost: redirectHost(ar.Redirect), Request: reqToken, APIKeysURL: s.cfg.APIKeysURL, Form: true,
	}
	if key == "" {
		again.Error = "Paste your MeetStream API key to continue."
		s.page(w, 400, again)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := s.cfg.CheckKey(ctx, key); err != nil {
		if errors.Is(err, ErrBadKey) {
			again.Error = "MeetStream did not accept that key. Check you copied all of it, or create a new one."
			s.page(w, 400, again)
			return
		}
		s.cfg.Logger.Warn("oauth: key check failed", "err", err.Error())
		again.Error = "We could not reach MeetStream to check the key. Try again in a moment."
		s.page(w, 502, again)
		return
	}
	s.issueCode(w, r, ar, key)
}

// ConsentPurpose is the AAD for the consent context the dashboard opens.
const ConsentPurpose = "meetstream-mcp-consent-v1"

// consentContext is sealed for the dashboard: everything its consent screen
// displays, bound to one pending request.
type consentContext struct {
	V            int    `json:"v"`
	RequestHash  string `json:"request_hash"`
	ClientName   string `json:"client_name"`
	RedirectURI  string `json:"redirect_uri"`
	RedirectHost string `json:"redirect_host"`
	Exp          int64  `json:"exp"`
}

// grant is what the dashboard seals with the shared grant secret.
type grant struct {
	V           int    `json:"v"`
	RequestHash string `json:"request_hash"`
	UserID      string `json:"user_id"`
	APIKey      string `json:"api_key"`
	APIKeyName  string `json:"api_key_name"`
	Iat         int64  `json:"iat"`
	Exp         int64  `json:"exp"`
	Jti         string `json:"jti"`
}

// callback receives the dashboard's answer (dashboard mode).
func (s *Server) callback(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Mode != ModeDashboard {
		http.NotFound(w, r)
		return
	}
	_ = r.ParseForm()
	reqToken := r.Form.Get("request")
	ar, err := s.openRequest(reqToken)
	if err != nil {
		s.page(w, 400, pageData{Title: "This sign-in has expired", Error: "Start the connection again from your app."})
		return
	}
	if e := r.Form.Get("error"); e != "" {
		code := "access_denied"
		if e != "access_denied" {
			code = "server_error"
		}
		redirectError(w, r, ar.Redirect, code, "sign-in was not completed", ar.State, s.cfg.PublicURL)
		return
	}
	var g grant
	if err := s.cfg.GrantBox.Open(GrantPurpose, r.Form.Get("grant"), &g); err != nil {
		s.cfg.Logger.Warn("oauth: grant did not open")
		redirectError(w, r, ar.Redirect, "server_error", "invalid grant from dashboard", ar.State, s.cfg.PublicURL)
		return
	}
	now := time.Now().Unix()
	switch {
	case g.V != 1, g.APIKey == "", g.Jti == "":
		redirectError(w, r, ar.Redirect, "server_error", "malformed grant", ar.State, s.cfg.PublicURL)
		return
	case now > g.Exp || g.Exp-g.Iat > 600:
		redirectError(w, r, ar.Redirect, "server_error", "grant expired", ar.State, s.cfg.PublicURL)
		return
	case subtle.ConstantTimeCompare([]byte(g.RequestHash), []byte(seal.Hash(reqToken))) != 1:
		redirectError(w, r, ar.Redirect, "server_error", "grant does not match this request", ar.State, s.cfg.PublicURL)
		return
	case !s.used.claim("grant:"+g.Jti, time.Unix(g.Exp, 0)):
		redirectError(w, r, ar.Redirect, "server_error", "grant already used", ar.State, s.cfg.PublicURL)
		return
	}
	s.issueCode(w, r, ar, g.APIKey)
}

type codePayload struct {
	ClientID  string `json:"c"`
	Redirect  string `json:"r"`
	Challenge string `json:"cc"`
	Key       string `json:"k"`
	Scope     string `json:"sc"`
	Exp       int64  `json:"e"`
	Jti       string `json:"j"`
}

func (s *Server) issueCode(w http.ResponseWriter, r *http.Request, ar *authReq, apiKey string) {
	code, err := s.cfg.Box.Seal(purposeCode, codePayload{
		ClientID: ar.ClientID, Redirect: ar.Redirect, Challenge: ar.Challenge, Key: apiKey,
		Scope: ar.Scope, Exp: time.Now().Add(2 * time.Minute).Unix(), Jti: randID(),
	})
	if err != nil {
		redirectError(w, r, ar.Redirect, "server_error", "could not issue code", ar.State, s.cfg.PublicURL)
		return
	}
	u, _ := url.Parse(ar.Redirect)
	q := u.Query()
	q.Set("code", codePrefix+code)
	if ar.State != "" {
		q.Set("state", ar.State)
	}
	q.Set("iss", s.cfg.PublicURL)
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

// ── Token endpoint ────────────────────────────────────────────────────────

type tokenPayload struct {
	ClientID string `json:"c"`
	Key      string `json:"k"`
	Scope    string `json:"sc"`
	Exp      int64  `json:"e"`
	Jti      string `json:"j,omitempty"`
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		oauthError(w, 400, "invalid_request", "could not parse form body")
		return
	}
	clientID, secret, hasBasic := r.BasicAuth()
	if !hasBasic {
		clientID, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	} else {
		clientID, _ = url.QueryUnescape(clientID)
		secret, _ = url.QueryUnescape(secret)
	}
	c, err := s.openClient(clientID)
	if err != nil {
		oauthError(w, 401, "invalid_client", "unknown client_id")
		return
	}
	if c.Auth != "none" && !s.cfg.Box.VerifyMAC(purposeSecret, clientID, secret) {
		oauthError(w, 401, "invalid_client", "client authentication failed")
		return
	}
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		s.exchangeCode(w, r, clientID)
	case "refresh_token":
		s.refresh(w, r, clientID)
	default:
		oauthError(w, 400, "unsupported_grant_type", "use authorization_code or refresh_token")
	}
}

func (s *Server) exchangeCode(w http.ResponseWriter, r *http.Request, clientID string) {
	raw := r.PostForm.Get("code")
	var cp codePayload
	if !strings.HasPrefix(raw, codePrefix) || s.cfg.Box.Open(purposeCode, strings.TrimPrefix(raw, codePrefix), &cp) != nil {
		oauthError(w, 400, "invalid_grant", "invalid authorization code")
		return
	}
	if time.Now().Unix() > cp.Exp {
		oauthError(w, 400, "invalid_grant", "authorization code expired")
		return
	}
	if cp.ClientID != clientID {
		oauthError(w, 400, "invalid_grant", "code was issued to another client")
		return
	}
	if ru := r.PostForm.Get("redirect_uri"); ru != "" && ru != cp.Redirect {
		oauthError(w, 400, "invalid_grant", "redirect_uri does not match")
		return
	}
	verifier := r.PostForm.Get("code_verifier")
	if len(verifier) < 43 || len(verifier) > 128 {
		oauthError(w, 400, "invalid_grant", "code_verifier is required (43-128 characters)")
		return
	}
	sum := sha256.Sum256([]byte(verifier))
	if subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(cp.Challenge)) != 1 {
		oauthError(w, 400, "invalid_grant", "PKCE verification failed")
		return
	}
	if res := r.PostForm.Get("resource"); res != "" && !s.matchesResource(res) {
		oauthError(w, 400, "invalid_target", "unknown resource")
		return
	}
	if !s.used.claim("code:"+cp.Jti, time.Unix(cp.Exp, 0)) {
		oauthError(w, 400, "invalid_grant", "authorization code already used")
		return
	}
	s.issueTokens(w, clientID, cp.Key)
}

func (s *Server) refresh(w http.ResponseWriter, r *http.Request, clientID string) {
	raw := r.PostForm.Get("refresh_token")
	var tp tokenPayload
	if !strings.HasPrefix(raw, RefreshPrefix) || s.cfg.Box.Open(purposeRefresh, strings.TrimPrefix(raw, RefreshPrefix), &tp) != nil {
		oauthError(w, 400, "invalid_grant", "invalid refresh token")
		return
	}
	if time.Now().Unix() > tp.Exp {
		oauthError(w, 400, "invalid_grant", "refresh token expired, sign in again")
		return
	}
	if tp.ClientID != clientID {
		oauthError(w, 400, "invalid_grant", "refresh token was issued to another client")
		return
	}
	s.issueTokens(w, clientID, tp.Key)
}

func (s *Server) issueTokens(w http.ResponseWriter, clientID, apiKey string) {
	now := time.Now()
	at, err1 := s.cfg.Box.Seal(purposeAccess, tokenPayload{ClientID: clientID, Key: apiKey, Scope: Scope, Exp: now.Add(s.cfg.AccessTTL).Unix()})
	rt, err2 := s.cfg.Box.Seal(purposeRefresh, tokenPayload{ClientID: clientID, Key: apiKey, Scope: Scope, Exp: now.Add(s.cfg.RefreshTTL).Unix(), Jti: randID()})
	if err1 != nil || err2 != nil {
		oauthError(w, 500, "server_error", "could not issue tokens")
		return
	}
	writeJSON(w, 200, map[string]any{
		"access_token":  AccessPrefix + at,
		"token_type":    "Bearer",
		"expires_in":    int64(s.cfg.AccessTTL.Seconds()),
		"refresh_token": RefreshPrefix + rt,
		"scope":         Scope,
	})
}

// revoke accepts revocation requests (RFC 7009). Tokens are stateless, so the
// real revocation is deleting the API key in the dashboard; we still answer
// 200 as the RFC requires for unknown or already-invalid tokens.
func (s *Server) revoke(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// VerifyAccessToken opens an OAuth access token and returns the API key in it.
func (s *Server) VerifyAccessToken(tok string) (apiKey string, exp time.Time, clientID string, err error) {
	var tp tokenPayload
	if !strings.HasPrefix(tok, AccessPrefix) || s.cfg.Box.Open(purposeAccess, strings.TrimPrefix(tok, AccessPrefix), &tp) != nil {
		return "", time.Time{}, "", seal.ErrInvalid
	}
	exp = time.Unix(tp.Exp, 0)
	if time.Now().After(exp) {
		return "", exp, tp.ClientID, errors.New("expired")
	}
	return tp.Key, exp, tp.ClientID, nil
}

// ── helpers ───────────────────────────────────────────────────────────────

func randID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func displayName(n string) string {
	if strings.TrimSpace(n) == "" {
		return "An application"
	}
	return n
}

func redirectHost(ru string) string {
	u, err := url.Parse(ru)
	if err != nil {
		return ""
	}
	if u.Host != "" {
		return u.Host
	}
	return u.Scheme + ":"
}

func redirectError(w http.ResponseWriter, r *http.Request, redirect, code, desc, state, issuer string) {
	u, err := url.Parse(redirect)
	if err != nil {
		http.Error(w, code, http.StatusBadRequest)
		return
	}
	q := u.Query()
	q.Set("error", code)
	q.Set("error_description", desc)
	if state != "" {
		q.Set("state", state)
	}
	q.Set("iss", issuer)
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func oauthError(w http.ResponseWriter, status int, code, desc string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": desc})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// cors allows browser-based MCP clients to discover, register and redeem codes.
// No cookies are involved anywhere, so a wildcard origin is safe.
func cors(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Mcp-Protocol-Version")
		w.Header().Set("Access-Control-Max-Age", "86400")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// usedSet remembers single-use identifiers (codes, grants) until they expire.
type usedSet struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

func newUsedSet() *usedSet { return &usedSet{seen: map[string]time.Time{}} }

// claim returns true the first time id is seen.
func (u *usedSet) claim(id string, until time.Time) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	now := time.Now()
	if len(u.seen) > 10000 {
		for k, t := range u.seen {
			if now.After(t) {
				delete(u.seen, k)
			}
		}
	}
	if t, ok := u.seen[id]; ok && now.Before(t) {
		return false
	}
	u.seen[id] = until.Add(time.Minute)
	return true
}

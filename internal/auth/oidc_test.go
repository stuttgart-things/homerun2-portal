package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeIdP is a minimal OIDC provider: discovery, JWKS, and a token endpoint
// that checks the PKCE verifier and returns an RS256 ID token.
type fakeIdP struct {
	t   *testing.T
	srv *httptest.Server
	key *rsa.PrivateKey

	mu        sync.Mutex
	challenge string         // code_challenge of the last authorize
	nonce     string         // nonce of the last authorize
	claims    map[string]any // extra claims of the next ID token
	badNonce  bool
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	return startFakeIdP(t, httptest.NewServer)
}

// startFakeIdP serves the provider with start: httptest.NewServer, or
// httptest.NewTLSServer for a certificate no system trusts.
func startFakeIdP(t *testing.T, start func(http.Handler) *httptest.Server) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &fakeIdP{t: t, key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                p.srv.URL,
			"authorization_endpoint":                p.srv.URL + "/authorize",
			"token_endpoint":                        p.srv.URL + "/token",
			"jwks_uri":                              p.srv.URL + "/keys",
			"end_session_endpoint":                  p.srv.URL + "/end_session",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("GET /keys", func(w http.ResponseWriter, r *http.Request) {
		b64 := base64.RawURLEncoding.EncodeToString
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "k1", "alg": "RS256", "use": "sig",
			"n": b64(key.N.Bytes()), "e": b64(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		p.mu.Lock()
		defer p.mu.Unlock()
		if r.PostForm.Get("code") != "good-code" || base64.RawURLEncoding.EncodeToString(sum[:]) != p.challenge {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		nonce := p.nonce
		if p.badNonce {
			nonce = "other"
		}
		claims := map[string]any{
			"iss": p.srv.URL, "aud": "portal", "sub": "123",
			"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(), "nonce": nonce,
		}
		for k, v := range p.claims {
			claims[k] = v
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at", "token_type": "Bearer", "expires_in": 3600, "id_token": p.sign(claims),
		})
	})
	p.srv = start(mux)
	t.Cleanup(p.srv.Close)
	return p
}

func (p *fakeIdP) sign(claims map[string]any) string {
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	input := enc(map[string]string{"alg": "RS256", "kid": "k1", "typ": "JWT"}) + "." + enc(claims)
	sum := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, p.key, crypto.SHA256, sum[:])
	if err != nil {
		p.t.Fatal(err)
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func newOIDCAuth(t *testing.T, issuer string, mod func(*Config)) *Auth {
	t.Helper()
	failDelay = 0
	cfg := Config{
		Mode:       "oidc",
		UsersFile:  usersFile(t, "admin:"+hash(t, "break-glass")+"\n"),
		SessionKey: key,
		SessionTTL: time.Hour,
		OIDC: OIDCConfig{
			Issuer:      issuer,
			ClientID:    "portal",
			RedirectURL: "https://portal.example" + CallbackPath,
		},
	}
	if mod != nil {
		mod(&cfg)
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// start runs OIDCStart and records what the browser would send to the
// provider; it returns the flow cookie and the state.
func (p *fakeIdP) start(t *testing.T, a *Auth, next string) (*http.Cookie, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	if msg := a.OIDCStart(rec, httptest.NewRequest(http.MethodGet, OIDCStartPath+"?next="+url.QueryEscape(next), nil)); msg != "" {
		t.Fatalf("start: %s", msg)
	}
	if rec.Code != http.StatusFound {
		t.Fatalf("start: status %d", rec.Code)
	}
	loc, _ := url.Parse(rec.Header().Get("Location"))
	q := loc.Query()
	if !strings.HasPrefix(loc.String(), p.srv.URL+"/authorize") || q.Get("client_id") != "portal" ||
		q.Get("code_challenge_method") != "S256" || q.Get("redirect_uri") != "https://portal.example/auth/callback" ||
		!strings.Contains(q.Get("scope"), "openid") {
		t.Fatalf("authorize URL: %s", loc)
	}
	p.mu.Lock()
	p.challenge, p.nonce = q.Get("code_challenge"), q.Get("nonce")
	p.mu.Unlock()
	var flow *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == flowCookie {
			flow = c
		}
	}
	if flow == nil || !flow.HttpOnly || flow.Path != "/auth/" {
		t.Fatalf("flow cookie: %+v", flow)
	}
	return flow, q.Get("state")
}

func callback(a *Auth, flow *http.Cookie, query string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, CallbackPath+"?"+query, nil)
	if flow != nil {
		r.AddCookie(flow)
	}
	rec := httptest.NewRecorder()
	if msg := a.OIDCCallback(rec, r); msg != "" {
		rec.Body.WriteString(msg)
	}
	return rec
}

func sessionOf(t *testing.T, a *Auth, rec *httptest.ResponseRecorder) session {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == CookieName {
			s, ok := a.verify(c.Value)
			if !ok {
				t.Fatal("session cookie does not verify")
			}
			return s
		}
	}
	t.Fatalf("no session cookie (status %d, %s)", rec.Code, rec.Body)
	return session{}
}

func TestOIDCConfig(t *testing.T) {
	for name, c := range map[string]OIDCConfig{
		"no issuer":         {ClientID: "c", RedirectURL: "https://p/auth/callback"},
		"relative issuer":   {Issuer: "zitadel", ClientID: "c", RedirectURL: "https://p/auth/callback"},
		"no client":         {Issuer: "https://z", RedirectURL: "https://p/auth/callback"},
		"no redirect":       {Issuer: "https://z", ClientID: "c"},
		"wrong redirect":    {Issuer: "https://z", ClientID: "c", RedirectURL: "https://p/callback"},
		"relative redirect": {Issuer: "https://z", ClientID: "c", RedirectURL: "/auth/callback"},
	} {
		if _, err := New(Config{Mode: "oidc", SessionKey: key, OIDC: c}); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	// The users file is optional with oidc, and the provider need not be up.
	a, err := New(Config{Mode: "OIDC", SessionKey: key, OIDC: OIDCConfig{
		Issuer: "http://127.0.0.1:1", ClientID: "c", RedirectURL: "https://p/auth/callback"}})
	if err != nil {
		t.Fatal(err)
	}
	if a.LocalEnabled() || !a.OIDCEnabled() || a.OIDCName() != "Zitadel" {
		t.Errorf("local %v, oidc %v %q", a.LocalEnabled(), a.OIDCEnabled(), a.OIDCName())
	}
	if got := a.oidc.cfg.Scopes; strings.Join(got, " ") != "openid profile email" {
		t.Errorf("scopes %v", got)
	}
}

func TestOIDCLogin(t *testing.T) {
	p := newFakeIdP(t)
	a := newOIDCAuth(t, p.srv.URL, nil)
	if !a.LocalEnabled() {
		t.Error("users file not loaded")
	}

	flow, state := p.start(t, a, "/ui/clusterbook")
	p.claims = map[string]any{
		"preferred_username": "patrick",
		"urn:zitadel:iam:org:project:roles": map[string]any{
			"portal-user": map[string]any{"1": "sthings"}, "portal-admin": map[string]any{"1": "sthings"}},
	}
	rec := callback(a, flow, "code=good-code&state="+url.QueryEscape(state))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/ui/clusterbook" {
		t.Fatalf("callback: %d %s %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	s := sessionOf(t, a, rec)
	if s.User != "patrick" || s.Src != SourceOIDC || strings.Join(s.Groups, ",") != "portal-admin,portal-user" {
		t.Errorf("session %+v", s)
	}

	// The middleware passes the identity on and knows where it came from.
	var id Identity
	h := a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ = FromContext(r.Context())
		if r.Header.Get(HeaderUser) != "patrick" || r.Header.Get(HeaderGroups) != "portal-admin,portal-user" {
			t.Errorf("headers %v", r.Header)
		}
	}))
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range rec.Result().Cookies() {
		r.AddCookie(c)
	}
	h.ServeHTTP(httptest.NewRecorder(), r)
	if id.User != "patrick" || id.Source != SourceOIDC {
		t.Errorf("identity %+v", id)
	}

	// Logout ends the provider session too and comes back to the login.
	lrec := httptest.NewRecorder()
	a.Logout(lrec, logoutRequest(id))
	loc, _ := url.Parse(lrec.Header().Get("Location"))
	if !strings.HasPrefix(loc.String(), p.srv.URL+"/end_session") || loc.Query().Get("client_id") != "portal" ||
		loc.Query().Get("post_logout_redirect_uri") != "https://portal.example/auth/login" {
		t.Errorf("logout to %s", loc)
	}

	// A local login still goes straight back to the login page.
	lrec = httptest.NewRecorder()
	a.Logout(lrec, logoutRequest(Identity{User: "admin", Source: SourceLocal}))
	if got := lrec.Header().Get("Location"); got != LoginPath {
		t.Errorf("local logout to %s", got)
	}
}

func logoutRequest(id Identity) *http.Request {
	r := httptest.NewRequest(http.MethodPost, LogoutPath, nil)
	return r.WithContext(context.WithValue(r.Context(), ctxKey{}, id))
}

func TestOIDCCallbackRejects(t *testing.T) {
	p := newFakeIdP(t)
	p.claims = map[string]any{"preferred_username": "mallory", "urn:zitadel:iam:org:project:roles": map[string]any{"portal-user": map[string]any{}}}

	for name, tc := range map[string]struct {
		mod    func(*Config)
		query  func(state string) string
		noFlow bool
		bad    bool
		want   int
	}{
		"no flow cookie": {noFlow: true, query: func(s string) string { return "code=good-code&state=" + s }, want: http.StatusBadRequest},
		"wrong state":    {query: func(string) string { return "code=good-code&state=forged" }, want: http.StatusBadRequest},
		"provider error": {query: func(s string) string { return "error=access_denied&state=" + s }, want: http.StatusUnauthorized},
		"wrong code":     {query: func(s string) string { return "code=stolen&state=" + s }, want: http.StatusBadGateway},
		"wrong nonce":    {bad: true, query: func(s string) string { return "code=good-code&state=" + s }, want: http.StatusUnauthorized},
		"no allowed group": {
			mod:   func(c *Config) { c.OIDC.AllowedGroups = []string{"portal-admin"} },
			query: func(s string) string { return "code=good-code&state=" + s }, want: http.StatusForbidden,
		},
	} {
		t.Run(name, func(t *testing.T) {
			a := newOIDCAuth(t, p.srv.URL, tc.mod)
			p.badNonce = tc.bad
			defer func() { p.badNonce = false }()
			flow, state := p.start(t, a, "/")
			if tc.noFlow {
				flow = nil
			}
			rec := callback(a, flow, tc.query(url.QueryEscape(state)))
			if rec.Code != tc.want {
				t.Errorf("status %d, want %d (%s)", rec.Code, tc.want, rec.Body)
			}
			for _, c := range rec.Result().Cookies() {
				if c.Name == CookieName {
					t.Error("session cookie set")
				}
			}
		})
	}
}

// With the provider down the portal starts, the button reports it, and the
// local emergency access still logs in.
func TestOIDCProviderDown(t *testing.T) {
	providerTimeout = time.Second
	a := newOIDCAuth(t, "http://127.0.0.1:1", nil)

	rec := httptest.NewRecorder()
	msg := a.OIDCStart(rec, httptest.NewRequest(http.MethodGet, OIDCStartPath, nil))
	if rec.Code != http.StatusBadGateway || !strings.Contains(msg, "not reachable") {
		t.Errorf("start: %d %q", rec.Code, msg)
	}

	rec = httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, LoginPath, strings.NewReader("username=admin&password=break-glass"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if ok, msg := a.Login(rec, r); !ok {
		t.Fatalf("emergency login: %s", msg)
	}
	if s := sessionOf(t, a, rec); s.User != "admin" || s.Src != "" {
		t.Errorf("session %+v", s)
	}
}

// Sessions do not cross modes: an OIDC session is void once OIDC is off.
func TestOIDCSessionNeedsOIDC(t *testing.T) {
	local := newTestAuth(t)
	v := local.issueSession(session{User: "alice", Src: SourceOIDC})
	if _, ok := local.verify(v); ok {
		t.Error("oidc session accepted with AUTH_MODE=local")
	}
	if _, ok := local.verify(local.issueSession(session{User: "alice", Src: "ldap"})); ok {
		t.Error("unknown source accepted")
	}
}

// A provider with a private CA: unreachable without AUTH_OIDC_CA_FILE, a
// full login with it; a file without a certificate fails startup.
func TestOIDCCAFile(t *testing.T) {
	p := startFakeIdP(t, httptest.NewTLSServer)
	p.claims = map[string]any{"preferred_username": "patrick"}

	without := newOIDCAuth(t, p.srv.URL, nil)
	rec := httptest.NewRecorder()
	if msg := without.OIDCStart(rec, httptest.NewRequest(http.MethodGet, OIDCStartPath, nil)); rec.Code != http.StatusBadGateway {
		t.Errorf("without CA file: %d %q, want 502", rec.Code, msg)
	}

	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: p.srv.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	a := newOIDCAuth(t, p.srv.URL, func(c *Config) { c.OIDC.CAFile = ca })
	flow, state := p.start(t, a, "/")
	if s := sessionOf(t, a, callback(a, flow, "code=good-code&state="+url.QueryEscape(state))); s.User != "patrick" {
		t.Errorf("session %+v", s)
	}

	for name, file := range map[string]string{"missing": filepath.Join(t.TempDir(), "nope.pem"), "no PEM": usersFile(t, "not a certificate")} {
		if _, err := New(Config{Mode: "oidc", SessionKey: key, OIDC: OIDCConfig{
			Issuer: "https://z", ClientID: "c", RedirectURL: "https://p/auth/callback", CAFile: file}}); err == nil {
			t.Errorf("%s CA file accepted", name)
		}
	}
}

func TestGroupsClaim(t *testing.T) {
	c := &oidcClient{}
	both := map[string]any{"groups": []any{"g"}, ZitadelRolesClaim: map[string]any{"r": nil}, "teams": "t1,t2"}
	if got := c.groups(both); strings.Join(got, ",") != "g" {
		t.Errorf("default with groups: %v", got)
	}
	if got := c.groups(map[string]any{ZitadelRolesClaim: map[string]any{"r": nil}}); strings.Join(got, ",") != "r" {
		t.Errorf("default with Zitadel roles: %v", got)
	}
	c.cfg.GroupsClaim = "teams"
	if got := c.groups(both); strings.Join(got, ",") != "t1,t2" {
		t.Errorf("configured claim: %v", got)
	}
}

func TestGroupsFrom(t *testing.T) {
	for _, tc := range []struct {
		in   any
		want string
	}{
		{[]any{"a", "b", 3}, "a,b"},
		{"a, b,,c", "a,b,c"},
		{map[string]any{"z": nil, "a": nil}, "a,z"},
		{nil, ""},
		{42.0, ""},
	} {
		if got := strings.Join(groupsFrom(tc.in), ","); got != tc.want {
			t.Errorf("groupsFrom(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

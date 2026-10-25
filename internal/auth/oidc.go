package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// OIDC routes: the "log in with" button starts at OIDCStartPath, the
// provider sends the browser back to CallbackPath (the redirect URI to
// register at the provider).
const (
	OIDCStartPath = "/auth/oidc"
	CallbackPath  = "/auth/callback"
)

// flowCookie carries state, nonce and PKCE verifier from the start of an
// OIDC login to its callback.
const (
	flowCookie = "portal_oidc"
	flowTTL    = 10 * time.Minute
)

// ZitadelRolesClaim is where Zitadel puts the project roles of a user when
// the project asserts them (role → granting organisations).
const ZitadelRolesClaim = "urn:zitadel:iam:org:project:roles"

// providerTimeout bounds discovery and the token exchange.
var providerTimeout = 10 * time.Second

// OIDCConfig is what AUTH_OIDC_* configure.
type OIDCConfig struct {
	// Issuer is the provider's URL, e.g. https://zitadel.example.com.
	Issuer       string
	ClientID     string
	ClientSecret string
	// RedirectURL is https://<portal>/auth/callback.
	RedirectURL string
	Scopes      []string
	// UserClaim names the user (default preferred_username; email and sub
	// are the fallbacks).
	UserClaim string
	// GroupsClaim holds the groups: a list, a comma-separated string, or an
	// object whose keys are the groups. Unset, "groups" is read, else
	// Zitadel's project roles (ZitadelRolesClaim).
	GroupsClaim string
	// AllowedGroups, if set, lets in only users with one of them.
	AllowedGroups []string
	// Name labels the login button.
	Name string
}

// oidcClient talks to the provider. Discovery happens on first use and is
// retried until it works, so a provider that is down does not keep the
// portal from starting — the local emergency access must keep working.
type oidcClient struct {
	cfg OIDCConfig

	mu         sync.Mutex
	verifier   *oidc.IDTokenVerifier
	oauth      *oauth2.Config
	endSession string
}

func newOIDCClient(cfg OIDCConfig) (*oidcClient, error) {
	if err := absURL("AUTH_OIDC_ISSUER", cfg.Issuer); err != nil {
		return nil, err
	}
	if cfg.ClientID == "" {
		return nil, errors.New("AUTH_OIDC_CLIENT_ID: required with AUTH_MODE=oidc")
	}
	if err := absURL("AUTH_OIDC_REDIRECT_URL", cfg.RedirectURL); err != nil {
		return nil, err
	}
	if u, _ := url.Parse(cfg.RedirectURL); u.Path != CallbackPath {
		return nil, fmt.Errorf("AUTH_OIDC_REDIRECT_URL %q: the path must be %s", cfg.RedirectURL, CallbackPath)
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{"profile", "email"}
	}
	if !slices.Contains(cfg.Scopes, oidc.ScopeOpenID) {
		cfg.Scopes = append([]string{oidc.ScopeOpenID}, cfg.Scopes...)
	}
	if cfg.UserClaim == "" {
		cfg.UserClaim = "preferred_username"
	}
	if cfg.Name == "" {
		cfg.Name = "Zitadel"
	}
	return &oidcClient{cfg: cfg}, nil
}

func absURL(name, raw string) error {
	u, err := url.Parse(raw)
	if raw == "" || err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("%s %q: want an absolute http(s) URL", name, raw)
	}
	return nil
}

// discover fetches the provider's configuration once it is reachable.
func (c *oidcClient) discover(ctx context.Context) (*oidc.IDTokenVerifier, *oauth2.Config, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.verifier != nil {
		return c.verifier, c.oauth, nil
	}
	ctx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	p, err := oidc.NewProvider(ctx, c.cfg.Issuer)
	if err != nil {
		return nil, nil, err
	}
	var extra struct {
		EndSession string `json:"end_session_endpoint"`
	}
	_ = p.Claims(&extra)
	c.endSession = extra.EndSession
	c.verifier = p.Verifier(&oidc.Config{ClientID: c.cfg.ClientID})
	c.oauth = &oauth2.Config{
		ClientID:     c.cfg.ClientID,
		ClientSecret: c.cfg.ClientSecret,
		Endpoint:     p.Endpoint(),
		RedirectURL:  c.cfg.RedirectURL,
		Scopes:       c.cfg.Scopes,
	}
	return c.verifier, c.oauth, nil
}

// logoutURL ends the session at the provider and returns to the login page
// (register it as post-logout redirect URI); "" when the provider has no
// end_session_endpoint or was never reached.
func (c *oidcClient) logoutURL() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.endSession == "" {
		return ""
	}
	u, err := url.Parse(c.endSession)
	if err != nil {
		return ""
	}
	q := u.Query()
	q.Set("client_id", c.cfg.ClientID)
	q.Set("post_logout_redirect_uri", c.postLogoutURL())
	u.RawQuery = q.Encode()
	return u.String()
}

// postLogoutURL is the login page on the portal's public address.
func (c *oidcClient) postLogoutURL() string {
	u, _ := url.Parse(c.cfg.RedirectURL)
	u.Path, u.RawQuery = LoginPath, ""
	return u.String()
}

// OIDCEnabled tells the login page to show the "log in with" button.
func (a *Auth) OIDCEnabled() bool { return a.oidc != nil }

// OIDCName labels that button.
func (a *Auth) OIDCName() string {
	if a.oidc == nil {
		return ""
	}
	return a.oidc.cfg.Name
}

// flow is the content of the flow cookie.
type flow struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	Next     string `json:"r"`
	Exp      int64  `json:"e"`
}

func randomString() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// OIDCStart sends the browser to the provider. When the provider cannot be
// reached it returns a message for the login page instead (status already
// written), so the local login stays usable.
func (a *Auth) OIDCStart(w http.ResponseWriter, r *http.Request) (msg string) {
	if a.oidc == nil {
		w.WriteHeader(http.StatusNotFound)
		return "Single sign-on is not configured."
	}
	_, oauth, err := a.oidc.discover(r.Context())
	if err != nil {
		slog.Warn("oidc discovery failed", "issuer", a.oidc.cfg.Issuer, "error", err)
		w.WriteHeader(http.StatusBadGateway)
		return a.oidc.cfg.Name + " is not reachable right now."
	}
	f := flow{
		State:    randomString(),
		Nonce:    randomString(),
		Verifier: oauth2.GenerateVerifier(),
		Next:     SafeNext(r.URL.Query().Get("next")),
		Exp:      a.now().Add(flowTTL).Unix(),
	}
	payload, _ := json.Marshal(f)
	http.SetCookie(w, &http.Cookie{
		Name:     flowCookie,
		Value:    a.sign(payload),
		Path:     "/auth/",
		MaxAge:   int(flowTTL / time.Second),
		HttpOnly: true,
		Secure:   secure(r),
		// Lax: the provider's redirect back is a top-level GET.
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, oauth.AuthCodeURL(f.State, oidc.Nonce(f.Nonce), oauth2.S256ChallengeOption(f.Verifier)), http.StatusFound)
	return ""
}

// OIDCCallback finishes the login: state, code exchange with the PKCE
// verifier, ID token signature, audience, expiry and nonce. On success it
// sets the session and redirects; otherwise it returns a message for the
// login page (status already written).
func (a *Auth) OIDCCallback(w http.ResponseWriter, r *http.Request) (msg string) {
	if a.oidc == nil {
		w.WriteHeader(http.StatusNotFound)
		return "Single sign-on is not configured."
	}
	http.SetCookie(w, &http.Cookie{
		Name: flowCookie, Value: "", Path: "/auth/", MaxAge: -1,
		HttpOnly: true, Secure: secure(r), SameSite: http.SameSiteLaxMode,
	})
	var f flow
	c, err := r.Cookie(flowCookie)
	if err == nil {
		payload, ok := a.open(c.Value)
		if !ok || json.Unmarshal(payload, &f) != nil {
			f = flow{}
		}
	}
	q := r.URL.Query()
	if f.State == "" || a.now().Unix() >= f.Exp ||
		subtle.ConstantTimeCompare([]byte(f.State), []byte(q.Get("state"))) != 1 {
		w.WriteHeader(http.StatusBadRequest)
		return "The login took too long or was started elsewhere. Please try again."
	}
	if e := q.Get("error"); e != "" {
		slog.Info("oidc login refused by provider", "error", e, "description", q.Get("error_description"))
		w.WriteHeader(http.StatusUnauthorized)
		return a.oidc.cfg.Name + " refused the login (" + e + ")."
	}
	verifier, oauth, err := a.oidc.discover(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		return a.oidc.cfg.Name + " is not reachable right now."
	}
	ctx, cancel := context.WithTimeout(r.Context(), providerTimeout)
	defer cancel()
	tok, err := oauth.Exchange(ctx, q.Get("code"), oauth2.VerifierOption(f.Verifier))
	if err != nil {
		slog.Warn("oidc code exchange failed", "error", err)
		w.WriteHeader(http.StatusBadGateway)
		return "The login at " + a.oidc.cfg.Name + " could not be completed."
	}
	raw, _ := tok.Extra("id_token").(string)
	idt, err := verifier.Verify(ctx, raw)
	if err != nil || subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(f.Nonce)) != 1 {
		slog.Warn("oidc id token rejected", "error", err)
		w.WriteHeader(http.StatusUnauthorized)
		return "The login at " + a.oidc.cfg.Name + " could not be verified."
	}
	var claims map[string]any
	if err := idt.Claims(&claims); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return "The login at " + a.oidc.cfg.Name + " could not be verified."
	}
	user := firstString(claims, a.oidc.cfg.UserClaim, "preferred_username", "email", "sub")
	if user == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return "The login at " + a.oidc.cfg.Name + " carried no user name."
	}
	groups := a.oidc.groups(claims)
	if len(a.oidc.cfg.AllowedGroups) > 0 && !intersects(groups, a.oidc.cfg.AllowedGroups) {
		slog.Info("oidc login without an allowed group", "user", user, "groups", groups)
		w.WriteHeader(http.StatusForbidden)
		return user + " has no access to this portal."
	}
	a.setSession(w, r, a.issueSession(session{User: user, Groups: groups, Src: SourceOIDC}))
	http.Redirect(w, r, SafeNext(f.Next), http.StatusFound)
	return ""
}

// groups reads the configured groups claim, or "groups" and then Zitadel's
// roles when none is configured.
func (c *oidcClient) groups(claims map[string]any) []string {
	if c.cfg.GroupsClaim != "" {
		return groupsFrom(claims[c.cfg.GroupsClaim])
	}
	if v, ok := claims["groups"]; ok {
		return groupsFrom(v)
	}
	return groupsFrom(claims[ZitadelRolesClaim])
}

func firstString(claims map[string]any, names ...string) string {
	for _, n := range names {
		if s, ok := claims[n].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// groupsFrom reads a groups claim: a list of strings, a comma-separated
// string, or an object whose keys are the groups (Zitadel's role claim maps
// each role to the organisations that granted it).
func groupsFrom(v any) []string {
	var out []string
	switch g := v.(type) {
	case []any:
		for _, x := range g {
			if s, ok := x.(string); ok && s != "" {
				out = append(out, s)
			}
		}
	case string:
		for _, s := range strings.Split(g, ",") {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	case map[string]any:
		for k := range g {
			out = append(out, k)
		}
		slices.Sort(out)
	}
	return out
}

func intersects(a, b []string) bool {
	return slices.ContainsFunc(a, func(x string) bool { return slices.Contains(b, x) })
}

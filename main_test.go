package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stuttgart-things/sthings-portal/internal/auth"
	"github.com/stuttgart-things/sthings-portal/internal/branding"
	"github.com/stuttgart-things/sthings-portal/internal/config"
	"github.com/stuttgart-things/sthings-portal/internal/health"
	"github.com/stuttgart-things/sthings-portal/internal/services"
	"github.com/stuttgart-things/sthings-portal/internal/web"
)

// TestMux runs the portal in front of a fake led-catcher served under
// BASE_PATH=/led, the way the real one is deployed behind it.
func TestMux(t *testing.T) {
	led := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_, _ = fmt.Fprint(w, `{"status":"ok","version":"v1.8.0"}`)
		case "/led/":
			_, _ = fmt.Fprint(w, `<body sse-connect="/led/api/events/stream">led simulator</body>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer led.Close()

	svcs, err := services.Parse("led=" + led.URL)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Services: svcs, HealthInterval: time.Second, HealthTimeout: time.Second}
	checker := health.NewChecker(svcs, time.Second)
	checker.CheckAll(context.Background())
	portal := httptest.NewServer(newMux(cfg, checker, web.BuildInfo{Version: "test"}))
	defer portal.Close()

	for path, want := range map[string]string{
		"/":                           "LED Matrix",
		"/ui/led":                     `src="/led/"`,
		"/led/":                       "led simulator",
		"/health":                     `"status":"healthy"`,
		"/static/homerun2.css":        `@import url("homerun2-tokens.css")`,
		"/static/homerun2-tokens.css": "--hr-accent:",
		"/static/htmx.min.js":         "htmx",
		"/api/services":               `"version":"v1.8.0"`,
		"/kiosk":                      `data-url="/led/"`,
		"/kiosk/overview":             `class="hr-bare"`,
		"/static/kiosk.js":            "/api/services",
	} {
		resp, err := http.Get(portal.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), want) {
			t.Errorf("GET %s = %d, body lacks %q", path, resp.StatusCode, want)
		}
	}

	// The bare prefix redirects to the subtree, like the service itself does.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(portal.URL + "/led")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode/100 != 3 || resp.Header.Get("Location") != "/led/" {
		t.Errorf("GET /led = %d → %q, want a redirect to /led/", resp.StatusCode, resp.Header.Get("Location"))
	}
}

// TestThemeFilesExist keeps every theme's files in static/.
func TestThemeFilesExist(t *testing.T) {
	for name, theme := range branding.Themes {
		for _, f := range []string{theme.Tokens, theme.Stylesheet} {
			if f == "" {
				continue
			}
			if _, err := fs.Stat(staticFS, "static/"+f); err != nil {
				t.Errorf("theme %s: %v", name, err)
			}
		}
	}
}

// TestMuxBranding: the theme's palette is what the service UIs get as
// /static/homerun2-tokens.css, and the header graphic is served.
func TestMuxBranding(t *testing.T) {
	img := filepath.Join(t.TempDir(), "lab.svg")
	if err := os.WriteFile(img, []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), 0o644); err != nil {
		t.Fatal(err)
	}
	brand, err := branding.Load("machinery", "", "", img)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{HealthInterval: time.Second, HealthTimeout: time.Second, Branding: brand}
	portal := httptest.NewServer(newMux(cfg, health.NewChecker(nil, time.Second), web.BuildInfo{}))
	defer portal.Close()

	for path, want := range map[string]string{
		"/static/homerun2-tokens.css":  "--hr-accent: #6c8aff",
		"/static/themes/machinery.css": ".hr-logo",
		"/branding/header":             "<svg",
		"/":                            `<img src="/branding/header" alt="machinery">`,
		"/kiosk":                       `<img src="/branding/header" alt="machinery">`,
	} {
		resp, err := http.Get(portal.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), want) {
			t.Errorf("GET %s = %d, body lacks %q", path, resp.StatusCode, want)
		}
	}
}

// identityService answers with the identity headers the portal forwards.
func identityService(t *testing.T) []services.Service {
	t.Helper()
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_, _ = fmt.Fprint(w, `{"status":"ok"}`)
			return
		}
		_, _ = fmt.Fprintf(w, "user=%q groups=%q", r.Header.Get(auth.HeaderUser), r.Header.Get(auth.HeaderGroups))
	}))
	t.Cleanup(svc.Close)
	svcs, err := services.Parse("led=" + svc.URL)
	if err != nil {
		t.Fatal(err)
	}
	return svcs
}

// TestMuxAuth: with AUTH_MODE=local nothing but the login and the public
// files answers without a session; after login the services get the
// identity, never one the client made up.
func TestMuxAuth(t *testing.T) {
	h, err := auth.HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	users := filepath.Join(t.TempDir(), "users")
	if err := os.WriteFile(users, []byte("alice:"+string(h)+":lab-admins\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := auth.New(auth.Config{Mode: "local", UsersFile: users, SessionKey: strings.Repeat("k", 32)})
	if err != nil {
		t.Fatal(err)
	}
	svcs := identityService(t)
	cfg := config.Config{Services: svcs, HealthInterval: time.Second, HealthTimeout: time.Second, Auth: a}
	portal := httptest.NewServer(newMux(cfg, health.NewChecker(svcs, time.Second), web.BuildInfo{}))
	defer portal.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	get := func(path string, header ...string) (int, string) {
		req, _ := http.NewRequest(http.MethodGet, portal.URL+path, nil)
		for i := 0; i+1 < len(header); i += 2 {
			req.Header.Set(header[i], header[i+1])
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}

	// Public without a session; everything else sends a browser to the login.
	for _, path := range []string{"/health", "/static/homerun2.css"} {
		if code, _ := get(path); code != http.StatusOK {
			t.Errorf("GET %s without session: %d", path, code)
		}
	}
	if code, body := get("/ui/led", "Accept", "text/html"); code != http.StatusOK || !strings.Contains(body, `name="next" value="/ui/led"`) {
		t.Errorf("GET /ui/led without session: %d, want the login form returning to /ui/led", code)
	}
	if code, _ := get("/led/api"); code != http.StatusUnauthorized {
		t.Errorf("service API without session: %d, want 401", code)
	}

	// Log in.
	resp, err := client.PostForm(portal.URL+"/auth/login", url.Values{"username": {"alice"}, "password": {"secret"}, "next": {"/ui/led"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != "/ui/led" {
		t.Fatalf("login: ended at %s with %d", resp.Request.URL.Path, resp.StatusCode)
	}

	if _, body := get("/"); !strings.Contains(body, `<form class="hr-user" method="post" action="/auth/logout"><span title="logged in">alice</span>`) {
		t.Error("overview: expected the user and a logout button")
	}
	if _, body := get("/led/", auth.HeaderUser, "root", auth.HeaderGroups, "cluster-admins"); body != `user="alice" groups="lab-admins"` {
		t.Errorf("service got %s, want alice's identity only", body)
	}

	// Log out: the session is gone.
	resp, err = client.PostForm(portal.URL+"/auth/logout", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if code, _ := get("/led/api"); code != http.StatusUnauthorized {
		t.Errorf("after logout: %d, want 401", code)
	}
}

// TestMuxNoAuthStripsIdentity: without auth nobody is logged in, so a
// client must not be able to hand a service an identity either.
func TestMuxNoAuthStripsIdentity(t *testing.T) {
	svcs := identityService(t)
	cfg := config.Config{Services: svcs, HealthInterval: time.Second, HealthTimeout: time.Second}
	portal := httptest.NewServer(newMux(cfg, health.NewChecker(svcs, time.Second), web.BuildInfo{}))
	defer portal.Close()

	req, _ := http.NewRequest(http.MethodGet, portal.URL+"/led/", nil)
	req.Header.Set(auth.HeaderUser, "root")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != `user="" groups=""` {
		t.Errorf("service got %s, want no identity", body)
	}
	if strings.Contains(get200(t, portal.URL+"/"), "/auth/logout") {
		t.Error("overview shows a logout without auth")
	}
}

func get200(t *testing.T, u string) string {
	t.Helper()
	resp, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d", u, resp.StatusCode)
	}
	return string(body)
}

// TestMuxOIDCEmergencyAccess: with AUTH_MODE=oidc and the provider down, the
// login page offers the provider and the local emergency access, the button
// says the provider is unreachable, and the local login still gets in.
func TestMuxOIDCEmergencyAccess(t *testing.T) {
	h, err := auth.HashPassword("break-glass")
	if err != nil {
		t.Fatal(err)
	}
	users := filepath.Join(t.TempDir(), "users")
	if err := os.WriteFile(users, []byte("admin:"+h+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := auth.New(auth.Config{Mode: "oidc", UsersFile: users, SessionKey: strings.Repeat("k", 32),
		OIDC: auth.OIDCConfig{Issuer: "http://127.0.0.1:1", ClientID: "portal", RedirectURL: "https://portal.example/auth/callback"}})
	if err != nil {
		t.Fatal(err)
	}
	svcs := identityService(t)
	cfg := config.Config{Services: svcs, HealthInterval: time.Second, HealthTimeout: time.Second, Auth: a}
	portal := httptest.NewServer(newMux(cfg, health.NewChecker(svcs, time.Second), web.BuildInfo{}))
	defer portal.Close()

	page := get200(t, portal.URL+"/auth/login?next=/ui/led")
	for _, want := range []string{`href="/auth/oidc?next=%2fui%2fled">Log in with Zitadel</a>`, `<details class="hr-login-local">`, `action="/auth/login"`} {
		if !strings.Contains(page, want) {
			t.Errorf("login page lacks %s", want)
		}
	}

	resp, err := http.Get(portal.URL + "/auth/oidc?next=/ui/led")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway || !strings.Contains(string(body), "Zitadel is not reachable right now.") ||
		!strings.Contains(string(body), `<details class="hr-login-local" open>`) {
		t.Errorf("provider down: %d, want 502 with the message and the emergency form open", resp.StatusCode)
	}

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	resp, err = client.PostForm(portal.URL+"/auth/login", url.Values{"username": {"admin"}, "password": {"break-glass"}, "next": {"/led/"}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != `user="admin" groups=""` {
		t.Errorf("after emergency login the service got %s", body)
	}
}

// TestMuxKiosk: a display opens /kiosk with the kiosk token once and then
// shows the kiosk and the services read-only, as user kiosk.
func TestMuxKiosk(t *testing.T) {
	h, err := auth.HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	users := filepath.Join(t.TempDir(), "users")
	if err := os.WriteFile(users, []byte("alice:"+h+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("t", 40)
	a, err := auth.New(auth.Config{Mode: "local", UsersFile: users, SessionKey: strings.Repeat("k", 32), KioskToken: token})
	if err != nil {
		t.Fatal(err)
	}
	svcs := identityService(t)
	cfg := config.Config{Services: svcs, HealthInterval: time.Second, HealthTimeout: time.Second, Auth: a}
	portal := httptest.NewServer(newMux(cfg, health.NewChecker(svcs, time.Second), web.BuildInfo{}))
	defer portal.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	resp, err := client.Get(portal.URL + "/kiosk?services=overview,led&seconds=20&kiosk_token=" + token)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != "/kiosk" || strings.Contains(resp.Request.URL.RawQuery, "kiosk_token") {
		t.Fatalf("kiosk: ended at %s with %d", resp.Request.URL, resp.StatusCode)
	}

	resp, err = client.Get(portal.URL + "/led/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != `user="kiosk" groups="kiosk"` {
		t.Errorf("service got %s, want the kiosk identity", body)
	}
	resp, err = client.Post(portal.URL+"/led/api", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("POST as kiosk: %d, want 403", resp.StatusCode)
	}
}

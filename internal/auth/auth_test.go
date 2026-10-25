package auth

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const key = "0123456789abcdef0123456789abcdef"

func hash(t *testing.T, pw string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return string(h)
}

func usersFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "users")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func newTestAuth(t *testing.T) *Auth {
	t.Helper()
	failDelay = 0
	a, err := New(Config{
		Mode:       "local",
		UsersFile:  usersFile(t, "# lab admins\nalice:"+hash(t, "secret")+":lab-admins,viewers\n\nbob:"+hash(t, "hunter2")+"\n"),
		SessionKey: key,
		SessionTTL: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestNew(t *testing.T) {
	if a, err := New(Config{}); a != nil || err != nil {
		t.Errorf("no mode: got %v, %v; want nil, nil", a, err)
	}
	if a, err := New(Config{Mode: "none"}); a != nil || err != nil {
		t.Errorf("none: got %v, %v", a, err)
	}
	for name, cfg := range map[string]Config{
		"unknown mode": {Mode: "ldap"},
		"short key":    {Mode: "local", SessionKey: "short", UsersFile: usersFile(t, "a:"+hash(t, "x"))},
		"no file":      {Mode: "local", SessionKey: key},
	} {
		if _, err := New(cfg); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestLoadUsers(t *testing.T) {
	a := newTestAuth(t)
	if got := a.users["alice"].Groups; strings.Join(got, ",") != "lab-admins,viewers" {
		t.Errorf("alice groups = %v", got)
	}
	if len(a.users["bob"].Groups) != 0 {
		t.Errorf("bob groups = %v", a.users["bob"].Groups)
	}
	for name, content := range map[string]string{
		"empty":     "# nobody\n",
		"no hash":   "alice\n",
		"md5 hash":  "alice:$apr1$abc$def\n",
		"plaintext": "alice:secret\n",
		"duplicate": "alice:" + hash(t, "a") + "\nalice:" + hash(t, "b") + "\n",
	} {
		if _, err := LoadUsers(usersFile(t, content)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestCheck(t *testing.T) {
	a := newTestAuth(t)
	if u, ok := a.Check("alice", "secret"); !ok || u.Name != "alice" {
		t.Error("alice with the right password was refused")
	}
	for _, c := range [][2]string{{"alice", "wrong"}, {"mallory", "secret"}, {"", ""}} {
		if _, ok := a.Check(c[0], c[1]); ok {
			t.Errorf("Check(%q, %q) succeeded", c[0], c[1])
		}
	}
}

func TestSession(t *testing.T) {
	a := newTestAuth(t)
	v := a.issue(a.users["alice"])
	if s, ok := a.verify(v); !ok || s.User != "alice" {
		t.Fatalf("fresh session refused: %+v", s)
	}
	enc, sig, _ := strings.Cut(v, ".")
	for name, bad := range map[string]string{
		"no signature":  enc,
		"bad signature": enc + "." + strings.Repeat("A", len(sig)),
		"other payload": base64.RawURLEncoding.EncodeToString([]byte(`{"u":"bob","e":99999999999}`)) + "." + sig,
		"garbage":       "x.y",
	} {
		if _, ok := a.verify(bad); ok {
			t.Errorf("%s: accepted", name)
		}
	}
	other := *a
	other.key = []byte("another-key-another-key-another-key")
	if _, ok := other.verify(v); ok {
		t.Error("a session signed with another key was accepted")
	}

	a.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if _, ok := a.verify(v); ok {
		t.Error("an expired session was accepted")
	}
	a.now = time.Now
	delete(a.users, "alice")
	if _, ok := a.verify(v); ok {
		t.Error("a session of a removed user was accepted")
	}
}

func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{
		"/":                "/",
		"/ui/led":          "/ui/led",
		"/led/?x=1":        "/led/?x=1",
		"":                 "/",
		"https://evil.com": "/",
		"//evil.com":       "/",
		"/\\evil.com":      "/",
		"/auth/login":      "/",
	} {
		if got := SafeNext(in); got != want {
			t.Errorf("SafeNext(%q) = %q, want %q", in, got, want)
		}
	}
}

// echo answers with the identity headers it receives.
var echo = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte(r.Header.Get(HeaderUser) + "|" + r.Header.Get(HeaderGroups)))
})

func TestMiddleware(t *testing.T) {
	a := newTestAuth(t)
	h := a.Middleware(echo)
	do := func(req *http.Request) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}

	for _, path := range []string{"/health", "/static/homerun2.css", "/auth/login", "/branding/header"} {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set(HeaderUser, "spoofed")
		if w := do(req); w.Code != 200 || w.Body.String() != "|" {
			t.Errorf("public %s: got %d %q, want 200 without identity", path, w.Code, w.Body.String())
		}
	}

	page := httptest.NewRequest("GET", "/ui/led?x=1", nil)
	page.Header.Set("Accept", "text/html")
	if w := do(page); w.Code != http.StatusFound || w.Header().Get("Location") != "/auth/login?next=%2Fui%2Fled%3Fx%3D1" {
		t.Errorf("page without session: got %d %q", w.Code, w.Header().Get("Location"))
	}
	hx := httptest.NewRequest("GET", "/ui/status", nil)
	hx.Header.Set("HX-Request", "true")
	hx.Header.Set("HX-Current-URL", "https://portal.example/ui/core")
	if w := do(hx); w.Code != http.StatusUnauthorized || w.Header().Get("HX-Redirect") != "/auth/login?next=%2Fui%2Fcore" {
		t.Errorf("htmx without session: got %d %q", w.Code, w.Header().Get("HX-Redirect"))
	}
	if w := do(httptest.NewRequest("POST", "/led/api/events", nil)); w.Code != http.StatusUnauthorized {
		t.Errorf("API without session: got %d, want 401", w.Code)
	}

	req := httptest.NewRequest("GET", "/led/", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: a.issue(a.users["alice"])})
	req.Header.Set(HeaderUser, "root")
	req.Header.Set(HeaderGroups, "cluster-admins")
	if w := do(req); w.Code != 200 || w.Body.String() != "alice|lab-admins,viewers" {
		t.Errorf("with session: got %d %q, want alice's identity only", w.Code, w.Body.String())
	}
	bad := httptest.NewRequest("GET", "/led/", nil)
	bad.AddCookie(&http.Cookie{Name: CookieName, Value: "forged.cookie"})
	if w := do(bad); w.Code != http.StatusUnauthorized {
		t.Errorf("forged cookie: got %d", w.Code)
	}
}

func TestLoginAndLogout(t *testing.T) {
	a := newTestAuth(t)
	post := func(form url.Values, header ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", LoginPath, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for i := 0; i+1 < len(header); i += 2 {
			req.Header.Set(header[i], header[i+1])
		}
		w := httptest.NewRecorder()
		a.Login(w, req)
		return w
	}

	w := post(url.Values{"username": {"alice"}, "password": {"secret"}, "next": {"/ui/led"}}, "X-Forwarded-Proto", "https")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/ui/led" {
		t.Fatalf("login: got %d %q", w.Code, w.Header().Get("Location"))
	}
	c := w.Result().Cookies()[0]
	if c.Name != CookieName || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.MaxAge != 3600 {
		t.Errorf("cookie = %+v", c)
	}
	if s, ok := a.verify(c.Value); !ok || s.User != "alice" {
		t.Error("login cookie does not verify")
	}

	if w := post(url.Values{"username": {"alice"}, "password": {"secret"}, "next": {"//evil.com"}}); w.Header().Get("Location") != "/" {
		t.Errorf("open redirect: Location %q", w.Header().Get("Location"))
	}
	if w := post(url.Values{"username": {"alice"}, "password": {"nope"}}); w.Code != http.StatusUnauthorized || len(w.Result().Cookies()) != 0 {
		t.Errorf("wrong password: got %d with cookies %v", w.Code, w.Result().Cookies())
	}
	if w := post(url.Values{"username": {"alice"}, "password": {"secret"}}, "Origin", "https://evil.example"); w.Code != http.StatusForbidden {
		t.Errorf("cross-site login: got %d, want 403", w.Code)
	}

	out := httptest.NewRecorder()
	a.Logout(out, httptest.NewRequest("POST", LogoutPath, nil))
	if c := out.Result().Cookies()[0]; c.MaxAge >= 0 || out.Header().Get("Location") != LoginPath {
		t.Errorf("logout: cookie %+v, Location %q", c, out.Header().Get("Location"))
	}
}

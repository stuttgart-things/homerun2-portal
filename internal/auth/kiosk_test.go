package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const kioskToken = "kiosk-0123456789abcdef0123456789abcdef"

func newKioskAuth(t *testing.T, token string) *Auth {
	t.Helper()
	failDelay = 0
	a, err := New(Config{
		Mode:       "local",
		UsersFile:  usersFile(t, "alice:"+hash(t, "secret")+"\n"),
		SessionKey: key,
		SessionTTL: time.Hour,
		KioskToken: token,
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestKioskTokenConfig(t *testing.T) {
	if _, err := New(Config{Mode: "local", UsersFile: usersFile(t, "alice:"+hash(t, "x")+"\n"),
		SessionKey: key, KioskToken: "short"}); err == nil {
		t.Error("short kiosk token accepted")
	}
}

func TestKioskLogin(t *testing.T) {
	a := newKioskAuth(t, kioskToken)
	var seen *http.Request
	h := a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen = r }))

	// The token URL sets the session and drops the token from the URL.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/kiosk?services=overview,led&seconds=30&kiosk_token="+kioskToken, nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/kiosk?seconds=30&services=overview%2Cled" {
		t.Fatalf("token URL: %d → %s", rec.Code, rec.Header().Get("Location"))
	}
	if seen != nil {
		t.Error("the token URL reached the handler")
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != CookieName {
		t.Fatalf("cookies %v", cookies)
	}
	kiosk := cookies[0]

	do := func(method, path string) *httptest.ResponseRecorder {
		seen = nil
		r := httptest.NewRequest(method, path, nil)
		r.AddCookie(kiosk)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}
	// It reads, as kiosk.
	if rec := do(http.MethodGet, "/led/"); rec.Code != http.StatusOK || seen == nil ||
		seen.Header.Get(HeaderUser) != "kiosk" || seen.Header.Get(HeaderGroups) != "kiosk" {
		t.Fatalf("GET as kiosk: %d %v", rec.Code, seen)
	}
	if id, _ := FromContext(seen.Context()); id.Source != SourceKiosk {
		t.Errorf("identity %+v", id)
	}
	// It does not write.
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if rec := do(m, "/clusterbook/api/v1/networks/10.31.103/assign"); rec.Code != http.StatusForbidden || seen != nil {
			t.Errorf("%s as kiosk: %d", m, rec.Code)
		}
	}
	// Fresh sessions are not renewed on every request...
	if rec := do(http.MethodGet, "/"); len(rec.Result().Cookies()) != 0 {
		t.Error("fresh kiosk session renewed")
	}
	// ...but past half their lifetime, so a rotation never ends.
	a.now = func() time.Time { return time.Now().Add(40 * time.Minute) }
	rec = do(http.MethodGet, "/")
	if c := rec.Result().Cookies(); len(c) != 1 || c[0].MaxAge != 3600 {
		t.Errorf("kiosk session not renewed: %v", c)
	}
	a.now = time.Now

	// A new token ends the old kiosk sessions.
	other := newKioskAuth(t, kioskToken+"-rotated")
	if _, ok := other.verify(kiosk.Value); ok {
		t.Error("kiosk session survived a new token")
	}
	// No token configured, no kiosk sessions.
	if _, ok := newKioskAuth(t, "").verify(a.issueSession(a.kioskSession())); ok {
		t.Error("kiosk session without AUTH_KIOSK_TOKEN")
	}
}

func TestKioskLoginRejects(t *testing.T) {
	for name, tc := range map[string]struct {
		token, method, url string
	}{
		"wrong token":       {kioskToken, http.MethodGet, "/kiosk?kiosk_token=guess"},
		"empty token":       {kioskToken, http.MethodGet, "/kiosk?kiosk_token="},
		"not configured":    {"", http.MethodGet, "/kiosk?kiosk_token=" + kioskToken},
		"not a GET":         {kioskToken, http.MethodPost, "/kiosk?kiosk_token=" + kioskToken},
		"login path target": {kioskToken, http.MethodGet, "/auth/login?kiosk_token=guess"},
	} {
		t.Run(name, func(t *testing.T) {
			a := newKioskAuth(t, tc.token)
			h := a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Error("reached the handler")
			}))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.url, nil))
			loc := rec.Header().Get("Location")
			if rec.Code != http.StatusFound || !strings.HasPrefix(loc, LoginPath+"?next=") || strings.Contains(loc, "kiosk_token") {
				t.Errorf("%d → %s", rec.Code, loc)
			}
			if len(rec.Result().Cookies()) != 0 {
				t.Error("cookie set")
			}
		})
	}
}

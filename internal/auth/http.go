package auth

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// LoginPath is the portal's login page; LogoutPath ends the session.
const (
	LoginPath  = "/auth/login"
	LogoutPath = "/auth/logout"
)

// public paths need no login: the portal's own health, its static files
// (the login page uses them) and the login itself.
func public(path string) bool {
	return path == "/health" || path == LoginPath || path == "/branding/header" ||
		strings.HasPrefix(path, "/static/")
}

// StripIdentity removes identity headers a client sent; only the portal sets
// them. It runs for every request, with or without auth.
func StripIdentity(r *http.Request) {
	r.Header.Del(HeaderUser)
	r.Header.Del(HeaderGroups)
}

// Middleware requires a session for everything but the public paths, and
// passes the identity on: in the request context for the portal's pages, as
// headers for the services behind the proxy.
func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		StripIdentity(r)
		if public(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie(CookieName)
		var s session
		ok := err == nil
		if ok {
			s, ok = a.verify(c.Value)
		}
		if !ok {
			a.unauthorized(w, r)
			return
		}
		r.Header.Set(HeaderUser, s.User)
		if len(s.Groups) > 0 {
			r.Header.Set(HeaderGroups, strings.Join(s.Groups, ","))
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, Identity{User: s.User, Groups: s.Groups})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// unauthorized sends a browser to the login and back afterwards. An htmx
// request gets HX-Redirect (a 302 would land the login page inside the
// fragment); anything else that is not a page gets a bare 401.
func (a *Auth) unauthorized(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "true" {
		back := "/"
		if u, err := url.Parse(r.Header.Get("HX-Current-URL")); err == nil && u.Path != "" {
			back = u.RequestURI()
		}
		w.Header().Set("HX-Redirect", LoginURL(back))
		http.Error(w, "login required", http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html") {
		http.Redirect(w, r, LoginURL(r.URL.RequestURI()), http.StatusFound)
		return
	}
	http.Error(w, "login required", http.StatusUnauthorized)
}

// LoginURL is the login page that returns to next afterwards.
func LoginURL(next string) string {
	return LoginPath + "?next=" + url.QueryEscape(SafeNext(next))
}

// SafeNext keeps next on this portal: a path, not "//host" or a URL.
func SafeNext(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") ||
		strings.HasPrefix(next, LoginPath) {
		return "/"
	}
	return next
}

// failDelay slows down every failed login.
var failDelay = 500 * time.Millisecond

// Login checks a POSTed form. On success it sets the session cookie,
// redirects to next and returns true; otherwise the caller shows the form
// again with msg (the status is already 401 or 403).
func (a *Auth) Login(w http.ResponseWriter, r *http.Request) (ok bool, msg string) {
	if !sameOrigin(r) {
		w.WriteHeader(http.StatusForbidden)
		return false, "This login form came from another site."
	}
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return false, "Invalid form."
	}
	u, ok := a.Check(strings.TrimSpace(r.PostForm.Get("username")), r.PostForm.Get("password"))
	if !ok {
		time.Sleep(failDelay)
		w.WriteHeader(http.StatusUnauthorized)
		return false, "Wrong user name or password."
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    a.issue(u),
		Path:     "/",
		MaxAge:   int(a.ttl / time.Second),
		HttpOnly: true,
		Secure:   secure(r),
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, SafeNext(r.PostForm.Get("next")), http.StatusSeeOther)
	return true, ""
}

// Logout clears the session and returns to the login page.
func (a *Auth) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: secure(r), SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, LoginPath, http.StatusSeeOther)
}

// secure marks the cookie Secure when the browser talks HTTPS to us or to
// the gateway in front of us.
func secure(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// sameOrigin rejects a login POSTed from another site (login CSRF). Browsers
// send Origin on POST; a request without it (curl) is allowed.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return strings.EqualFold(u.Host, host)
}

package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"time"
)

// A kiosk display (an edge box that boots into a browser and rotates
// /kiosk) cannot log in. With AUTH_KIOSK_TOKEN set, it opens any portal URL
// with ?kiosk_token=<token> once: the portal sets a kiosk session and
// redirects to the same URL without the token, so it stays out of the
// address bar, the history and Referer headers.
//
// The kiosk session only reads: GET, HEAD and OPTIONS pass, anything else
// is refused — a passer-by at the display cannot assign an IP or order a
// claim. It renews itself while the display shows pages, so a rotation that
// runs for days does not end on the login page. A new token ends all kiosk
// sessions.

// KioskTokenParam carries the token.
const KioskTokenParam = "kiosk_token"

// KioskUser is the identity of a kiosk session, also its only group.
const KioskUser = "kiosk"

// kioskFingerprint ties a kiosk session to the token it was opened with.
func (a *Auth) kioskFingerprint() string {
	mac := hmac.New(sha256.New, a.key)
	mac.Write([]byte("kiosk\x00" + a.kioskToken))
	return hex.EncodeToString(mac.Sum(nil)[:8])
}

// kioskLogin checks the token of a request and redirects to its URL without
// the token: with a kiosk session when it matches, to the login otherwise.
func (a *Auth) kioskLogin(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	token := q.Get(KioskTokenParam)
	q.Del(KioskTokenParam)
	u := *r.URL
	u.RawQuery = q.Encode()
	target := SafeNext(u.RequestURI())

	if a.kioskToken == "" || r.Method != http.MethodGet ||
		subtle.ConstantTimeCompare([]byte(token), []byte(a.kioskToken)) != 1 {
		time.Sleep(failDelay)
		http.Redirect(w, r, LoginURL(target), http.StatusFound)
		return
	}
	a.setSession(w, r, a.issueSession(a.kioskSession()))
	http.Redirect(w, r, target, http.StatusFound)
}

func (a *Auth) kioskSession() session {
	return session{User: KioskUser, Groups: []string{KioskUser}, Src: SourceKiosk, Kiosk: a.kioskFingerprint()}
}

// kioskAllowed refuses what a kiosk must not do and renews the session once
// half of its lifetime is gone.
func (a *Auth) kioskAllowed(w http.ResponseWriter, r *http.Request, s session) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
	default:
		http.Error(w, "the kiosk display is read-only", http.StatusForbidden)
		return false
	}
	if time.Unix(s.Exp, 0).Sub(a.now()) < a.ttl/2 {
		a.setSession(w, r, a.issueSession(a.kioskSession()))
	}
	return true
}

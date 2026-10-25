// Package auth puts a login in front of the whole portal (#24).
//
// AUTH_MODE=local checks users from a file in htpasswd's bcrypt format
// (user:$2y$…, optionally :group,group), keeps the login in a signed cookie,
// and hands the identity to the services behind the portal as
// X-Forwarded-User / X-Forwarded-Groups — after stripping whatever the
// client sent under those names. AUTH_MODE=oidc logs in at an OIDC provider
// (Zitadel) and keeps the users file as an emergency access next to it. The
// modules this needs are allowed here only (ADR 0001).
package auth

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Modes are the values of AUTH_MODE.
const (
	ModeNone  = "none"
	ModeLocal = "local"
	ModeOIDC  = "oidc"
)

// The identity headers the portal sets for the services; a client's own
// values are always removed.
const (
	HeaderUser   = "X-Forwarded-User"
	HeaderGroups = "X-Forwarded-Groups"
)

// MinSessionKey is the shortest AUTH_SESSION_KEY accepted (bytes).
const MinSessionKey = 32

// Config is what AUTH_* configure.
type Config struct {
	Mode       string
	UsersFile  string
	SessionKey string
	SessionTTL time.Duration
	// OIDC is used with AUTH_MODE=oidc.
	OIDC OIDCConfig
}

// User is one entry of the users file.
type User struct {
	Name   string
	Hash   []byte
	Groups []string
}

// Auth checks logins and sessions. A nil *Auth means AUTH_MODE=none.
type Auth struct {
	users map[string]User
	key   []byte
	ttl   time.Duration
	// dummy is compared against for unknown users, so a login takes as
	// long for a name that does not exist as for a wrong password.
	dummy []byte
	now   func() time.Time
	// oidc is set with AUTH_MODE=oidc.
	oidc *oidcClient
}

// New builds the authenticator; nil (and no error) for AUTH_MODE=none.
// Anything invalid is an error, so the pod does not start half-protected.
func New(cfg Config) (*Auth, error) {
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	switch mode {
	case "", ModeNone:
		return nil, nil
	case ModeLocal, ModeOIDC:
	default:
		return nil, fmt.Errorf("AUTH_MODE %q: want none, local or oidc", cfg.Mode)
	}
	if len(cfg.SessionKey) < MinSessionKey {
		return nil, fmt.Errorf("AUTH_SESSION_KEY: at least %d bytes needed, got %d", MinSessionKey, len(cfg.SessionKey))
	}
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = 12 * time.Hour
	}
	a := &Auth{users: map[string]User{}, key: []byte(cfg.SessionKey), ttl: cfg.SessionTTL, now: time.Now}
	if mode == ModeOIDC {
		c, err := newOIDCClient(cfg.OIDC)
		if err != nil {
			return nil, err
		}
		a.oidc = c
	}
	// With oidc the users file is the emergency access for when the provider
	// is down or misconfigured; it may be left out, the KCL manifests do not.
	if mode == ModeLocal || cfg.UsersFile != "" {
		users, err := LoadUsers(cfg.UsersFile)
		if err != nil {
			return nil, err
		}
		a.users = users
	}
	dummy, err := bcrypt.GenerateFromPassword([]byte("not a password"), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	a.dummy = dummy
	return a, nil
}

// LocalEnabled tells the login page to show the user/password form.
func (a *Auth) LocalEnabled() bool { return len(a.users) > 0 }

// LoadUsers reads the users file: one "name:bcrypt-hash[:group,group]" per
// line, blank lines and # comments ignored — `htpasswd -nbB name password`
// writes such a line. Only bcrypt hashes are accepted.
func LoadUsers(path string) (map[string]User, error) {
	if path == "" {
		return nil, errors.New("AUTH_USERS_FILE: required with AUTH_MODE=local")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("AUTH_USERS_FILE: %w", err)
	}
	defer func() { _ = f.Close() }()

	users := map[string]User{}
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 2 || parts[0] == "" {
			return nil, fmt.Errorf("AUTH_USERS_FILE line %d: want name:bcrypt-hash[:groups]", n)
		}
		name, hash := parts[0], parts[1]
		if _, err := bcrypt.Cost([]byte(hash)); err != nil {
			return nil, fmt.Errorf("AUTH_USERS_FILE line %d (%s): not a bcrypt hash — create one with htpasswd -nbB", n, name)
		}
		if _, dup := users[name]; dup {
			return nil, fmt.Errorf("AUTH_USERS_FILE line %d: user %s twice", n, name)
		}
		u := User{Name: name, Hash: []byte(hash)}
		if len(parts) == 3 {
			for _, g := range strings.Split(parts[2], ",") {
				if g = strings.TrimSpace(g); g != "" {
					u.Groups = append(u.Groups, g)
				}
			}
		}
		users[name] = u
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("AUTH_USERS_FILE: %w", err)
	}
	if len(users) == 0 {
		return nil, errors.New("AUTH_USERS_FILE: no users")
	}
	return users, nil
}

// Check returns the user when name and password match.
func (a *Auth) Check(name, password string) (User, bool) {
	u, ok := a.users[name]
	hash := u.Hash
	if !ok {
		hash = a.dummy
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(password)) != nil || !ok {
		return User{}, false
	}
	return u, true
}

// Where a session's login happened.
const (
	SourceLocal = "local"
	SourceOIDC  = "oidc"
)

// session is the signed cookie's content.
type session struct {
	User   string   `json:"u"`
	Groups []string `json:"g,omitempty"`
	Exp    int64    `json:"e"`
	// Src is SourceOIDC for an OIDC login, empty for a local one.
	Src string `json:"s,omitempty"`
}

// CookieName is the session cookie.
const CookieName = "portal_session"

func (a *Auth) sign(payload []byte) string {
	mac := hmac.New(sha256.New, a.key)
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// issue returns the cookie value for u.
func (a *Auth) issue(u User) string {
	return a.issueSession(session{User: u.Name, Groups: u.Groups})
}

func (a *Auth) issueSession(s session) string {
	s.Exp = a.now().Add(a.ttl).Unix()
	payload, _ := json.Marshal(s)
	return a.sign(payload)
}

// open returns the payload of a signed value if the signature holds.
func (a *Auth) open(value string) ([]byte, bool) {
	enc, sig, ok := strings.Cut(value, ".")
	if !ok {
		return nil, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return nil, false
	}
	if !hmac.Equal([]byte(a.sign(payload)), []byte(enc+"."+sig)) {
		return nil, false
	}
	return payload, true
}

// verify returns the session of a cookie value: signature and expiry; a local
// user must still be in the users file (removing a line logs them out once
// the portal restarted), an OIDC login is only valid while OIDC is on.
func (a *Auth) verify(value string) (session, bool) {
	payload, ok := a.open(value)
	if !ok {
		return session{}, false
	}
	var s session
	if json.Unmarshal(payload, &s) != nil || a.now().Unix() >= s.Exp {
		return session{}, false
	}
	switch s.Src {
	case "":
		if _, ok := a.users[s.User]; !ok {
			return session{}, false
		}
	case SourceOIDC:
		if a.oidc == nil {
			return session{}, false
		}
	default:
		return session{}, false
	}
	return s, true
}

type ctxKey struct{}

// Identity is the logged-in user, for the portal's own pages.
type Identity struct {
	User   string
	Groups []string
	// Source is SourceLocal or SourceOIDC.
	Source string
}

// FromContext returns the identity of the request, if any.
func FromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	return id, ok
}

// HashPassword returns a bcrypt hash for a users-file line, as htpasswd -nbB
// writes it.
func HashPassword(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(h), err
}

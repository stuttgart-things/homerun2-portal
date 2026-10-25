// Package auth puts a login in front of the whole portal (#24).
//
// AUTH_MODE=local checks users from a file in htpasswd's bcrypt format
// (user:$2y$…, optionally :group,group), keeps the login in a signed cookie,
// and hands the identity to the services behind the portal as
// X-Forwarded-User / X-Forwarded-Groups — after stripping whatever the
// client sent under those names. OIDC (Zitadel) follows as a second mode;
// the module it needs is allowed here only (ADR 0001).
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
}

// New builds the authenticator; nil (and no error) for AUTH_MODE=none.
// Anything invalid is an error, so the pod does not start half-protected.
func New(cfg Config) (*Auth, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Mode)) {
	case "", ModeNone:
		return nil, nil
	case ModeLocal:
	default:
		return nil, fmt.Errorf("AUTH_MODE %q: want none or local", cfg.Mode)
	}
	if len(cfg.SessionKey) < MinSessionKey {
		return nil, fmt.Errorf("AUTH_SESSION_KEY: at least %d bytes needed, got %d", MinSessionKey, len(cfg.SessionKey))
	}
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = 12 * time.Hour
	}
	users, err := LoadUsers(cfg.UsersFile)
	if err != nil {
		return nil, err
	}
	dummy, err := bcrypt.GenerateFromPassword([]byte("not a password"), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	return &Auth{users: users, key: []byte(cfg.SessionKey), ttl: cfg.SessionTTL, dummy: dummy, now: time.Now}, nil
}

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

// session is the signed cookie's content.
type session struct {
	User   string   `json:"u"`
	Groups []string `json:"g,omitempty"`
	Exp    int64    `json:"e"`
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
	payload, _ := json.Marshal(session{User: u.Name, Groups: u.Groups, Exp: a.now().Add(a.ttl).Unix()})
	return a.sign(payload)
}

// verify returns the session of a cookie value: signature, expiry, and the
// user must still be in the users file (removing a line logs them out).
func (a *Auth) verify(value string) (session, bool) {
	enc, sig, ok := strings.Cut(value, ".")
	if !ok {
		return session{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return session{}, false
	}
	if !hmac.Equal([]byte(a.sign(payload)), []byte(enc+"."+sig)) {
		return session{}, false
	}
	var s session
	if json.Unmarshal(payload, &s) != nil || a.now().Unix() >= s.Exp {
		return session{}, false
	}
	if _, ok := a.users[s.User]; !ok {
		return session{}, false
	}
	return s, true
}

type ctxKey struct{}

// Identity is the logged-in user, for the portal's own pages.
type Identity struct {
	User   string
	Groups []string
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

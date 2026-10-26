// Package config loads the portal's configuration from the environment, once
// at startup.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/stuttgart-things/sthings-portal/internal/auth"
	"github.com/stuttgart-things/sthings-portal/internal/branding"
	"github.com/stuttgart-things/sthings-portal/internal/services"
)

// Config is everything the portal reads from the environment.
type Config struct {
	Port           string
	Services       []services.Service
	HealthInterval time.Duration
	HealthTimeout  time.Duration
	Branding       branding.Branding
	// Auth is nil with AUTH_MODE=none (the default).
	Auth *auth.Auth
}

// Load reads the environment. Invalid values are errors, not defaults: a
// typo in PORTAL_SERVICES should stop the pod, not hide a service.
func Load() (Config, error) {
	svcs, err := services.Parse(os.Getenv("PORTAL_SERVICES"))
	if err != nil {
		return Config{}, err
	}
	interval, err := duration("HEALTH_INTERVAL", 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	timeout, err := duration("HEALTH_TIMEOUT", 2*time.Second)
	if err != nil {
		return Config{}, err
	}
	brand, err := branding.Load(os.Getenv("PORTAL_THEME"), os.Getenv("PORTAL_TITLE"),
		os.Getenv("PORTAL_SUBTITLE"), os.Getenv("PORTAL_HEADER_IMAGE"), os.Getenv("PORTAL_FAVICON"))
	if err != nil {
		return Config{}, err
	}
	ttl, err := duration("AUTH_SESSION_TTL", 12*time.Hour)
	if err != nil {
		return Config{}, err
	}
	authn, err := auth.New(auth.Config{
		Mode:       os.Getenv("AUTH_MODE"),
		UsersFile:  os.Getenv("AUTH_USERS_FILE"),
		SessionKey: os.Getenv("AUTH_SESSION_KEY"),
		SessionTTL: ttl,
		OIDC: auth.OIDCConfig{
			Issuer:        os.Getenv("AUTH_OIDC_ISSUER"),
			ClientID:      os.Getenv("AUTH_OIDC_CLIENT_ID"),
			ClientSecret:  os.Getenv("AUTH_OIDC_CLIENT_SECRET"),
			RedirectURL:   os.Getenv("AUTH_OIDC_REDIRECT_URL"),
			Scopes:        list(os.Getenv("AUTH_OIDC_SCOPES"), " "),
			UserClaim:     strings.TrimSpace(os.Getenv("AUTH_OIDC_USER_CLAIM")),
			GroupsClaim:   strings.TrimSpace(os.Getenv("AUTH_OIDC_GROUPS_CLAIM")),
			AllowedGroups: list(os.Getenv("AUTH_OIDC_ALLOWED_GROUPS"), ","),
			Name:          strings.TrimSpace(os.Getenv("AUTH_OIDC_NAME")),
			CAFile:        strings.TrimSpace(os.Getenv("AUTH_OIDC_CA_FILE")),
		},
		KioskToken: os.Getenv("AUTH_KIOSK_TOKEN"),
	})
	if err != nil {
		return Config{}, err
	}
	return Config{
		Auth:           authn,
		Branding:       brand,
		Port:           getEnv("PORT", "8080"),
		Services:       svcs,
		HealthInterval: interval,
		HealthTimeout:  timeout,
	}, nil
}

func duration(name string, def time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s %q: want a positive Go duration such as 10s", name, raw)
	}
	return d, nil
}

// list splits raw at sep (a space also splits a comma list), dropping blanks.
func list(raw, sep string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool { return string(r) == sep || r == ' ' })
}

func getEnv(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// SetupLogging configures slog as the default logger from LOG_FORMAT and LOG_LEVEL.
func SetupLogging() {
	format := strings.ToLower(getEnv("LOG_FORMAT", "json"))

	var level slog.Level
	switch strings.ToLower(getEnv("LOG_LEVEL", "info")) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if format == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}
	slog.SetDefault(slog.New(handler))
}

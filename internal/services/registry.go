// Package services holds the homerun2 services the portal fronts.
//
// Each service is mounted under its own path prefix, /<key>/, and the portal
// forwards that path to it unchanged. The service has to answer under the
// prefix itself, so it runs with BASE_PATH=/<key> (homerun2-led-catcher#136).
package services

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Service is one homerun2 service behind the portal.
type Service struct {
	// Key is the path segment the service is mounted under: "led" → /led/.
	Key string `json:"key"`
	// Title is shown in the navigation and on the overview.
	Title string `json:"title"`
	// TitleSet is true when PORTAL_SERVICES names the title (key|Title). A
	// title that is not set may be replaced by the one in the service's
	// manifest.
	TitleSet bool `json:"-"`
	// URL is where the portal reaches the service, e.g. the cluster Service.
	URL *url.URL `json:"-"`
}

// Path is the prefix the service is served under, without a trailing slash.
func (s Service) Path() string { return "/" + s.Key }

// knownTitles names the homerun2 services by their usual keys. A key not
// listed here is shown as is.
var knownTitles = map[string]string{
	"led":    "LED Matrix",
	"core":   "Core Catcher",
	"light":  "Light Catcher",
	"wled":   "WLED Mock",
	"pitch":  "Demo Pitcher",
	"omni":   "Omni Pitcher",
	"notify": "Notification Catcher",
}

// reserved are the portal's own top-level paths; a service mounted on one
// would shadow the portal or be shadowed by it. "overview" is not a path but
// names the overview in /kiosk?services=.
var reserved = map[string]bool{
	"api": true, "auth": true, "branding": true, "health": true, "kiosk": true, "overview": true, "static": true, "ui": true,
}

var keyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Parse reads PORTAL_SERVICES: comma-separated key=url pairs, in navigation
// order. "led=http://homerun2-led-catcher.homerun2.svc" mounts that service
// under /led/. An optional title follows the key after a pipe:
// "led|Matrix=http://...".
//
// Anything malformed is an error rather than skipped: a portal that quietly
// leaves out a service looks like the service is gone.
func Parse(raw string) ([]Service, error) {
	var out []Service
	seen := map[string]bool{}
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		name, target, ok := strings.Cut(entry, "=")
		if !ok {
			return nil, fmt.Errorf("PORTAL_SERVICES entry %q: want key=url", entry)
		}
		key, title, _ := strings.Cut(strings.TrimSpace(name), "|")
		key = strings.TrimSpace(key)
		title = strings.TrimSpace(title)
		if !keyPattern.MatchString(key) {
			return nil, fmt.Errorf("PORTAL_SERVICES key %q: lowercase letters, digits and - only", key)
		}
		if reserved[key] {
			return nil, fmt.Errorf("PORTAL_SERVICES key %q: reserved for the portal itself", key)
		}
		if seen[key] {
			return nil, fmt.Errorf("PORTAL_SERVICES key %q: listed twice", key)
		}
		u, err := url.Parse(strings.TrimSpace(target))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("PORTAL_SERVICES %s: %q is not an http(s) URL", key, target)
		}
		if u.Path != "" && u.Path != "/" {
			// The request path is forwarded as is; a path here would be
			// joined in front of it and the service would see /x/led/...
			return nil, fmt.Errorf("PORTAL_SERVICES %s: URL %q must not have a path", key, target)
		}
		u.Path = ""
		titleSet := title != ""
		if title == "" {
			title = knownTitles[key]
		}
		if title == "" {
			title = key
		}
		seen[key] = true
		out = append(out, Service{Key: key, Title: title, TitleSet: titleSet, URL: u})
	}
	return out, nil
}

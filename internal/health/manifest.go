package health

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"unicode"

	"github.com/stuttgart-things/homerun2-portal/internal/services"
)

// Fragment is one piece of a service's UI the portal composes into its own
// page: an HTML partial at URL, reloaded every RefreshSeconds (0: once).
type Fragment struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	URL            string `json:"url"`
	RefreshSeconds int    `json:"refreshSeconds"`
}

// minRefreshSeconds keeps a service from making every open portal tab poll it
// faster than this.
const minRefreshSeconds = 2

// maxFragments bounds what one manifest can put on the overview.
const maxFragments = 4

// maxTitleRunes bounds a manifest title in the navigation.
const maxTitleRunes = 40

// Manifest is what the portal takes from a service's manifest.json.
type Manifest struct {
	// Title names the service in the navigation, the header and on its card,
	// unless PORTAL_SERVICES sets one.
	Title string
	// Version is shown when the service's /health does not report one.
	Version   string
	Fragments []Fragment
}

// manifest fetches <service>/<key>/ui/manifest.json and returns what the
// portal may use of it. A service without a manifest gets the zero Manifest;
// that is not an error, the overview then shows its card alone.
func (c *Checker) manifest(ctx context.Context, s services.Service) Manifest {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL.String()+s.Path()+"/ui/manifest.json", http.NoBody)
	if err != nil {
		return Manifest{}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return Manifest{}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Manifest{}
	}
	var body struct {
		Title     string     `json:"title"`
		Version   string     `json:"version"`
		Fragments []Fragment `json:"fragments"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body); err != nil {
		return Manifest{}
	}
	return Manifest{
		Title:     cleanText(body.Title, maxTitleRunes),
		Version:   cleanText(body.Version, maxTitleRunes),
		Fragments: validFragments(s, body.Fragments),
	}
}

// cleanText makes a manifest string fit for one line of the navigation:
// control characters and runs of spaces become one space, and it is cut to
// limit runes. The templates escape it.
func cleanText(s string, limit int) string {
	s = strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}), " ")
	if r := []rune(s); len(r) > limit {
		s = strings.TrimSpace(string(r[:limit-1])) + "…"
	}
	return s
}

// validFragments keeps the fragments the portal can load safely: a plain path
// under the service's own prefix, so a manifest cannot point the browser at
// another service or another host. Refresh intervals are clamped.
func validFragments(s services.Service, in []Fragment) []Fragment {
	var out []Fragment
	prefix := s.Path() + "/"
	for _, f := range in {
		if len(out) == maxFragments {
			break
		}
		if !strings.HasPrefix(f.URL, prefix) || strings.ContainsAny(f.URL, "\"'<> \\") || strings.Contains(f.URL, "..") {
			continue
		}
		if f.Title == "" {
			f.Title = f.ID
		}
		if f.RefreshSeconds < 0 {
			f.RefreshSeconds = 0
		}
		if f.RefreshSeconds > 0 && f.RefreshSeconds < minRefreshSeconds {
			f.RefreshSeconds = minRefreshSeconds
		}
		out = append(out, f)
	}
	return out
}

package health

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

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

// manifest fetches <service>/<key>/ui/manifest.json and returns the fragments
// the portal may load. A service without a manifest has none; that is not an
// error, the overview then shows its card alone.
func (c *Checker) manifest(ctx context.Context, s services.Service) []Fragment {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL.String()+s.Path()+"/ui/manifest.json", http.NoBody)
	if err != nil {
		return nil
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var body struct {
		Fragments []Fragment `json:"fragments"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body); err != nil {
		return nil
	}
	return validFragments(s, body.Fragments)
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

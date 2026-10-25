package health

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stuttgart-things/sthings-portal/internal/services"
)

func TestCheckReadsTheManifest(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_, _ = fmt.Fprint(w, `{"status":"ok"}`)
		case "/led/ui/manifest.json":
			_, _ = fmt.Fprint(w, `{"fragments":[{"id":"events","title":"Latest","url":"/led/ui/fragment/events","refreshSeconds":5}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer backend.Close()

	c := NewChecker([]services.Service{svc(t, "led", backend.URL)}, time.Second)
	c.CheckAll(context.Background())
	got := c.Snapshot()[0].Fragments
	want := []Fragment{{ID: "events", Title: "Latest", URL: "/led/ui/fragment/events", RefreshSeconds: 5}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("fragments = %+v, want %+v", got, want)
	}
}

func TestNoManifestMeansNoFragments(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_, _ = fmt.Fprint(w, `{"status":"ok"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer backend.Close()

	c := NewChecker([]services.Service{svc(t, "core", backend.URL)}, time.Second)
	c.CheckAll(context.Background())
	if s := c.Snapshot()[0]; !s.Up || s.Fragments != nil {
		t.Fatalf("status = %+v, want up without fragments", s)
	}
}

func TestValidFragments(t *testing.T) {
	u, _ := url.Parse("http://led")
	s := services.Service{Key: "led", URL: u}
	got := validFragments(s, []Fragment{
		{ID: "ok", URL: "/led/ui/fragment/ok", RefreshSeconds: 5},
		{ID: "fast", URL: "/led/ui/fragment/fast", RefreshSeconds: 1},  // clamped
		{ID: "once", URL: "/led/ui/fragment/once", RefreshSeconds: -3}, // no polling
		{ID: "other-service", URL: "/core/ui/fragment/x"},
		{ID: "other-host", URL: "https://evil.example/led/x"},
		{ID: "protocol-relative", URL: "//evil.example/led/"},
		{ID: "traversal", URL: "/led/../core/x"},
		{ID: "quote", URL: `/led/x" onload="alert(1)`},
		{ID: "prefix-lookalike", URL: "/ledger/x"},
	})
	want := []Fragment{
		{ID: "ok", Title: "ok", URL: "/led/ui/fragment/ok", RefreshSeconds: 5},
		{ID: "fast", Title: "fast", URL: "/led/ui/fragment/fast", RefreshSeconds: minRefreshSeconds},
		{ID: "once", Title: "once", URL: "/led/ui/fragment/once", RefreshSeconds: 0},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("validFragments =\n %+v\nwant\n %+v", got, want)
	}
}

func TestValidFragmentsCapsTheCount(t *testing.T) {
	u, _ := url.Parse("http://led")
	var in []Fragment
	for i := range maxFragments + 3 {
		in = append(in, Fragment{ID: fmt.Sprint(i), URL: fmt.Sprintf("/led/ui/fragment/%d", i)})
	}
	if got := validFragments(services.Service{Key: "led", URL: u}, in); len(got) != maxFragments {
		t.Fatalf("got %d fragments, want %d", len(got), maxFragments)
	}
}

// manifestBackend answers /health without a version, and the manifest with
// manifest until down is set; then every request fails.
func manifestBackend(t *testing.T, manifest string, down *bool) *httptest.Server {
	t.Helper()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case *down:
			http.Error(w, "down", http.StatusServiceUnavailable)
		case r.URL.Path == "/health":
			_, _ = fmt.Fprint(w, `{"status":"ok"}`)
		case r.URL.Path == "/core/ui/manifest.json":
			_, _ = fmt.Fprint(w, manifest)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(backend.Close)
	return backend
}

func TestTheManifestNamesTheService(t *testing.T) {
	down := false
	backend := manifestBackend(t, `{"title":"Core  Catcher\n","version":"v1.2.0","fragments":[]}`, &down)
	s := svc(t, "core", backend.URL) // title from the key, not set in PORTAL_SERVICES
	c := NewChecker([]services.Service{s}, time.Second)

	c.CheckAll(context.Background())
	if st := c.Snapshot()[0]; st.Title != "Core Catcher" || st.Version != "v1.2.0" {
		t.Fatalf("status = %+v, want the manifest's title and version", st)
	}

	down = true
	c.CheckAll(context.Background())
	if st := c.Snapshot()[0]; st.Up || st.Title != "Core Catcher" {
		t.Fatalf("status = %+v, want down under the manifest's last title", st)
	}
}

func TestAConfiguredTitleWins(t *testing.T) {
	down := false
	backend := manifestBackend(t, `{"title":"Core Catcher"}`, &down)
	s := svc(t, "core", backend.URL)
	s.Title, s.TitleSet = "Messages", true
	c := NewChecker([]services.Service{s}, time.Second)

	c.CheckAll(context.Background())
	if st := c.Snapshot()[0]; st.Title != "Messages" {
		t.Fatalf("title = %q, want the one from PORTAL_SERVICES", st.Title)
	}
}

func TestHealthVersionWinsOverTheManifest(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_, _ = fmt.Fprint(w, `{"status":"ok","version":"v2.0.0"}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"version":"dev"}`)
	}))
	defer backend.Close()

	c := NewChecker([]services.Service{svc(t, "led", backend.URL)}, time.Second)
	c.CheckAll(context.Background())
	if st := c.Snapshot()[0]; st.Version != "v2.0.0" || st.Title != "led" {
		t.Fatalf("status = %+v", st)
	}
}

func TestCleanText(t *testing.T) {
	for in, want := range map[string]string{
		"  LED\tMatrix \n":      "LED Matrix",
		"a\x00b":                "a b",
		"":                      "",
		strings.Repeat("x", 50): strings.Repeat("x", maxTitleRunes-1) + "…",
	} {
		if got := cleanText(in, maxTitleRunes); got != want {
			t.Errorf("cleanText(%q) = %q, want %q", in, got, want)
		}
	}
}

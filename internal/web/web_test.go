package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stuttgart-things/sthings-portal/internal/branding"
	"github.com/stuttgart-things/sthings-portal/internal/health"
	"github.com/stuttgart-things/sthings-portal/internal/services"
)

func setup(t *testing.T) *http.ServeMux {
	t.Helper()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"status":"ok","version":"v1.8.0","commit":"abcdef0123"}`)
	}))
	t.Cleanup(backend.Close)
	u, _ := url.Parse(backend.URL)
	svcs := []services.Service{
		{Key: "led", Title: "LED Matrix", URL: u},
		{Key: "core", Title: "Core <Catcher>", URL: u},
	}
	checker := health.NewChecker(svcs, time.Second)
	checker.CheckAll(context.Background())
	mux := http.NewServeMux()
	New(svcs, checker, BuildInfo{Version: "v0.1.0", Commit: "0123456789", Date: "2026-10-20T06:56:56Z"}, 10*time.Second, branding.Default()).Register(mux)
	return mux
}

func get(t *testing.T, mux http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestIndexListsEveryService(t *testing.T) {
	rec := get(t, setup(t), "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`href="/ui/led"`, `href="/led/"`, `href="/ui/core"`, "v1.8.0", "(abcdef0)",
		`hx-get="/ui/status" hx-trigger="every 10s"`,
		"<dt>version</dt><dd>v0.1.0</dd>", "<dt>commit</dt><dd>0123456</dd>",
		"<dt>built</dt><dd>2026-10-20T06:56:56Z</dd>", `src="/static/sthings-logo.png"`,
		"Core &lt;Catcher&gt;", // titles come from configuration and are escaped
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index lacks %q", want)
		}
	}
}

func TestNavFootLeavesOutAnUnknownBuildDate(t *testing.T) {
	for _, date := range []string{"", "unknown"} {
		mux := http.NewServeMux()
		New(nil, health.NewChecker(nil, time.Second), BuildInfo{Version: "dev", Date: date}, time.Second, branding.Default()).Register(mux)
		if body := get(t, mux, "/").Body.String(); strings.Contains(body, "<dt>built</dt>") {
			t.Errorf("date %q: the nav foot shows a build date", date)
		}
	}
}

func TestServicePageEmbedsTheServiceUnderItsPath(t *testing.T) {
	body := get(t, setup(t), "/ui/led").Body.String()
	if !strings.Contains(body, `<iframe class="hr-frame" src="/led/"`) {
		t.Error("service page does not embed /led/")
	}
	if !strings.Contains(body, `href="/ui/led" aria-current="page"`) {
		t.Error("navigation does not mark the current service")
	}
	if !strings.Contains(body, "<title>LED Matrix · homerun2 portal</title>") {
		t.Error("title does not name the service")
	}
}

func TestUnknownServiceIsNotFound(t *testing.T) {
	if rec := get(t, setup(t), "/ui/nope"); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestStatusPartial(t *testing.T) {
	body := get(t, setup(t), "/ui/status").Body.String()
	if strings.Contains(body, "<html") {
		t.Fatal("the partial is a whole page")
	}
	if strings.Count(body, `class="hr-dot up"`) != 2 {
		t.Errorf("want two up dots: %s", body)
	}
}

func TestAPIServices(t *testing.T) {
	rec := get(t, setup(t), "/api/services")
	var got []health.Status
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Key != "led" || !got[0].Up || got[0].Path != "/led" {
		t.Fatalf("api = %+v", got)
	}
}

func TestNoServicesExplainsTheConfiguration(t *testing.T) {
	mux := http.NewServeMux()
	New(nil, health.NewChecker(nil, time.Second), BuildInfo{}, time.Second, branding.Default()).Register(mux)
	if body := get(t, mux, "/").Body.String(); !strings.Contains(body, "PORTAL_SERVICES") {
		t.Error("an empty portal does not say how to configure it")
	}
}

func TestIndexComposesTheServiceFragments(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_, _ = fmt.Fprint(w, `{"status":"ok"}`)
		case "/led/ui/manifest.json":
			_, _ = fmt.Fprint(w, `{"fragments":[{"id":"events","title":"Latest <events>","url":"/led/ui/fragment/events","refreshSeconds":5}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(backend.Close)
	u, _ := url.Parse(backend.URL)
	svcs := []services.Service{{Key: "led", Title: "LED Matrix", URL: u}}
	checker := health.NewChecker(svcs, time.Second)
	checker.CheckAll(context.Background())
	mux := http.NewServeMux()
	New(svcs, checker, BuildInfo{}, 10*time.Second, branding.Default()).Register(mux)

	body := get(t, mux, "/").Body.String()
	for _, want := range []string{
		`hx-get="/led/ui/fragment/events" hx-trigger="load, every 5s"`,
		`Latest &lt;events&gt;`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index lacks %s", want)
		}
	}
}

func TestNavigationTakesTheManifestTitle(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_, _ = fmt.Fprint(w, `{"status":"ok","version":"v1.2.0"}`)
		case "/core/ui/manifest.json":
			_, _ = fmt.Fprint(w, `{"title":"Core <Messages>"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(backend.Close)
	u, _ := url.Parse(backend.URL)
	svcs := []services.Service{{Key: "core", Title: "Core Catcher", URL: u}}
	checker := health.NewChecker(svcs, time.Second)
	checker.CheckAll(context.Background())
	mux := http.NewServeMux()
	New(svcs, checker, BuildInfo{}, 10*time.Second, branding.Default()).Register(mux)

	body := get(t, mux, "/ui/core").Body.String()
	for _, want := range []string{
		`<title>Core &lt;Messages&gt; · homerun2 portal</title>`,
		`href="/ui/core" aria-current="page" title="v1.2.0">Core &lt;Messages&gt;<span class="hr-path">/core</span>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("service page lacks %s", want)
		}
	}
	if strings.Contains(body, "Core Catcher") {
		t.Error("the default title is still shown")
	}
}

func TestBrandingInPages(t *testing.T) {
	mux := http.NewServeMux()
	brand, err := branding.Load("machinery", "Lab", "LABUL", "")
	if err != nil {
		t.Fatal(err)
	}
	New(nil, health.NewChecker(nil, time.Second), BuildInfo{}, time.Second, brand).Register(mux)

	body := get(t, mux, "/").Body.String()
	for _, want := range []string{
		"<title>Lab</title>",
		`<a class="hr-logo" href="/">Lab<small>LABUL</small></a>`,
		`<link rel="stylesheet" href="/static/themes/machinery.css">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index lacks %q", want)
		}
	}
	if strings.Contains(body, "Press+Start+2P") {
		t.Error("machinery theme loads the pixel font")
	}
	if !strings.Contains(get(t, mux, "/kiosk").Body.String(), "Lab<small>KIOSK</small>") {
		t.Error("kiosk lacks the theme title")
	}

	// The default keeps today's look.
	def := get(t, setup(t), "/").Body.String()
	for _, want := range []string{"<title>homerun2 portal</title>", "HOMERUN²<small>PORTAL</small>", "Press+Start+2P"} {
		if !strings.Contains(def, want) {
			t.Errorf("default index lacks %q", want)
		}
	}
	if strings.Contains(def, "themes/") {
		t.Error("default loads a theme stylesheet")
	}
}

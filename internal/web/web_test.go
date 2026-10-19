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

	"github.com/stuttgart-things/homerun2-portal/internal/health"
	"github.com/stuttgart-things/homerun2-portal/internal/services"
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
	New(svcs, checker, BuildInfo{Version: "v0.1.0", Commit: "0123456789"}, 10*time.Second).Register(mux)
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
		`hx-get="/ui/status" hx-trigger="every 10s"`, "portal v0.1.0 (0123456)",
		"Core &lt;Catcher&gt;", // titles come from configuration and are escaped
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index lacks %q", want)
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
	New(nil, health.NewChecker(nil, time.Second), BuildInfo{}, time.Second).Register(mux)
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
	New(svcs, checker, BuildInfo{}, 10*time.Second).Register(mux)

	body := get(t, mux, "/").Body.String()
	for _, want := range []string{
		`class="hr-card hr-card-wide"`,
		`hx-get="/led/ui/fragment/events" hx-trigger="load, every 5s"`,
		`Latest &lt;events&gt;`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index lacks %s", want)
		}
	}
}

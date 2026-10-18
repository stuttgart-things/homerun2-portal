package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stuttgart-things/homerun2-portal/internal/config"
	"github.com/stuttgart-things/homerun2-portal/internal/health"
	"github.com/stuttgart-things/homerun2-portal/internal/services"
	"github.com/stuttgart-things/homerun2-portal/internal/web"
)

// TestMux runs the portal in front of a fake led-catcher served under
// BASE_PATH=/led, the way the real one is deployed behind it.
func TestMux(t *testing.T) {
	led := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_, _ = fmt.Fprint(w, `{"status":"ok","version":"v1.8.0"}`)
		case "/led/":
			_, _ = fmt.Fprint(w, `<body sse-connect="/led/api/events/stream">led simulator</body>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer led.Close()

	svcs, err := services.Parse("led=" + led.URL)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Services: svcs, HealthInterval: time.Second, HealthTimeout: time.Second}
	checker := health.NewChecker(svcs, time.Second)
	checker.CheckAll(context.Background())
	portal := httptest.NewServer(newMux(cfg, checker, web.BuildInfo{Version: "test"}))
	defer portal.Close()

	for path, want := range map[string]string{
		"/":                    "LED Matrix",
		"/ui/led":              `src="/led/"`,
		"/led/":                "led simulator",
		"/health":              `"status":"healthy"`,
		"/static/homerun2.css": "--hr-accent",
		"/static/htmx.min.js":  "htmx",
		"/api/services":        `"version":"v1.8.0"`,
	} {
		resp, err := http.Get(portal.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), want) {
			t.Errorf("GET %s = %d, body lacks %q", path, resp.StatusCode, want)
		}
	}

	// The bare prefix redirects to the subtree, like the service itself does.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(portal.URL + "/led")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode/100 != 3 || resp.Header.Get("Location") != "/led/" {
		t.Errorf("GET /led = %d → %q, want a redirect to /led/", resp.StatusCode, resp.Header.Get("Location"))
	}
}

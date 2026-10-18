package health

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stuttgart-things/homerun2-portal/internal/services"
)

func svc(t *testing.T, key, raw string) services.Service {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return services.Service{Key: key, Title: key, URL: u}
}

func TestCheckAll(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprint(w, `{"status":"ok","version":"v1.8.0","commit":"0123456789abcdef"}`)
	}))
	defer up.Close()
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = fmt.Fprint(w, `{"status":"unhealthy","version":"v2.0.0"}`)
	}))
	defer failing.Close()
	gone := httptest.NewServer(http.NotFoundHandler())
	goneURL := gone.URL
	gone.Close()

	c := NewChecker([]services.Service{
		svc(t, "led", up.URL), svc(t, "core", failing.URL), svc(t, "light", goneURL),
	}, time.Second)

	if s := c.Snapshot(); len(s) != 3 || s[0].Checked {
		t.Fatalf("before the first check: %+v", s)
	}

	c.CheckAll(context.Background())
	s := c.Snapshot()

	if !s[0].Up || s[0].Version != "v1.8.0" || s[0].Commit != "0123456" || s[0].Latency == "" || s[0].Path != "/led" {
		t.Errorf("led = %+v", s[0])
	}
	if s[1].Up || s[1].Error != "HTTP 503 unhealthy" || s[1].Version != "v2.0.0" {
		t.Errorf("core = %+v", s[1])
	}
	if s[2].Up || s[2].Error != "unreachable" || !s[2].Checked {
		t.Errorf("light = %+v", s[2])
	}
}

func TestATimeoutIsDown(t *testing.T) {
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer slow.Close()
	defer close(release)

	c := NewChecker([]services.Service{svc(t, "led", slow.URL)}, 50*time.Millisecond)
	c.CheckAll(context.Background())
	if s := c.Snapshot()[0]; s.Up || s.Error != "unreachable" {
		t.Fatalf("slow service = %+v", s)
	}
}

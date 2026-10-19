package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stuttgart-things/homerun2-portal/internal/services"
)

func service(t *testing.T, raw string) services.Service {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return services.Service{Key: "led", Title: "LED Matrix", URL: u}
}

func TestForwardsThePathUnchanged(t *testing.T) {
	var gotPath, gotQuery, gotPrefix, gotHost string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		gotPrefix, gotHost = r.Header.Get("X-Forwarded-Prefix"), r.Header.Get("X-Forwarded-Host")
		_, _ = fmt.Fprint(w, "ok")
	}))
	defer backend.Close()

	front := httptest.NewServer(New(service(t, backend.URL)))
	defer front.Close()

	resp, err := http.Get(front.URL + "/led/api/events?limit=5")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if gotPath != "/led/api/events" || gotQuery != "limit=5" {
		t.Errorf("backend saw %s?%s, want /led/api/events?limit=5", gotPath, gotQuery)
	}
	if gotPrefix != "/led" {
		t.Errorf("X-Forwarded-Prefix = %q, want /led", gotPrefix)
	}
	if gotHost == "" {
		t.Error("X-Forwarded-Host not set")
	}
}

func TestForwardsTheGatewayScheme(t *testing.T) {
	var got string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Forwarded-Proto")
	}))
	defer backend.Close()

	front := httptest.NewServer(New(service(t, backend.URL)))
	defer front.Close()

	for _, tc := range []struct{ in, want string }{
		{"https", "https"},
		{"http", "http"},
		{"", "http"},
		{"javascript", "http"},
	} {
		req, err := http.NewRequest(http.MethodGet, front.URL+"/led/", http.NoBody)
		if err != nil {
			t.Fatal(err)
		}
		if tc.in != "" {
			req.Header.Set("X-Forwarded-Proto", tc.in)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if got != tc.want {
			t.Errorf("incoming X-Forwarded-Proto %q: service saw %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestStreamsServerSentEventsUnbuffered(t *testing.T) {
	release := make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "event: events-update\ndata: first\n\n")
		w.(http.Flusher).Flush()
		<-release // the stream stays open, as an SSE stream does
	}))
	defer backend.Close()
	defer close(release)

	front := httptest.NewServer(New(service(t, backend.URL)))
	defer front.Close()

	resp, err := http.Get(front.URL + "/led/api/events/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	line := make(chan string, 1)
	go func() {
		l, _ := bufio.NewReader(resp.Body).ReadString('\n')
		line <- l
	}()
	select {
	case l := <-line:
		if l != "event: events-update\n" {
			t.Fatalf("first line = %q", l)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the event did not arrive while the stream was open: the proxy buffers")
	}
}

func TestUnreachableServiceIsABadGatewayPage(t *testing.T) {
	backend := httptest.NewServer(http.NotFoundHandler())
	addr := backend.URL
	backend.Close() // nothing listens there any more

	rec := httptest.NewRecorder()
	New(service(t, addr)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/led/", nil))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), "LED Matrix") {
		t.Errorf("body does not name the service: %s", body)
	}
}

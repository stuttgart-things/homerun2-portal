// Package web serves the portal's own pages: the overview, a page per
// service, the kiosk, and the status pills the header refreshes.
package web

import (
	"bytes"
	"embed"
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"time"

	"github.com/stuttgart-things/sthings-portal/internal/branding"
	"github.com/stuttgart-things/sthings-portal/internal/health"
	"github.com/stuttgart-things/sthings-portal/internal/services"
)

//go:embed templates/*.html
var templateFS embed.FS

// BuildInfo is the portal's own version, shown in the navigation.
type BuildInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

// Web holds what the pages render.
type Web struct {
	services []services.Service
	byKey    map[string]services.Service
	checker  *health.Checker
	build    BuildInfo
	refresh  time.Duration
	brand    branding.Branding

	index   *template.Template
	service *template.Template
	kiosk   *template.Template
}

// New parses the templates; refresh is how often the header re-reads the
// status, brand the deployment's look.
func New(svcs []services.Service, checker *health.Checker, build BuildInfo, refresh time.Duration, brand branding.Branding) *Web {
	byKey := make(map[string]services.Service, len(svcs))
	for _, s := range svcs {
		byKey[s.Key] = s
	}
	if len(build.Commit) > 7 {
		build.Commit = build.Commit[:7]
	}
	return &Web{
		services: svcs,
		byKey:    byKey,
		checker:  checker,
		build:    build,
		refresh:  refresh,
		brand:    brand,
		index:    template.Must(template.ParseFS(templateFS, "templates/layout.html", "templates/index.html", "templates/bare.html")),
		service:  template.Must(template.ParseFS(templateFS, "templates/layout.html", "templates/service.html")),
		kiosk:    template.Must(template.ParseFS(templateFS, "templates/layout.html", "templates/kiosk.html")),
	}
}

// Register adds the portal's own routes to mux.
func (w *Web) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", w.Index)
	mux.HandleFunc("GET /ui/status", w.StatusPartial)
	mux.HandleFunc("GET /ui/{key}", w.Service)
	mux.HandleFunc("GET /api/services", w.APIServices)
	mux.HandleFunc("GET /kiosk", w.Kiosk)
	mux.HandleFunc("GET /kiosk/overview", w.KioskOverview)
}

type page struct {
	Status  []health.Status
	Current *services.Service
	// CurrentTitle names the current service as the navigation does.
	CurrentTitle   string
	Build          BuildInfo
	RefreshSeconds int
	Brand          branding.Branding
}

func (w *Web) page(current *services.Service) page {
	secs := int(w.refresh / time.Second)
	if secs < 1 {
		secs = 1
	}
	p := page{
		Status:         w.checker.Snapshot(),
		Current:        current,
		Build:          w.build,
		RefreshSeconds: secs,
		Brand:          w.brand,
	}
	for _, st := range p.Status {
		if current != nil && st.Key == current.Key {
			p.CurrentTitle = st.Title
		}
	}
	return p
}

// Index is the overview: one card per service.
func (w *Web) Index(rw http.ResponseWriter, r *http.Request) {
	render(rw, w.index, "layout", w.page(nil))
}

// Service is the portal page of one service: navigation around its own UI.
func (w *Web) Service(rw http.ResponseWriter, r *http.Request) {
	s, ok := w.byKey[r.PathValue("key")]
	if !ok {
		http.NotFound(rw, r)
		return
	}
	render(rw, w.service, "layout", w.page(&s))
}

// StatusPartial is the header's status pills, polled by htmx.
func (w *Web) StatusPartial(rw http.ResponseWriter, r *http.Request) {
	render(rw, w.index, "status", w.checker.Snapshot())
}

// APIServices is the same status as JSON.
func (w *Web) APIServices(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(rw).Encode(w.checker.Snapshot()); err != nil {
		slog.Error("encoding services", "error", err)
	}
}

// render executes into a buffer first, so a template error is a clean 500
// rather than half a page.
func render(rw http.ResponseWriter, t *template.Template, name string, data any) {
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		slog.Error("rendering template", "template", name, "error", err)
		http.Error(rw, "internal error", http.StatusInternalServerError)
		return
	}
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(rw)
}

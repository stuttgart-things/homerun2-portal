// Package proxy forwards /<key>/... to a homerun2 service.
package proxy

import (
	"html/template"
	"log/slog"
	"net/http"
	"net/http/httputil"

	"github.com/stuttgart-things/homerun2-portal/internal/services"
)

// New returns a reverse proxy for svc.
//
// The path is forwarded unchanged: /led/api/events reaches the service as
// /led/api/events, which it serves under BASE_PATH=/led. Stripping the prefix
// here instead would leave the service handing out URLs without it.
//
// FlushInterval -1 flushes every write at once, so server-sent events (the
// led-catcher's event stream) arrive as they are sent, not buffered.
func New(svc services.Service) http.Handler {
	return &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(svc.URL)
			r.SetXForwarded()
			// The portal runs without TLS behind the gateway, so SetXForwarded
			// says http. Pass the gateway's scheme on, so a service can still
			// mark its cookies Secure; only the two real values are accepted.
			if proto := r.In.Header.Get("X-Forwarded-Proto"); proto == "https" || proto == "http" {
				r.Out.Header.Set("X-Forwarded-Proto", proto)
			}
			r.Out.Header.Set("X-Forwarded-Prefix", svc.Path())
		},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.Warn("service unreachable", "service", svc.Key, "path", r.URL.Path, "error", err)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadGateway)
			_ = unreachable.Execute(w, svc)
		},
	}
}

var unreachable = template.Must(template.New("unreachable").Parse(
	`<!DOCTYPE html><html lang="en"><head><meta charset="utf-8"><title>{{.Title}} unreachable</title>` +
		`<link rel="stylesheet" href="/static/homerun2.css"></head>` +
		`<body class="hr-unreachable"><p><strong>{{.Title}}</strong> is not reachable right now.</p>` +
		`<p class="hr-muted">The portal could not connect to the service behind {{.Path}}/.</p></body></html>`))

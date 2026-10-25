// Package handlers holds the portal's own health endpoint.
package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/stuttgart-things/sthings-portal/internal/web"
)

// NewHealthHandler answers GET /health with status and build info. It
// reports the portal itself; a service behind it being down does not make
// the portal unhealthy — that is what the status pills are for.
func NewHealthHandler(info web.BuildInfo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "healthy",
			"time":    time.Now().Format(time.RFC3339),
			"version": info.Version,
			"commit":  info.Commit,
			"date":    info.Date,
		})
	}
}

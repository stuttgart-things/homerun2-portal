// Package health polls the /health endpoint of every service behind the portal.
//
// All homerun2 services answer GET /health with JSON carrying at least
// status and version; led-catcher keeps it at the root under any BASE_PATH.
package health

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/stuttgart-things/homerun2-portal/internal/services"
)

// Status is the last known state of one service.
type Status struct {
	Key     string `json:"key"`
	Title   string `json:"title"`
	Path    string `json:"path"`
	Up      bool   `json:"up"`
	Checked bool   `json:"checked"` // false until the first check finished
	Version string `json:"version,omitempty"`
	Commit  string `json:"commit,omitempty"`
	Latency string `json:"latency,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Checker keeps the latest Status of each service.
type Checker struct {
	services []services.Service
	client   *http.Client

	mu     sync.RWMutex
	status map[string]Status
}

// NewChecker checks services with the given per-request timeout.
func NewChecker(svcs []services.Service, timeout time.Duration) *Checker {
	c := &Checker{
		services: svcs,
		client:   &http.Client{Timeout: timeout},
		status:   make(map[string]Status, len(svcs)),
	}
	for _, s := range svcs {
		c.status[s.Key] = Status{Key: s.Key, Title: s.Title, Path: s.Path()}
	}
	return c
}

// Run checks every service now and then every interval, until ctx ends.
func (c *Checker) Run(ctx context.Context, interval time.Duration) {
	c.CheckAll(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.CheckAll(ctx)
		}
	}
}

// CheckAll checks every service concurrently and records the results.
func (c *Checker) CheckAll(ctx context.Context) {
	var wg sync.WaitGroup
	for _, s := range c.services {
		wg.Go(func() {
			st := c.check(ctx, s)
			c.mu.Lock()
			c.status[s.Key] = st
			c.mu.Unlock()
		})
	}
	wg.Wait()
}

// Snapshot returns the latest status of every service, in configuration order.
func (c *Checker) Snapshot() []Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Status, 0, len(c.services))
	for _, s := range c.services {
		out = append(out, c.status[s.Key])
	}
	return out
}

func (c *Checker) check(ctx context.Context, s services.Service) Status {
	st := Status{Key: s.Key, Title: s.Title, Path: s.Path(), Checked: true}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL.String()+"/health", nil)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	start := time.Now()
	resp, err := c.client.Do(req)
	if err != nil {
		st.Error = "unreachable"
		return st
	}
	defer resp.Body.Close()
	st.Latency = time.Since(start).Round(time.Millisecond).String()

	var body struct {
		Status  string `json:"status"`
		Version string `json:"version"`
		Commit  string `json:"commit"`
	}
	// The body is informational; a service that answers 200 with something
	// else is still up.
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body)
	st.Version, st.Commit = body.Version, shortCommit(body.Commit)

	if resp.StatusCode != http.StatusOK {
		st.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
		if body.Status != "" {
			st.Error += " " + body.Status
		}
		return st
	}
	st.Up = true
	return st
}

func shortCommit(c string) string {
	if len(c) > 7 {
		return c[:7]
	}
	return c
}

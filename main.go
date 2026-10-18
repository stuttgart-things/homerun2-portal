package main

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/stuttgart-things/homerun2-portal/internal/config"
	"github.com/stuttgart-things/homerun2-portal/internal/handlers"
	"github.com/stuttgart-things/homerun2-portal/internal/health"
	"github.com/stuttgart-things/homerun2-portal/internal/proxy"
	"github.com/stuttgart-things/homerun2-portal/internal/web"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

//go:embed static/*
var staticFS embed.FS

func main() {
	config.SetupLogging()

	slog.Info("starting homerun2-portal",
		"version", version,
		"commit", commit,
		"date", date,
		"go", runtime.Version(),
	)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	if len(cfg.Services) == 0 {
		slog.Warn("no services configured: set PORTAL_SERVICES")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	checker := health.NewChecker(cfg.Services, cfg.HealthTimeout)
	go checker.Run(ctx, cfg.HealthInterval)

	build := web.BuildInfo{Version: version, Commit: commit, Date: date}
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           newMux(cfg, checker, build),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		slog.Info("server listening", "port", cfg.Port, "services", len(cfg.Services))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	stop()

	slog.Info("shutting down server")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// Open event streams through the proxy never end on their own; Shutdown
	// waits for them until the timeout, then Close cuts them.
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Warn("closing remaining connections", "error", err)
		_ = srv.Close()
	}
	slog.Info("server exited gracefully")
}

// newMux wires the portal's own routes and one reverse proxy per service.
func newMux(cfg config.Config, checker *health.Checker, build web.BuildInfo) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handlers.NewHealthHandler(build))

	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		slog.Error("failed to create static sub-filesystem", "error", err)
		os.Exit(1)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	web.New(cfg.Services, checker, build, cfg.HealthInterval).Register(mux)

	for _, s := range cfg.Services {
		// "/led/" matches the whole subtree; "/led" redirects to it.
		mux.Handle(s.Path()+"/", proxy.New(s))
		slog.Info("service mounted", "key", s.Key, "path", s.Path()+"/", "url", s.URL.String())
	}
	return mux
}

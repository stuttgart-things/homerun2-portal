// Dagger CI module for homerun2-portal
//
// Provides build, lint, test, image build, and security scanning functions.
// Delegates to external stuttgart-things Dagger modules where possible.

package main

import (
	"context"
	"dagger/dagger/internal/dagger"
	"fmt"
)

type Dagger struct{}

// Lint runs golangci-lint on the source code
func (m *Dagger) Lint(
	ctx context.Context,
	src *dagger.Directory,
	// +optional
	// +default="500s"
	timeout string,
) *dagger.Container {
	return dag.Go().Lint(src, dagger.GoLintOpts{
		Timeout: timeout,
	})
}

// Build compiles the Go binary
func (m *Dagger) Build(
	ctx context.Context,
	src *dagger.Directory,
	// +optional
	// +default="main"
	binName string,
	// +optional
	// +default=""
	ldflags string,
	// +optional
	// +default="1.26.6"
	goVersion string,
	// +optional
	// +default="linux"
	os string,
	// +optional
	// +default="amd64"
	arch string,
) *dagger.Directory {
	return dag.Go().BuildBinary(src, dagger.GoBuildBinaryOpts{
		GoVersion:  goVersion,
		Os:         os,
		Arch:       arch,
		BinName:    binName,
		Ldflags:    ldflags,
		GoMainFile: "main.go",
	})
}

// BuildImage builds a container image using ko and optionally pushes it
func (m *Dagger) BuildImage(
	ctx context.Context,
	src *dagger.Directory,
	// +optional
	// +default="ko.local/homerun2-portal"
	repo string,
	// +optional
	// +default="false"
	push string,
) (string, error) {
	return dag.Go().KoBuild(ctx, src, dagger.GoKoBuildOpts{
		Repo: repo,
		Push: push,
	})
}

// ScanImage scans a container image for vulnerabilities using Trivy
func (m *Dagger) ScanImage(
	ctx context.Context,
	imageRef string,
	// +optional
	// +default="HIGH,CRITICAL"
	severity string,
) *dagger.File {
	return dag.Trivy().ScanImage(imageRef, dagger.TrivyScanImageOpts{
		Severity: severity,
	})
}

// BuildAndTestBinary runs the unit tests, builds the binary and checks it in
// front of a stand-in service: the portal's own pages, the health endpoint,
// and a request proxied to the service under its prefix.
func (m *Dagger) BuildAndTestBinary(
	ctx context.Context,
	source *dagger.Directory,
	// +optional
	// +default="1.26.6"
	goVersion string,
	// +optional
	// +default="8080"
	port int,
) (*dagger.File, error) {

	unit := dag.Container().
		From(fmt.Sprintf("golang:%s", goVersion)).
		WithDirectory("/src", source).
		WithWorkdir("/src").
		WithExec([]string{"go", "test", "./..."})
	if _, err := unit.Sync(ctx); err != nil {
		return nil, fmt.Errorf("unit tests failed: %w", err)
	}

	binDir := dag.Go().BuildBinary(source, dagger.GoBuildBinaryOpts{
		GoVersion:  goVersion,
		Os:         "linux",
		Arch:       "amd64",
		GoMainFile: "main.go",
		BinName:    "main",
	})

	// A service that answers under /led/, as led-catcher does with BASE_PATH=/led.
	stub := dag.Container().
		From("nginx:alpine").
		WithNewFile("/usr/share/nginx/html/health", `{"status":"ok","version":"stub"}`).
		WithNewFile("/usr/share/nginx/html/led/index.html", "stub led service").
		WithExposedPort(80).
		AsService()

	testCmd := fmt.Sprintf(`
exec > /app/test-output.log 2>&1
set -e

./main &
BIN_PID=$!
sleep 3

echo "=== /health ==="
curl -fsS http://localhost:%[1]d/health

echo "=== overview ==="
curl -fsS http://localhost:%[1]d/ | grep -q "LED Matrix"

echo "=== service status ==="
curl -fsS http://localhost:%[1]d/api/services | grep -q '"up":true'

echo "=== proxied service ==="
curl -fsS http://localhost:%[1]d/led/ | grep -q "stub led service"

echo "=== All tests passed! ==="
kill $BIN_PID 2>/dev/null || true
`, port)

	result := dag.Container().
		From("alpine:latest").
		WithExec([]string{"apk", "add", "--no-cache", "curl"}).
		WithDirectory("/app", binDir).
		WithWorkdir("/app").
		WithServiceBinding("led", stub).
		WithEnvVariable("PORT", fmt.Sprint(port)).
		WithEnvVariable("PORTAL_SERVICES", "led=http://led").
		WithEnvVariable("HEALTH_INTERVAL", "1s").
		WithExec([]string{"sh", "-c", testCmd})

	testLog := result.File("/app/test-output.log")
	if _, err := result.Sync(ctx); err != nil {
		return testLog, fmt.Errorf("tests failed - check test-output.log for details: %w", err)
	}
	return testLog, nil
}

package branding

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefaultsToHomerun2(t *testing.T) {
	b, err := Load("", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if b.Name != "homerun2" || b.Title != "HOMERUN²" || b.Subtitle != "PORTAL" || b.PageTitle != "homerun2 portal" || !b.PixelFont {
		t.Errorf("default branding = %+v", b.Theme)
	}
	if b.HasHeaderImage() {
		t.Error("default has a header image")
	}
}

func TestLoadThemeAndOverrides(t *testing.T) {
	b, err := Load(" Machinery ", "Lab", "LABUL", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if b.Name != "machinery" || b.Tokens != "themes/machinery-tokens.css" || b.Stylesheet != "themes/machinery.css" || b.PixelFont {
		t.Errorf("machinery theme = %+v", b.Theme)
	}
	if b.Title != "Lab" || b.Subtitle != "LABUL" || b.PageTitle != "Lab" {
		t.Errorf("overrides not applied: %+v", b.Theme)
	}
	if _, err := Load("neon", "", "", "", ""); err == nil || !strings.Contains(err.Error(), "homerun2, machinery") {
		t.Errorf("unknown theme: err = %v, want the list of themes", err)
	}
}

func TestHeaderImage(t *testing.T) {
	dir := t.TempDir()
	svg := filepath.Join(dir, "header.svg")
	if err := os.WriteFile(svg, []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := Load("machinery", "", "", svg, "")
	if err != nil {
		t.Fatal(err)
	}
	if !b.HasHeaderImage() {
		t.Fatal("header image not loaded")
	}
	rec := httptest.NewRecorder()
	b.ServeHeaderImage(rec, httptest.NewRequest(http.MethodGet, HeaderImagePath, nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/svg+xml" || !strings.Contains(rec.Body.String(), "<svg") {
		t.Errorf("serve = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("an SVG must be served with a CSP that blocks script, got %q", csp)
	}

	rec = httptest.NewRecorder()
	Default().ServeHeaderImage(rec, httptest.NewRequest(http.MethodGet, HeaderImagePath, nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("no image configured: got %d, want 404", rec.Code)
	}

	big := filepath.Join(dir, "big.png")
	if err := os.WriteFile(big, make([]byte, MaxHeaderImage+1), 0o644); err != nil {
		t.Fatal(err)
	}
	gif := filepath.Join(dir, "header.gif")
	if err := os.WriteFile(gif, []byte("GIF89a"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{big, gif, filepath.Join(dir, "missing.png")} {
		if _, err := Load("", "", "", path, ""); err == nil {
			t.Errorf("Load(%s) succeeded, want an error", filepath.Base(path))
		}
	}
}

func TestFaviconDefaultPerTheme(t *testing.T) {
	for name := range Themes {
		b, err := Load(name, "", "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		if b.Accent == "" {
			t.Errorf("theme %s has no accent for its favicon", name)
		}
		rec := httptest.NewRecorder()
		b.ServeFavicon(rec, httptest.NewRequest(http.MethodGet, FaviconPath, nil))
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/svg+xml" || b.FaviconType() != "image/svg+xml" {
			t.Errorf("theme %s: serve = %d %q", name, rec.Code, rec.Header().Get("Content-Type"))
		}
		if !strings.Contains(rec.Body.String(), `fill="`+b.Accent+`"`) {
			t.Errorf("theme %s: default favicon lacks the accent %s: %s", name, b.Accent, rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") == "" {
			t.Errorf("theme %s: favicon without Cache-Control", name)
		}
	}
}

func TestFaviconFile(t *testing.T) {
	dir := t.TempDir()
	for file, typ := range map[string]string{"icon.svg": "image/svg+xml", "icon.PNG": "image/png", "favicon.ico": "image/x-icon"} {
		path := filepath.Join(dir, file)
		if err := os.WriteFile(path, []byte("icon "+file), 0o644); err != nil {
			t.Fatal(err)
		}
		b, err := Load("machinery", "", "", "", path)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		b.ServeFavicon(rec, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != typ || rec.Body.String() != "icon "+file || b.FaviconType() != typ {
			t.Errorf("%s: serve = %d %q %q, want %s", file, rec.Code, rec.Header().Get("Content-Type"), rec.Body.String(), typ)
		}
		if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") {
			t.Errorf("%s: a favicon must be served with a CSP that blocks script, got %q", file, csp)
		}
	}

	big := filepath.Join(dir, "big.png")
	if err := os.WriteFile(big, make([]byte, MaxFavicon+1), 0o644); err != nil {
		t.Fatal(err)
	}
	jpg := filepath.Join(dir, "icon.jpg")
	if err := os.WriteFile(jpg, []byte("jpg"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{big, jpg, filepath.Join(dir, "missing.ico")} {
		if _, err := Load("", "", "", "", path); err == nil || !strings.Contains(err.Error(), "PORTAL_FAVICON") {
			t.Errorf("Load(%s): err = %v, want a PORTAL_FAVICON error", filepath.Base(path), err)
		}
	}
}

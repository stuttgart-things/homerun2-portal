// Package branding is the look of one portal deployment: a theme (palette,
// font, header text) and optionally a header graphic, chosen at deploy time.
//
// The theme's palette is what the portal serves as
// /static/homerun2-tokens.css, so the service UIs behind it — which link that
// file when a request carries X-Forwarded-Prefix — take the theme on too.
package branding

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Theme is a preset for one use case.
type Theme struct {
	// Name is the PORTAL_THEME value.
	Name string
	// Title and Subtitle are the header text when there is no header image.
	Title    string
	Subtitle string
	// PageTitle ends every <title>, e.g. "homerun2 portal".
	PageTitle string
	// Tokens is the palette, a file under static/ served as
	// /static/homerun2-tokens.css.
	Tokens string
	// Stylesheet adjusts the layout for the theme; a file under static/,
	// empty for none.
	Stylesheet string
	// PixelFont loads Press Start 2P, the font of the HOMERUN² banners.
	PixelFont bool
	// Accent is the palette's --hr-accent, the colour of the built-in
	// favicon.
	Accent string
}

// Themes are the presets PORTAL_THEME picks from.
var Themes = map[string]Theme{
	"homerun2": {
		Name: "homerun2", Title: "HOMERUN²", Subtitle: "PORTAL", PageTitle: "homerun2 portal",
		Tokens: "homerun2-tokens.css", PixelFont: true, Accent: "#6366f1",
	},
	"machinery": {
		Name: "machinery", Title: "machinery", Subtitle: "LAB PORTAL", PageTitle: "machinery portal",
		Tokens: "themes/machinery-tokens.css", Stylesheet: "themes/machinery.css",
		Accent: "#6c8aff",
	},
}

// DefaultTheme is the theme without PORTAL_THEME: the portal as it was.
const DefaultTheme = "homerun2"

// MaxHeaderImage bounds the header graphic, which is held in memory.
const MaxHeaderImage = 1 << 20

var imageTypes = map[string]string{
	".svg":  "image/svg+xml",
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
}

// MaxFavicon bounds the favicon file, which is held in memory too.
const MaxFavicon = MaxHeaderImage

var faviconTypes = map[string]string{
	".svg": "image/svg+xml",
	".png": "image/png",
	".ico": "image/x-icon",
}

// Branding is the theme with the deployment's overrides applied.
type Branding struct {
	Theme
	headerImage     []byte
	headerImageType string
	favicon         []byte
	faviconType     string
}

// Default is the homerun2 theme without overrides.
func Default() Branding {
	return Branding{Theme: Themes[DefaultTheme]}
}

// Load builds the branding from PORTAL_THEME, PORTAL_TITLE, PORTAL_SUBTITLE,
// PORTAL_HEADER_IMAGE and PORTAL_FAVICON (files, typically mounted from a
// ConfigMap). An unknown theme or an unreadable, oversized or unknown image
// type is an error: the pod should not start with a look nobody chose.
func Load(theme, title, subtitle, headerImage, favicon string) (Branding, error) {
	theme = strings.ToLower(strings.TrimSpace(theme))
	if theme == "" {
		theme = DefaultTheme
	}
	t, ok := Themes[theme]
	if !ok {
		return Branding{}, fmt.Errorf("PORTAL_THEME %q: want one of %s", theme, strings.Join(themeNames(), ", "))
	}
	b := Branding{Theme: t}
	if title = strings.TrimSpace(title); title != "" {
		b.Title = title
		b.PageTitle = title
	}
	if subtitle = strings.TrimSpace(subtitle); subtitle != "" {
		b.Subtitle = subtitle
	}
	if headerImage = strings.TrimSpace(headerImage); headerImage != "" {
		data, typ, err := readImage("PORTAL_HEADER_IMAGE", headerImage, imageTypes, MaxHeaderImage, "an .svg, .png, .jpg or .webp file")
		if err != nil {
			return Branding{}, err
		}
		b.headerImage, b.headerImageType = data, typ
	}
	if favicon = strings.TrimSpace(favicon); favicon != "" {
		data, typ, err := readImage("PORTAL_FAVICON", favicon, faviconTypes, MaxFavicon, "an .svg, .png or .ico file")
		if err != nil {
			return Branding{}, err
		}
		b.favicon, b.faviconType = data, typ
	}
	return b, nil
}

// readImage reads the file of the variable env, if its extension is one of
// types and it is at most max bytes; want names the allowed types.
func readImage(env, path string, types map[string]string, max int64, want string) ([]byte, string, error) {
	typ, ok := types[strings.ToLower(filepath.Ext(path))]
	if !ok {
		return nil, "", fmt.Errorf("%s %q: want %s", env, path, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", env, err)
	}
	if info.Size() > max {
		return nil, "", fmt.Errorf("%s %q: %d bytes, at most %d", env, path, info.Size(), max)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", env, err)
	}
	return data, typ, nil
}

func themeNames() []string {
	names := make([]string, 0, len(Themes))
	for n := range Themes {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// HasHeaderImage tells the templates to show the graphic instead of the text.
func (b Branding) HasHeaderImage() bool { return len(b.headerImage) > 0 }

// HeaderImagePath is where the portal serves the graphic.
const HeaderImagePath = "/branding/header"

// ServeHeaderImage serves the graphic. An SVG can carry script, so the
// response forbids everything but inline styles should someone open it
// directly; as an <img> it never runs script anyway.
func (b Branding) ServeHeaderImage(w http.ResponseWriter, r *http.Request) {
	if !b.HasHeaderImage() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", b.headerImageType)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(b.headerImage)
}

// FaviconPath is where the portal serves the favicon; it answers
// /favicon.ico too, which browsers ask for on their own.
const FaviconPath = "/branding/favicon"

// defaultFavicon is the favicon without PORTAL_FAVICON: four tiles, the
// services behind the portal, on the theme's accent.
const defaultFavicon = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">` +
	`<rect width="32" height="32" rx="7" fill="%s"/>` +
	`<g fill="#fff"><rect x="7" y="7" width="8" height="8" rx="2"/><rect x="17" y="7" width="8" height="8" rx="2" fill-opacity=".7"/>` +
	`<rect x="7" y="17" width="8" height="8" rx="2" fill-opacity=".7"/><rect x="17" y="17" width="8" height="8" rx="2"/></g></svg>`

// Favicon is the deployment's favicon and its Content-Type: the
// PORTAL_FAVICON file, else the theme's built-in one.
func (b Branding) Favicon() ([]byte, string) {
	if len(b.favicon) > 0 {
		return b.favicon, b.faviconType
	}
	return []byte(fmt.Sprintf(defaultFavicon, b.Accent)), "image/svg+xml"
}

// FaviconType is the favicon's Content-Type, for the <link rel="icon">.
func (b Branding) FaviconType() string {
	_, typ := b.Favicon()
	return typ
}

// ServeFavicon serves the favicon, with the header graphic's protection
// against script in an SVG opened directly.
func (b Branding) ServeFavicon(w http.ResponseWriter, r *http.Request) {
	data, typ := b.Favicon()
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(data)
}

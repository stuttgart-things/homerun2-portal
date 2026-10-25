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
}

// Themes are the presets PORTAL_THEME picks from.
var Themes = map[string]Theme{
	"homerun2": {
		Name: "homerun2", Title: "HOMERUN²", Subtitle: "PORTAL", PageTitle: "homerun2 portal",
		Tokens: "homerun2-tokens.css", PixelFont: true,
	},
	"machinery": {
		Name: "machinery", Title: "machinery", Subtitle: "LAB PORTAL", PageTitle: "machinery portal",
		Tokens: "themes/machinery-tokens.css", Stylesheet: "themes/machinery.css",
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

// Branding is the theme with the deployment's overrides applied.
type Branding struct {
	Theme
	headerImage     []byte
	headerImageType string
}

// Default is the homerun2 theme without overrides.
func Default() Branding {
	return Branding{Theme: Themes[DefaultTheme]}
}

// Load builds the branding from PORTAL_THEME, PORTAL_TITLE, PORTAL_SUBTITLE
// and PORTAL_HEADER_IMAGE (a file, typically mounted from a ConfigMap). An
// unknown theme or an unreadable, oversized or unknown image type is an
// error: the pod should not start with a look nobody chose.
func Load(theme, title, subtitle, headerImage string) (Branding, error) {
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
		typ, ok := imageTypes[strings.ToLower(filepath.Ext(headerImage))]
		if !ok {
			return Branding{}, fmt.Errorf("PORTAL_HEADER_IMAGE %q: want an .svg, .png, .jpg or .webp file", headerImage)
		}
		info, err := os.Stat(headerImage)
		if err != nil {
			return Branding{}, fmt.Errorf("PORTAL_HEADER_IMAGE: %w", err)
		}
		if info.Size() > MaxHeaderImage {
			return Branding{}, fmt.Errorf("PORTAL_HEADER_IMAGE %q: %d bytes, at most %d", headerImage, info.Size(), MaxHeaderImage)
		}
		data, err := os.ReadFile(headerImage)
		if err != nil {
			return Branding{}, fmt.Errorf("PORTAL_HEADER_IMAGE: %w", err)
		}
		b.headerImage, b.headerImageType = data, typ
	}
	return b, nil
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

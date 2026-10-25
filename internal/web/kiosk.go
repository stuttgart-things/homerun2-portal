package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/stuttgart-things/sthings-portal/internal/branding"
	"github.com/stuttgart-things/sthings-portal/internal/health"
)

// overviewKey names the overview among the kiosk's services.
const overviewKey = "overview"

// Kiosk defaults and bounds, in seconds and percent.
const (
	kioskSeconds = 30
	kioskPause   = 60
	kioskMinSecs = 5
	kioskMaxSecs = 86400
	kioskMinZoom = 25
	kioskMaxZoom = 400
)

// slide is one page the kiosk shows.
type slide struct {
	Key   string
	Title string
	URL   string
	Zoom  int
}

// kioskOptions is what /kiosk was asked to show.
type kioskOptions struct {
	Slides []slide
	// Seconds per slide; 0 holds the first one.
	Seconds int
	// Pause holds the current slide for this long after input; 0 is off.
	Pause int
	// Reset reloads the kiosk this long after the last input; 0 is off.
	Reset int
}

type kioskPage struct {
	kioskOptions
	Status         []health.Status
	RefreshSeconds int
	Brand          branding.Branding
}

// parseKiosk reads the query of /kiosk. Anything it does not understand is
// an error: a kiosk that quietly shows something else is hard to notice.
func (w *Web) parseKiosk(q url.Values, status []health.Status) (kioskOptions, error) {
	opts := kioskOptions{Seconds: kioskSeconds, Pause: kioskPause}
	var err error
	if opts.Seconds, err = seconds(q, "seconds", opts.Seconds, kioskMinSecs); err != nil {
		return opts, err
	}
	if opts.Pause, err = seconds(q, "pause", opts.Pause, 1); err != nil {
		return opts, err
	}
	if opts.Reset, err = seconds(q, "reset", 0, 1); err != nil {
		return opts, err
	}

	defZoom, zoom, err := parseZoom(q.Get("zoom"))
	if err != nil {
		return opts, err
	}

	titles := make(map[string]string, len(status))
	for _, st := range status {
		titles[st.Key] = st.Title
	}

	var keys []string
	if raw := q.Get("services"); raw != "" {
		for k := range strings.SplitSeq(raw, ",") {
			if k = strings.TrimSpace(k); k != "" {
				keys = append(keys, k)
			}
		}
	} else {
		for _, s := range w.services {
			keys = append(keys, s.Key)
		}
	}
	shown := make(map[string]bool, len(keys))
	for _, k := range keys {
		shown[k] = true
		sl := slide{Key: k, Zoom: defZoom}
		if z, ok := zoom[k]; ok {
			sl.Zoom = z
		}
		switch s, ok := w.byKey[k]; {
		case k == overviewKey:
			sl.Title, sl.URL = "Overview", "/kiosk/overview"
		case ok:
			sl.Title, sl.URL = titles[k], s.Path()+"/"
			if sl.Title == "" {
				sl.Title = s.Title
			}
		default:
			return opts, fmt.Errorf("services: no service %q", k)
		}
		opts.Slides = append(opts.Slides, sl)
	}
	for k := range zoom {
		if !shown[k] {
			return opts, fmt.Errorf("zoom: %q is not among the services", k)
		}
	}
	return opts, nil
}

// seconds reads a duration in whole seconds: 0 (off) or min..kioskMaxSecs.
func seconds(q url.Values, name string, def, minimum int) (int, error) {
	raw := q.Get(name)
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || (n != 0 && (n < minimum || n > kioskMaxSecs)) {
		return 0, fmt.Errorf("%s: %q is not 0 or %d..%d seconds", name, raw, minimum, kioskMaxSecs)
	}
	return n, nil
}

// parseZoom reads "130" (every slide) and "led:130,core:80" (per service).
func parseZoom(raw string) (int, map[string]int, error) {
	def, per := 100, map[string]int{}
	for entry := range strings.SplitSeq(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		key, pct, hasKey := strings.Cut(entry, ":")
		if !hasKey {
			pct = key
		}
		n, err := strconv.Atoi(strings.TrimSpace(pct))
		if err != nil || n < kioskMinZoom || n > kioskMaxZoom {
			return 0, nil, fmt.Errorf("zoom: %q is not %d..%d percent", entry, kioskMinZoom, kioskMaxZoom)
		}
		if hasKey {
			per[strings.TrimSpace(key)] = n
		} else {
			def = n
		}
	}
	return def, per, nil
}

// Kiosk shows the services full screen, one after another.
func (w *Web) Kiosk(rw http.ResponseWriter, r *http.Request) {
	status := w.checker.Snapshot()
	opts, err := w.parseKiosk(r.URL.Query(), status)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	render(rw, w.kiosk, "kiosk", kioskPage{
		kioskOptions:   opts,
		Status:         status,
		RefreshSeconds: w.page(nil).RefreshSeconds,
		Brand:          w.brand,
	})
}

// KioskOverview is the overview without the shell, as a kiosk slide.
func (w *Web) KioskOverview(rw http.ResponseWriter, r *http.Request) {
	render(rw, w.index, "bare", w.page(nil))
}

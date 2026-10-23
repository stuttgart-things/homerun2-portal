package web

import (
	"net/http"
	"strings"
	"testing"
)

func TestKioskListsEveryServiceByDefault(t *testing.T) {
	rec := get(t, setup(t), "/kiosk")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-seconds="30" data-pause="60" data-reset="0" data-refresh="10"`,
		`<li data-key="led" data-title="LED Matrix" data-url="/led/" data-zoom="100">`,
		`<li data-key="core" data-title="Core &lt;Catcher&gt;" data-url="/core/" data-zoom="100">`,
		`<span class="hr-kiosk-title" id="hr-kiosk-title">LED Matrix</span>`,
		`hx-get="/ui/status" hx-trigger="every 10s"`,
		`src="/static/kiosk.js"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("kiosk lacks %q", want)
		}
	}
	if strings.Contains(body, `class="hr-nav"`) {
		t.Error("the kiosk shows the navigation")
	}
}

func TestKioskTakesItsOptionsFromTheQuery(t *testing.T) {
	body := get(t, setup(t), "/kiosk?services=core,overview,led&seconds=45&pause=0&reset=300&zoom=80,led:130").Body.String()
	for _, want := range []string{
		`data-seconds="45" data-pause="0" data-reset="300"`,
		`<li data-key="core" data-title="Core &lt;Catcher&gt;" data-url="/core/" data-zoom="80"></li>
        <li data-key="overview" data-title="Overview" data-url="/kiosk/overview" data-zoom="80"></li>
        <li data-key="led" data-title="LED Matrix" data-url="/led/" data-zoom="130">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("kiosk lacks %q", want)
		}
	}
}

func TestKioskRejectsWhatItDoesNotUnderstand(t *testing.T) {
	mux := setup(t)
	for query, want := range map[string]string{
		"services=led,nope":          `no service "nope"`,
		"seconds=2":                  "seconds",
		"seconds=x":                  "seconds",
		"pause=-1":                   "pause",
		"reset=100000":               "reset",
		"zoom=10":                    "zoom",
		"zoom=led:abc":               "zoom",
		"services=led&zoom=core:120": `"core" is not among the services`,
	} {
		rec := get(t, mux, "/kiosk?"+query)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s = %d %q, want 400 with %q", query, rec.Code, rec.Body.String(), want)
		}
	}
}

func TestKioskHoldsWithSecondsZero(t *testing.T) {
	if rec := get(t, setup(t), "/kiosk?seconds=0"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-seconds="0"`) {
		t.Fatalf("seconds=0 = %d", rec.Code)
	}
}

func TestKioskOverviewIsTheCardsWithoutTheShell(t *testing.T) {
	body := get(t, setup(t), "/kiosk/overview").Body.String()
	if !strings.Contains(body, `<body class="hr-bare">`) || !strings.Contains(body, `href="/ui/led"`) {
		t.Error("the bare overview lacks the cards")
	}
	for _, shell := range []string{`class="hr-nav"`, `class="hr-header"`} {
		if strings.Contains(body, shell) {
			t.Errorf("the bare overview has %s", shell)
		}
	}
}

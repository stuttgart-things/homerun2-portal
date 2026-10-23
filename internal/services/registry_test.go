package services

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	got, err := Parse(" led=http://led-catcher:8080 , core|Messages=http://core-catcher/,notify=http://homerun2-notification-catcher,custom=https://x.example ")
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		key, title, url, path string
		titleSet              bool
	}{
		{"led", "LED Matrix", "http://led-catcher:8080", "/led", false},
		{"core", "Messages", "http://core-catcher", "/core", true},
		{"notify", "Notification Catcher", "http://homerun2-notification-catcher", "/notify", false},
		{"custom", "custom", "https://x.example", "/custom", false},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d services, want %d", len(got), len(want))
	}
	for i, w := range want {
		s := got[i]
		if s.Key != w.key || s.Title != w.title || s.URL.String() != w.url || s.Path() != w.path || s.TitleSet != w.titleSet {
			t.Errorf("service %d = %s %q %s %s, want %s %q %s %s", i, s.Key, s.Title, s.URL, s.Path(), w.key, w.title, w.url, w.path)
		}
	}
}

func TestParseEmpty(t *testing.T) {
	got, err := Parse("  ")
	if err != nil || len(got) != 0 {
		t.Fatalf("Parse(empty) = %v, %v", got, err)
	}
}

func TestParseRejects(t *testing.T) {
	for raw, want := range map[string]string{
		"led":                             "want key=url",
		"LED=http://a":                    "lowercase",
		"../x=http://a":                   "lowercase",
		"static=http://a":                 "reserved",
		"led=http://a,led=http://b":       "listed twice",
		"led=ftp://a":                     "not an http(s) URL",
		"led=a:8080":                      "not an http(s) URL",
		"led=http://":                     "not an http(s) URL",
		"led=http://led-catcher/sub/path": "must not have a path",
	} {
		_, err := Parse(raw)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%q) = %v, want error containing %q", raw, err, want)
		}
	}
}

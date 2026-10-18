package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("PORTAL_SERVICES", "")
	t.Setenv("PORT", "")
	t.Setenv("HEALTH_INTERVAL", "")
	t.Setenv("HEALTH_TIMEOUT", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "8080" || cfg.HealthInterval != 10*time.Second || cfg.HealthTimeout != 2*time.Second || len(cfg.Services) != 0 {
		t.Fatalf("defaults = %+v", cfg)
	}
}

func TestLoad(t *testing.T) {
	t.Setenv("PORTAL_SERVICES", "led=http://led-catcher")
	t.Setenv("PORT", "9000")
	t.Setenv("HEALTH_INTERVAL", "30s")
	t.Setenv("HEALTH_TIMEOUT", "500ms")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "9000" || cfg.HealthInterval != 30*time.Second || cfg.HealthTimeout != 500*time.Millisecond {
		t.Fatalf("cfg = %+v", cfg)
	}
	if len(cfg.Services) != 1 || cfg.Services[0].Key != "led" {
		t.Fatalf("services = %+v", cfg.Services)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	for name, value := range map[string]string{
		"PORTAL_SERVICES": "led",
		"HEALTH_INTERVAL": "ten",
		"HEALTH_TIMEOUT":  "-1s",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("PORTAL_SERVICES", "")
			t.Setenv("HEALTH_INTERVAL", "")
			t.Setenv("HEALTH_TIMEOUT", "")
			t.Setenv(name, value)
			if _, err := Load(); err == nil {
				t.Fatalf("%s=%q: want an error", name, value)
			}
		})
	}
}

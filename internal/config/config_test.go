package config

import (
	"testing"
	"time"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"WEATHER_CLIENTRAW_URL", "WEATHER_GIF_URLS", "WEATHER_GIF_FALLBACK",
		"WEATHER_VERIFY_FALLBACK", "WEATHER_INTERVAL", "WEATHER_EXPORT_INTERVAL",
		"WEATHER_HTTP_TIMEOUT", "WEATHER_MAX_BODY_BYTES", "WEATHER_STATION_NAME",
		"WEATHER_STATION_TIMEZONE", "WEATHER_LOG_LEVEL",
	} {
		t.Setenv(key, "")
	}
}

func TestLoadDefaults(t *testing.T) {
	clearEnv(t)
	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Interval != time.Minute {
		t.Errorf("Interval = %v, want 1m", cfg.Interval)
	}
	if cfg.ExportInterval != 30*time.Second {
		t.Errorf("ExportInterval = %v, want 30s", cfg.ExportInterval)
	}
	if !cfg.GIFFallback {
		t.Error("GIFFallback = false, want true")
	}
	if cfg.StationName != "Cumbuco" {
		t.Errorf("StationName = %q, want Cumbuco", cfg.StationName)
	}
	if cfg.Location == nil || cfg.Location.String() != "America/Fortaleza" {
		t.Errorf("Location = %v, want America/Fortaleza", cfg.Location)
	}
	if cfg.MaxBodyBytes < 4096 {
		t.Errorf("MaxBodyBytes = %d, too small", cfg.MaxBodyBytes)
	}
}

func TestLoadFlagsOverrideEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv("WEATHER_STATION_NAME", "env-station")
	cfg, err := Load([]string{"-station", "flag-station", "-interval", "2m"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.StationName != "flag-station" {
		t.Errorf("StationName = %q, want flag-station", cfg.StationName)
	}
	if cfg.Interval != 2*time.Minute {
		t.Errorf("Interval = %v, want 2m", cfg.Interval)
	}
}

func TestLoadValidation(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"short interval", []string{"-interval", "1s"}},
		{"short export interval", []string{"-export-interval", "1s"}},
		{"unknown timezone", []string{"-timezone", "Mars/Olympus"}},
		{"unknown log level", []string{"-log-level", "loud"}},
		{"unexpected argument", []string{"extra"}},
		{"small body limit", []string{"-max-body-bytes", "10"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			if _, err := Load(tc.args); err == nil {
				t.Fatal("Load() error = nil, want error")
			}
		})
	}
}

func TestLoadDisablesFallbackWithoutURLs(t *testing.T) {
	clearEnv(t)
	cfg, err := Load([]string{"-fallback-url", " , "})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.GIFFallback {
		t.Error("GIFFallback = true with no fallback URLs")
	}
}

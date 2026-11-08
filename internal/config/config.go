// Package config loads runtime configuration from flags and environment
// variables. Flags take precedence over the environment.
package config

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

const (
	defaultClientrawURL = "https://www.cumbuco-oceanview.com/weather/clientraw.txt"
	defaultGIFURL       = "https://www.cumbuco-oceanview.com/weather/index.gif"
	defaultStationName  = "Cumbuco"
	defaultTimezone     = "America/Fortaleza"
)

// Config is the fully validated runtime configuration.
type Config struct {
	ClientrawURL    string
	GIFURLs         []string
	GIFFallback     bool
	VerifyFallback  bool
	Interval        time.Duration
	ExportInterval  time.Duration
	HTTPTimeout     time.Duration
	MaxBodyBytes    int64
	StationName     string
	StationTimezone string
	LogLevel        slog.Level

	// Location is StationTimezone resolved. Populated by Load.
	Location *time.Location
}

// Load parses args (excluding the program name) and the environment.
func Load(args []string) (Config, error) {
	var (
		cfg      Config
		level    = "info"
		gifURLs  = envString("WEATHER_GIF_URLS", defaultGIFURL)
		fallback = envBool("WEATHER_GIF_FALLBACK", true)
		verify   = envBool("WEATHER_VERIFY_FALLBACK", false)
		interval = envDuration("WEATHER_INTERVAL", time.Minute)
		export   = envDuration("WEATHER_EXPORT_INTERVAL", 30*time.Second)
		timeout  = envDuration("WEATHER_HTTP_TIMEOUT", 10*time.Second)
		maxBody  = envInt64("WEATHER_MAX_BODY_BYTES", 2<<20)
		station  = envString("WEATHER_STATION_NAME", defaultStationName)
		zone     = envString("WEATHER_STATION_TIMEZONE", defaultTimezone)
	)

	fs := flag.NewFlagSet("weather-otel", flag.ContinueOnError)
	fs.StringVar(&cfg.ClientrawURL, "url", envString("WEATHER_CLIENTRAW_URL", defaultClientrawURL),
		"clientraw.txt URL (primary source)")
	fs.StringVar(&gifURLs, "fallback-url", gifURLs,
		"comma-separated GIF URLs used when the primary source fails")
	fs.BoolVar(&fallback, "gif-fallback", fallback,
		"enable the fixed-layout OCR fallback")
	fs.BoolVar(&verify, "verify-fallback", verify,
		"also scrape the fallback and report numeric deltas")
	fs.DurationVar(&interval, "interval", interval, "scrape interval")
	fs.DurationVar(&export, "export-interval", export, "OTLP export interval")
	fs.DurationVar(&timeout, "timeout", timeout, "per-request HTTP timeout")
	fs.Int64Var(&maxBody, "max-body-bytes", maxBody, "maximum accepted response body size")
	fs.StringVar(&station, "station", station, "station name used as a resource attribute")
	fs.StringVar(&zone, "timezone", zone, "IANA timezone of the station clock")
	fs.StringVar(&level, "log-level", envString("WEATHER_LOG_LEVEL", "info"), "debug|info|warn|error")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if fs.NArg() > 0 {
		return Config{}, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if strings.TrimSpace(cfg.ClientrawURL) == "" {
		return Config{}, errors.New("url must not be empty")
	}
	if interval < 10*time.Second {
		return Config{}, errors.New("interval must be at least 10s (be polite to the station)")
	}
	if export < 5*time.Second {
		return Config{}, errors.New("export-interval must be at least 5s")
	}
	if timeout <= 0 {
		return Config{}, errors.New("timeout must be positive")
	}
	if maxBody < 4096 {
		return Config{}, errors.New("max-body-bytes must be at least 4096")
	}

	loc, err := time.LoadLocation(zone)
	if err != nil {
		return Config{}, fmt.Errorf("load timezone %q: %w", zone, err)
	}

	lvl, err := parseLevel(level)
	if err != nil {
		return Config{}, err
	}

	var urls []string
	for _, u := range strings.Split(gifURLs, ",") {
		if u = strings.TrimSpace(u); u != "" {
			urls = append(urls, u)
		}
	}

	cfg.GIFURLs = urls
	cfg.GIFFallback = fallback && len(urls) > 0
	cfg.VerifyFallback = verify
	cfg.Interval = interval
	cfg.ExportInterval = export
	cfg.HTTPTimeout = timeout
	cfg.MaxBodyBytes = maxBody
	cfg.StationName = station
	cfg.StationTimezone = zone
	cfg.LogLevel = lvl
	cfg.Location = loc

	return cfg, nil
}

func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "", "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown log level %q", s)
	}
}

func envString(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	d, err := time.ParseDuration(strings.TrimSpace(v))
	if err != nil {
		return fallback
	}
	return d
}

func envInt64(key string, fallback int64) int64 {
	v, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	var n int64
	if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &n); err != nil {
		return fallback
	}
	return n
}

// Command weather-otel scrapes the Cumbuco OceanView weather station and
// exports the observations as OpenTelemetry metrics over OTLP/HTTP.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // embed the IANA zone database for scratch/distroless images

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"

	"github.com/eftechcombr/weather/internal/clientraw"
	"github.com/eftechcombr/weather/internal/config"
	"github.com/eftechcombr/weather/internal/gifocr"
	"github.com/eftechcombr/weather/internal/metrics"
	"github.com/eftechcombr/weather/internal/source"
	"github.com/eftechcombr/weather/internal/weather"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

const (
	serviceName  = "cumbuco-weather-otel"
	meterName    = "github.com/eftechcombr/weather"
	shutdownWait = 10 * time.Second
)

// userAgent is a value, not a constant, because version is injected at link
// time.
var userAgent = serviceName + "/" + version

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "weather-otel: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := source.New(cfg.HTTPTimeout, cfg.MaxBodyBytes, userAgent)
	primary := weather.NewClientrawProvider(client, cfg.ClientrawURL, cfg.Location, cfg.StationName)

	var fallback *weather.GIFProvider
	if cfg.GIFFallback {
		fallback, err = weather.NewGIFProvider(client, cfg.GIFURLs, cfg.StationName)
		if err != nil {
			return err
		}
	}

	var provider weather.Provider = primary
	if fallback != nil {
		provider = &weather.Fallback{Primary: primary, Secondary: fallback}
	}

	res, err := buildResource(cfg)
	if err != nil {
		return fmt.Errorf("build resource: %w", err)
	}

	exporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return fmt.Errorf("create OTLP/HTTP metric exporter: %w", err)
	}

	reader := metric.NewPeriodicReader(exporter, metric.WithInterval(cfg.ExportInterval))
	meterProvider := metric.NewMeterProvider(
		metric.WithReader(reader),
		metric.WithResource(res),
	)

	recorder, err := metrics.New(meterProvider.Meter(meterName))
	if err != nil {
		return fmt.Errorf("create metric instruments: %w", err)
	}

	store := weather.NewStore()
	if err := recorder.Start(store); err != nil {
		return fmt.Errorf("register metric callback: %w", err)
	}

	logger.Info("weather exporter started",
		"version", version,
		"station", cfg.StationName,
		"clientraw_url", cfg.ClientrawURL,
		"gif_fallback", cfg.GIFFallback,
		"interval", cfg.Interval.String(),
		"export_interval", cfg.ExportInterval.String(),
	)

	scrapeLoop(ctx, cfg, logger, provider, fallback, store, recorder)

	recorder.Stop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownWait)
	defer cancel()

	var errs []error
	if err := meterProvider.Shutdown(shutdownCtx); err != nil {
		errs = append(errs, fmt.Errorf("shutdown meter provider: %w", err))
	}
	if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func buildResource(cfg config.Config) (*resource.Resource, error) {
	name := os.Getenv("OTEL_SERVICE_NAME")
	if name == "" {
		name = serviceName
	}
	return resource.Merge(resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(name),
			semconv.ServiceVersion(version),
			attribute.String("weather.station", cfg.StationName),
		),
	)
}

func scrapeLoop(
	ctx context.Context,
	cfg config.Config,
	logger *slog.Logger,
	provider weather.Provider,
	fallback *weather.GIFProvider,
	store *weather.Store,
	recorder *metrics.Recorder,
) {
	scrape := func() {
		start := time.Now()
		reading, err := provider.Fetch(ctx)
		elapsed := time.Since(start)

		if err != nil {
			if ctx.Err() != nil {
				return
			}
			store.SetFailure(weather.SourceClientraw, elapsed)
			recorder.RecordFetchError(ctx, errorReason(err))
			logger.Warn("scrape failed", "error", err, "elapsed", elapsed.Round(time.Millisecond))
			return
		}

		store.SetSuccess(reading, elapsed)
		logger.Debug("scrape ok",
			"source", reading.Source,
			"observed_at", reading.ObservedAt,
			"elapsed", elapsed.Round(time.Millisecond),
		)

		if cfg.VerifyFallback && fallback != nil && reading.Source == weather.SourceClientraw {
			if d, derr := verifyFallback(ctx, fallback, reading); derr != nil {
				logger.Warn("fallback verification failed", "error", derr)
			} else {
				recorder.SetVerifyDeltas(d)
			}
		}
	}

	scrape()

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("shutting down")
			return
		case <-ticker.C:
			scrape()
		}
	}
}

// verifyFallback scrapes the GIF panel and compares it with the primary
// reading. It is only used with -verify-fallback, to monitor OCR drift.
func verifyFallback(ctx context.Context, fallback *weather.GIFProvider, primary *weather.Reading) ([]metrics.VerifyDelta, error) {
	secondary, err := fallback.Fetch(ctx)
	if err != nil {
		return nil, err
	}
	return deltas(primary, secondary), nil
}

func deltas(a, b *weather.Reading) []metrics.VerifyDelta {
	var out []metrics.VerifyDelta
	add := func(field, unit string, x, y *float64) {
		if x == nil || y == nil {
			return
		}
		d := *x - *y
		if d < 0 {
			d = -d
		}
		out = append(out, metrics.VerifyDelta{Field: field, Unit: unit, Delta: d})
	}

	add("wind", "kn", a.WindKts, b.WindKts)
	add("wind_average", "kn", a.AverageWindKts, b.AverageWindKts)
	add("gust_max_today", "kn", a.GustMaxTodayKts, b.GustMaxTodayKts)
	add("gust_max_last_hour", "kn", a.GustMaxLastHourKts, b.GustMaxLastHourKts)
	add("max_average_wind", "kn", a.MaxAverageWindKts, b.MaxAverageWindKts)
	add("wind_direction", "deg", a.WindDirDeg, b.WindDirDeg)
	add("temperature", "Cel", a.TemperatureC, b.TemperatureC)
	add("indoor_temperature", "Cel", a.IndoorTemperatureC, b.IndoorTemperatureC)
	add("dew_point", "Cel", a.DewPointC, b.DewPointC)
	add("heat_index", "Cel", a.HeatIndexC, b.HeatIndexC)
	add("temperature_max", "Cel", a.TemperatureMaxC, b.TemperatureMaxC)
	add("temperature_min", "Cel", a.TemperatureMinC, b.TemperatureMinC)
	add("humidity", "%", a.HumidityPct, b.HumidityPct)
	add("indoor_humidity", "%", a.IndoorHumidityPct, b.IndoorHumidityPct)
	add("pressure", "hPa", a.PressureHpa, b.PressureHpa)
	add("pressure_trend", "hPa/h", a.PressureTrendHpaHr, b.PressureTrendHpaHr)
	add("rain_today", "mm", a.RainTodayMm, b.RainTodayMm)
	add("rain_yesterday", "mm", a.RainYesterdayMm, b.RainYesterdayMm)
	add("rain_month", "mm", a.RainMonthMm, b.RainMonthMm)
	add("rain_year", "mm", a.RainYearMm, b.RainYearMm)
	add("rain_rate", "mm/min", a.RainRateMmMin, b.RainRateMmMin)
	return out
}

func errorReason(err error) string {
	var fb *weather.FallbackError
	switch {
	case errors.As(err, &fb):
		return "fallback_failed"
	case errors.Is(err, gifocr.ErrUnsupportedLayout):
		return "layout"
	case errors.Is(err, gifocr.ErrUnreadable):
		return "unreadable"
	case errors.Is(err, source.ErrBodyTooLarge):
		return "body_too_large"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, clientraw.ErrNoObservations):
		return "no_observations"
	default:
		return "parse"
	}
}

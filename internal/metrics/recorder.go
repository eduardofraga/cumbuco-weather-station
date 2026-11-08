// Package metrics maps weather readings onto OpenTelemetry instruments.
package metrics

import (
	"context"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eftechcombr/weather/internal/weather"
)

// Unit strings follow UCUM, as recommended for OpenTelemetry instruments.
const (
	unitKnots       = "[kn_i]"
	unitDegree      = "deg"
	unitCelsius     = "Cel"
	unitPercent     = "%"
	unitHectopascal = "hPa"
	unitMillimetre  = "mm"
	unitMmPerHour   = "mm/h"
	unitHpaPerHour  = "hPa/h"
	unitWattPerM2   = "W/m2"
	unitSecond      = "s"
	unitOne         = "1"
)

// VerifyDelta is a numeric difference between the primary and fallback source
// for one field, used to monitor OCR drift.
type VerifyDelta struct {
	Field string
	Unit  string
	Delta float64
}

// Recorder owns the instruments and the observable callback.
type Recorder struct {
	meter metric.Meter
	store *weather.Store

	windSpeed     metric.Float64ObservableGauge
	windGust      metric.Float64ObservableGauge
	windDirection metric.Float64ObservableGauge
	temperature   metric.Float64ObservableGauge
	feelsLike     metric.Float64ObservableGauge
	humidity      metric.Float64ObservableGauge
	pressure      metric.Float64ObservableGauge
	pressureTrend metric.Float64ObservableGauge
	precipitation metric.Float64ObservableGauge
	precipRate    metric.Float64ObservableGauge
	solar         metric.Float64ObservableGauge
	uvIndex       metric.Float64ObservableGauge
	dataAge       metric.Float64ObservableGauge
	up            metric.Float64ObservableGauge
	fetchDuration metric.Float64ObservableGauge
	fallbackDelta metric.Float64ObservableGauge

	fetchErrors metric.Int64Counter

	registration metric.Registration
	deltas       atomic.Pointer[[]VerifyDelta]
}

// New creates every instrument. Start must be called to begin collection.
func New(meter metric.Meter) (*Recorder, error) {
	r := &Recorder{meter: meter}

	observe := func(name, unit, description string) (metric.Float64ObservableGauge, error) {
		return meter.Float64ObservableGauge(name,
			metric.WithUnit(unit),
			metric.WithDescription(description),
		)
	}

	var err error
	if r.windSpeed, err = observe("weather.wind.speed", unitKnots, "Wind speed in knots"); err != nil {
		return nil, err
	}
	if r.windGust, err = observe("weather.wind.gust", unitKnots, "Maximum gust in knots"); err != nil {
		return nil, err
	}
	if r.windDirection, err = observe("weather.wind.direction", unitDegree, "Wind direction in degrees"); err != nil {
		return nil, err
	}
	if r.temperature, err = observe("weather.temperature", unitCelsius, "Air temperature in degrees Celsius"); err != nil {
		return nil, err
	}
	if r.feelsLike, err = observe("weather.temperature.feels_like", unitCelsius, "Derived apparent temperatures"); err != nil {
		return nil, err
	}
	if r.humidity, err = observe("weather.humidity", unitPercent, "Relative humidity"); err != nil {
		return nil, err
	}
	if r.pressure, err = observe("weather.pressure", unitHectopascal, "Barometric pressure"); err != nil {
		return nil, err
	}
	if r.pressureTrend, err = observe("weather.pressure.trend", unitHpaPerHour, "Barometric pressure trend"); err != nil {
		return nil, err
	}
	if r.precipitation, err = observe("weather.precipitation", unitMillimetre, "Accumulated rain"); err != nil {
		return nil, err
	}
	if r.precipRate, err = observe("weather.precipitation.rate", unitMmPerHour, "Rain rate"); err != nil {
		return nil, err
	}
	if r.solar, err = observe("weather.solar.radiation", unitWattPerM2, "Solar radiation"); err != nil {
		return nil, err
	}
	if r.uvIndex, err = observe("weather.uv.index", unitOne, "UV index"); err != nil {
		return nil, err
	}
	if r.dataAge, err = observe("weather.station.data_age", unitSecond, "Seconds since the station observation"); err != nil {
		return nil, err
	}
	if r.up, err = observe("weather.station.up", unitOne, "1 when the last scrape succeeded, 0 otherwise"); err != nil {
		return nil, err
	}
	if r.fetchDuration, err = observe("weather.fetch.duration", unitSecond, "Duration of the last scrape"); err != nil {
		return nil, err
	}
	if r.fallbackDelta, err = observe("weather.fallback.delta", unitOne, "Numeric difference between primary and fallback source (verify mode)"); err != nil {
		return nil, err
	}

	r.fetchErrors, err = meter.Int64Counter("weather.fetch.errors",
		metric.WithUnit(unitOne),
		metric.WithDescription("Failed scrapes by reason"),
	)
	if err != nil {
		return nil, err
	}

	return r, nil
}

// Start registers the observable callback.
func (r *Recorder) Start(store *weather.Store) error {
	r.store = store
	reg, err := r.meter.RegisterCallback(r.collect,
		r.windSpeed, r.windGust, r.windDirection, r.temperature, r.feelsLike,
		r.humidity, r.pressure, r.pressureTrend, r.precipitation, r.precipRate,
		r.solar, r.uvIndex, r.dataAge, r.up, r.fetchDuration, r.fallbackDelta,
	)
	if err != nil {
		return err
	}
	r.registration = reg
	return nil
}

// Stop unregisters the callback.
func (r *Recorder) Stop() {
	if r.registration != nil {
		_ = r.registration.Unregister()
	}
}

// RecordFetchError increments the error counter.
func (r *Recorder) RecordFetchError(ctx context.Context, reason string) {
	if r.fetchErrors == nil {
		return
	}
	r.fetchErrors.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
}

// SetVerifyDeltas publishes the latest fallback comparison values.
func (r *Recorder) SetVerifyDeltas(d []VerifyDelta) {
	r.deltas.Store(&d)
}

func (r *Recorder) collect(_ context.Context, o metric.Observer) error {
	if reading, ok := r.store.Reading(); ok {
		src := attribute.String("source", reading.Source)

		r.currentAnd(o, r.windSpeed, reading.WindKts, "aggregation", "current", src)
		r.currentAnd(o, r.windSpeed, reading.AverageWindKts, "aggregation", "average_1m", src)
		r.currentAnd(o, r.windGust, reading.GustMaxTodayKts, "window", "max_today", src)
		r.currentAnd(o, r.windGust, reading.GustMaxLastHourKts, "window", "max_last_hour", src)
		r.currentAnd(o, r.windDirection, reading.WindDirDeg, "aggregation", "current", src)
		r.currentAnd(o, r.windDirection, reading.WindDirAverageDeg, "aggregation", "average", src)

		r.currentAnd(o, r.temperature, reading.TemperatureC, "aggregation", "current", src)
		r.currentAnd(o, r.temperature, reading.TemperatureMaxC, "aggregation", "max_today", src)
		r.currentAnd(o, r.temperature, reading.TemperatureMinC, "aggregation", "min_today", src)
		r.currentAnd(o, r.temperature, reading.IndoorTemperatureC, "sensor", "indoor", src)

		r.currentAnd(o, r.feelsLike, reading.DewPointC, "kind", "dew_point", src)
		r.currentAnd(o, r.feelsLike, reading.HeatIndexC, "kind", "heat_index", src)
		r.currentAnd(o, r.feelsLike, reading.WindChillC, "kind", "wind_chill", src)
		r.currentAnd(o, r.feelsLike, reading.HumidexC, "kind", "humidex", src)
		r.currentAnd(o, r.feelsLike, reading.ApparentTemperatureC, "kind", "apparent", src)

		r.currentAnd(o, r.humidity, reading.HumidityPct, "sensor", "outdoor", src)
		r.currentAnd(o, r.humidity, reading.IndoorHumidityPct, "sensor", "indoor", src)

		r.currentAnd(o, r.pressure, reading.PressureHpa, "aggregation", "current", src)
		r.currentAnd(o, r.pressure, reading.PressureMaxHpa, "aggregation", "max_today", src)
		r.currentAnd(o, r.pressure, reading.PressureMinHpa, "aggregation", "min_today", src)
		r.currentAnd(o, r.pressureTrend, reading.PressureTrendHpaHr, "", "", src)

		r.currentAnd(o, r.precipitation, reading.RainTodayMm, "period", "today", src)
		r.currentAnd(o, r.precipitation, reading.RainYesterdayMm, "period", "yesterday", src)
		r.currentAnd(o, r.precipitation, reading.RainMonthMm, "period", "month", src)
		r.currentAnd(o, r.precipitation, reading.RainYearMm, "period", "year", src)

		if reading.RainRateMmMin != nil {
			o.ObserveFloat64(r.precipRate, *reading.RainRateMmMin*60,
				metric.WithAttributes(src, attribute.String("aggregation", "current")))
		}
		if reading.RainRateMaxMmMin != nil {
			o.ObserveFloat64(r.precipRate, *reading.RainRateMaxMmMin*60,
				metric.WithAttributes(src, attribute.String("aggregation", "max")))
		}

		r.currentAnd(o, r.solar, reading.SolarWm2, "", "", src)
		r.currentAnd(o, r.uvIndex, reading.UVIndex, "", "", src)

		if !reading.ObservedAt.IsZero() {
			age := time.Since(reading.ObservedAt).Seconds()
			if age < 0 {
				age = 0
			}
			o.ObserveFloat64(r.dataAge, age, metric.WithAttributes(src))
		}
	}

	if src := r.store.Source(); src != "" {
		up := 0.0
		if r.store.Up() {
			up = 1.0
		}
		o.ObserveFloat64(r.up, up, metric.WithAttributes(attribute.String("source", src)))
	}

	if !r.store.LastFetch().IsZero() {
		o.ObserveFloat64(r.fetchDuration, r.store.FetchSeconds(),
			metric.WithAttributes(attribute.String("source", r.store.Source())))
	}

	if deltas := r.deltas.Load(); deltas != nil {
		for _, d := range *deltas {
			o.ObserveFloat64(r.fallbackDelta, d.Delta, metric.WithAttributes(
				attribute.String("field", d.Field),
				attribute.String("unit", d.Unit),
			))
		}
	}

	return nil
}

// currentAnd observes one optional value with a single attributes pair.
func (r *Recorder) currentAnd(
	o metric.Observer,
	inst metric.Float64ObservableGauge,
	v *float64,
	key, value string,
	src attribute.KeyValue,
) {
	if v == nil {
		return
	}
	attrs := []attribute.KeyValue{src}
	if key != "" {
		attrs = append(attrs, attribute.String(key, value))
	}
	o.ObserveFloat64(inst, *v, metric.WithAttributes(attrs...))
}

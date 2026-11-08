package metrics

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eftechcombr/weather/internal/weather"
)

func newRecorder(t *testing.T) (*Recorder, *weather.Store, *metric.ManualReader) {
	t.Helper()
	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	rec, err := New(mp.Meter("test"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	store := weather.NewStore()
	if err := rec.Start(store); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(rec.Stop)
	return rec, store, reader
}

func collect(t *testing.T, reader *metric.ManualReader) map[string]metricdata.Metrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	out := make(map[string]metricdata.Metrics)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			out[m.Name] = m
		}
	}
	return out
}

func floatGauge(t *testing.T, metrics map[string]metricdata.Metrics, name string) metricdata.Gauge[float64] {
	t.Helper()
	m, ok := metrics[name]
	if !ok {
		t.Fatalf("metric %q not found", name)
	}
	g, ok := m.Data.(metricdata.Gauge[float64])
	if !ok {
		t.Fatalf("metric %q data = %T, want Gauge[float64]", name, m.Data)
	}
	return g
}

func pointByAttr(t *testing.T, g metricdata.Gauge[float64], key, value string) metricdata.DataPoint[float64] {
	t.Helper()
	for _, p := range g.DataPoints {
		if v, ok := p.Attributes.Value(attribute.Key(key)); ok && v.AsString() == value {
			return p
		}
	}
	for _, p := range g.DataPoints {
		t.Logf("point: %v", p.Attributes.ToSlice())
	}
	t.Fatalf("no data point with %s=%s", key, value)
	return metricdata.DataPoint[float64]{}
}

func f(v float64) *float64 { return &v }

func TestCollectReading(t *testing.T) {
	_, store, reader := newRecorder(t)

	store.SetSuccess(&weather.Reading{
		Source:          weather.SourceClientraw,
		ObservedAt:      time.Now().Add(-5 * time.Second),
		AverageWindKts:  f(10.3),
		WindKts:         f(11.5),
		WindDirDeg:      f(129),
		GustMaxTodayKts: f(22),
		TemperatureC:    f(27.8),
		PressureHpa:     f(1011.8),
		HumidityPct:     f(77),
		RainTodayMm:     f(0),
		RainRateMmMin:   f(0.5),
	}, 120*time.Millisecond)

	got := collect(t, reader)

	if m := got["weather.wind.speed"]; m.Unit != "[kn_i]" {
		t.Errorf("wind speed unit = %q, want [kn_i]", m.Unit)
	}
	wind := floatGauge(t, got, "weather.wind.speed")
	if p := pointByAttr(t, wind, "aggregation", "current"); p.Value != 11.5 {
		t.Errorf("current wind = %v, want 11.5", p.Value)
	}
	if p := pointByAttr(t, wind, "aggregation", "average_1m"); p.Value != 10.3 {
		t.Errorf("average wind = %v, want 10.3", p.Value)
	}

	// Rain rate is stored in mm/min and exported in mm/h.
	rate := floatGauge(t, got, "weather.precipitation.rate")
	if p := pointByAttr(t, rate, "aggregation", "current"); p.Value != 30 {
		t.Errorf("rain rate = %v, want 30", p.Value)
	}

	up := floatGauge(t, got, "weather.station.up")
	if p := pointByAttr(t, up, "source", weather.SourceClientraw); p.Value != 1 {
		t.Errorf("station.up = %v, want 1", p.Value)
	}

	age := floatGauge(t, got, "weather.station.data_age")
	if p := pointByAttr(t, age, "source", weather.SourceClientraw); p.Value < 4 || p.Value > 60 {
		t.Errorf("data_age = %v, want about 5", p.Value)
	}
}

func TestCollectOmitsMissingFields(t *testing.T) {
	_, store, reader := newRecorder(t)
	store.SetSuccess(&weather.Reading{
		Source:     weather.SourceGIF,
		ObservedAt: time.Now(),
		WindKts:    f(8.6),
	}, 10*time.Millisecond)

	got := collect(t, reader)
	wind := floatGauge(t, got, "weather.wind.speed")
	for _, p := range wind.DataPoints {
		if _, ok := p.Attributes.Value("aggregation"); ok {
			if v, _ := p.Attributes.Value("aggregation"); v.AsString() == "average_1m" {
				t.Fatalf("average wind exported although the reading omits it")
			}
		}
	}
	if m, ok := got["weather.temperature"]; ok {
		g, _ := m.Data.(metricdata.Gauge[float64])
		if len(g.DataPoints) != 0 {
			t.Errorf("temperature points = %d, want 0", len(g.DataPoints))
		}
	}
}

func TestFailureKeepsLastReadingAndReportsDown(t *testing.T) {
	_, store, reader := newRecorder(t)
	store.SetSuccess(&weather.Reading{
		Source:     weather.SourceClientraw,
		ObservedAt: time.Now(),
		WindKts:    f(5),
	}, 10*time.Millisecond)
	store.SetFailure(weather.SourceClientraw, 15*time.Millisecond)

	got := collect(t, reader)
	wind := floatGauge(t, got, "weather.wind.speed")
	if len(wind.DataPoints) != 1 {
		t.Errorf("wind points = %d, want 1 (last good reading retained)", len(wind.DataPoints))
	}
	up := floatGauge(t, got, "weather.station.up")
	if p := pointByAttr(t, up, "source", weather.SourceClientraw); p.Value != 0 {
		t.Errorf("station.up = %v, want 0", p.Value)
	}
}

func TestFetchErrorCounter(t *testing.T) {
	rec, _, reader := newRecorder(t)
	rec.RecordFetchError(context.Background(), "parse")
	rec.RecordFetchError(context.Background(), "parse")
	rec.RecordFetchError(context.Background(), "timeout")

	got := collect(t, reader)
	m, ok := got["weather.fetch.errors"]
	if !ok {
		t.Fatal("weather.fetch.errors not found")
	}
	sum, ok := m.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("data = %T, want Sum[int64]", m.Data)
	}
	total := map[string]int64{}
	for _, p := range sum.DataPoints {
		v, _ := p.Attributes.Value("reason")
		total[v.AsString()] += p.Value
	}
	if total["parse"] != 2 || total["timeout"] != 1 {
		t.Errorf("counts = %v, want parse=2 timeout=1", total)
	}
}

func TestVerifyDeltas(t *testing.T) {
	rec, _, reader := newRecorder(t)
	rec.SetVerifyDeltas([]VerifyDelta{{Field: "pressure", Unit: "hPa", Delta: 0.2}})

	got := collect(t, reader)
	g := floatGauge(t, got, "weather.fallback.delta")
	if len(g.DataPoints) != 1 {
		t.Fatalf("points = %d, want 1", len(g.DataPoints))
	}
	if g.DataPoints[0].Value != 0.2 {
		t.Errorf("delta = %v, want 0.2", g.DataPoints[0].Value)
	}
}

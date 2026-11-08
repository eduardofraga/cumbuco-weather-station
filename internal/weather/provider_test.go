package weather

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/eftechcombr/weather/internal/source"
)

type fakeProvider struct {
	name    string
	reading *Reading
	err     error
	calls   int
}

func (p *fakeProvider) Name() string { return p.name }
func (p *fakeProvider) Fetch(context.Context) (*Reading, error) {
	p.calls++
	return p.reading, p.err
}

func TestFallbackUsesPrimary(t *testing.T) {
	primary := &fakeProvider{name: SourceClientraw, reading: &Reading{Source: SourceClientraw}}
	secondary := &fakeProvider{name: SourceGIF, reading: &Reading{Source: SourceGIF}}

	got, err := (&Fallback{Primary: primary, Secondary: secondary}).Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got.Source != SourceClientraw {
		t.Errorf("source = %q, want clientraw", got.Source)
	}
	if secondary.calls != 0 {
		t.Errorf("secondary calls = %d, want 0", secondary.calls)
	}
}

func TestFallbackUsesSecondary(t *testing.T) {
	primary := &fakeProvider{name: SourceClientraw, err: errors.New("boom")}
	secondary := &fakeProvider{name: SourceGIF, reading: &Reading{Source: SourceGIF}}

	got, err := (&Fallback{Primary: primary, Secondary: secondary}).Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got.Source != SourceGIF {
		t.Errorf("source = %q, want gif", got.Source)
	}
}

func TestFallbackReportsBothErrors(t *testing.T) {
	primary := &fakeProvider{name: SourceClientraw, err: errors.New("primary boom")}
	secondary := &fakeProvider{name: SourceGIF, err: errors.New("secondary boom")}

	_, err := (&Fallback{Primary: primary, Secondary: secondary}).Fetch(context.Background())
	var fb *FallbackError
	if !errors.As(err, &fb) {
		t.Fatalf("error = %v, want *FallbackError", err)
	}
	if !errors.Is(err, primary.err) {
		t.Errorf("error does not wrap the primary error")
	}
}

func TestStoreRetainsLastReadingOnFailure(t *testing.T) {
	s := NewStore()
	if _, ok := s.Reading(); ok {
		t.Fatal("empty store returned a reading")
	}

	r := &Reading{Source: SourceClientraw, WindKts: ptr(5)}
	s.SetSuccess(r, 10*time.Millisecond)
	if !s.Up() || s.Source() != SourceClientraw || s.FetchSeconds() != 0.01 {
		t.Fatalf("unexpected success state: up=%v source=%q fetch=%v", s.Up(), s.Source(), s.FetchSeconds())
	}

	s.SetFailure(SourceClientraw, 20*time.Millisecond)
	if s.Up() {
		t.Error("Up() = true after failure")
	}
	if got, ok := s.Reading(); !ok || got != r {
		t.Error("Reading() did not retain the last good snapshot")
	}
	if s.FetchSeconds() != 0.02 {
		t.Errorf("FetchSeconds() = %v, want 0.02", s.FetchSeconds())
	}
}

func ptr(v float64) *float64 { return &v }

func newTestClient() *source.Client {
	c := source.New(2*time.Second, 1<<20, "weather-test")
	c.BaseDelay = time.Millisecond
	return c
}

func TestClientrawProviderCaches304(t *testing.T) {
	body, err := os.ReadFile("../clientraw/testdata/clientraw.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	lm := time.Date(2026, time.November, 7, 19, 2, 0, 0, time.UTC)

	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests > 1 {
			if r.Header.Get("If-Modified-Since") == "" {
				t.Error("second request did not send If-Modified-Since")
			}
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Last-Modified", lm.Format(http.TimeFormat))
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	loc, err := time.LoadLocation("America/Fortaleza")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	p := NewClientrawProvider(newTestClient(), srv.URL, loc, "Cumbuco")

	first, err := p.Fetch(context.Background())
	if err != nil {
		t.Fatalf("first Fetch: %v", err)
	}
	if first.Source != SourceClientraw {
		t.Errorf("source = %q, want clientraw", first.Source)
	}
	if first.WindKts == nil || *first.WindKts != 9.6 {
		t.Errorf("wind = %v, want 9.6", first.WindKts)
	}

	second, err := p.Fetch(context.Background())
	if err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	if second.WindKts == nil || *second.WindKts != 9.6 {
		t.Errorf("cached wind = %v, want 9.6", second.WindKts)
	}
}

func TestGIFProvider(t *testing.T) {
	gifData, err := os.ReadFile("../gifocr/testdata/reference.gif")
	if err != nil {
		t.Fatalf("read reference.gif: %v", err)
	}
	lm := time.Date(2026, time.November, 7, 19, 2, 0, 0, time.UTC)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Last-Modified", lm.Format(http.TimeFormat))
		w.Header().Set("Content-Type", "image/gif")
		_, _ = w.Write(gifData)
	}))
	defer srv.Close()

	p, err := NewGIFProvider(newTestClient(), []string{srv.URL}, "Cumbuco")
	if err != nil {
		t.Fatalf("NewGIFProvider: %v", err)
	}

	r, err := p.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if r.Source != SourceGIF {
		t.Errorf("source = %q, want gif", r.Source)
	}
	if !r.ObservedAt.Equal(lm) {
		t.Errorf("ObservedAt = %v, want %v", r.ObservedAt, lm)
	}
	if r.WindKts == nil || *r.WindKts != 8.6 {
		t.Errorf("wind = %v, want 8.6", r.WindKts)
	}
	if r.TemperatureC == nil || *r.TemperatureC != 27.8 {
		t.Errorf("temperature = %v, want 27.8", r.TemperatureC)
	}
	if r.PressureHpa == nil || *r.PressureHpa != 1011.8 {
		t.Errorf("pressure = %v, want 1011.8", r.PressureHpa)
	}
	if r.UVIndex != nil || r.SolarWm2 != nil {
		t.Error("GIF reading must not invent UV/solar values")
	}
}

func TestGIFProviderTriesNextURL(t *testing.T) {
	gifData, err := os.ReadFile("../gifocr/testdata/reference.gif")
	if err != nil {
		t.Fatalf("read reference.gif: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing.gif" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(gifData)
	}))
	defer srv.Close()

	p, err := NewGIFProvider(newTestClient(), []string{srv.URL + "/missing.gif", srv.URL + "/index.gif"}, "Cumbuco")
	if err != nil {
		t.Fatalf("NewGIFProvider: %v", err)
	}
	if _, err := p.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
}

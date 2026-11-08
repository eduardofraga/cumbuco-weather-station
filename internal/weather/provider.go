// Package weather defines the observation model shared by all data sources and
// the provider abstraction used by the scrape loop.
package weather

import (
	"context"
	"math"
	"sync/atomic"
	"time"
)

// Source identifies where a reading came from.
const (
	SourceClientraw = "clientraw"
	SourceGIF       = "gif"
)

// Reading is an immutable snapshot of the station's observations. Optional
// fields are pointers so a source can omit values it could not read instead of
// exporting a sentinel.
type Reading struct {
	Source      string
	StationName string
	ObservedAt  time.Time // station observation time (or image timestamp)
	FetchedAt   time.Time

	AverageWindKts       *float64
	WindKts              *float64
	WindDirDeg           *float64
	WindDirAverageDeg    *float64
	GustMaxTodayKts      *float64
	GustMaxLastHourKts   *float64
	MaxAverageWindKts    *float64
	TemperatureC         *float64
	IndoorTemperatureC   *float64
	DewPointC            *float64
	WindChillC           *float64
	HumidexC             *float64
	HeatIndexC           *float64
	ApparentTemperatureC *float64
	TemperatureMaxC      *float64
	TemperatureMinC      *float64
	HumidityPct          *float64
	IndoorHumidityPct    *float64
	PressureHpa          *float64
	PressureMaxHpa       *float64
	PressureMinHpa       *float64
	PressureTrendHpaHr   *float64
	RainTodayMm          *float64
	RainYesterdayMm      *float64
	RainMonthMm          *float64
	RainYearMm           *float64
	RainRateMmMin        *float64
	RainRateMaxMmMin     *float64
	UVIndex              *float64
	SolarWm2             *float64
}

// Provider fetches one reading from a source.
type Provider interface {
	Name() string
	Fetch(ctx context.Context) (*Reading, error)
}

// Store holds the latest successful reading and fetch health counters.
// Readers are lock-free.
type Store struct {
	reading      atomic.Pointer[Reading]
	up           atomic.Bool
	source       atomic.Value  // string
	fetchSeconds atomic.Uint64 // math.Float64bits
	lastFetch    atomic.Int64  // unix nanos
}

// NewStore returns an empty Store.
func NewStore() *Store { return &Store{} }

// SetSuccess stores a successful reading.
func (s *Store) SetSuccess(r *Reading, elapsed time.Duration) {
	s.reading.Store(r)
	s.up.Store(true)
	s.source.Store(r.Source)
	s.setFetch(elapsed)
}

// SetFailure records a failed attempt without discarding the last good
// reading.
func (s *Store) SetFailure(source string, elapsed time.Duration) {
	s.up.Store(false)
	if source != "" {
		s.source.Store(source)
	}
	s.setFetch(elapsed)
}

func (s *Store) setFetch(elapsed time.Duration) {
	s.fetchSeconds.Store(math.Float64bits(elapsed.Seconds()))
	s.lastFetch.Store(time.Now().UnixNano())
}

// Reading returns the last successful reading, if any.
func (s *Store) Reading() (*Reading, bool) {
	r := s.reading.Load()
	return r, r != nil
}

// Up reports whether the most recent attempt succeeded.
func (s *Store) Up() bool { return s.up.Load() }

// Source returns the source of the most recent attempt.
func (s *Store) Source() string {
	v, _ := s.source.Load().(string)
	return v
}

// FetchSeconds returns the duration of the most recent attempt.
func (s *Store) FetchSeconds() float64 {
	return math.Float64frombits(s.fetchSeconds.Load())
}

// LastFetch returns the time of the most recent attempt.
func (s *Store) LastFetch() time.Time {
	n := s.lastFetch.Load()
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n)
}

// Fallback tries Primary and, on failure, Secondary.
type Fallback struct {
	Primary   Provider
	Secondary Provider
}

// Name implements Provider.
func (f *Fallback) Name() string { return f.Primary.Name() }

// Fetch returns the first successful reading.
func (f *Fallback) Fetch(ctx context.Context) (*Reading, error) {
	r, primaryErr := f.Primary.Fetch(ctx)
	if primaryErr == nil {
		return r, nil
	}
	if f.Secondary == nil {
		return nil, primaryErr
	}
	r, secondaryErr := f.Secondary.Fetch(ctx)
	if secondaryErr == nil {
		return r, nil
	}
	return nil, &FallbackError{Primary: primaryErr, Secondary: secondaryErr}
}

// FallbackError reports both failures.
type FallbackError struct {
	Primary   error
	Secondary error
}

func (e *FallbackError) Error() string {
	return "primary source: " + e.Primary.Error() + "; fallback source: " + e.Secondary.Error()
}

func (e *FallbackError) Unwrap() error { return e.Primary }

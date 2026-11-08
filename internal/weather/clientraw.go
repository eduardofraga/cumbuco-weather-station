package weather

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/eftechcombr/weather/internal/clientraw"
	"github.com/eftechcombr/weather/internal/source"
)

// ClientrawProvider fetches and parses clientraw.txt, reusing the last parsed
// body when the server answers 304 Not Modified.
type ClientrawProvider struct {
	client  *source.Client
	url     string
	loc     *time.Location
	station string

	mu          sync.Mutex
	lastMod     time.Time
	cached      *clientraw.Data
	cachedFetch time.Time
}

// NewClientrawProvider builds the primary provider.
func NewClientrawProvider(client *source.Client, url string, loc *time.Location, station string) *ClientrawProvider {
	if loc == nil {
		loc = time.UTC
	}
	return &ClientrawProvider{client: client, url: url, loc: loc, station: station}
}

// Name implements Provider.
func (p *ClientrawProvider) Name() string { return SourceClientraw }

// Fetch implements Provider.
func (p *ClientrawProvider) Fetch(ctx context.Context) (*Reading, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	res, err := p.client.Get(ctx, p.url, p.lastMod)
	if err != nil {
		return nil, fmt.Errorf("clientraw fetch: %w", err)
	}

	if res.NotModified {
		if p.cached == nil {
			return nil, errors.New("clientraw: 304 Not Modified without a cached body")
		}
		return p.reading(p.cached, p.cachedFetch), nil
	}

	data, err := clientraw.Parse(res.Body, p.loc)
	if err != nil {
		return nil, err
	}
	if !res.LastModified.IsZero() {
		p.lastMod = res.LastModified
	}
	p.cached = data
	p.cachedFetch = res.FetchedAt

	return p.reading(data, res.FetchedAt), nil
}

func (p *ClientrawProvider) reading(d *clientraw.Data, fetched time.Time) *Reading {
	name := d.StationName
	if name == "" {
		name = p.station
	}
	observed := d.ObservedAt
	if observed.IsZero() {
		observed = fetched
	}
	return &Reading{
		Source:               SourceClientraw,
		StationName:          name,
		ObservedAt:           observed,
		FetchedAt:            fetched,
		AverageWindKts:       d.AverageWindKts,
		WindKts:              d.WindKts,
		WindDirDeg:           d.WindDirDeg,
		WindDirAverageDeg:    d.WindDirAverageDeg,
		GustMaxTodayKts:      d.GustMaxTodayKts,
		GustMaxLastHourKts:   d.GustMaxLastHourKts,
		MaxAverageWindKts:    d.MaxAverageWindKts,
		TemperatureC:         d.TemperatureC,
		IndoorTemperatureC:   d.IndoorTemperatureC,
		DewPointC:            d.DewPointC,
		WindChillC:           d.WindChillC,
		HumidexC:             d.HumidexC,
		HeatIndexC:           d.HeatIndexC,
		ApparentTemperatureC: d.ApparentTemperatureC,
		TemperatureMaxC:      d.TemperatureMaxC,
		TemperatureMinC:      d.TemperatureMinC,
		HumidityPct:          d.HumidityPct,
		IndoorHumidityPct:    d.IndoorHumidityPct,
		PressureHpa:          d.PressureHpa,
		PressureMaxHpa:       d.PressureMaxHpa,
		PressureMinHpa:       d.PressureMinHpa,
		PressureTrendHpaHr:   d.PressureTrendHpaHr,
		RainTodayMm:          d.RainTodayMm,
		RainYesterdayMm:      d.RainYesterdayMm,
		RainMonthMm:          d.RainMonthMm,
		RainYearMm:           d.RainYearMm,
		RainRateMmMin:        d.RainRateMmMin,
		RainRateMaxMmMin:     d.RainRateMaxMmMin,
		UVIndex:              d.UVIndex,
		SolarWm2:             d.SolarWm2,
	}
}

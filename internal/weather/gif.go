package weather

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eftechcombr/weather/internal/gifocr"
	"github.com/eftechcombr/weather/internal/source"
)

// GIFProvider reads the core values from the station's rendered index.gif using
// the fixed-layout OCR in internal/gifocr. It is only used when the primary
// text feed is unavailable, and deliberately omits fields the panel does not
// render (UV, solar radiation, wind chill, humidex, apparent temperature).
type GIFProvider struct {
	client  *source.Client
	urls    []string
	station string
	atlas   *gifocr.Atlas
}

// NewGIFProvider builds the fallback provider. The glyph atlas is embedded in
// the binary, so construction fails only if the embedded data is corrupt.
func NewGIFProvider(client *source.Client, urls []string, station string) (*GIFProvider, error) {
	if len(urls) == 0 {
		return nil, errors.New("gif provider: no URLs configured")
	}
	if client == nil {
		return nil, errors.New("gif provider: nil client")
	}
	atlas, err := gifocr.DefaultAtlas()
	if err != nil {
		return nil, fmt.Errorf("gif provider: %w", err)
	}
	return &GIFProvider{client: client, urls: urls, station: station, atlas: atlas}, nil
}

// Name implements Provider.
func (p *GIFProvider) Name() string { return SourceGIF }

// Fetch implements Provider. Each configured URL is tried in order; the first
// image that decodes and yields a usable reading wins.
func (p *GIFProvider) Fetch(ctx context.Context) (*Reading, error) {
	var errs []error
	for _, url := range p.urls {
		res, err := p.client.Get(ctx, url, time.Time{})
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", url, err))
			continue
		}
		if len(res.Body) == 0 {
			errs = append(errs, fmt.Errorf("%s: empty body", url))
			continue
		}

		img, err := gifocr.DecodeGIF(res.Body)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", url, err))
			continue
		}
		obs, _, err := gifocr.Recognize(img, p.atlas)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", url, err))
			continue
		}

		observed := res.LastModified
		if observed.IsZero() {
			observed = res.FetchedAt
		}
		return p.reading(obs, observed, res.FetchedAt), nil
	}
	return nil, fmt.Errorf("gif: no usable image: %w", errors.Join(errs...))
}

func (p *GIFProvider) reading(o *gifocr.Observation, observed, fetched time.Time) *Reading {
	return &Reading{
		Source:             SourceGIF,
		StationName:        p.station,
		ObservedAt:         observed,
		FetchedAt:          fetched,
		AverageWindKts:     o.AverageWindKts,
		WindKts:            o.WindKts,
		WindDirDeg:         o.WindDirDeg,
		GustMaxTodayKts:    o.GustMaxTodayKts,
		GustMaxLastHourKts: o.GustMaxLastHourKts,
		MaxAverageWindKts:  o.MaxAverageWindKts,
		TemperatureC:       o.TemperatureC,
		IndoorTemperatureC: o.IndoorTemperatureC,
		DewPointC:          o.DewPointC,
		HeatIndexC:         o.HeatIndexC,
		TemperatureMaxC:    o.TemperatureMaxC,
		TemperatureMinC:    o.TemperatureMinC,
		HumidityPct:        o.HumidityPct,
		IndoorHumidityPct:  o.IndoorHumidityPct,
		PressureHpa:        o.PressureHpa,
		PressureTrendHpaHr: o.PressureTrendHpaHr,
		RainTodayMm:        o.RainTodayMm,
		RainYesterdayMm:    o.RainYesterdayMm,
		RainMonthMm:        o.RainMonthMm,
		RainYearMm:         o.RainYearMm,
		RainRateMmMin:      o.RainRateMmMin,
	}
}

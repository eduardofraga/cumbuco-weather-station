package gifocr

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"strconv"
)

//go:embed glyphs.json
var defaultAtlasJSON []byte

// ErrUnsupportedLayout is returned when the image is not the expected size.
var ErrUnsupportedLayout = errors.New("gifocr: unsupported image layout")

// ErrUnreadable is returned when too few core values could be recognised.
var ErrUnreadable = errors.New("gifocr: not enough readable values")

// DefaultAtlas returns the embedded glyph atlas.
func DefaultAtlas() (*Atlas, error) { return LoadSlice(defaultAtlasJSON) }

// Observation holds the values that can be recovered from the rendered panel.
type Observation struct {
	AverageWindKts     *float64
	WindKts            *float64
	WindDirDeg         *float64
	GustMaxTodayKts    *float64
	GustMaxLastHourKts *float64
	MaxAverageWindKts  *float64
	TemperatureC       *float64
	TemperatureMaxC    *float64
	TemperatureMinC    *float64
	IndoorTemperatureC *float64
	DewPointC          *float64
	HeatIndexC         *float64
	HumidityPct        *float64
	IndoorHumidityPct  *float64
	PressureHpa        *float64
	PressureTrendHpaHr *float64
	RainTodayMm        *float64
	RainYesterdayMm    *float64
	RainMonthMm        *float64
	RainYearMm         *float64
	RainRateMmMin      *float64
}

// DecodeGIF decodes the first frame of a GIF.
func DecodeGIF(data []byte) (image.Image, error) {
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode gif: %w", err)
	}
	if len(g.Image) == 0 {
		return nil, errors.New("decode gif: no frames")
	}
	return g.Image[0], nil
}

// Recognize extracts values from the image. texts maps each region to the
// recognised glyph sequence ("?" marks unrecognised glyphs) and is useful for
// diagnostics and tests.
func Recognize(img image.Image, atlas *Atlas) (*Observation, map[string]string, error) {
	if img.Bounds().Dx() != LayoutWidth || img.Bounds().Dy() != LayoutHeight {
		return nil, nil, fmt.Errorf("%w: got %dx%d, want %dx%d",
			ErrUnsupportedLayout, img.Bounds().Dx(), img.Bounds().Dy(), LayoutWidth, LayoutHeight)
	}
	if atlas == nil {
		return nil, nil, errors.New("gifocr: nil atlas")
	}

	obs := &Observation{}
	texts := make(map[string]string, len(Regions))
	recognised := 0

	for _, region := range Regions {
		glyphs := regionGlyphs(img, atlas, region)
		texts[region.Name] = string(glyphs)

		raw, ok := extract(glyphs, region.Extract)
		if !ok {
			continue
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			continue
		}
		if assign(obs, region.Name, v) {
			recognised++
		}
	}

	if !hasCore(obs) || recognised < 6 {
		return obs, texts, fmt.Errorf("%w: %d values", ErrUnreadable, recognised)
	}
	return obs, texts, nil
}

// RegionValue returns the numeric text extracted from a single region and the
// full recognised glyph sequence ("?" marks unrecognised glyphs). Atlas
// generation uses it to verify that each learned region reproduces its
// expected value.
func RegionValue(img image.Image, atlas *Atlas, region Region) (value, text string, ok bool) {
	glyphs := regionGlyphs(img, atlas, region)
	raw, ok := extract(glyphs, region.Extract)
	return raw, string(glyphs), ok
}

func regionGlyphs(img image.Image, atlas *Atlas, region Region) []rune {
	segments := Segment(img, region)
	glyphs := make([]rune, 0, len(segments))
	for _, seg := range segments {
		if r, ok := atlas.Match(seg); ok {
			glyphs = append(glyphs, r)
		} else {
			glyphs = append(glyphs, '?')
		}
	}
	return glyphs
}

func extract(runes []rune, mode ExtractMode) (string, bool) {
	switch mode {
	case LastNumber:
		return lastNumber(runes)
	default:
		return firstNumber(runes)
	}
}

func firstNumber(rs []rune) (string, bool) {
	i := 0
	for i < len(rs) && rs[i] == '?' {
		i++
	}
	if i >= len(rs) {
		return "", false
	}
	j := i
	if j < len(rs) && (rs[j] == '-' || rs[j] == '+') {
		j++
	}
	if !collectNumber(rs, &j) {
		return "", false
	}
	return string(rs[i:j]), true
}

func lastNumber(rs []rune) (string, bool) {
	i := len(rs) - 1
	for i >= 0 && rs[i] == '?' {
		i--
	}
	if i < 0 {
		return "", false
	}
	end := i + 1
	if !collectNumberBackward(rs, &i) {
		return "", false
	}
	start := i + 1
	if start > 0 && (rs[start-1] == '-' || rs[start-1] == '+') {
		start--
	}
	return string(rs[start:end]), true
}

// collectNumberBackward moves *i backwards over digits and at most one decimal
// point. On return *i is the rune before the number (or -1) and the caller can
// use *i+1 as the start index.
func collectNumberBackward(rs []rune, i *int) bool {
	seen := false
	dots := 0
	for *i >= 0 {
		r := rs[*i]
		switch {
		case r >= '0' && r <= '9':
			seen = true
			*i--
		case r == '.' && dots == 0:
			dots++
			*i--
		default:
			return seen
		}
	}
	return seen
}

// collectNumber advances *i past digits and at most one decimal point. It
// leaves *i at the first non-numeric rune and reports whether a digit was seen.
func collectNumber(rs []rune, i *int) bool {
	seen := false
	dots := 0
	for *i < len(rs) && *i >= 0 {
		r := rs[*i]
		switch {
		case r >= '0' && r <= '9':
			seen = true
			*i++
		case r == '.' && dots == 0:
			dots++
			*i++
		default:
			return seen
		}
	}
	return seen
}

func assign(o *Observation, name string, v float64) bool {
	switch name {
	case "avg_wind":
		if !inRange(v, 0, 250) {
			return false
		}
		o.AverageWindKts = &v
	case "wind":
		if !inRange(v, 0, 250) {
			return false
		}
		o.WindKts = &v
	case "wind_direction":
		// A misread degree symbol can extend the run; keep the leading degree.
		if v > 360 && v >= 1000 {
			v = float64(int(v) / 10)
		}
		if !inRange(v, 0, 360) {
			return false
		}
		o.WindDirDeg = &v
	case "gust_max_today":
		if !inRange(v, 0, 250) {
			return false
		}
		o.GustMaxTodayKts = &v
	case "gust_max_last_hour":
		if !inRange(v, 0, 250) {
			return false
		}
		o.GustMaxLastHourKts = &v
	case "max_average_wind":
		if !inRange(v, 0, 250) {
			return false
		}
		o.MaxAverageWindKts = &v
	case "temperature":
		if !inRange(v, -90, 60) {
			return false
		}
		o.TemperatureC = &v
	case "max_temperature":
		if !inRange(v, -90, 60) {
			return false
		}
		o.TemperatureMaxC = &v
	case "min_temperature":
		if !inRange(v, -90, 60) {
			return false
		}
		o.TemperatureMinC = &v
	case "indoor_temperature":
		if !inRange(v, -90, 60) {
			return false
		}
		o.IndoorTemperatureC = &v
	case "dew_point":
		if !inRange(v, -90, 60) {
			return false
		}
		o.DewPointC = &v
	case "heat_index":
		if !inRange(v, -90, 90) {
			return false
		}
		o.HeatIndexC = &v
	case "humidity":
		if !inRange(v, 0, 100) {
			return false
		}
		o.HumidityPct = &v
	case "indoor_humidity":
		if !inRange(v, 0, 100) {
			return false
		}
		o.IndoorHumidityPct = &v
	case "pressure":
		if !inRange(v, 800, 1100) {
			return false
		}
		o.PressureHpa = &v
	case "pressure_rate":
		if !inRange(v, -50, 50) {
			return false
		}
		o.PressureTrendHpaHr = &v
	case "rain_today":
		if !inRange(v, 0, 20000) {
			return false
		}
		o.RainTodayMm = &v
	case "rain_yesterday":
		if !inRange(v, 0, 20000) {
			return false
		}
		o.RainYesterdayMm = &v
	case "rain_month":
		if !inRange(v, 0, 20000) {
			return false
		}
		o.RainMonthMm = &v
	case "rain_year":
		if !inRange(v, 0, 20000) {
			return false
		}
		o.RainYearMm = &v
	case "rain_rate":
		if !inRange(v, 0, 500) {
			return false
		}
		o.RainRateMmMin = &v
	default:
		return false
	}
	return true
}

func inRange(v, min, max float64) bool { return v >= min && v <= max }

// hasCore requires the fields a fallback reading is useless without.
func hasCore(o *Observation) bool {
	return o.WindKts != nil && o.TemperatureC != nil && o.PressureHpa != nil
}

package gifocr

import (
	"errors"
	"image"
	"os"
	"testing"
)

func referenceImage(t *testing.T) image.Image {
	t.Helper()
	data, err := os.ReadFile("testdata/reference.gif")
	if err != nil {
		t.Fatalf("read reference.gif: %v", err)
	}
	img, err := DecodeGIF(data)
	if err != nil {
		t.Fatalf("DecodeGIF: %v", err)
	}
	return img
}

func TestRecognizeReference(t *testing.T) {
	atlas, err := DefaultAtlas()
	if err != nil {
		t.Fatalf("DefaultAtlas: %v", err)
	}

	obs, texts, err := Recognize(referenceImage(t), atlas)
	if err != nil {
		t.Fatalf("Recognize: %v\n%s", err, texts)
	}

	cases := []struct {
		name string
		got  *float64
		want float64
	}{
		{"avg_wind", obs.AverageWindKts, 9.6},
		{"wind", obs.WindKts, 8.6},
		{"wind_direction", obs.WindDirDeg, 117},
		{"gust_max_today", obs.GustMaxTodayKts, 22.0},
		{"gust_max_last_hour", obs.GustMaxLastHourKts, 12.4},
		{"max_average_wind", obs.MaxAverageWindKts, 19.4},
		{"temperature", obs.TemperatureC, 27.8},
		{"max_temperature", obs.TemperatureMaxC, 29.7},
		{"min_temperature", obs.TemperatureMinC, 26.0},
		{"indoor_temperature", obs.IndoorTemperatureC, 29.7},
		{"dew_point", obs.DewPointC, 23.4},
		{"heat_index", obs.HeatIndexC, 31.3},
		{"humidity", obs.HumidityPct, 77},
		{"indoor_humidity", obs.IndoorHumidityPct, 71},
		{"pressure", obs.PressureHpa, 1011.8},
		{"pressure_rate", obs.PressureTrendHpaHr, 0.6},
		{"rain_today", obs.RainTodayMm, 0.0},
		{"rain_yesterday", obs.RainYesterdayMm, 0.0},
		{"rain_month", obs.RainMonthMm, 0.8},
		{"rain_year", obs.RainYearMm, 951.0},
		{"rain_rate", obs.RainRateMmMin, 0.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got == nil {
				t.Fatalf("%s is nil (text %q)", tc.name, texts[tc.name])
			}
			if *tc.got != tc.want {
				t.Errorf("%s = %v, want %v (text %q)", tc.name, *tc.got, tc.want, texts[tc.name])
			}
		})
	}
}

// TestUnitsAreNotDigits guards against the atlas containing glyph variants
// learned from units such as "mm" or "%". If those matched, values like
// "0.8 mm" would be read as "0.800".
func TestUnitsAreNotDigits(t *testing.T) {
	atlas, err := DefaultAtlas()
	if err != nil {
		t.Fatalf("DefaultAtlas: %v", err)
	}
	_, texts, err := Recognize(referenceImage(t), atlas)
	if err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	if got := texts["rain_month"]; got != "0.8??" {
		t.Errorf("rain_month text = %q, want %q", got, "0.8??")
	}
	if got := texts["humidity"]; got != "77?" {
		t.Errorf("humidity text = %q, want %q", got, "77?")
	}
	if got := texts["wind_direction"]; got != "???117?" {
		t.Errorf("wind_direction text = %q, want %q", got, "???117?")
	}
}

func TestRecognizeRejectsWrongLayout(t *testing.T) {
	atlas, err := DefaultAtlas()
	if err != nil {
		t.Fatalf("DefaultAtlas: %v", err)
	}
	small := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	if _, _, err := Recognize(small, atlas); !errors.Is(err, ErrUnsupportedLayout) {
		t.Fatalf("Recognize() error = %v, want ErrUnsupportedLayout", err)
	}
}

func TestRecognizeRejectsNilAtlas(t *testing.T) {
	if _, _, err := Recognize(referenceImage(t), nil); err == nil {
		t.Fatal("Recognize() error = nil, want error")
	}
}

func TestFirstNumber(t *testing.T) {
	tests := []struct {
		text string
		want string
		ok   bool
	}{
		{"9.6???", "9.6", true},
		{"-0.3?????", "-0.3", true},
		{"+0.6??????", "+0.6", true},
		{"0.8??", "0.8", true},
		{"???", "", false},
		{"117?", "117", true},
	}
	for _, tc := range tests {
		t.Run(tc.text, func(t *testing.T) {
			got, ok := firstNumber([]rune(tc.text))
			if ok != tc.ok || got != tc.want {
				t.Errorf("firstNumber(%q) = %q, %v; want %q, %v", tc.text, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestLastNumber(t *testing.T) {
	tests := []struct {
		text string
		want string
		ok   bool
	}{
		{"???117?", "117", true},
		{"ESE 117", "117", true},
		{"-12.5", "-12.5", true},
		{"", "", false},
		{"???", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.text, func(t *testing.T) {
			got, ok := lastNumber([]rune(tc.text))
			if ok != tc.ok || got != tc.want {
				t.Errorf("lastNumber(%q) = %q, %v; want %q, %v", tc.text, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestSegmentRejectsWhiteLabels(t *testing.T) {
	region, ok := RegionByName("temperature")
	if !ok {
		t.Fatal("temperature region missing")
	}
	segments := Segment(referenceImage(t), region)
	if len(segments) < 3 {
		t.Fatalf("segments = %d, want at least 3", len(segments))
	}
	// "27.8" is rendered as 5,5,1,5 pixel-wide glyphs; the white label and the
	// °C unit must not add extra leading glyphs. The two trailing segments are
	// the degree sign and the C.
	if len(segments) != 6 {
		t.Fatalf("segments = %d, want 6", len(segments))
	}
	want := []int{5, 5, 1, 5}
	for i, w := range want {
		if segments[i].W != w {
			t.Errorf("digit %d width = %d, want %d", i, segments[i].W, w)
		}
	}
}

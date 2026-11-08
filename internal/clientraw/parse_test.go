package clientraw

import (
	"os"
	"strings"
	"testing"
	"time"
)

func loadFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/clientraw.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return b
}

func TestParseFixture(t *testing.T) {
	loc, err := time.LoadLocation("America/Fortaleza")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	d, err := Parse(loadFixture(t), loc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if d.Version != "C10.37S153" {
		t.Errorf("Version = %q, want %q", d.Version, "C10.37S153")
	}
	if d.StationName != "Cumbuco" {
		t.Errorf("StationName = %q, want %q", d.StationName, "Cumbuco")
	}
	wantTime := time.Date(2026, time.November, 7, 19, 2, 7, 0, loc)
	if !d.ObservedAt.Equal(wantTime) {
		t.Errorf("ObservedAt = %v, want %v", d.ObservedAt, wantTime)
	}

	cases := []struct {
		name string
		got  *float64
		want float64
	}{
		{"AverageWindKts", d.AverageWindKts, 9.3},
		{"WindKts", d.WindKts, 9.6},
		{"WindDirDeg", d.WindDirDeg, 153},
		{"TemperatureC", d.TemperatureC, 27.9},
		{"HumidityPct", d.HumidityPct, 77},
		{"PressureHpa", d.PressureHpa, 1011.8},
		{"RainTodayMm", d.RainTodayMm, 0.0},
		{"RainMonthMm", d.RainMonthMm, 0.8},
		{"RainYearMm", d.RainYearMm, 951.0},
		{"RainRateMmMin", d.RainRateMmMin, 0.0},
		{"IndoorTemperatureC", d.IndoorTemperatureC, 29.7},
		{"IndoorHumidityPct", d.IndoorHumidityPct, 71},
		{"RainYesterdayMm", d.RainYesterdayMm, 0.0},
		{"GustMaxTodayKts", d.GustMaxTodayKts, 22.0},
		{"DewPointC", d.DewPointC, 23.5},
		{"HeatIndexC", d.HeatIndexC, 31.4},
		{"MaxAverageWindKts", d.MaxAverageWindKts, 19.4},
		{"WindDirAverageDeg", d.WindDirAverageDeg, 124},
		{"PressureTrendHpaHr", d.PressureTrendHpaHr, 0.6},
		{"PressureMaxHpa", d.PressureMaxHpa, 1012.8},
		{"PressureMinHpa", d.PressureMinHpa, 1009.5},
		{"GustMaxLastHourKts", d.GustMaxLastHourKts, 12.0},
		{"TemperatureMaxC", d.TemperatureMaxC, 29.7},
		{"TemperatureMinC", d.TemperatureMinC, 26.0},
		{"WindChillC", d.WindChillC, 27.9},
		{"HumidexC", d.HumidexC, 38.6},
		{"ApparentTemperatureC", d.ApparentTemperatureC, 29.0},
		{"UVIndex", d.UVIndex, 0.0},
		{"SolarWm2", d.SolarWm2, 0.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got == nil {
				t.Fatalf("%s is nil, want %v", tc.name, tc.want)
			}
			if *tc.got != tc.want {
				t.Errorf("%s = %v, want %v", tc.name, *tc.got, tc.want)
			}
		})
	}
}

func TestParseRejectsSentinels(t *testing.T) {
	fields := strings.Fields(string(loadFixture(t)))
	fields[fieldTemp] = "-100.0"
	fields[fieldHumidity] = "255.0"
	fields[fieldBaro] = "1011.8"

	d, err := Parse([]byte(strings.Join(fields, " ")), time.UTC)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if d.TemperatureC != nil {
		t.Errorf("TemperatureC = %v, want nil (sentinel -100)", *d.TemperatureC)
	}
	if d.HumidityPct != nil {
		t.Errorf("HumidityPct = %v, want nil (sentinel 255)", *d.HumidityPct)
	}
	if d.PressureHpa == nil || *d.PressureHpa != 1011.8 {
		t.Errorf("PressureHpa = %v, want 1011.8", d.PressureHpa)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"too few fields", "12345 1 2"},
		{"wrong header", replaceHeader(loadFixture(t), "99999")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse([]byte(tc.body), time.UTC); err == nil {
				t.Fatal("Parse() error = nil, want error")
			}
		})
	}
}

func replaceHeader(body []byte, header string) string {
	s := string(body)
	i := 0
	for i < len(s) && s[i] != ' ' {
		i++
	}
	return header + s[i:]
}

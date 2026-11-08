// Package clientraw parses the Weather Display clientraw.txt observation
// file. Field indices follow the documented clientraw layout (0-based).
package clientraw

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Field indices used by this package.
const (
	fieldAvgWind         = 1
	fieldWind            = 2
	fieldWindDir         = 3
	fieldTemp            = 4
	fieldHumidity        = 5
	fieldBaro            = 6
	fieldRainToday       = 7
	fieldRainMonth       = 8
	fieldRainYear        = 9
	fieldRainRate        = 10 // mm/min
	fieldRainRateMax     = 11 // mm/min
	fieldIndoorTemp      = 12
	fieldIndoorHum       = 13
	fieldRainYesterday   = 19
	fieldHour            = 29
	fieldMinute          = 30
	fieldSecond          = 31
	fieldStation         = 32
	fieldDay             = 35
	fieldMonth           = 36
	fieldWindChill       = 44
	fieldHumidex         = 45
	fieldTempMax         = 46
	fieldTempMin         = 47
	fieldBaroTrend       = 50
	fieldGustMax         = 71
	fieldDewPoint        = 72
	fieldUV              = 79
	fieldHeatIndex       = 112
	fieldMaxAvgWind      = 113
	fieldWindDirAvg      = 117
	fieldSolar           = 127
	fieldApparentTemp    = 130
	fieldBaroMax         = 131
	fieldBaroMin         = 132
	fieldGustMaxLastHour = 133
	fieldYear            = 141
	minimumTokenCount    = 150
	maximumTokenCount    = 250
	expectedHeader       = "12345"
)

// Data is a parsed clientraw observation.
type Data struct {
	// Raw keeps the original tokens (without the version trailer).
	Raw     []string
	Version string

	StationName string
	ObservedAt  time.Time

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

// Parse converts a clientraw.txt body into Data. loc is used to interpret the
// station's wall-clock timestamp (the file carries no timezone).
func Parse(body []byte, loc *time.Location) (*Data, error) {
	if loc == nil {
		loc = time.UTC
	}

	fields := strings.Fields(string(body))
	if len(fields) < minimumTokenCount {
		return nil, fmt.Errorf("clientraw: too few fields: got %d, want at least %d", len(fields), minimumTokenCount)
	}
	if len(fields) > maximumTokenCount {
		return nil, fmt.Errorf("clientraw: too many fields: got %d, want at most %d", len(fields), maximumTokenCount)
	}
	if fields[0] != expectedHeader {
		return nil, fmt.Errorf("clientraw: unexpected header %q", fields[0])
	}

	// The final token is a version marker such as "!!C10.37S153!!".
	version := ""
	if last := fields[len(fields)-1]; strings.HasPrefix(last, "!!") && strings.HasSuffix(last, "!!") {
		version = strings.TrimSuffix(strings.TrimPrefix(last, "!!"), "!!")
		fields = fields[:len(fields)-1]
	}

	d := &Data{
		Raw:     fields,
		Version: version,

		AverageWindKts:       number(fields, fieldAvgWind, 0, 250),
		WindKts:              number(fields, fieldWind, 0, 250),
		WindDirDeg:           number(fields, fieldWindDir, 0, 360),
		WindDirAverageDeg:    number(fields, fieldWindDirAvg, 0, 360),
		GustMaxTodayKts:      number(fields, fieldGustMax, 0, 250),
		GustMaxLastHourKts:   number(fields, fieldGustMaxLastHour, 0, 250),
		MaxAverageWindKts:    number(fields, fieldMaxAvgWind, 0, 250),
		TemperatureC:         number(fields, fieldTemp, -90, 60),
		IndoorTemperatureC:   number(fields, fieldIndoorTemp, -90, 60),
		DewPointC:            number(fields, fieldDewPoint, -90, 60),
		WindChillC:           number(fields, fieldWindChill, -90, 60),
		HumidexC:             number(fields, fieldHumidex, -90, 90),
		HeatIndexC:           number(fields, fieldHeatIndex, -90, 90),
		ApparentTemperatureC: number(fields, fieldApparentTemp, -90, 90),
		TemperatureMaxC:      number(fields, fieldTempMax, -90, 60),
		TemperatureMinC:      number(fields, fieldTempMin, -90, 60),
		HumidityPct:          number(fields, fieldHumidity, 0, 100),
		IndoorHumidityPct:    number(fields, fieldIndoorHum, 0, 100),
		PressureHpa:          number(fields, fieldBaro, 800, 1100),
		PressureMaxHpa:       number(fields, fieldBaroMax, 800, 1100),
		PressureMinHpa:       number(fields, fieldBaroMin, 800, 1100),
		PressureTrendHpaHr:   number(fields, fieldBaroTrend, -50, 50),
		RainTodayMm:          number(fields, fieldRainToday, 0, 20000),
		RainYesterdayMm:      number(fields, fieldRainYesterday, 0, 20000),
		RainMonthMm:          number(fields, fieldRainMonth, 0, 20000),
		RainYearMm:           number(fields, fieldRainYear, 0, 20000),
		RainRateMmMin:        number(fields, fieldRainRate, 0, 500),
		RainRateMaxMmMin:     number(fields, fieldRainRateMax, 0, 500),
		UVIndex:              number(fields, fieldUV, 0, 30),
		SolarWm2:             number(fields, fieldSolar, 0, 2000),
	}

	if name := token(fields, fieldStation); name != "" {
		d.StationName = strings.TrimSuffix(name, stationTimeSuffix(name))
	}
	d.ObservedAt = stationTime(fields, loc)

	return d, nil
}

func token(fields []string, i int) string {
	if i < 0 || i >= len(fields) {
		return ""
	}
	return fields[i]
}

func number(fields []string, i int, min, max float64) *float64 {
	s := token(fields, i)
	if s == "" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	if v < min || v > max {
		// Includes the Weather Display sentinels (-100, 255, -50) used when a
		// sensor is absent.
		return nil
	}
	return &v
}

func stationTime(fields []string, loc *time.Location) time.Time {
	day, okDay := integer(fields, fieldDay)
	month, okMonth := integer(fields, fieldMonth)
	year, okYear := integer(fields, fieldYear)
	hour, okHour := integer(fields, fieldHour)
	minute, okMinute := integer(fields, fieldMinute)
	second, okSecond := integer(fields, fieldSecond)
	if !okDay || !okMonth || !okYear || !okHour || !okMinute || !okSecond {
		return time.Time{}
	}
	if day < 1 || day > 31 || month < 1 || month > 12 || year < 1990 || year > 2200 {
		return time.Time{}
	}
	if hour > 23 || minute > 59 || second > 60 {
		return time.Time{}
	}
	return time.Date(year, time.Month(month), day, hour, minute, second, 0, loc)
}

func integer(fields []string, i int) (int, bool) {
	s := token(fields, i)
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

// stationTimeSuffix returns the "-HH:MM:SS" tail of the station name field, or
// "" when the field does not carry one.
func stationTimeSuffix(name string) string {
	i := strings.LastIndexByte(name, '-')
	if i < 0 || len(name)-i != 9 {
		return ""
	}
	tail := name[i+1:]
	if len(tail) != 8 || tail[2] != ':' || tail[5] != ':' {
		return ""
	}
	for _, j := range []int{0, 1, 3, 4, 6, 7} {
		if tail[j] < '0' || tail[j] > '9' {
			return ""
		}
	}
	return name[i:]
}

// ErrNoObservations is returned by providers when a source yields no usable
// values.
var ErrNoObservations = errors.New("no observations")

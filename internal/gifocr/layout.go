// Package gifocr reads the core weather values from the station's rendered
// index.gif. The image is produced by Weather Display with a fixed bitmap font
// and layout, so recognition uses a small glyph atlas instead of a general OCR
// engine.
package gifocr

import (
	"image"
	"image/draw"
)

// Image dimensions the layout was built for.
const (
	LayoutWidth  = 818
	LayoutHeight = 543
)

// ExtractMode selects which number of a recognised glyph sequence is kept.
type ExtractMode int

const (
	// FirstNumber takes the first numeric run (labels and units are ignored).
	FirstNumber ExtractMode = iota
	// LastNumber takes the last numeric run (used for "ESE 117 °").
	LastNumber
)

// Region is a rectangular value area plus the field it populates.
type Region struct {
	Name    string
	X0, X1  int
	Y0, Y1  int
	Extract ExtractMode
}

// Regions lists the value areas of the reference layout. Coordinates are
// inclusive. Bands are exactly nine rows tall, matching the bitmap font.
var Regions = []Region{
	// Left column (values follow their black label, so the crop spans the
	// whole panel width and colour filtering selects the cyan value text).
	{"avg_wind", 0, 147, 18, 26, FirstNumber},
	{"wind", 0, 147, 38, 46, FirstNumber},
	{"temperature", 0, 147, 57, 65, FirstNumber},
	{"temperature_rate", 0, 147, 79, 87, FirstNumber},
	{"pressure", 0, 147, 100, 108, FirstNumber},
	{"pressure_rate", 0, 147, 120, 128, FirstNumber},
	{"humidity", 0, 147, 143, 151, FirstNumber},
	{"dew_point_depression", 0, 147, 164, 172, FirstNumber},
	{"indoor_temperature", 0, 147, 186, 194, FirstNumber},
	{"indoor_humidity", 0, 147, 207, 215, FirstNumber},

	// Extreme conditions column ("Values reset at 0 hr").
	{"gust_max_today", 150, 245, 248, 256, FirstNumber},
	{"gust_max_last_hour", 150, 245, 270, 278, FirstNumber},
	{"max_average_wind", 150, 245, 294, 302, FirstNumber},
	{"max_temperature", 150, 245, 318, 326, FirstNumber},
	{"min_temperature", 150, 245, 342, 350, FirstNumber},
	// The row below this table (y=367) reads "0.0 mm/min (0.0 mm/hr)" and is a
	// rain-rate line, not a max value, so it is intentionally not a region.

	// Right-hand derived values.
	{"heat_index", 245, 340, 172, 180, FirstNumber},
	{"dew_point", 245, 340, 191, 199, FirstNumber},
	{"wet_bulb", 245, 340, 209, 217, FirstNumber},

	// Wind direction header, e.g. "ESE 117 °".
	{"wind_direction", 245, 360, 28, 36, LastNumber},

	// Rainfall box.
	{"rain_today", 0, 170, 425, 433, FirstNumber},
	{"rain_yesterday", 0, 170, 447, 455, FirstNumber},
	{"rain_month", 0, 170, 470, 478, FirstNumber},
	{"rain_year", 0, 170, 493, 501, FirstNumber},
	{"rain_rate", 0, 170, 515, 523, FirstNumber},
}

// RegionByName returns the region with the given name.
func RegionByName(name string) (Region, bool) {
	for _, r := range Regions {
		if r.Name == name {
			return r, true
		}
	}
	return Region{}, false
}

// Segment returns the glyph bitmaps found in the region. A glyph is a run of
// columns containing value-coloured pixels.
func Segment(img image.Image, r Region) []Bitmap {
	// Work on an NRGBA copy so At() is cheap and palette independent.
	bounds := img.Bounds()
	if bounds.Dx() != LayoutWidth || bounds.Dy() != LayoutHeight {
		return nil
	}
	nrgba, ok := img.(*image.NRGBA)
	if !ok {
		nrgba = image.NewNRGBA(image.Rect(0, 0, LayoutWidth, LayoutHeight))
		draw.Draw(nrgba, nrgba.Bounds(), img, bounds.Min, draw.Src)
	}

	active := make([]bool, r.X1-r.X0+1)
	for x := r.X0; x <= r.X1; x++ {
		for y := r.Y0; y <= r.Y1; y++ {
			if isValuePixel(nrgba, x, y) {
				active[x-r.X0] = true
				break
			}
		}
	}

	var out []Bitmap
	for i := 0; i < len(active); i++ {
		if !active[i] {
			continue
		}
		j := i
		for j+1 < len(active) && active[j+1] {
			j++
		}
		out = append(out, crop(nrgba, r, r.X0+i, r.X0+j))
		i = j
	}
	return out
}

func crop(img *image.NRGBA, r Region, x0, x1 int) Bitmap {
	h := r.Y1 - r.Y0 + 1
	b := Bitmap{W: x1 - x0 + 1, Rows: make([]string, h)}
	for y := 0; y < h; y++ {
		row := make([]byte, b.W)
		for x := 0; x < b.W; x++ {
			if isValuePixel(img, x0+x, r.Y0+y) {
				row[x] = '1'
			} else {
				row[x] = '0'
			}
		}
		b.Rows[y] = string(row)
	}
	return b
}

// isValuePixel reports whether the pixel belongs to the cyan/white value font.
// Labels are black and the panel background is mid-blue, so this rejects both.
func isValuePixel(img *image.NRGBA, x, y int) bool {
	c := img.NRGBAAt(x, y)
	if c.A < 128 {
		return false
	}
	if c.G < 230 || c.B < 230 {
		return false
	}
	// Values (and their units) are pure cyan (0x00ffff). Labels, headers and
	// the date footer are white (0xffffff), so the red channel separates them:
	// this also rejects the gray borders (0xc0c0c0) and the background.
	return c.R < 100
}

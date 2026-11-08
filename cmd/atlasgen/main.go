// Command atlasgen rebuilds the GIF OCR glyph atlas from a reference capture
// and a manifest of expected values.
//
// Usage:
//
//	go run ./cmd/atlasgen -gif internal/gifocr/testdata/reference.gif \
//	    -manifest internal/gifocr/testdata/manifest.json \
//	    -out internal/gifocr/glyphs.json
//
// The manifest maps region names (see internal/gifocr.Regions) to the value
// visible in the image. Only digits, '.', '-' and '+' are learned; labels and
// units are ignored.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"

	"github.com/eftechcombr/weather/internal/gifocr"
)

func main() {
	gifPath := flag.String("gif", "internal/gifocr/testdata/reference.gif", "reference GIF")
	manifestPath := flag.String("manifest", "internal/gifocr/testdata/manifest.json", "expected values")
	outPath := flag.String("out", "internal/gifocr/glyphs.json", "atlas output path")
	flag.Parse()

	data, err := os.ReadFile(*gifPath)
	if err != nil {
		fatal("read gif: %v", err)
	}
	img, err := gifocr.DecodeGIF(data)
	if err != nil {
		fatal("%v", err)
	}

	raw, err := os.ReadFile(*manifestPath)
	if err != nil {
		fatal("read manifest: %v", err)
	}
	var manifest map[string]string
	if err := json.Unmarshal(raw, &manifest); err != nil {
		fatal("parse manifest: %v", err)
	}

	atlas := gifocr.NewAtlas()
	names := make([]string, 0, len(manifest))
	for name := range manifest {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		expected := manifest[name]
		region, ok := gifocr.RegionByName(name)
		if !ok {
			fatal("unknown region %q", name)
		}
		segments := gifocr.Segment(img, region)
		runes := []rune(expected)
		if len(segments) < len(runes) {
			fatal("region %q: got %d glyphs, expected at least %d for %q",
				name, len(segments), len(runes), expected)
		}
		for i, r := range runes {
			switch {
			case r >= '0' && r <= '9', r == '.', r == '-', r == '+':
				atlas.Add(r, segments[i])
			default:
				fatal("region %q: unsupported manifest character %q", name, r)
			}
		}
		fmt.Printf("%-24s %-10q glyphs=%d\n", name, expected, len(segments))
	}

	// Verify that every trained region round-trips to its expected value. This
	// catches misaligned regions, where the manifest would otherwise teach the
	// atlas that a unit letter or a clipped fragment is a digit.
	for _, name := range names {
		region, _ := gifocr.RegionByName(name)
		got, text, ok := gifocr.RegionValue(img, atlas, region)
		if !ok {
			fatal("region %q: no value recognised (text %q)", name, text)
		}
		want, err := strconv.ParseFloat(manifest[name], 64)
		if err != nil {
			fatal("region %q: bad manifest value %q", name, manifest[name])
		}
		value, err := strconv.ParseFloat(got, 64)
		if err != nil || value != want {
			fatal("region %q: recognised %q (%q), want %q", name, got, text, manifest[name])
		}
	}

	if err := atlas.Save(*outPath); err != nil {
		fatal("save atlas: %v", err)
	}

	// Round-trip the reference image to prove the atlas is usable.
	obs, texts, err := gifocr.Recognize(img, atlas)
	if err != nil {
		fatal("round-trip: %v", err)
	}
	fmt.Println("\nround-trip recognition:")
	textNames := make([]string, 0, len(texts))
	for name := range texts {
		textNames = append(textNames, name)
	}
	sort.Strings(textNames)
	for _, name := range textNames {
		fmt.Printf("  %-24s %q\n", name, texts[name])
	}
	if obs.WindKts != nil && obs.TemperatureC != nil && obs.PressureHpa != nil {
		fmt.Printf("\ncore: wind=%.1f temp=%.1f pressure=%.1f\n", *obs.WindKts, *obs.TemperatureC, *obs.PressureHpa)
	}
	fmt.Printf("\nwrote %s (%d runes)\n", *outPath, len(atlas.Glyphs))
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "atlasgen: "+format+"\n", args...)
	os.Exit(1)
}

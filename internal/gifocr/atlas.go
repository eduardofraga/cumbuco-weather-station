package gifocr

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Bitmap is a rasterised glyph. Rows contains one string of '0'/'1' per pixel
// row, top to bottom.
type Bitmap struct {
	W    int      `json:"w"`
	Rows []string `json:"rows"`
}

// H returns the bitmap height.
func (b Bitmap) H() int { return len(b.Rows) }

// Pixels returns the number of set pixels.
func (b Bitmap) Pixels() int {
	n := 0
	for _, r := range b.Rows {
		for i := 0; i < len(r); i++ {
			if r[i] == '1' {
				n++
			}
		}
	}
	return n
}

// Equal reports whether two bitmaps are identical.
func (b Bitmap) Equal(o Bitmap) bool {
	if b.W != o.W || len(b.Rows) != len(o.Rows) {
		return false
	}
	for i := range b.Rows {
		if b.Rows[i] != o.Rows[i] {
			return false
		}
	}
	return true
}

// Distance returns the Hamming distance between two bitmaps, or -1 when their
// dimensions differ.
func (b Bitmap) Distance(o Bitmap) int {
	if b.W != o.W || len(b.Rows) != len(o.Rows) {
		return -1
	}
	d := 0
	for i := range b.Rows {
		if len(b.Rows[i]) != len(o.Rows[i]) {
			return -1
		}
		for j := 0; j < len(b.Rows[i]); j++ {
			if b.Rows[i][j] != o.Rows[i][j] {
				d++
			}
		}
	}
	return d
}

// Atlas maps runes to the bitmap variants observed in the reference image.
type Atlas struct {
	Glyphs map[string][]Bitmap `json:"glyphs"`
}

// NewAtlas returns an empty atlas.
func NewAtlas() *Atlas { return &Atlas{Glyphs: map[string][]Bitmap{}} }

// Add stores a bitmap for r, deduplicating identical variants.
func (a *Atlas) Add(r rune, b Bitmap) {
	if a.Glyphs == nil {
		a.Glyphs = map[string][]Bitmap{}
	}
	key := string(r)
	for _, existing := range a.Glyphs[key] {
		if existing.Equal(b) {
			return
		}
	}
	a.Glyphs[key] = append(a.Glyphs[key], b)
}

// Match returns the rune whose bitmap is identical to b. The reference font is
// a bitmap font rendered deterministically, so exact matches are reliable and
// fuzzy matching would only invite unit letters to masquerade as digits.
func (a *Atlas) Match(b Bitmap) (rune, bool) {
	for key, variants := range a.Glyphs {
		runes := []rune(key)
		if len(runes) != 1 {
			continue
		}
		for _, v := range variants {
			if b.Equal(v) {
				return runes[0], true
			}
		}
	}
	return 0, false
}

// Save writes the atlas as indented JSON.
func (a *Atlas) Save(path string) error {
	keys := make([]string, 0, len(a.Glyphs))
	for k := range a.Glyphs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ordered := make(map[string][]Bitmap, len(keys))
	for _, k := range keys {
		variants := append([]Bitmap(nil), a.Glyphs[k]...)
		sort.Slice(variants, func(i, j int) bool {
			return bitmapKey(variants[i]) < bitmapKey(variants[j])
		})
		ordered[k] = variants
	}
	data, err := json.MarshalIndent(struct {
		Glyphs map[string][]Bitmap `json:"glyphs"`
	}{ordered}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func bitmapKey(b Bitmap) string {
	return fmt.Sprintf("%d|%s", b.W, strings.Join(b.Rows, "/"))
}

// LoadSlice parses atlas JSON.
func LoadSlice(data []byte) (*Atlas, error) {
	var a Atlas
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("parse glyph atlas: %w", err)
	}
	if len(a.Glyphs) == 0 {
		return nil, fmt.Errorf("glyph atlas is empty")
	}
	return &a, nil
}

// Load reads an atlas from disk.
func Load(path string) (*Atlas, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LoadSlice(data)
}

package raster

import (
	"bytes"
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// resvg returns the rasterizer, skipping the test when resvg is not
// installed (it is in the Docker image).
func resvg(t *testing.T) *Resvg {
	t.Helper()
	r := New(os.Getenv("TERAKOYA_RESVG"), os.Getenv("TERAKOYA_FONTS_DIR"))
	if !r.Available() {
		t.Skip("resvg not installed")
	}
	return r
}

func TestRasterize(t *testing.T) {
	r := resvg(t)
	svg := filepath.Join(t.TempDir(), "a.svg")
	os.WriteFile(svg, []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="100" height="40">
<text x="5" y="25" font-family="'Times New Roman', 'DejaVu Serif', serif" font-style="italic">a α б</text></svg>`), 0o644)
	data, err := r.Rasterize(context.Background(), svg)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 200 || b.Dy() != 80 {
		t.Errorf("size = %v, want 2x", b)
	}
	// White background, dark text somewhere.
	if r, g, b, _ := img.At(0, 0).RGBA(); r>>8 != 255 || g>>8 != 255 || b>>8 != 255 {
		t.Errorf("background = %v", img.At(0, 0))
	}
	dark := false
	for x := 0; x < 200 && !dark; x++ {
		for y := 0; y < 80; y++ {
			if r, _, _, _ := img.At(x, y).RGBA(); r>>8 < 100 {
				dark = true
				break
			}
		}
	}
	if !dark {
		t.Error("no text drawn")
	}

	os.WriteFile(svg, []byte(`<svg`), 0o644)
	if _, err := r.Rasterize(context.Background(), svg); err == nil {
		t.Error("broken SVG accepted")
	}
}

func TestMissingBinary(t *testing.T) {
	r := New("/nonexistent/resvg", "")
	if _, err := r.Rasterize(context.Background(), "x.svg"); err != ErrNoResvg {
		t.Errorf("err = %v", err)
	}
}

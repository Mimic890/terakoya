package raster

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Limits of a drawing that is rasterized.
const (
	// MaxSide is the largest width or height of a drawing, in pixels, and
	// of the PNG made from it.
	MaxSide = 4096
	// MaxFileSize is the largest SVG file.
	MaxFileSize = 5 << 20
)

// Size is the width and height of a drawing in CSS pixels.
type Size struct {
	W, H float64
}

// defaultSize is what resvg assumes for a drawing without a size.
var defaultSize = Size{100, 100}

// Check reads the SVG at svgPath before it is rasterized. The drawing may
// be at most MaxSide pixels wide and high, and the pictures it embeds must
// be data: URIs or files inside root that are not symbolic links; resvg
// would otherwise read any file it is pointed at. It returns the size.
func Check(svgPath, root string) (Size, error) {
	info, err := os.Lstat(svgPath)
	if err != nil {
		return Size{}, err
	}
	if !info.Mode().IsRegular() {
		return Size{}, fmt.Errorf("not a regular file")
	}
	if info.Size() > MaxFileSize {
		return Size{}, fmt.Errorf("file is %d bytes, the limit is %d", info.Size(), MaxFileSize)
	}
	data, err := os.ReadFile(svgPath)
	if err != nil {
		return Size{}, err
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	dec.Entity = xml.HTMLEntity
	size, root2 := Size{}, false
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Size{}, fmt.Errorf("not a valid SVG: %w", err)
		}
		el, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if !root2 {
			root2 = true
			if el.Name.Local != "svg" {
				return Size{}, fmt.Errorf("the root element is <%s>, not <svg>", el.Name.Local)
			}
			if size, err = svgSize(el); err != nil {
				return Size{}, err
			}
		}
		for _, a := range el.Attr {
			if a.Name.Local != "href" {
				continue
			}
			if err := checkRef(strings.TrimSpace(a.Value), filepath.Dir(svgPath), root); err != nil {
				return Size{}, err
			}
		}
	}
	if !root2 {
		return Size{}, errors.New("not a valid SVG: no <svg> element")
	}
	if size.W > MaxSide || size.H > MaxSide {
		return Size{}, fmt.Errorf("drawing is %.0f×%.0f px, the limit is %d×%d", size.W, size.H, MaxSide, MaxSide)
	}
	return size, nil
}

// svgSize reads the size of the root element: width and height, or the
// viewBox where they are missing or relative.
func svgSize(el xml.StartElement) (Size, error) {
	var w, h, box string
	for _, a := range el.Attr {
		switch a.Name.Local {
		case "width":
			w = a.Value
		case "height":
			h = a.Value
		case "viewBox":
			box = a.Value
		}
	}
	vb, hasBox := Size{}, false
	if f := strings.FieldsFunc(box, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' || r == '\n' }); len(f) == 4 {
		bw, err1 := strconv.ParseFloat(f[2], 64)
		bh, err2 := strconv.ParseFloat(f[3], 64)
		if err1 == nil && err2 == nil && bw > 0 && bh > 0 {
			vb, hasBox = Size{bw, bh}, true
		}
	}
	pw, okW, err := length(w)
	if err != nil {
		return Size{}, fmt.Errorf("width: %w", err)
	}
	ph, okH, err := length(h)
	if err != nil {
		return Size{}, fmt.Errorf("height: %w", err)
	}
	switch {
	case okW && okH:
		return Size{pw, ph}, nil
	case okW && hasBox:
		return Size{pw, pw * vb.H / vb.W}, nil
	case okH && hasBox:
		return Size{ph * vb.W / vb.H, ph}, nil
	case hasBox:
		return vb, nil
	case okW:
		return Size{pw, defaultSize.H}, nil
	case okH:
		return Size{defaultSize.W, ph}, nil
	}
	return defaultSize, nil
}

// units converts SVG length units to CSS pixels.
var units = map[string]float64{"": 1, "px": 1, "pt": 4.0 / 3, "pc": 16, "mm": 96 / 25.4, "cm": 96 / 2.54,
	"in": 96, "em": 16, "ex": 8}

// length parses an absolute length in pixels; a missing or percentage
// length reports ok false.
func length(s string) (px float64, ok bool, err error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasSuffix(s, "%") {
		return 0, false, nil
	}
	i := len(s)
	for i > 0 && (s[i-1] >= 'a' && s[i-1] <= 'z' || s[i-1] >= 'A' && s[i-1] <= 'Z') {
		i--
	}
	scale, known := units[strings.ToLower(s[i:])]
	if !known {
		return 0, false, fmt.Errorf("unknown unit in %q", s)
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s[:i]), 64)
	if err != nil || v < 0 || math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, false, fmt.Errorf("bad length %q", s)
	}
	return v * scale, true, nil
}

// checkRef allows a reference within the document, a data: URI, or a
// relative path to a regular file inside root.
func checkRef(ref, dir, root string) error {
	switch {
	case ref == "", strings.HasPrefix(ref, "#"), strings.HasPrefix(strings.ToLower(ref), "data:"):
		return nil
	}
	if u, err := url.Parse(ref); err != nil || u.Scheme != "" || u.Host != "" {
		return fmt.Errorf("refers to %q; only pictures inside the vault may be used", ref)
	}
	if root == "" {
		return fmt.Errorf("refers to the file %q; no vault to look it up in", ref)
	}
	p, err := url.PathUnescape(ref)
	if err != nil {
		p = ref
	}
	if filepath.IsAbs(p) {
		return fmt.Errorf("refers to %q; use a path relative to the drawing", ref)
	}
	target := filepath.Join(dir, filepath.FromSlash(p))
	if !inside(target, root) {
		return fmt.Errorf("refers to %q, outside the vault", ref)
	}
	// Every part of the path must be a real folder or file, so that a
	// symbolic link cannot lead out of the vault.
	rel, _ := filepath.Rel(root, target)
	cur := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if err != nil {
			return fmt.Errorf("refers to %q, which does not exist", ref)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refers to %q through a symbolic link", ref)
		}
	}
	return nil
}

// inside reports whether p lies within root.
func inside(p, root string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(p))
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// Zoom is the scale a drawing of the given size is rasterized at: twice
// its size, at most MaxSide pixels on the longer side.
func Zoom(s Size) float64 {
	zoom := 2.0
	if longest := math.Max(s.W, s.H); longest > 0 && longest*zoom > MaxSide {
		zoom = MaxSide / longest
	}
	return zoom
}

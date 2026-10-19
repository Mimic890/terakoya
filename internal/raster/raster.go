// Package raster turns SVG drawings into PNG images Telegram can show,
// using the resvg command-line tool with bundled DejaVu fonts, so labels in
// Latin, Greek and Cyrillic look the same everywhere.
package raster

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Rasterizer converts an SVG file to PNG.
type Rasterizer interface {
	Rasterize(ctx context.Context, svgPath string) ([]byte, error)
	// Tag identifies the output settings; it is part of the cache key, so
	// changing the scale or the fonts converts every drawing once more.
	Tag() string
}

// FontDirs are searched for the DejaVu fonts when no directory is set:
// Alpine, then Debian and Ubuntu.
var FontDirs = []string{"/usr/share/fonts/dejavu", "/usr/share/fonts/truetype/dejavu"}

// ErrNoResvg is returned when the resvg binary cannot be found.
var ErrNoResvg = errors.New("resvg is not installed; it is included in the Docker image, or set TERAKOYA_RESVG")

// Resvg runs the resvg CLI at twice the drawing's size, at most MaxSide
// pixels, on a white background, with system fonts disabled and only the DejaVu fonts loaded.
type Resvg struct {
	Bin      string // path or name of the binary, default "resvg"
	FontsDir string // default: the first of FontDirs that exists
	// Root is the vault: the only place pictures a drawing embeds may
	// come from (see Check).
	Root    string
	Timeout time.Duration
}

// New returns a Resvg with defaults filled in, for drawings in the vault
// at root.
func New(bin, fontsDir, root string) *Resvg {
	if bin == "" {
		bin = "resvg"
	}
	if fontsDir == "" {
		for _, d := range FontDirs {
			if st, err := os.Stat(d); err == nil && st.IsDir() {
				fontsDir = d
				break
			}
		}
	}
	// Drawings come from the loaded catalog, whose root has its links
	// resolved; the root here must match it.
	if abs, err := filepath.Abs(root); err == nil {
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			abs = real
		}
		root = abs
	}
	return &Resvg{Bin: bin, FontsDir: fontsDir, Root: root, Timeout: 30 * time.Second}
}

// Tag is the settings tag of the output.
func (r *Resvg) Tag() string { return "resvg-2x-dejavu" }

// Available reports whether the binary can be run.
func (r *Resvg) Available() bool {
	_, err := exec.LookPath(r.Bin)
	return err == nil
}

// Rasterize converts the SVG at svgPath, after Check.
func (r *Resvg) Rasterize(ctx context.Context, svgPath string) ([]byte, error) {
	if !r.Available() {
		return nil, ErrNoResvg
	}
	size, err := Check(svgPath, r.Root)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()
	args := []string{"--zoom", strconv.FormatFloat(Zoom(size), 'f', -1, 64), "--background", "white", "--skip-system-fonts",
		"--serif-family", "DejaVu Serif", "--sans-serif-family", "DejaVu Sans", "--font-family", "DejaVu Sans",
		"--monospace-family", "DejaVu Sans Mono"}
	if r.FontsDir != "" {
		args = append(args, "--use-fonts-dir", r.FontsDir)
	}
	args = append(args, svgPath, "-c")
	cmd := exec.CommandContext(ctx, r.Bin, args...)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("resvg: %s", msg)
	}
	if !bytes.HasPrefix(out.Bytes(), []byte("\x89PNG")) {
		return nil, fmt.Errorf("resvg: output is not a PNG")
	}
	return out.Bytes(), nil
}

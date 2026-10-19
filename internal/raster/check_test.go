package raster

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckSize(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		svg  string
		want Size
		err  string
	}{
		{`<svg width="200" height="100"/>`, Size{200, 100}, ""},
		{`<svg width="1in" height="2in"/>`, Size{96, 192}, ""},
		{`<svg width="3pt" height="25.4mm"/>`, Size{4, 96}, ""},
		{`<svg viewBox="0 0 300 150"/>`, Size{300, 150}, ""},
		{`<svg width="100%" height="100%" viewBox="0,0,40,20"/>`, Size{40, 20}, ""},
		{`<svg width="400" viewBox="0 0 200 100"/>`, Size{400, 200}, ""},
		{`<svg/>`, Size{100, 100}, ""},
		{`<?xml version="1.0"?><!DOCTYPE svg><svg width="4096" height="1">&nbsp;</svg>`, Size{4096, 1}, ""},
		{`<svg width="4097" height="10"/>`, Size{}, "limit"},
		{`<svg viewBox="0 0 100000 10"/>`, Size{}, "limit"},
		{`<svg width="50000mm" height="1"/>`, Size{}, "limit"},
		{`<svg width="12furlongs" height="1"/>`, Size{}, "unknown unit"},
		{`<html/>`, Size{}, "not <svg>"},
		{`<svg`, Size{}, "not a valid SVG"},
	}
	for _, c := range cases {
		p := filepath.Join(dir, "a.svg")
		os.WriteFile(p, []byte(c.svg), 0o644)
		got, err := Check(p, dir)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%s: err = %v, want %q", c.svg, err, c.err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%s = %v, %v; want %v", c.svg, got, err, c.want)
		}
	}
	if z := Zoom(Size{100, 50}); z != 2 {
		t.Errorf("zoom = %v", z)
	}
	if z := Zoom(Size{4096, 10}); z != 1 {
		t.Errorf("zoom of the largest = %v", z)
	}
}

func TestCheckRefs(t *testing.T) {
	root := t.TempDir()
	draw := filepath.Join(root, "Г", "_assets")
	os.MkdirAll(draw, 0o755)
	os.WriteFile(filepath.Join(draw, "фон.png"), []byte("png"), 0o644)
	os.WriteFile(filepath.Join(root, "общий.png"), []byte("png"), 0o644)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret.png"), []byte("png"), 0o644)
	os.Symlink(filepath.Join(outside, "secret.png"), filepath.Join(draw, "link.png"))
	os.Symlink(outside, filepath.Join(draw, "dir"))

	cases := map[string]string{
		`#grad`:                       "",
		`data:image/png;base64,iVBOR`: "",
		`фон.png`:                     "",
		`%D1%84%D0%BE%D0%BD.png`:      "",
		`../../общий.png`:             "",
		`../../../secret.png`:         "outside the vault",
		`/etc/passwd`:                 "relative to the drawing",
		`file:///etc/passwd`:          "only pictures inside the vault",
		`https://example.com/a.png`:   "only pictures inside the vault",
		`//example.com/a.png`:         "only pictures inside the vault",
		`link.png`:                    "symbolic link",
		`dir/secret.png`:              "symbolic link",
		`нет.png`:                     "does not exist",
	}
	for ref, want := range cases {
		for _, attr := range []string{"href", "xlink:href"} {
			p := filepath.Join(draw, "a.svg")
			os.WriteFile(p, []byte(`<svg xmlns:xlink="http://www.w3.org/1999/xlink" width="10" height="10"><g><image `+attr+`="`+ref+`"/></g></svg>`), 0o644)
			_, err := Check(p, root)
			switch {
			case want == "" && err != nil:
				t.Errorf("%s=%q: %v", attr, ref, err)
			case want != "" && (err == nil || !strings.Contains(err.Error(), want)):
				t.Errorf("%s=%q: err = %v, want %q", attr, ref, err, want)
			}
		}
	}
	// The drawing itself may not be a link.
	os.WriteFile(filepath.Join(outside, "x.svg"), []byte(`<svg/>`), 0o644)
	os.Symlink(filepath.Join(outside, "x.svg"), filepath.Join(draw, "x.svg"))
	if _, err := Check(filepath.Join(draw, "x.svg"), root); err == nil {
		t.Error("linked drawing accepted")
	}
	// Too big a file.
	big := filepath.Join(draw, "big.svg")
	os.WriteFile(big, []byte("<svg>"+strings.Repeat(" ", MaxFileSize)+"</svg>"), 0o644)
	if _, err := Check(big, root); err == nil || !strings.Contains(err.Error(), "bytes") {
		t.Errorf("big file: %v", err)
	}
}

// Package lint validates a vault: its structure, links and images, SVG
// drawings, math and Telegram message limits.
package lint

import (
	"context"
	"path"
	"sort"
	"strings"

	"github.com/Mimic890/terakoya/internal/content"
	"github.com/Mimic890/terakoya/internal/raster"
	"github.com/Mimic890/terakoya/internal/render"
)

// Run lints the vault at dir. ras converts SVGs to check them; if it is
// nil or not available, the check is skipped with an info line. The error
// is set only if dir cannot be read.
func Run(ctx context.Context, dir string, ras raster.Rasterizer) ([]content.Issue, error) {
	cat, issues, err := content.Load(dir)
	if err != nil {
		return nil, err
	}
	used := map[string]bool{}
	for _, n := range cat.Notes() {
		res := render.Convert(cat, n, render.Options{})
		issues = append(issues, res.Issues...)
		for _, m := range res.Media() {
			used[m.Path] = true
		}
	}
	issues = append(issues, images(ctx, cat, used, ras)...)

	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Path != issues[j].Path {
			return issues[i].Path < issues[j].Path
		}
		return issues[i].Line < issues[j].Line
	})
	return issues, nil
}

// images warns about pictures no note shows and reports SVGs that fail to
// rasterize: too big, referring to files outside the vault, or rejected
// by resvg.
func images(ctx context.Context, cat *content.Catalog, used map[string]bool, ras raster.Rasterizer) []content.Issue {
	var out []content.Issue
	var svgs []string
	for _, rel := range cat.Files() {
		ext := strings.ToLower(path.Ext(rel))
		if !render.ImageExts[ext] {
			continue
		}
		if !used[rel] {
			out = append(out, content.Issue{Path: rel, Severity: content.Warning, Msg: "picture is not shown by any note"})
		}
		if ext == ".svg" {
			svgs = append(svgs, rel)
		}
	}
	// Size and the files a drawing refers to are checked without resvg.
	var ok []string
	for _, rel := range svgs {
		if _, err := raster.Check(cat.Root+"/"+rel, cat.Root); err != nil {
			out = append(out, content.Issue{Path: rel, Severity: content.Error, Msg: "cannot rasterize: " + err.Error()})
			continue
		}
		ok = append(ok, rel)
	}
	if len(ok) == 0 {
		return out
	}
	if r, isResvg := ras.(*raster.Resvg); ras == nil || isResvg && !r.Available() {
		return append(out, content.Issue{Path: ".", Severity: content.Info,
			Msg: "SVG drawings were not rendered: " + raster.ErrNoResvg.Error()})
	}
	for _, rel := range ok {
		if _, err := ras.Rasterize(ctx, cat.Root+"/"+rel); err != nil {
			out = append(out, content.Issue{Path: rel, Severity: content.Error, Msg: "cannot rasterize: " + err.Error()})
		}
	}
	return out
}

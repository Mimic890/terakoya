// Package lint validates a content directory: structure, frontmatter,
// links, images, math and Telegram message limits.
package lint

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Mimic890/terakoya/internal/content"
	"github.com/Mimic890/terakoya/internal/render"
)

// Run lints the catalog at dir. The error is set only if dir cannot be read.
func Run(dir string) ([]content.Issue, error) {
	cat, issues, err := content.Load(dir)
	if err != nil {
		return nil, err
	}
	used := map[string]bool{}
	for _, a := range cat.Articles() {
		res := render.Convert(cat, a, render.Options{})
		issues = append(issues, res.Issues...)
		for _, m := range res.Media {
			used[m.AbsPath] = true
		}
	}
	issues = append(issues, ambiguousTitles(cat)...)
	issues = append(issues, unusedAssets(cat, used)...)

	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Path != issues[j].Path {
			return issues[i].Path < issues[j].Path
		}
		return issues[i].Line < issues[j].Line
	})
	return issues, nil
}

// ambiguousTitles warns when two articles share a title or alias, since
// [[Title]] links to them only resolve within the same grade.
func ambiguousTitles(cat *content.Catalog) []content.Issue {
	owners := map[string][]*content.Article{}
	for _, a := range cat.Articles() {
		seen := map[string]bool{}
		for _, k := range append([]string{a.Title}, a.Aliases...) {
			k = strings.ToLower(strings.Join(strings.Fields(k), " "))
			if !seen[k] {
				seen[k] = true
				owners[k] = append(owners[k], a)
			}
		}
	}
	var out []content.Issue
	for _, list := range owners {
		if len(list) < 2 {
			continue
		}
		for _, a := range list[1:] {
			out = append(out, content.Issue{
				Path: a.Path, Line: 1, Severity: content.Warning,
				Msg: "title or alias is shared with " + list[0].Path + "; [[links]] to it are ambiguous across grades",
			})
		}
	}
	return out
}

// unusedAssets warns about image files that no article references.
func unusedAssets(cat *content.Catalog, used map[string]bool) []content.Issue {
	var out []content.Issue
	_ = filepath.WalkDir(cat.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != cat.Root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !render.ImageExts[strings.ToLower(filepath.Ext(p))] || used[p] {
			return nil
		}
		rel, _ := filepath.Rel(cat.Root, p)
		out = append(out, content.Issue{Path: filepath.ToSlash(rel), Severity: content.Warning, Msg: "image is not used by any article"})
		return nil
	})
	return out
}

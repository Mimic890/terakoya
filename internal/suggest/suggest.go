// Package suggest turns what a user sends to the bot into a file change for
// the content repository: it fills in frontmatter, picks file names and
// validates the result.
package suggest

import (
	"bytes"
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Mimic890/terakoya/internal/content"
	"github.com/Mimic890/terakoya/internal/render"
)

// Code identifies why a submission was rejected. The bot maps codes to
// user-facing texts.
type Code string

const (
	TooLarge        Code = "too_large" // arg: limit in KB
	NotUTF8         Code = "not_utf8"
	Empty           Code = "empty"
	NoFrontmatter   Code = "no_frontmatter"
	OpenFrontmatter Code = "open_frontmatter"
	BadFrontmatter  Code = "bad_frontmatter" // arg: parser error
	NoTitle         Code = "no_title"
	BadID           Code = "bad_id"     // args: id, max length
	TakenID         Code = "taken_id"   // args: id, other title
	OverLimit       Code = "over_limit" // arg: limit message
	BadPath         Code = "bad_path"   // arg: path
)

// Problem is a reason to reject a submission.
type Problem struct {
	Code Code
	Args []any
}

func (p *Problem) Error() string { return fmt.Sprintf("%s %v", p.Code, p.Args) }

func problem(code Code, args ...any) error { return &Problem{Code: code, Args: args} }

// Result is a validated article ready to commit.
type Result struct {
	// Path is relative to the catalog root.
	Path  string
	Title string
	Data  []byte
	// Issues are lint findings that do not block the submission; they are
	// listed in the pull request for the reviewer.
	Issues []content.Issue
	// Unchanged is set when an edit is identical to the current article.
	Unchanged bool
}

// Edit prepares a new version of an existing article. A submission without
// frontmatter keeps the article's current frontmatter.
func Edit(cat *content.Catalog, a *content.Article, submitted []byte, uploaded []string, maxBytes int64) (*Result, error) {
	data, err := clean(submitted, maxBytes)
	if err != nil {
		return nil, err
	}
	_, _, _, ok, err := content.SplitFrontmatter(data)
	if err != nil {
		return nil, problem(OpenFrontmatter)
	}
	if !ok {
		yamlSrc, _, _, _, _ := content.SplitFrontmatter(a.Raw)
		data = append([]byte("---\n"+yamlSrc+"---\n\n"), bytes.TrimLeft(data, "\n")...)
	}
	res, err := check(cat, a.Grade, a.Path, a, data, uploaded)
	if err != nil {
		return nil, err
	}
	res.Unchanged = bytes.Equal(normalize(data), normalize(a.Raw))
	return res, nil
}

// New prepares a new article in grade g. Without frontmatter, the first
// non-empty line (minus any leading #) becomes the title.
func New(cat *content.Catalog, g *content.Grade, submitted []byte, uploaded []string, maxBytes int64) (*Result, error) {
	data, err := clean(submitted, maxBytes)
	if err != nil {
		return nil, err
	}
	order := nextOrder(g)
	_, _, _, ok, err := content.SplitFrontmatter(data)
	if err != nil {
		return nil, problem(OpenFrontmatter)
	}
	if !ok {
		title := firstLine(data)
		if title == "" {
			return nil, problem(Empty)
		}
		fm := fmt.Sprintf("---\ntitle: %s\norder: %d\n---\n\n", yamlString(title), order)
		data = append([]byte(fm), bytes.TrimLeft(data, "\n")...)
	}
	yamlSrc, _, _, _, _ := content.SplitFrontmatter(data)
	fm, _, err := content.ParseFrontmatter(yamlSrc)
	if err != nil || strings.TrimSpace(fm.Title) == "" {
		return nil, problem(NoTitle)
	}
	slug := Slug(fm.Title, 40)
	if slug == "" {
		slug = "topic"
	}
	dir := g.Subject.Dir + "/" + g.Dir
	name := fmt.Sprintf("%02d-%s.md", order, slug)
	for i := 2; existsInGrade(g, dir+"/"+name); i++ {
		name = fmt.Sprintf("%02d-%s-%d.md", order, slug, i)
	}
	return check(cat, g, dir+"/"+name, nil, data, uploaded)
}

// clean checks size and encoding and normalizes line endings.
func clean(data []byte, maxBytes int64) ([]byte, error) {
	if int64(len(data)) > maxBytes {
		return nil, problem(TooLarge, maxBytes>>10)
	}
	if !utf8.Valid(data) {
		return nil, problem(NotUTF8)
	}
	data = bytes.TrimPrefix(data, []byte("\ufeff"))
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, problem(Empty)
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		data = append(data, '\n')
	}
	return data, nil
}

func normalize(b []byte) []byte {
	return bytes.TrimSpace(bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")))
}

// check parses the article as the loader would and renders it against the
// catalog. Blocking problems become errors; lint findings are returned.
func check(cat *content.Catalog, g *content.Grade, relPath string, self *content.Article, data []byte, uploaded []string) (*Result, error) {
	if err := confine(relPath); err != nil {
		return nil, err
	}
	yamlSrc, body, bodyLine, ok, err := content.SplitFrontmatter(data)
	if err != nil {
		return nil, problem(OpenFrontmatter)
	}
	if !ok {
		return nil, problem(NoFrontmatter)
	}
	fm, _, err := content.ParseFrontmatter(yamlSrc)
	if err != nil {
		return nil, problem(BadFrontmatter, err.Error())
	}
	title := strings.TrimSpace(fm.Title)
	if title == "" {
		return nil, problem(NoTitle)
	}
	id := fm.ID
	if id == "" {
		if self != nil && self.Path == relPath {
			id = derivedID(self)
		} else {
			id = g.Subject.Dir + "-" + g.Dir + "-" + strings.TrimSuffix(orderPrefix.ReplaceAllString(path.Base(relPath), ""), ".md")
		}
	}
	if !content.ValidID(id) {
		return nil, problem(BadID, id, content.MaxIDLength)
	}
	if other := cat.Article(id); other != nil && other != self {
		return nil, problem(TakenID, id, other.Title)
	}

	art := &content.Article{Grade: g, ID: id, Title: title, Path: relPath, Raw: data, Body: body, BodyLine: bodyLine}
	r := render.Convert(cat, art, render.Options{})
	var issues []content.Issue
	for _, is := range r.Issues {
		if strings.HasPrefix(is.Msg, "message limit:") {
			return nil, problem(OverLimit, strings.TrimPrefix(is.Msg, "message limit: "))
		}
		if pendingImage(is.Msg, uploaded) {
			continue
		}
		issues = append(issues, is)
	}
	return &Result{Path: relPath, Title: title, Data: data, Issues: issues}, nil
}

// derivedID is the ID an article has when its frontmatter sets none.
func derivedID(a *content.Article) string {
	return a.Grade.Subject.Dir + "-" + a.Grade.Dir + "-" + a.Slug
}

var orderPrefix = regexp.MustCompile(`^(\d+)[-_. ]+`)

// pendingImage reports whether a "file not found" issue is about an image
// the user uploaded with this suggestion; it only exists on the branch.
func pendingImage(msg string, uploaded []string) bool {
	if !strings.HasSuffix(msg, ": file not found") {
		return false
	}
	for _, name := range uploaded {
		if strings.Contains(msg, "image assets/"+name+":") || strings.Contains(msg, "image ./assets/"+name+":") {
			return true
		}
	}
	return false
}

// confine makes sure a catalog-relative path cannot leave the catalog.
func confine(rel string) error {
	clean := path.Clean(rel)
	if clean != rel || path.IsAbs(rel) || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(rel, "\\") {
		return problem(BadPath, rel)
	}
	return nil
}

// RepoPath joins the catalog subdirectory and a catalog-relative path,
// refusing anything that escapes the catalog.
func RepoPath(subdir, rel string) (string, error) {
	if err := confine(rel); err != nil {
		return "", err
	}
	return path.Join(subdir, rel), nil
}

func existsInGrade(g *content.Grade, rel string) bool {
	for _, a := range g.Articles {
		if a.Path == rel {
			return true
		}
	}
	return false
}

func nextOrder(g *content.Grade) int {
	max := 0
	for _, a := range g.Articles {
		if a.Order < 1<<30 && a.Order > max {
			max = a.Order
		}
	}
	return max + 1
}

func firstLine(data []byte) string {
	for _, l := range strings.Split(string(data), "\n") {
		l = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "#"))
		if l != "" {
			return l
		}
	}
	return ""
}

// yamlString quotes a title when YAML would otherwise misread it.
func yamlString(s string) string {
	if strings.ContainsAny(s, ":#'\"[]{},&*!|>%@`") || strings.TrimSpace(s) != s {
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
	}
	return s
}

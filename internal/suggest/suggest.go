// Package suggest turns what a user sends to the bot into file changes for
// the vault: it picks file names and folders, links new notes from the
// main page and validates the result.
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
	TooLarge  Code = "too_large" // arg: limit in KB
	NotUTF8   Code = "not_utf8"
	Empty     Code = "empty"
	NoTitle   Code = "no_title"
	TakenName Code = "taken_name" // arg: the taken file name
	BadPath   Code = "bad_path"   // arg: path
)

// Problem is a reason to reject a submission.
type Problem struct {
	Code Code
	Args []any
}

func (p *Problem) Error() string { return fmt.Sprintf("%s %v", p.Code, p.Args) }

func problem(code Code, args ...any) error { return &Problem{Code: code, Args: args} }

// File is a file to write in the vault.
type File struct {
	Path string // vault-relative
	Data []byte
}

// Result is a validated change ready to commit.
type Result struct {
	// Path is the note the suggestion is about, vault-relative.
	Path  string
	Title string
	Data  []byte
	// Also are further files changed with it, such as the main page that
	// links a new note.
	Also []File
	// Issues are lint findings that do not block the submission; they are
	// listed in the pull request for the reviewer.
	Issues []content.Issue
}

// Files returns every file of the change.
func (r *Result) Files() []File {
	return append([]File{{Path: r.Path, Data: r.Data}}, r.Also...)
}

// Edit validates a new version of an existing note.
func Edit(cat *content.Catalog, n *content.Note, submitted []byte, maxBytes int64) (*Result, error) {
	data, err := clean(submitted, maxBytes)
	if err != nil {
		return nil, err
	}
	return check(cat, n.Topic, n.Path, data), nil
}

// NewTopic prepares a topic in grade g: the folder "<number> <title>"
// with the main page "<title>.md". The number follows the largest one in
// the grade, padded to the width the grade uses.
func NewTopic(cat *content.Catalog, g *content.Grade, title, text string, maxBytes int64) (*Result, error) {
	title, name, err := names(cat, title)
	if err != nil {
		return nil, err
	}
	body, err := clean([]byte(text), maxBytes)
	if err != nil {
		return nil, err
	}
	number, width := 0, 2
	for _, t := range g.Topics {
		if t.HasNumber {
			number = max(number, t.Number)
			width = max(width, t.Width)
		}
	}
	dir := fmt.Sprintf("%s/%0*d %s", g.Path(), width, number+1, name)
	rel := dir + "/" + name + ".md"
	if err := confine(rel); err != nil {
		return nil, err
	}
	data := append([]byte("# "+title+"\n\n"), body...)
	t := &content.Topic{Grade: g, Dir: path.Base(dir)}
	return check(cat, t, rel, data), nil
}

// NewNote prepares a note of a kind (a folder name such as "теоремы") in
// topic t and links it from the main page. A corollary ("следствия") of
// parent goes next to the parent's file and is linked under it.
func NewNote(cat *content.Catalog, t *content.Topic, kind string, parent *content.Note, title, text string, maxBytes int64) (*Result, error) {
	title, name, err := names(cat, title)
	if err != nil {
		return nil, err
	}
	body, err := clean([]byte(text), maxBytes)
	if err != nil {
		return nil, err
	}
	dir := t.Path() + "/" + kindFolder(t, kind)
	if kind == "следствия" && parent != nil && parent.Kind != "следствия" {
		dir = path.Dir(parent.Path) + "/" + kindFolder(t, kind)
	}
	rel := dir + "/" + name + ".md"
	if err := confine(rel); err != nil {
		return nil, err
	}
	main := t.Main
	back := fmt.Sprintf("\n---\n↩ [%s](%s)\n", main.Title, LinkTarget(main.FileName()))
	data := append(append([]byte("# "+title+"\n\n"), body...), back...)
	res := check(cat, t, rel, data)
	link := fmt.Sprintf("[%s](%s)", title, LinkTarget(name+".md"))
	res.Also = []File{{Path: main.Path, Data: []byte(InsertLink(cat, main, kind, parent, link))}}
	return res, nil
}

// kindFolder is the topic's existing folder for a kind, in whatever case
// it is written, or the kind itself.
func kindFolder(t *content.Topic, kind string) string {
	for _, n := range t.Notes {
		if n.Kind == kind {
			return path.Base(path.Dir(n.Path))
		}
	}
	return kind
}

// names checks a title and returns it cleaned up with the file name stem
// made from it. File names must be unique in the vault, so that a link
// with just the name finds the note.
func names(cat *content.Catalog, title string) (string, string, error) {
	title = strings.Join(strings.Fields(title), " ")
	name := content.SafeFileName(title)
	if title == "" || name == "" {
		return "", "", problem(NoTitle)
	}
	if len(cat.FileNamed(name+".md")) > 0 {
		return "", "", problem(TakenName, name+".md")
	}
	return title, name, nil
}

// LinkTarget writes a file name as the vault writes link targets: spaces
// and parentheses percent-encoded, Cyrillic as is.
func LinkTarget(name string) string {
	return strings.NewReplacer("%", "%25", " ", "%20", "(", "%28", ")", "%29", "#", "%23",
		"<", "%3C", ">", "%3E", "[", "%5B", "]", "%5D").Replace(name)
}

var listLine = regexp.MustCompile(`^([ \t]*)(?:[-*+]|\d+[.)])[ \t]`)

// InsertLink adds "- link" to a main page. A corollary goes under its
// parent's list item, one level deeper; other notes go after the last item
// linking a note of the same kind. Without such an item, a "## <Kind>"
// section is added at the end.
func InsertLink(cat *content.Catalog, main *content.Note, kind string, parent *content.Note, link string) string {
	lines := strings.Split(strings.TrimRight(main.Body, "\n"), "\n")
	// at is the index of the item to insert after, by what it links.
	at, indent := -1, ""
	for i, l := range lines {
		m := listLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		n := firstNote(cat, main, l)
		switch {
		case n == nil:
		case kind == "следствия" && parent != nil:
			if n == parent {
				at, indent = i, m[1]+"\t"
			}
		case n.Kind == kind && n.Topic == main.Topic:
			at, indent = i, m[1]
		}
	}
	item := "- " + link
	if at < 0 {
		heading := content.KindHeading(kind)
		out := strings.Join(lines, "\n") + "\n\n## " + heading + "\n\n" + item + "\n"
		return out
	}
	// Skip the item's own continuation and nested lines.
	end := at + 1
	base := width(listLine.FindStringSubmatch(lines[at])[1])
	for end < len(lines) {
		l := lines[end]
		if strings.TrimSpace(l) == "" {
			break
		}
		if m := listLine.FindStringSubmatch(l); m != nil && width(m[1]) <= base {
			break
		}
		if !strings.HasPrefix(l, " ") && !strings.HasPrefix(l, "\t") && listLine.FindStringSubmatch(l) == nil {
			break
		}
		end++
	}
	out := append(append(append([]string{}, lines[:end]...), indent+item), lines[end:]...)
	return strings.Join(out, "\n") + "\n"
}

// firstNote returns the note the first resolvable link of a line points to.
func firstNote(cat *content.Catalog, from *content.Note, line string) *content.Note {
	for _, lk := range content.Links(line) {
		if n, err := cat.ResolveNote(from, lk); err == nil {
			return n
		}
	}
	return nil
}

func width(s string) int {
	n := 0
	for _, c := range s {
		if c == '\t' {
			n += 4
		} else {
			n++
		}
	}
	return n
}

// clean checks size and encoding and normalizes line endings.
func clean(data []byte, maxBytes int64) ([]byte, error) {
	if int64(len(data)) > maxBytes {
		return nil, problem(TooLarge, maxBytes>>10)
	}
	if !utf8.Valid(data) {
		return nil, problem(NotUTF8)
	}
	data = content.CleanSource(data)
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, problem(Empty)
	}
	data = append(bytes.TrimRight(data, "\n"), '\n')
	return data, nil
}

// check renders the note as it would be shown and returns its lint
// findings for the reviewer.
func check(cat *content.Catalog, t *content.Topic, rel string, data []byte) *Result {
	n := &content.Note{Topic: t, Path: rel, Raw: data, Body: string(data)}
	content.ScanLines(n.Body, func(_ int, l string, code bool) {
		if n.Title == "" && !code && strings.HasPrefix(l, "# ") {
			n.Title = strings.TrimSpace(strings.TrimPrefix(l, "# "))
		}
	})
	if n.Title == "" {
		n.Title = content.TitleFromFile(path.Base(rel))
	}
	r := render.Convert(cat, n, render.Options{})
	return &Result{Path: rel, Title: n.Title, Data: data, Issues: r.Issues}
}

// confine makes sure a vault-relative path cannot leave the vault.
func confine(rel string) error {
	clean := path.Clean(rel)
	if clean != rel || path.IsAbs(rel) || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(rel, "\\") {
		return problem(BadPath, rel)
	}
	return nil
}

// RepoPath checks a vault-relative path from a session before it is
// committed.
func RepoPath(rel string) (string, error) {
	if err := confine(rel); err != nil {
		return "", err
	}
	return rel, nil
}

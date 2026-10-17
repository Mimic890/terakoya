package content

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// FileExts are the files a vault may contain; everything else is ignored.
var FileExts = map[string]bool{".md": true, ".svg": true, ".png": true, ".jpg": true, ".jpeg": true}

// AssetsDir is the folder with a topic's pictures.
const AssetsDir = "_assets"

// Hidden reports whether a file or folder is ignored because its name
// starts with a dot: .obsidian, .trash, .git, .gitignore.
func Hidden(name string) bool { return strings.HasPrefix(name, ".") }

// Load reads the vault at root. Structural problems are returned as issues;
// the catalog skips what it cannot use.
func Load(root string) (*Catalog, []Issue, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, err
	}
	l := &loader{cat: &Catalog{Root: abs, byID: map[string]*Note{}, byPath: map[string]*Note{}, groups: map[string]any{},
		files: map[string]bool{}, byName: map[string][]string{}, byFolded: map[string][]string{}}}
	if err := l.indexFiles(); err != nil {
		return nil, nil, err
	}
	entries, err := l.readDir("")
	if err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			l.unexpected(e.Name(), "file in the vault root; subjects are folders")
			continue
		}
		if s := l.subject(e.Name()); s != nil {
			l.cat.Subjects = append(l.cat.Subjects, s)
		}
	}
	sort.SliceStable(l.cat.Subjects, func(i, j int) bool { return l.cat.Subjects[i].Name < l.cat.Subjects[j].Name })
	for _, n := range l.cat.Notes() {
		if other := l.cat.byID[n.ID]; other != nil && other != n {
			l.issue(n.Path, 0, Error, "ID %s collides with %s; rename one of them", n.ID, other.Path)
			continue
		}
		l.cat.byID[n.ID] = n
	}
	return l.cat, l.issues, nil
}

type loader struct {
	cat    *Catalog
	issues []Issue
}

func (l *loader) issue(p string, line int, sev Severity, format string, args ...any) {
	l.issues = append(l.issues, Issue{Path: p, Line: line, Severity: sev, Msg: fmt.Sprintf(format, args...)})
}

func (l *loader) unexpected(p, why string) { l.issue(p, 0, Warning, "unexpected %s", why) }

// readDir lists a vault folder without hidden entries.
func (l *loader) readDir(rel string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(filepath.Join(l.cat.Root, filepath.FromSlash(rel)))
	if err != nil {
		return nil, err
	}
	var out []os.DirEntry
	for _, e := range entries {
		if !Hidden(e.Name()) {
			out = append(out, e)
		}
	}
	return out, nil
}

// indexFiles records every usable file for link resolution, and reports
// files of other types.
func (l *loader) indexFiles() error {
	return filepath.WalkDir(l.cat.Root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == l.cat.Root {
			return nil
		}
		if Hidden(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(l.cat.Root, p)
		rel = filepath.ToSlash(rel)
		if !FileExts[strings.ToLower(path.Ext(rel))] {
			l.unexpected(rel, "file type; only .md, .svg, .png, .jpg and .jpeg are used")
			return nil
		}
		l.cat.files[rel] = true
		name := path.Base(rel)
		l.cat.byName[name] = append(l.cat.byName[name], rel)
		l.cat.byFolded[strings.ToLower(name)] = append(l.cat.byFolded[strings.ToLower(name)], rel)
		return nil
	})
}

func (l *loader) subject(name string) *Subject {
	s := &Subject{ID: ID(name), Name: name}
	l.cat.groups[s.ID] = s
	entries, err := l.readDir(name)
	if err != nil {
		l.issue(name, 0, Error, "%v", err)
		return nil
	}
	for _, e := range entries {
		rel := name + "/" + e.Name()
		n, ok := ParseGrade(e.Name())
		switch {
		case !e.IsDir():
			l.unexpected(rel, "file in a subject folder; grades are folders named «<N> класс»")
		case !ok:
			l.unexpected(rel, "folder in a subject; grades are named «<N> класс»")
		default:
			g := &Grade{ID: ID(rel), Subject: s, Dir: e.Name(), Number: n}
			l.cat.groups[g.ID] = g
			l.grade(g)
			s.Grades = append(s.Grades, g)
		}
	}
	sort.SliceStable(s.Grades, func(i, j int) bool { return s.Grades[i].Number < s.Grades[j].Number })
	return s
}

func (l *loader) grade(g *Grade) {
	entries, err := l.readDir(g.Path())
	if err != nil {
		l.issue(g.Path(), 0, Error, "%v", err)
		return
	}
	numbers := map[int]string{}
	for _, e := range entries {
		rel := g.Path() + "/" + e.Name()
		if !e.IsDir() {
			l.unexpected(rel, "file in a grade folder; topics are folders")
			continue
		}
		t := &Topic{Grade: g, Dir: e.Name()}
		t.Number, _, t.Width, t.HasNumber = ParseNumber(e.Name())
		if !t.HasNumber {
			l.issue(rel, 0, Warning, "topic folder has no number, so it is listed last; name it like «05 %s»", e.Name())
		} else if other, dup := numbers[t.Number]; dup {
			l.issue(rel, 0, Error, "topic number %d is also used by «%s»", t.Number, other)
		} else {
			numbers[t.Number] = e.Name()
		}
		if l.topic(t) {
			g.Topics = append(g.Topics, t)
		}
	}
	sort.SliceStable(g.Topics, func(i, j int) bool {
		a, b := g.Topics[i], g.Topics[j]
		if a.HasNumber != b.HasNumber {
			return a.HasNumber
		}
		if a.HasNumber && a.Number != b.Number {
			return a.Number < b.Number
		}
		return stripNumber(a.Dir) < stripNumber(b.Dir)
	})
}

// topic loads the main page and the notes of a topic folder.
func (l *loader) topic(t *Topic) bool {
	entries, err := l.readDir(t.Path())
	if err != nil {
		l.issue(t.Path(), 0, Error, "%v", err)
		return false
	}
	var mains []string
	for _, e := range entries {
		rel := t.Path() + "/" + e.Name()
		switch {
		case e.IsDir() && e.Name() == AssetsDir:
		case e.IsDir():
			l.kindFolder(t, rel, strings.ToLower(e.Name()))
		case strings.EqualFold(path.Ext(e.Name()), ".md"):
			mains = append(mains, rel)
		case l.cat.files[rel]:
			l.unexpected(rel, "picture in the topic folder; put pictures into "+AssetsDir+"/")
		}
	}
	switch len(mains) {
	case 0:
		l.issue(t.Path(), 0, Error, "topic folder has no main page (.md file in the folder itself)")
		return false
	case 1:
	default:
		l.issue(t.Path(), 0, Error, "topic folder has %d .md files in its root; only the main page may be there: %s",
			len(mains), strings.Join(baseNames(mains), ", "))
	}
	t.Main = l.note(t, mains[0], "")
	if t.Main == nil {
		return false
	}
	l.order(t)
	return true
}

func baseNames(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = path.Base(p)
	}
	return out
}

// kindFolder loads the notes of a kind folder and its subfolders, such as
// теоремы/следствия. The kind of a note is the folder it is in.
func (l *loader) kindFolder(t *Topic, rel, kind string) {
	entries, err := l.readDir(rel)
	if err != nil {
		l.issue(rel, 0, Error, "%v", err)
		return
	}
	for _, e := range entries {
		p := rel + "/" + e.Name()
		switch {
		case e.IsDir():
			l.kindFolder(t, p, strings.ToLower(e.Name()))
		case strings.EqualFold(path.Ext(e.Name()), ".md"):
			if n := l.note(t, p, kind); n != nil {
				t.Notes = append(t.Notes, n)
			}
		}
	}
}

var h1 = regexp.MustCompile(`^#[ \t]+(.+?)[ \t#]*$`)

func (l *loader) note(t *Topic, rel, kind string) *Note {
	abs := filepath.Join(l.cat.Root, filepath.FromSlash(rel))
	raw, err := os.ReadFile(abs)
	if err != nil {
		l.issue(rel, 0, Error, "%v", err)
		return nil
	}
	n := &Note{Topic: t, ID: ID(rel), Kind: kind, Path: rel, AbsPath: abs, Raw: raw, Body: string(CleanSource(raw))}
	ScanLines(n.Body, func(line int, s string, code bool) {
		if n.TitleLine == 0 && !code {
			if m := h1.FindStringSubmatch(s); m != nil {
				n.Title, n.TitleLine = strings.TrimSpace(m[1]), line
			}
		}
	})
	if n.Title == "" {
		n.Title = TitleFromFile(path.Base(rel))
		l.issue(rel, 0, Warning, "note has no title line «# …»; using the file name")
	}
	l.cat.byPath[rel] = n
	return n
}

var listItem = regexp.MustCompile(`^([ \t]*)(?:[-*+]|\d+[.)])[ \t]`)

// order sorts a topic's notes as its main page links them. A note linked
// from a list item nested under another note's item gets that note as its
// parent. Unlinked notes follow, by the number in their "(…)", then by name.
func (l *loader) order(t *Topic) {
	inTopic := map[*Note]bool{}
	for _, n := range t.Notes {
		inTopic[n] = true
	}
	type item struct {
		indent int
		note   *Note
	}
	var ordered []*Note
	seen := map[*Note]bool{}
	var stack []item
	ScanLines(t.Main.Body, func(_ int, line string, code bool) {
		if code {
			return
		}
		m := listItem.FindStringSubmatch(line)
		indent := -1
		if m != nil {
			indent = width(m[1])
			for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
				stack = stack[:len(stack)-1]
			}
		} else if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			stack = nil
		}
		first := true
		for _, lk := range Links(line) {
			if lk.Image || IsRemote(lk.Target) {
				continue
			}
			n, err := l.cat.ResolveNote(t.Main, lk)
			if err != nil || !inTopic[n] {
				continue
			}
			if !seen[n] {
				seen[n] = true
				ordered = append(ordered, n)
				if m != nil && first && len(stack) > 0 {
					n.Parent = stack[len(stack)-1].note
				}
			}
			if m != nil && first {
				stack = append(stack, item{indent, n})
			}
			first = false
		}
	})
	var rest []*Note
	for _, n := range t.Notes {
		if !seen[n] {
			rest = append(rest, n)
		}
	}
	sort.SliceStable(rest, func(i, j int) bool {
		a, b := rest[i].FileName(), rest[j].FileName()
		if less, ok := lessHint(sortHint(a), sortHint(b)); ok {
			return less
		}
		return a < b
	})
	t.Notes = append(ordered, rest...)
}

// width counts indentation, a tab as four spaces.
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

// Package content loads an Obsidian vault of school material: subjects,
// grades, topics and the notes of each topic. Everything comes from folder
// names and note text; there are no config files and no frontmatter.
//
//	Геометрия/                         subject
//	  10 класс/                        grade
//	    05 Параллельность …/           topic, ordered by its number
//	      Параллельность ….md          main page, the only note in the root
//	      _assets/                     the topic's pictures
//	      теоремы/ задачи/ …           kind folders
//	        Признак … (5.1).md
//	        следствия/                 corollaries
package content

import (
	"bytes"
	"path"
	"sort"
	"strings"
)

// Catalog is a loaded vault.
type Catalog struct {
	// Root is the absolute path of the vault.
	Root     string
	Subjects []*Subject

	byID     map[string]*Note
	byPath   map[string]*Note
	groups   map[string]any // subject and grade IDs
	files    map[string]bool
	byName   map[string][]string // file name → vault-relative paths
	byFolded map[string][]string // lowercased file name → paths
}

// Subject is a top-level folder, such as Геометрия.
type Subject struct {
	ID     string
	Name   string // the folder name, shown as is
	Grades []*Grade
}

// Grade is a "<N> класс" folder.
type Grade struct {
	ID      string
	Subject *Subject
	Dir     string
	Number  int
	Topics  []*Topic
}

// Title is the folder name, e.g. "10 класс".
func (g *Grade) Title() string { return g.Dir }

// Path is the grade folder relative to the vault root.
func (g *Grade) Path() string { return g.Subject.Name + "/" + g.Dir }

// Topic is a folder with a main page and, usually, kind folders with
// theorems, tasks and so on.
type Topic struct {
	Grade *Grade
	Dir   string
	// Number is the numeric prefix of the folder; HasNumber is false for
	// folders without one, which sort last.
	Number    int
	HasNumber bool
	// Width is the number of digits of the prefix as written, e.g. 2 for "05".
	Width int
	Main  *Note
	// Notes are the topic's notes except the main page, in the order the
	// main page links them; notes it does not link come last.
	Notes []*Note
}

// Path is the topic folder relative to the vault root.
func (t *Topic) Path() string { return t.Grade.Path() + "/" + t.Dir }

// Title is the main page's title, or the folder name without its number.
func (t *Topic) Title() string {
	if t.Main != nil {
		return t.Main.Title
	}
	return stripNumber(t.Dir)
}

// Note is one Markdown file.
type Note struct {
	Topic *Topic
	ID    string
	Title string
	// Kind is the name of the folder the note is in, lowercased, such as
	// "теоремы"; empty for the main page.
	Kind string
	// Parent is the note the main page nests this one under, such as the
	// theorem of a corollary.
	Parent *Note
	// Path is relative to the vault root, slash-separated.
	Path    string
	AbsPath string
	// Raw is the file as stored.
	Raw []byte
	// Body is the file with line endings normalized and any byte order mark
	// removed.
	Body string
	// TitleLine is the 1-based line of the H1, or 0.
	TitleLine int
}

// IsMain reports whether the note is its topic's main page.
func (n *Note) IsMain() bool { return n.Topic != nil && n.Topic.Main == n }

// Subject returns the subject the note belongs to.
func (n *Note) Subject() *Subject { return n.Topic.Grade.Subject }

// Grade returns the grade the note belongs to.
func (n *Note) Grade() *Grade { return n.Topic.Grade }

// KindLabel is the singular, capitalized kind: «Теорема», «Задача».
func (n *Note) KindLabel() string { return KindLabel(n.Kind) }

// FileName is the base name of the note's file.
func (n *Note) FileName() string { return path.Base(n.Path) }

// Note looks up a note by ID.
func (c *Catalog) Note(id string) *Note { return c.byID[id] }

// NoteByPath looks up a note by its vault-relative path.
func (c *Catalog) NoteByPath(rel string) *Note { return c.byPath[rel] }

// Subject looks up a subject by ID.
func (c *Catalog) Subject(id string) *Subject {
	s, _ := c.groups[id].(*Subject)
	return s
}

// Grade looks up a grade by ID.
func (c *Catalog) Grade(id string) *Grade {
	g, _ := c.groups[id].(*Grade)
	return g
}

// HasFile reports whether the vault has a file at the relative path.
func (c *Catalog) HasFile(rel string) bool { return c.files[rel] }

// Files returns the vault-relative paths of all usable files, sorted.
func (c *Catalog) Files() []string {
	out := make([]string, 0, len(c.files))
	for p := range c.files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Notes returns every note in catalog order.
func (c *Catalog) Notes() []*Note {
	var out []*Note
	for _, s := range c.Subjects {
		for _, g := range s.Grades {
			for _, t := range g.Topics {
				if t.Main != nil {
					out = append(out, t.Main)
				}
				out = append(out, t.Notes...)
			}
		}
	}
	return out
}

// CleanSource drops a byte order mark and converts CRLF line endings.
func CleanSource(raw []byte) []byte {
	return bytes.ReplaceAll(bytes.TrimPrefix(raw, []byte("\ufeff")), []byte("\r\n"), []byte("\n"))
}

// SplitBacklink separates the trailing back-link block of a note, a "---"
// line followed only by "↩ [Тема](….md)" lines, from the rest. body+tail
// is the original text; tail is empty if there is no such block.
func SplitBacklink(text string) (body, tail string) {
	lines := strings.Split(text, "\n")
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	i := end
	for i > 0 && strings.HasPrefix(strings.TrimSpace(lines[i-1]), "↩") {
		i--
	}
	if i == end {
		return text, ""
	}
	j := i
	for j > 0 && strings.TrimSpace(lines[j-1]) == "" {
		j--
	}
	if j == 0 || !isRule(lines[j-1]) {
		return text, ""
	}
	start := j - 1
	for start > 0 && strings.TrimSpace(lines[start-1]) == "" {
		start--
	}
	cut := len(strings.Join(lines[:start], "\n"))
	return text[:cut], text[cut:]
}

func isRule(l string) bool {
	t := strings.ReplaceAll(strings.TrimSpace(l), " ", "")
	if len(t) < 3 {
		return false
	}
	return strings.Trim(t, "-") == "" || strings.Trim(t, "*") == "" || strings.Trim(t, "_") == ""
}

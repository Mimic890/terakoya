// Package content loads the catalog of subjects, grades and articles from a
// directory of plain Markdown files.
package content

import (
	"regexp"
	"strings"
)

// MaxIDLength keeps article IDs short enough for both the /start deep-link
// payload (64 chars) and callback data with a short prefix (64 bytes).
const MaxIDLength = 56

// MaxDirsLength limits subject and grade directory names together, so that
// callback data naming a grade fits into 64 bytes.
const MaxDirsLength = 56

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ValidID reports whether id can be used as an article ID.
func ValidID(id string) bool {
	return len(id) > 0 && len(id) <= MaxIDLength && idPattern.MatchString(id)
}

// Catalog is the loaded content tree.
type Catalog struct {
	// Root is the absolute path of the catalog directory.
	Root     string
	Subjects []*Subject

	byID    map[string]*Article
	byTitle map[string][]*Article
	bySlug  map[string][]*Article
}

// Subject is a top-level directory such as "geometry".
type Subject struct {
	Dir    string
	Title  string
	Emoji  string
	Order  int
	Grades []*Grade
}

// Label returns the title prefixed with the emoji, if any.
func (s *Subject) Label() string {
	if s.Emoji == "" {
		return s.Title
	}
	return s.Emoji + " " + s.Title
}

// Grade is a directory inside a subject, usually named after the school grade.
type Grade struct {
	Subject  *Subject
	Dir      string
	Title    string
	Notes    string
	Order    int
	Articles []*Article
}

// Article is a single Markdown file.
type Article struct {
	Grade   *Grade
	ID      string
	Title   string
	Order   int
	Aliases []string
	// Slug is the file name without extension and numeric order prefix.
	Slug string
	// Path is the file path relative to the catalog root, slash-separated.
	Path string
	// AbsPath is the absolute file path.
	AbsPath string
	// Raw is the whole file, frontmatter included.
	Raw []byte
	// Body is the file without frontmatter.
	Body string
	// BodyLine is the 1-based line number in Raw where Body starts.
	BodyLine int
}

// Subject returns the subject the article belongs to.
func (a *Article) Subject() *Subject { return a.Grade.Subject }

// Article looks up an article by ID.
func (c *Catalog) Article(id string) *Article { return c.byID[id] }

// Subject looks up a subject by directory name.
func (c *Catalog) Subject(dir string) *Subject {
	for _, s := range c.Subjects {
		if s.Dir == dir {
			return s
		}
	}
	return nil
}

// Grade looks up a grade by directory name.
func (s *Subject) Grade(dir string) *Grade {
	for _, g := range s.Grades {
		if g.Dir == dir {
			return g
		}
	}
	return nil
}

// Articles returns every article in catalog order.
func (c *Catalog) Articles() []*Article {
	var out []*Article
	for _, s := range c.Subjects {
		for _, g := range s.Grades {
			out = append(out, g.Articles...)
		}
	}
	return out
}

// Resolve finds the article a [[target]] link points to. The target is
// matched against article IDs first, then titles and aliases
// (case-insensitive), then file slugs, preferring the grade of from.
// It returns nil if the target is unknown or ambiguous.
func (c *Catalog) Resolve(from *Article, target string) *Article {
	a, _ := c.resolve(from, target)
	return a
}

// resolve is Resolve that also reports whether a miss was due to ambiguity.
func (c *Catalog) resolve(from *Article, target string) (*Article, bool) {
	target = strings.TrimSpace(target)
	if a := c.byID[target]; a != nil {
		return a, false
	}
	if m := c.byTitle[foldKey(target)]; len(m) > 0 {
		if len(m) == 1 {
			return m[0], false
		}
		return pickSameGrade(from, m)
	}
	if m := c.bySlug[target]; len(m) > 0 {
		if len(m) == 1 {
			return m[0], false
		}
		return pickSameGrade(from, m)
	}
	return nil, false
}

func pickSameGrade(from *Article, m []*Article) (*Article, bool) {
	if from != nil {
		var hit *Article
		for _, a := range m {
			if a.Grade == from.Grade {
				if hit != nil {
					return nil, true
				}
				hit = a
			}
		}
		if hit != nil {
			return hit, false
		}
	}
	return nil, true
}

func foldKey(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

package content

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	subjectFile = "_subject.yml"
	gradeFile   = "_grade.yml"
)

type subjectMeta struct {
	Title string `yaml:"title"`
	Emoji string `yaml:"emoji"`
	Order *int   `yaml:"order"`
}

type gradeMeta struct {
	Title string `yaml:"title"`
	Notes string `yaml:"notes"`
	Order *int   `yaml:"order"`
}

var orderPrefix = regexp.MustCompile(`^(\d+)[-_. ]+`)

// Load reads the catalog rooted at dir. Problems with individual files are
// returned as issues; the error is only set when the directory itself cannot
// be read.
func Load(dir string) (*Catalog, []Issue, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, nil, err
	}
	l := &loader{cat: &Catalog{
		Root:    root,
		byID:    map[string]*Article{},
		byTitle: map[string][]*Article{},
		bySlug:  map[string][]*Article{},
	}}
	for _, e := range entries {
		if !e.IsDir() || skipName(e.Name()) {
			continue
		}
		l.loadSubject(e.Name())
	}
	sort.SliceStable(l.cat.Subjects, func(i, j int) bool {
		a, b := l.cat.Subjects[i], l.cat.Subjects[j]
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.Dir < b.Dir
	})
	return l.cat, l.issues, nil
}

type loader struct {
	cat    *Catalog
	issues []Issue
}

func (l *loader) issue(path string, line int, sev Severity, format string, args ...any) {
	l.issues = append(l.issues, Issue{Path: path, Line: line, Severity: sev, Msg: fmt.Sprintf(format, args...)})
}

func skipName(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "assets"
}

func (l *loader) loadSubject(dir string) {
	abs := filepath.Join(l.cat.Root, dir)
	metaPath := dir + "/" + subjectFile
	var meta subjectMeta
	data, err := os.ReadFile(filepath.Join(abs, subjectFile))
	if os.IsNotExist(err) {
		l.issue(dir, 0, Warning, "directory has no %s and is ignored", subjectFile)
		return
	}
	if err != nil {
		l.issue(metaPath, 0, Error, "%v", err)
		return
	}
	if err := yaml.Unmarshal(data, &meta); err != nil {
		l.issue(metaPath, 0, Error, "invalid YAML: %v", err)
	}
	if strings.TrimSpace(meta.Title) == "" {
		l.issue(metaPath, 0, Error, "title is required")
		meta.Title = dir
	}
	s := &Subject{Dir: dir, Title: meta.Title, Emoji: meta.Emoji, Order: orderOr(meta.Order, 1<<30)}

	entries, err := os.ReadDir(abs)
	if err != nil {
		l.issue(dir, 0, Error, "%v", err)
		return
	}
	for _, e := range entries {
		name := e.Name()
		switch {
		case skipName(name):
		case e.IsDir():
			if g := l.loadGrade(s, name); g != nil {
				s.Grades = append(s.Grades, g)
			}
		case strings.HasSuffix(name, ".md"):
			l.issue(dir+"/"+name, 0, Warning, "articles must be inside a grade directory; file is ignored")
		}
	}
	sort.SliceStable(s.Grades, func(i, j int) bool {
		a, b := s.Grades[i], s.Grades[j]
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.Dir < b.Dir
	})
	l.cat.Subjects = append(l.cat.Subjects, s)
}

func (l *loader) loadGrade(s *Subject, dir string) *Grade {
	rel := s.Dir + "/" + dir
	abs := filepath.Join(l.cat.Root, s.Dir, dir)
	if len(s.Dir)+len(dir) > MaxDirsLength {
		l.issue(rel, 0, Error, "subject and grade directory names are longer than %d bytes together", MaxDirsLength)
		return nil
	}
	g := &Grade{Subject: s, Dir: dir, Order: 1 << 30}
	if n, err := strconv.Atoi(dir); err == nil {
		g.Order = n
	}
	data, err := os.ReadFile(filepath.Join(abs, gradeFile))
	switch {
	case err == nil:
		var meta gradeMeta
		if err := yaml.Unmarshal(data, &meta); err != nil {
			l.issue(rel+"/"+gradeFile, 0, Error, "invalid YAML: %v", err)
		}
		g.Title, g.Notes = meta.Title, meta.Notes
		g.Order = orderOr(meta.Order, g.Order)
	case !os.IsNotExist(err):
		l.issue(rel+"/"+gradeFile, 0, Error, "%v", err)
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		l.issue(rel, 0, Error, "%v", err)
		return nil
	}
	for _, e := range entries {
		name := e.Name()
		if skipName(name) {
			continue
		}
		if e.IsDir() {
			l.issue(rel+"/"+name, 0, Warning, "nested directories other than assets are ignored")
			continue
		}
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		if a := l.loadArticle(g, name); a != nil {
			g.Articles = append(g.Articles, a)
		}
	}
	sort.SliceStable(g.Articles, func(i, j int) bool {
		a, b := g.Articles[i], g.Articles[j]
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.Path < b.Path
	})
	return g
}

func (l *loader) loadArticle(g *Grade, name string) *Article {
	rel := g.Subject.Dir + "/" + g.Dir + "/" + name
	abs := filepath.Join(l.cat.Root, filepath.FromSlash(rel))
	raw, err := os.ReadFile(abs)
	if err != nil {
		l.issue(rel, 0, Error, "%v", err)
		return nil
	}
	stem := strings.TrimSuffix(name, ".md")
	slug := orderPrefix.ReplaceAllString(stem, "")
	a := &Article{
		Grade:   g,
		Slug:    slug,
		Path:    rel,
		AbsPath: abs,
		Raw:     raw,
		Order:   1 << 30,
	}
	if m := orderPrefix.FindStringSubmatch(stem); m != nil {
		a.Order, _ = strconv.Atoi(m[1])
	}

	yamlSrc, body, bodyLine, ok, err := SplitFrontmatter(raw)
	a.Body, a.BodyLine = body, bodyLine
	var fm Frontmatter
	switch {
	case err != nil:
		l.issue(rel, 1, Error, "%v", err)
	case !ok:
		l.issue(rel, 1, Error, "frontmatter with a title is required")
	default:
		var unknown []string
		fm, unknown, err = ParseFrontmatter(yamlSrc)
		if err != nil {
			l.issue(rel, 1, Error, "invalid frontmatter: %v", err)
		}
		sort.Strings(unknown)
		for _, k := range unknown {
			l.issue(rel, 1, Warning, "unknown frontmatter key %q", k)
		}
		if strings.TrimSpace(fm.Title) == "" {
			l.issue(rel, 1, Error, "frontmatter title is required")
		}
	}

	a.Title = strings.TrimSpace(fm.Title)
	if a.Title == "" {
		a.Title = slug
	}
	a.Order = orderOr(fm.Order, a.Order)
	for _, al := range fm.Aliases {
		if al = strings.TrimSpace(al); al != "" {
			a.Aliases = append(a.Aliases, al)
		}
	}

	if fm.ID != "" {
		a.ID = fm.ID
		if !ValidID(a.ID) {
			l.issue(rel, 1, Error, "id %q must be 1-%d characters of A-Z, a-z, 0-9, _ and -", a.ID, MaxIDLength)
			return nil
		}
	} else {
		a.ID = g.Subject.Dir + "-" + g.Dir + "-" + slug
		if !ValidID(a.ID) {
			l.issue(rel, 1, Error, "derived id %q is not valid (1-%d characters of A-Z, a-z, 0-9, _ and -); set id in frontmatter", a.ID, MaxIDLength)
			return nil
		}
	}
	if prev := l.cat.byID[a.ID]; prev != nil {
		l.issue(rel, 1, Error, "id %q is already used by %s", a.ID, prev.Path)
		return nil
	}

	l.cat.byID[a.ID] = a
	for _, key := range append([]string{a.Title}, a.Aliases...) {
		k := foldKey(key)
		l.cat.byTitle[k] = appendUnique(l.cat.byTitle[k], a)
	}
	l.cat.bySlug[slug] = append(l.cat.bySlug[slug], a)
	if stem != slug {
		l.cat.bySlug[stem] = append(l.cat.bySlug[stem], a)
	}
	return a
}

func appendUnique(list []*Article, a *Article) []*Article {
	for _, x := range list {
		if x == a {
			return list
		}
	}
	return append(list, a)
}

func orderOr(p *int, def int) int {
	if p != nil {
		return *p
	}
	return def
}

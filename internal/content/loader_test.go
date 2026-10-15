package content

import (
	"testing"
)

func TestLoadExamples(t *testing.T) {
	cat, issues, err := Load("../../examples/content")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) > 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	if len(cat.Subjects) != 1 {
		t.Fatalf("subjects = %d, want 1", len(cat.Subjects))
	}
	s := cat.Subjects[0]
	if s.Dir != "geometry" || s.Label() != "📐 Геометрия" {
		t.Errorf("subject = %q %q", s.Dir, s.Label())
	}
	if len(s.Grades) != 2 || s.Grades[0].Dir != "7" || s.Grades[1].Dir != "8" {
		t.Fatalf("grades not ordered: %+v", s.Grades)
	}
	g := s.Grade("7")
	if g.Title != "7 класс" {
		t.Errorf("grade title = %q", g.Title)
	}
	if len(g.Articles) != 2 {
		t.Fatalf("articles = %d", len(g.Articles))
	}
	a := g.Articles[0]
	if a.ID != "geometry-7-basic-notions" || a.Slug != "basic-notions" || a.Title != "Основные понятия" {
		t.Errorf("article = %q %q %q", a.ID, a.Slug, a.Title)
	}
	if a.BodyLine != 7 {
		t.Errorf("body line = %d, want 7", a.BodyLine)
	}
	// The frontmatter id overrides the path-derived one.
	if cat.Article("triangles") == nil || cat.Article("geometry-7-triangles") != nil {
		t.Error("frontmatter id not applied")
	}
}

func TestResolve(t *testing.T) {
	cat, _, err := Load("../../examples/content")
	if err != nil {
		t.Fatal(err)
	}
	from := cat.Article("geometry-7-basic-notions")
	cases := map[string]string{
		"triangles":                 "triangles",
		"Треугольники":              "triangles",
		"треугольник":               "triangles", // alias, case-insensitive
		"basic-notions":             "geometry-7-basic-notions",
		"01-basic-notions":          "geometry-7-basic-notions",
		"  Точка   и прямая ":       "geometry-7-basic-notions",
		"geometry-8-quadrilaterals": "geometry-8-quadrilaterals",
	}
	for target, want := range cases {
		a := cat.Resolve(from, target)
		if a == nil || a.ID != want {
			t.Errorf("Resolve(%q) = %v, want %s", target, a, want)
		}
	}
	if cat.Resolve(from, "nothing") != nil {
		t.Error("unknown target resolved")
	}
}

func TestSplitFrontmatter(t *testing.T) {
	cases := []struct {
		in       string
		yaml     string
		body     string
		line     int
		ok, fail bool
	}{
		{in: "---\ntitle: A\n---\nbody", yaml: "title: A\n", body: "body", line: 4, ok: true},
		{in: "\ufeff---\r\ntitle: A\r\n...\r\nbody\r\n", yaml: "title: A\n", body: "body\n", line: 4, ok: true},
		{in: "---\n---\n", yaml: "", body: "", line: 3, ok: true},
		{in: "no frontmatter\n", body: "no frontmatter\n", line: 1},
		{in: "---\ntitle: A\n", body: "---\ntitle: A\n", line: 1, fail: true},
	}
	for _, c := range cases {
		y, b, line, ok, err := SplitFrontmatter([]byte(c.in))
		if y != c.yaml || b != c.body || line != c.line || ok != c.ok || (err != nil) != c.fail {
			t.Errorf("SplitFrontmatter(%q) = %q, %q, %d, %v, %v", c.in, y, b, line, ok, err)
		}
	}
}

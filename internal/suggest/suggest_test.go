package suggest

import (
	"errors"
	"strings"
	"testing"

	"github.com/Mimic890/terakoya/internal/content"
)

func load(t *testing.T) *content.Catalog {
	t.Helper()
	cat, issues, err := content.Load("../../examples/content")
	if err != nil || len(issues) > 0 {
		t.Fatalf("load: %v %v", err, issues)
	}
	return cat
}

func code(err error) Code {
	var p *Problem
	if errors.As(err, &p) {
		return p.Code
	}
	return ""
}

func TestEditKeepsFrontmatter(t *testing.T) {
	cat := load(t)
	a := cat.Article("triangles")
	res, err := Edit(cat, a, []byte("# Треугольники\r\n\r\nНовый текст.\r\n"), nil, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	got := string(res.Data)
	if !strings.HasPrefix(got, "---\ntitle: Треугольники\norder: 2\nid: triangles\n") || !strings.HasSuffix(got, "Новый текст.\n") {
		t.Errorf("data = %q", got)
	}
	if res.Path != a.Path || res.Unchanged || strings.Contains(got, "\r") {
		t.Errorf("path %q unchanged %v", res.Path, res.Unchanged)
	}
}

func TestEditUnchanged(t *testing.T) {
	cat := load(t)
	a := cat.Article("triangles")
	res, err := Edit(cat, a, a.Raw, nil, 1<<20)
	if err != nil || !res.Unchanged {
		t.Fatalf("res = %+v, %v", res, err)
	}
}

func TestEditReportsLintButAllowsUploadedImages(t *testing.T) {
	cat := load(t)
	a := cat.Article("geometry-7-basic-notions")
	body := "---\ntitle: Основные понятия\n---\n\n![Рисунок](assets/new.png)\n\nСм. [[Нет такой темы]].\n"
	res, err := Edit(cat, a, []byte(body), []string{"new.png"}, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Issues) != 1 || !strings.Contains(res.Issues[0].Msg, "Нет такой темы") {
		t.Errorf("issues = %v", res.Issues)
	}
}

func TestRejections(t *testing.T) {
	cat := load(t)
	a := cat.Article("geometry-7-basic-notions")
	cases := map[string]struct {
		in   string
		want Code
	}{
		"too large":  {strings.Repeat("x", 2000), TooLarge},
		"not utf8":   {"\xff\xfe", NotUTF8},
		"empty":      {"  \n\n", Empty},
		"no title":   {"---\norder: 1\n---\ntext\n", NoTitle},
		"open fm":    {"---\ntitle: x\n", OpenFrontmatter},
		"bad yaml":   {"---\ntitle: [x\n---\n", BadFrontmatter},
		"bad id":     {"---\ntitle: x\nid: has space\n---\n", BadID},
		"taken id":   {"---\ntitle: x\nid: triangles\n---\n", TakenID},
		"over limit": {"---\ntitle: x\n---\n" + strings.Repeat("\n| "+strings.Repeat("a | ", 21)+"\n|"+strings.Repeat("-|", 21)+"\n", 1), OverLimit},
	}
	for name, c := range cases {
		max := int64(1 << 20)
		if name == "too large" {
			max = 1000
		}
		_, err := Edit(cat, a, []byte(c.in), nil, max)
		if got := code(err); got != c.want {
			t.Errorf("%s: code = %q (%v), want %q", name, got, err, c.want)
		}
	}
}

func TestNewTopicFromText(t *testing.T) {
	cat := load(t)
	g := cat.Subject("geometry").Grade("7")
	res, err := New(cat, g, []byte("# Подобие: признаки\n\nТекст.\n"), nil, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != "geometry/7/03-podobie-priznaki.md" {
		t.Errorf("path = %q", res.Path)
	}
	if res.Title != "Подобие: признаки" || !strings.HasPrefix(string(res.Data), "---\ntitle: \"Подобие: признаки\"\norder: 3\n---\n\n# Подобие") {
		t.Errorf("data = %q", res.Data)
	}
}

func TestNewTopicWithFrontmatterAndCollision(t *testing.T) {
	cat := load(t)
	g := cat.Subject("geometry").Grade("7")
	// Same title as an existing article but a different slug source.
	res, err := New(cat, g, []byte("---\ntitle: Triangles\n---\nx\n"), nil, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != "geometry/7/03-triangles.md" {
		t.Errorf("path = %q", res.Path)
	}
	if _, err := New(cat, g, []byte("---\ntitle: Dup\nid: triangles\n---\n"), nil, 1<<20); code(err) != TakenID {
		t.Errorf("duplicate id accepted: %v", err)
	}
}

func TestRepoPath(t *testing.T) {
	if p, err := RepoPath("content", "geometry/7/a.md"); err != nil || p != "content/geometry/7/a.md" {
		t.Errorf("RepoPath = %q, %v", p, err)
	}
	for _, bad := range []string{"../x.md", "geometry/../../x.md", "/etc/passwd", `geometry\x.md`, "geometry/./a.md"} {
		if _, err := RepoPath("content", bad); code(err) != BadPath {
			t.Errorf("RepoPath(%q) accepted", bad)
		}
	}
}

func TestNames(t *testing.T) {
	cases := map[string]string{
		"Треугольники":      "treugolniki",
		"Сумма углов: 180°": "summa-uglov-180",
		"Объём шара":        "obem-shara",
		"  Hello, World!  ": "hello-world",
		"Щука и ёж":         "shchuka-i-ezh",
	}
	for in, want := range cases {
		if got := Slug(in, 40); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Slug("очень длинное название темы про геометрию", 12); len(got) > 12 || strings.HasSuffix(got, "-") {
		t.Errorf("truncated slug = %q", got)
	}
	if got := Branch(42, "geometry/7/02-triangles.md", "abc123"); got != "suggest/42-triangles-abc123" {
		t.Errorf("Branch = %q", got)
	}
	if got := Branch(42, "geometry/7/05-Треугольники.md", "abc123"); got != "suggest/42-treugolniki-abc123" {
		t.Errorf("Branch = %q", got)
	}
	if got := ImageName("Рисунок 1.JPEG", []string{"risunok-1.jpg"}); got != "risunok-1-2.jpg" {
		t.Errorf("ImageName = %q", got)
	}
	if got := ImageName("vector.svg", nil); got != "" {
		t.Errorf("svg accepted: %q", got)
	}
	if id := ShortID(); len(id) != 6 {
		t.Errorf("ShortID = %q", id)
	}
}

func TestPRText(t *testing.T) {
	c := Change{Kind: "edit", Title: "Треугольники", RepoPath: "content/geometry/7/02-triangles.md",
		Images: []string{"content/geometry/7/assets/x.png"}, BaseSHA: "deadbeef", ShortID: "abc123",
		By:     Submitter{TgID: 42, Username: "anya", FirstName: "Аня"},
		Issues: []content.Issue{{Line: 3, Severity: content.Error, Msg: "link [[X]] does not match any article"}}}
	body := c.PRBody()
	for _, want := range []string{"`42`", "@anya", "content/geometry/7/02-triangles.md", "deadbeef", "line 3: error"} {
		if !strings.Contains(body, want) {
			t.Errorf("body misses %q:\n%s", want, body)
		}
	}
	if c.PRTitle() != "Suggest edit: Треугольники" || !strings.Contains(c.CommitMessage(), "Suggested-by: @anya (tg:42)") {
		t.Errorf("title %q message %q", c.PRTitle(), c.CommitMessage())
	}
}

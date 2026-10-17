package suggest

import (
	"errors"
	"strings"
	"testing"

	"github.com/Mimic890/terakoya/internal/content"
	"github.com/Mimic890/terakoya/internal/plain"
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
		"too large": {strings.Repeat("x", 2000), TooLarge},
		"not utf8":  {"\xff\xfe", NotUTF8},
		"empty":     {"  \n\n", Empty},
		"no title":  {"---\norder: 1\n---\ntext\n", NoTitle},
		"open fm":   {"---\ntitle: x\n", OpenFrontmatter},
		"bad yaml":  {"---\ntitle: [x\n---\n", BadFrontmatter},
		"bad id":    {"---\ntitle: x\nid: has space\n---\n", BadID},
		"taken id":  {"---\ntitle: x\nid: triangles\n---\n", TakenID},
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

func TestOverLimitIsAnIssue(t *testing.T) {
	cat := load(t)
	a := cat.Article("geometry-7-basic-notions")
	wide := "---\ntitle: x\n---\n\n| " + strings.Repeat("a | ", 21) + "\n|" + strings.Repeat("-|", 21) + "\n"
	res, err := Edit(cat, a, []byte(wide), nil, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Issues) == 0 || !strings.Contains(res.Issues[0].Msg, "message limit") {
		t.Errorf("issues = %v", res.Issues)
	}
}

func TestNewTopic(t *testing.T) {
	cat := load(t)
	g := cat.Subject("geometry").Grade("7")
	res, err := NewTopic(cat, g, "  Подобие:  признаки ", "Текст.\r\n\r\nЕщё.", nil, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != "geometry/7/03-podobie-priznaki.md" || res.Title != "Подобие: признаки" {
		t.Errorf("path %q title %q", res.Path, res.Title)
	}
	if want := "---\ntitle: \"Подобие: признаки\"\norder: 3\n---\n\n# Подобие: признаки\n\nТекст.\n\nЕщё.\n"; string(res.Data) != want {
		t.Errorf("data = %q", res.Data)
	}
	// A title whose slug is taken gets a numbered file name and ID.
	res, err = NewTopic(cat, g, "Basic notions", "x", nil, 1<<20)
	if err != nil || res.Path != "geometry/7/03-basic-notions-2.md" {
		t.Errorf("path = %v, %v", res, err)
	}
	if _, err := NewTopic(cat, g, " ", "x", nil, 1<<20); code(err) != NoTitle {
		t.Errorf("empty title: %v", err)
	}
	if _, err := NewTopic(cat, g, "x", " \n", nil, 1<<20); code(err) != Empty {
		t.Errorf("empty text: %v", err)
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
	if got := ImageName("geometry/7/02-triangles.md", "abc123", 2, ".jpg"); got != "triangles-abc123-2.jpg" {
		t.Errorf("ImageName = %q", got)
	}
	for _, c := range [][3]string{{"Рисунок.JPEG", "", ".jpg"}, {"x.png", "", ".png"}, {"blob", "image/webp", ".webp"},
		{"vector.svg", "image/svg+xml", ""}, {"notes.txt", "", ""}} {
		if got := ImageExt(c[0], c[1]); got != c[2] {
			t.Errorf("ImageExt(%q, %q) = %q", c[0], c[1], got)
		}
	}
	if id := ShortID(); len(id) != 6 {
		t.Errorf("ShortID = %q", id)
	}
}

func TestPRText(t *testing.T) {
	c := Change{Kind: KindEdit, Title: "Треугольники", RepoPath: "content/geometry/7/02-triangles.md",
		BaseSHA: "deadbeef", ShortID: "abc123", Section: "Сумма углов", Text: "текст с ``` внутри",
		Pictures: []Picture{{Path: "content/geometry/7/assets/x.png", Caption: "после\nпервого абзаца", URL: "https://host/raw/x.png"}},
		Merge:    &plain.Result{Mode: plain.Replaced, Changes: []plain.Change{{Before: "старое", After: "новое"}}},
		By:       Submitter{TgID: 42, Username: "anya", FirstName: "Аня"},
		Issues:   []content.Issue{{Line: 3, Severity: content.Error, Msg: "link [[X]] does not match any article"}}}
	body := c.PRBody()
	for _, want := range []string{"`42`", "@anya", "content/geometry/7/02-triangles.md", "deadbeef", "line 3: error",
		"Section: Сумма углов", "whole section was replaced", "> старое", "> новое",
		"`content/geometry/7/assets/x.png` — после первого абзаца", "![picture 1](https://host/raw/x.png)",
		"````text\nтекст с ``` внутри\n````"} {
		if !strings.Contains(body, want) {
			t.Errorf("body misses %q:\n%s", want, body)
		}
	}
	if c.PRTitle() != "Suggest edit: Треугольники" || !strings.Contains(c.CommitMessage(), "Suggested-by: @anya (tg:42)") {
		t.Errorf("title %q message %q", c.PRTitle(), c.CommitMessage())
	}

	r := Change{Kind: KindReport, Title: "Треугольники", RepoPath: "content/geometry/7/02-triangles.md", BaseSHA: "deadbeef",
		ShortID: "abc123", Text: "опечатка\nв теореме", ArticleURL: "https://host/src/commit/deadbeef/x", AcceptLabel: "accepted",
		Pictures: []Picture{{URL: "https://host/attachments/1"}}, By: Submitter{TgID: 42, FirstName: "Аня"}}
	body = r.IssueBody()
	for _, want := range []string{"Error reported in Telegram by Аня.", "> опечатка\n> в теореме", "[`content/geometry/7/02-triangles.md`](https://host/src/commit/deadbeef/x)",
		"1. no caption", "![picture 1](https://host/attachments/1)", "`accepted` label"} {
		if !strings.Contains(body, want) {
			t.Errorf("issue body misses %q:\n%s", want, body)
		}
	}
	if r.PRTitle() != "Error report: Треугольники" {
		t.Errorf("title %q", r.PRTitle())
	}
}

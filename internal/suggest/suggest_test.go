package suggest

import (
	"errors"
	"strings"
	"testing"

	"github.com/Mimic890/terakoya/internal/content"
	"github.com/Mimic890/terakoya/internal/plain"
)

const topic = "Геометрия/10 класс/05 Параллельность прямой и плоскости"

func load(t *testing.T) *content.Catalog {
	t.Helper()
	cat, issues, err := content.Load("../../examples/vault")
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

func TestEdit(t *testing.T) {
	cat := load(t)
	n := cat.NoteByPath(topic + "/теоремы/Прямая пересечения плоскостей (5.2).md")
	res, err := Edit(cat, n, []byte("# Прямая пересечения\r\n\r\nСм. [нет](Нет.md).\r\n"), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != n.Path || res.Title != "Прямая пересечения" || string(res.Data) != "# Прямая пересечения\n\nСм. [нет](Нет.md).\n" {
		t.Errorf("result = %+v", res)
	}
	if len(res.Issues) != 1 || !strings.Contains(res.Issues[0].Msg, "Нет.md: not found") {
		t.Errorf("issues = %v", res.Issues)
	}
	for in, want := range map[string]Code{"  \n": Empty, "\xff": NotUTF8, strings.Repeat("x", 2000): TooLarge} {
		if _, err := Edit(cat, n, []byte(in), 1000); code(err) != want {
			t.Errorf("Edit(%.10q) = %v, want %s", in, err, want)
		}
	}
}

func TestNewTopic(t *testing.T) {
	cat := load(t)
	g := cat.Subjects[0].Grades[0]
	res, err := NewTopic(cat, g, "  Перпендикулярность:  прямой и плоскости? ", "Текст.\r\n", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	want := "Геометрия/10 класс/06 Перпендикулярность прямой и плоскости/Перпендикулярность прямой и плоскости.md"
	if res.Path != want || res.Title != "Перпендикулярность: прямой и плоскости?" || len(res.Also) != 0 {
		t.Errorf("result = %q %q", res.Path, res.Title)
	}
	if string(res.Data) != "# Перпендикулярность: прямой и плоскости?\n\nТекст.\n" {
		t.Errorf("data = %q", res.Data)
	}
	// The title of an existing note is taken: its file name is in the vault.
	if _, err := NewTopic(cat, g, "Параллельность прямой и плоскости", "x", 1<<20); code(err) != TakenName {
		t.Errorf("taken name: %v", err)
	}
	if _, err := NewTopic(cat, g, " ?: ", "x", 1<<20); code(err) != NoTitle {
		t.Errorf("empty name: %v", err)
	}
}

func TestNewTopicNumbering(t *testing.T) {
	cat := load(t)
	g := cat.Subjects[0].Grades[0]
	tp := g.Topics[0]
	for _, c := range []struct {
		number, width int
		want          string
	}{{5, 2, "06 Х"}, {9, 1, "10 Х"}, {99, 2, "100 Х"}, {7, 3, "008 Х"}} {
		tp.Number, tp.Width = c.number, c.width
		res, err := NewTopic(cat, g, "Х", "x", 1<<20)
		if err != nil || !strings.Contains(res.Path, "/"+c.want+"/") {
			t.Errorf("after %d (width %d): %v %v", c.number, c.width, res, err)
		}
	}
}

func TestNewNote(t *testing.T) {
	cat := load(t)
	tp := cat.Subjects[0].Grades[0].Topics[0]
	main := tp.Main

	res, err := NewNote(cat, tp, "задачи", nil, "Ещё одна задача", "Условие.", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != topic+"/задачи/Ещё одна задача.md" {
		t.Errorf("path = %q", res.Path)
	}
	if string(res.Data) != "# Ещё одна задача\n\nУсловие.\n\n---\n↩ [Параллельность прямой и плоскости](Параллельность%20прямой%20и%20плоскости.md)\n" {
		t.Errorf("data = %q", res.Data)
	}
	if len(res.Issues) != 0 {
		t.Errorf("issues = %v", res.Issues)
	}
	page := string(res.Also[0].Data)
	if res.Also[0].Path != main.Path || !strings.HasSuffix(page, "точку $M \\in A_1B_1$\n- [Ещё одна задача](Ещё%20одна%20задача.md)\n") {
		t.Errorf("main page:\n%s", page)
	}

	// A theorem goes after the last theorem item, after its nested corollary.
	res, _ = NewNote(cat, tp, "теоремы", nil, "Теорема (5.3)", "Текст.", 1<<20)
	page = string(res.Also[0].Data)
	if !strings.Contains(page, "b \\parallel a$\n- [Теорема (5.3)](Теорема%20%285.3%29.md)\n\n## Задачи") {
		t.Errorf("theorem link:\n%s", page)
	}

	// A corollary goes under its parent, one level deeper, next to its file.
	parent := cat.NoteByPath(topic + "/теоремы/Признак параллельности прямой и плоскости (5.1).md")
	res, _ = NewNote(cat, tp, "следствия", parent, "Новое следствие", "Текст.", 1<<20)
	if res.Path != topic+"/теоремы/следствия/Новое следствие.md" {
		t.Errorf("corollary path = %q", res.Path)
	}
	page = string(res.Also[0].Data)
	if !strings.Contains(page, "\\Rightarrow a \\parallel \\alpha$\n\t- [Новое следствие](Новое%20следствие.md)\n- [Прямая пересечения") {
		t.Errorf("corollary link:\n%s", page)
	}

	// A kind the page does not link yet gets its own section.
	res, _ = NewNote(cat, tp, "аксиомы", nil, "Аксиома", "Текст.", 1<<20)
	page = string(res.Also[0].Data)
	if res.Path != topic+"/аксиомы/Аксиома.md" || !strings.HasSuffix(page, "\n\n## Аксиомы\n\n- [Аксиома](Аксиома.md)\n") {
		t.Errorf("new section:\n%s", page)
	}

	// The updated main page links resolve once the files exist.
	if got := LinkTarget("Две прямые, параллельные третьей (5.4).md"); got != "Две%20прямые,%20параллельные%20третьей%20%285.4%29.md" {
		t.Errorf("LinkTarget = %q", got)
	}
}

func TestRepoPath(t *testing.T) {
	if p, err := RepoPath("Геометрия/7 класс/a.md"); err != nil || p != "Геометрия/7 класс/a.md" {
		t.Errorf("RepoPath = %q, %v", p, err)
	}
	for _, bad := range []string{"../x.md", "geometry/../../x.md", "/etc/passwd", `geometry\x.md`, "geometry/./a.md"} {
		if _, err := RepoPath(bad); code(err) != BadPath {
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
	if got := Branch(42, "Г/10 класс/05 Тема/теоремы/Признак параллельности (5.1).md", "abc123"); got != "suggest/42-priznak-parallelnosti-abc123" {
		t.Errorf("Branch = %q", got)
	}
	if got := Branch(42, "Г/10 класс/05 Тема/05 Треугольники.md", "abc123"); got != "suggest/42-treugolniki-abc123" {
		t.Errorf("Branch = %q", got)
	}
	if got := ImageName("Г/10 класс/05 Тема/Тема.md", "abc123", 2, ".jpg"); got != "tema-abc123-2.jpg" {
		t.Errorf("ImageName = %q", got)
	}
	for _, c := range [][3]string{{"Рисунок.JPEG", "", ".jpg"}, {"x.png", "", ".png"}, {"blob", "image/png", ".png"}, {"x.webp", "image/webp", ""},
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
	c := Change{Kind: KindEdit, Title: "Треугольники", RepoPath: "Г/7 класс/01 Тема/Треугольники.md",
		BaseSHA: "deadbeef", ShortID: "abc123", Section: "Сумма углов", Text: "текст с ``` внутри",
		Pictures: []Picture{{Path: "Г/7 класс/01 Тема/_assets/x.png", Caption: "после\nпервого абзаца", URL: "https://host/raw/x.png"}},
		Merge:    &plain.Result{Mode: plain.Replaced, Changes: []plain.Change{{Before: "старое", After: "новое"}}},
		By:       Submitter{TgID: 42, Username: "anya", FirstName: "Аня"},
		Issues:   []content.Issue{{Line: 3, Severity: content.Error, Msg: "link [[X]] does not match any article"}}}
	body := c.PRBody()
	for _, want := range []string{"`42`", "@anya", "Г/7 класс/01 Тема/Треугольники.md", "deadbeef", "line 3: error",
		"Section: Сумма углов", "whole section was replaced", "> старое", "> новое",
		"`Г/7 класс/01 Тема/_assets/x.png` — после первого абзаца", "![picture 1](https://host/raw/x.png)",
		"````text\nтекст с ``` внутри\n````"} {
		if !strings.Contains(body, want) {
			t.Errorf("body misses %q:\n%s", want, body)
		}
	}
	if c.PRTitle() != "Suggest edit: Треугольники" || !strings.Contains(c.CommitMessage(), "Suggested-by: @anya (tg:42)") {
		t.Errorf("title %q message %q", c.PRTitle(), c.CommitMessage())
	}

	r := Change{Kind: KindReport, Title: "Треугольники", RepoPath: "Г/7 класс/01 Тема/Треугольники.md", BaseSHA: "deadbeef",
		ShortID: "abc123", Text: "опечатка\nв теореме", ArticleURL: "https://host/src/commit/deadbeef/x", AcceptLabel: "accepted",
		Pictures: []Picture{{URL: "https://host/attachments/1"}}, By: Submitter{TgID: 42, FirstName: "Аня"}}
	body = r.IssueBody()
	for _, want := range []string{"Error reported in Telegram by Аня.", "> опечатка\n> в теореме", "[`Г/7 класс/01 Тема/Треугольники.md`](https://host/src/commit/deadbeef/x)",
		"1. no caption", "![picture 1](https://host/attachments/1)", "`accepted` label"} {
		if !strings.Contains(body, want) {
			t.Errorf("issue body misses %q:\n%s", want, body)
		}
	}
	if r.PRTitle() != "Error report: Треугольники" {
		t.Errorf("title %q", r.PRTitle())
	}
}

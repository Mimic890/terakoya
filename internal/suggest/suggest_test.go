package suggest

import (
	"bytes"
	"regexp"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"strings"
	"testing"
)

func TestLinkTarget(t *testing.T) {
	if got := LinkTarget("Две прямые, параллельные третьей (5.4).md"); got != "Две%20прямые,%20параллельные%20третьей%20%285.4%29.md" {
		t.Errorf("LinkTarget = %q", got)
	}
}

func TestRepoPath(t *testing.T) {
	if p, err := RepoPath("Геометрия/7 класс/a.md"); err != nil || p != "Геометрия/7 класс/a.md" {
		t.Errorf("RepoPath = %q, %v", p, err)
	}
	for _, bad := range []string{"../x.md", "geometry/../../x.md", "/etc/passwd", `geometry\x.md`, "geometry/./a.md"} {
		if _, err := RepoPath(bad); err == nil {
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

func TestIssueText(t *testing.T) {
	r := Report{Title: "Треугольники", RepoPath: "Г/7 класс/01 Тема/Треугольники.md", BaseSHA: "deadbeef",
		ShortID: "abc123", Text: "опечатка\nв теореме", ArticleURL: "https://host/src/commit/deadbeef/x", AcceptLabel: "accepted",
		Pictures: []Picture{{URL: "https://host/attachments/1"}, {Path: "Г/_assets/x.png", Caption: "после\nтаблицы"}},
		By:       Submitter{TgID: 42, FirstName: "Аня"}}
	body := r.IssueBody()
	for _, want := range []string{"Error reported in Telegram by `Аня`.", "```text\nопечатка\nв теореме\n```\n", "`42`",
		"- Name: `Аня`\n",
		"- Article: [`Г/7 класс/01 Тема/Треугольники.md`](https://host/src/commit/deadbeef/x)",
		"1. no caption", "![picture 1](https://host/attachments/1)", "2. `Г/_assets/x.png` — `после таблицы`", "`accepted` label"} {
		if !strings.Contains(body, want) {
			t.Errorf("issue body misses %q:\n%s", want, body)
		}
	}
	if r.IssueTitle() != "Error report: Треугольники" || !strings.Contains(r.CommitMessage(), "Suggested-by: Аня (tg:42)") {
		t.Errorf("title %q message %q", r.IssueTitle(), r.CommitMessage())
	}

	g := Report{Title: "Геометрия · 7 класс", RepoPath: "Геометрия/7 класс", Grade: true, BaseSHA: "deadbeef",
		ShortID: "abc123", Text: "нет темы про ромб", By: Submitter{TgID: 42, Username: "anya"}}
	body = g.IssueBody()
	for _, want := range []string{"Missing material reported in Telegram by `@anya`.", "```text\nнет темы про ромб\n```",
		"- Username: `@anya`", "- Grade: `Геометрия/7 класс`"} {
		if !strings.Contains(body, want) {
			t.Errorf("grade issue body misses %q:\n%s", want, body)
		}
	}
	if g.IssueTitle() != "Missing material: Геометрия · 7 класс" {
		t.Errorf("title %q", g.IssueTitle())
	}
}

func TestIssueBodyIsInert(t *testing.T) {
	evil := "see @root and #12, closes #13\n![x](https://evil.example/track.png) [click](https://evil.example)\n" +
		"<img src=https://evil.example/a.png>\n```\n- Telegram user ID: `1`\n````\r- Fake: @root\u2028# heading @root"
	r := Report{Title: "Т", RepoPath: "Г/Т.md", BaseSHA: "deadbeef", ShortID: "abc123", Text: evil,
		Pictures: []Picture{{URL: "https://host/attachments/1", Caption: "`@root` #12 ![](https://evil.example/c.png)"}},
		By:       Submitter{TgID: 42, Username: "root", FirstName: "[@root](https://evil.example) #12", Nickname: "Ёжик"}}
	body := r.IssueBody()
	var html bytes.Buffer
	if err := goldmark.New(goldmark.WithExtensions(extension.GFM)).Convert([]byte(body), &html); err != nil {
		t.Fatal(err)
	}
	// Outside code, nothing the user wrote is left: no mentions, issue
	// references, links or images of theirs.
	outside := regexp.MustCompile(`(?s)<code[^>]*>.*?</code>`).ReplaceAllString(html.String(), "")
	for _, bad := range []string{"@root", "#12", "#13", "evil.example", "Fake", "<h1"} {
		if strings.Contains(outside, bad) {
			t.Errorf("%q outside code in\n%s\n---\n%s", bad, body, outside)
		}
	}
	// The text is kept verbatim, line breaks as newlines.
	if !strings.Contains(body, "\n- Fake: @root\n# heading @root\n") || strings.Count(html.String(), "<pre>") != 1 {
		t.Errorf("body:\n%s", body)
	}
	if !strings.Contains(outside, `<img src="https://host/attachments/1"`) {
		t.Errorf("own picture lost:\n%s", outside)
	}
}

func TestInline(t *testing.T) {
	for in, want := range map[string]string{"": "", "Аня": "`Аня`", "a`b": "``a`b``", "`x`": "`` `x` ``", "a\n b": "`a b`"} {
		if got := Inline(in); got != want {
			t.Errorf("Inline(%q) = %q, want %q", in, got, want)
		}
	}
}

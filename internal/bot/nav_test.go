package bot

import (
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/content"
	"github.com/Mimic890/terakoya/internal/i18n"
	"github.com/Mimic890/terakoya/internal/ranks"
)

func loadExamples(t *testing.T) *content.Catalog {
	t.Helper()
	cat, issues, err := content.Load("../../examples/content")
	if err != nil || len(issues) > 0 {
		t.Fatalf("load: %v %v", err, issues)
	}
	return cat
}

func callbacks(kb *models.InlineKeyboardMarkup) []string {
	var out []string
	for _, r := range kb.InlineKeyboard {
		for _, b := range r {
			out = append(out, b.CallbackData)
		}
	}
	return out
}

func TestViews(t *testing.T) {
	cat := loadExamples(t)
	ladder, _ := ranks.Parse(ranks.Default)
	tr := i18n.RU

	text, kb := homeView(tr, cat, ladder, "Аня", 3)
	if !strings.Contains(text, "Аня") || !strings.Contains(text, "senpai") {
		t.Errorf("home text = %q", text)
	}
	if got := callbacks(kb); len(got) != 1 || got[0] != "s:geometry" {
		t.Errorf("home callbacks = %v", got)
	}

	_, kb = subjectView(tr, cat.Subject("geometry"))
	if got := strings.Join(callbacks(kb), " "); got != "g:geometry:7 g:geometry:8 h" {
		t.Errorf("subject callbacks = %q", got)
	}

	g := cat.Subject("geometry").Grade("7")
	text, kb = gradeView(tr, g)
	if want := "📐 Геометрия · 7 класс\nНачала планиметрии.\n\nВыберите тему:"; text != want {
		t.Errorf("grade text = %q, want %q", text, want)
	}
	want := "a:geometry-7-basic-notions a:triangles n:geometry:7 s:geometry"
	if got := strings.Join(callbacks(kb), " "); got != want {
		t.Errorf("grade callbacks = %q, want %q", got, want)
	}

	if got := gradeTitle(tr, cat.Subject("geometry").Grade("8")); got != "8 класс" {
		t.Errorf("default grade title = %q", got)
	}
}

func TestCallbackDataFits(t *testing.T) {
	long := strings.Repeat("x", content.MaxIDLength)
	a := &content.Article{ID: long, Grade: &content.Grade{Dir: "7", Subject: &content.Subject{Dir: "s"}}}
	for _, d := range callbacks(articleKeyboard(i18n.RU, a)) {
		if len(d) > 64 {
			t.Errorf("callback %q is %d bytes", d, len(d))
		}
	}
	g := &content.Grade{Dir: strings.Repeat("g", content.MaxDirsLength/2), Subject: &content.Subject{Dir: strings.Repeat("s", content.MaxDirsLength/2)}}
	if d := gradeCallback(g); len(d) > 64 {
		t.Errorf("grade callback is %d bytes", len(d))
	}
}

func TestSplitCommand(t *testing.T) {
	cases := map[string][2]string{
		"/start":                 {"/start", ""},
		"/start triangles":       {"/start", "triangles"},
		"/Start@terakoya_bot  x": {"/start", "x"},
		"hello":                  {"", ""},
	}
	for in, want := range cases {
		cmd, arg := splitCommand(in)
		if cmd != want[0] || arg != want[1] {
			t.Errorf("splitCommand(%q) = %q, %q", in, cmd, arg)
		}
	}
}

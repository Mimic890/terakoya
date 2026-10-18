package bot

import (
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/content"
	"github.com/Mimic890/terakoya/internal/i18n"
)

func loadExamples(t *testing.T) *content.Catalog {
	t.Helper()
	cat, issues, err := content.Load("../../examples/vault")
	if err != nil || len(issues) > 0 {
		t.Fatalf("load: %v %v", err, issues)
	}
	return cat
}

func labels(kb *models.InlineKeyboardMarkup) []string {
	var out []string
	for _, r := range kb.InlineKeyboard {
		for _, b := range r {
			out = append(out, b.Text)
		}
	}
	return out
}

func TestViews(t *testing.T) {
	cat := loadExamples(t)
	tr := i18n.RU
	s := cat.Subjects[0]
	g := s.Grades[0]
	main := g.Topics[0].Main

	text, kb := homeView(tr, cat, func(string) string { return "" })
	if text != "Выберите предмет:" {
		t.Errorf("home text = %q", text)
	}
	if got := callbacks(kb); len(got) != 1 || got[0] != "s:"+s.ID || labels(kb)[0] != "Геометрия" {
		t.Errorf("home = %v %v", got, labels(kb))
	}

	_, kb = subjectView(tr, s)
	if got := strings.Join(callbacks(kb), " "); got != "g:"+g.ID+" h" || labels(kb)[0] != "10 класс" {
		t.Errorf("subject = %q %v", got, labels(kb))
	}

	text, kb = gradeView(tr, g)
	if want := "Геометрия · 10 класс\n\nВыберите тему:"; text != want {
		t.Errorf("grade text = %q, want %q", text, want)
	}
	if got := strings.Join(callbacks(kb), " "); got != "a:"+main.ID+" n:"+g.ID+" s:"+s.ID {
		t.Errorf("grade callbacks = %q", got)
	}
	if labels(kb)[0] != "Параллельность прямой и плоскости" {
		t.Errorf("topic label = %q", labels(kb)[0])
	}

	// Under every note, the main page included: back, suggest, download,
	// pin; no buttons for the topic's notes.
	if got := strings.Join(labels(articleKeyboard(tr, main, false, false, "")), ","); got != "« Назад,Предложить правку,Скачать,Закрепить" {
		t.Errorf("note keyboard = %q", got)
	}
	kb = articleKeyboard(tr, main, true, true, "")
	if got := strings.Join(callbacks(kb), " "); got != "bp:"+main.ID+" e:"+main.ID+" d:"+main.ID+" p:"+main.ID || labels(kb)[3] != "Открепить" {
		t.Errorf("pinned note keyboard = %q %v", got, labels(kb))
	}
}

func TestCallbackDataFits(t *testing.T) {
	cat := loadExamples(t)
	for _, n := range cat.Notes() {
		for _, d := range append(callbacks(articleKeyboard(i18n.RU, n, true, true, "")), cbParent+n.ID, cbOpen+n.ID, cbFromPins+n.ID,
			cbAllow+"-9223372036854775808", cbRequest+n.ID) {
			if len(d) > 64 {
				t.Errorf("callback %q is %d bytes", d, len(d))
			}
		}
	}
	for _, k := range content.Kinds {
		if len(cbKind+"99") > 64 || len(content.KindLabel(k)) == 0 {
			t.Error(k)
		}
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

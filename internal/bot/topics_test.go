package bot

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/i18n"
	"github.com/Mimic890/terakoya/internal/settings"
)

const (
	bigTopic   = "Геометрия/7 класс/120 Большая/Большая.md"
	plainTopic = "Геометрия/7 класс/Повторение/Повторение.md"
)

func topicHarness(t *testing.T) *harness {
	return newHarness(t, map[string]string{
		bigTopic:   "# Большая\n\nТекст.\n",
		plainTopic: "# Повторение\n\nТекст.\n",
	})
}

func TestTopicListRich(t *testing.T) {
	h := topicHarness(t)
	g := h.bot.catalog().NoteByPath(triangles).Grade()
	h.say("/start")
	h.say(i18n.RU.KeySubjects)
	h.tg.take()
	menu := h.bot.msgs.menu(h.ctx, 42)

	h.tapOn(menu, cbGrade+g.ID)
	calls := h.tg.take()
	md := richs(calls)
	if len(md) != 1 {
		t.Fatalf("calls = %+v", calls)
	}
	want := "# Геометрия · 7 класс\n\n" +
		btn(h.id(triangles), "§1 Треугольники") + "  \n" +
		btn(h.id(bigTopic), "§120 Большая") + "  \n" +
		btn(h.id(plainTopic), "Повторение")
	if !strings.HasPrefix(md[0], want) {
		t.Errorf("list =\n%s\nwant\n%s", md[0], want)
	}
	kb := lastRich(t, calls).params["reply_markup"]
	if got := strings.Join(callbacksOf(t, kb), " "); got != cbReportGrade+g.ID+" "+cbSubject+g.Subject.ID {
		t.Errorf("buttons = %s", got)
	}
	// The subject list it came from is gone.
	if got := ids(calls, "deleteMessage"); len(got) != 1 || got[0] != itoa(menu) {
		t.Errorf("deleted %v, want the subject list %d", got, menu)
	}

	// A topic opens in place of the list, and «Назад» brings the list back.
	list := h.tg.lastID("sendRichMessage")
	h.tapOn(list, cbArticle+h.id(triangles))
	calls = h.tg.take()
	if got := ids(calls, "deleteMessage"); len(got) != 1 || got[0] != itoa(list) {
		t.Errorf("deleted %v, want the list %d", got, list)
	}
	h.tapOn(h.tg.lastID("sendRichMessage"), cbBack+h.id(triangles))
	if s := richs(h.tg.take()); len(s) != 1 || !strings.HasPrefix(s[0], "# Геометрия · 7 класс") {
		t.Errorf("back = %q", s)
	}

	// «Назад» under the list goes to the grades, which replace it.
	list = h.tg.lastID("sendRichMessage")
	h.tapOn(list, cbSubject+g.Subject.ID)
	calls = h.tg.take()
	if s := richs(calls); len(s) != 1 || !strings.HasPrefix(s[0], "# Геометрия\n\n") || len(to(calls, "editMessageText", 42)) != 0 {
		t.Errorf("grades = %q", s)
	}
	if got := ids(calls, "deleteMessage"); len(got) != 1 || got[0] != itoa(list) {
		t.Errorf("deleted %v, want the list %d", got, list)
	}
}

func TestTopicListEmojiAndButtons(t *testing.T) {
	h := topicHarness(t)
	g := h.bot.catalog().NoteByPath(triangles).Grade()

	h.tg.emoji = []string{"301"}
	h.bot.settings.Update(func(s *settings.Settings) { s.Emoji.Subjects["Геометрия"] = "301" })
	if md := h.bot.topicListMarkdown(g); !strings.HasPrefix(md, "# ![⭐](tg://emoji?id=301) Геометрия · 7 класс\n\n") {
		t.Errorf("heading = %q", md)
	}

	// The button style keeps buttons, with the same labels.
	h.bot.settings.Update(func(s *settings.Settings) { s.ListStyle = settings.ListButtons })
	h.tapOn(700, cbGrade+g.ID)
	calls := h.tg.take()
	e := to(calls, "editMessageText", 42)
	if len(e) != 1 || !strings.HasPrefix(e[0].params["text"], "Геометрия · 7 класс\n\n"+i18n.RU.ChooseTopic) ||
		len(richs(calls)) != 0 {
		t.Fatalf("buttons = %+v", calls)
	}
	for _, want := range []string{`{"text":"§1 Треугольники","callback_data":"a:` + h.id(triangles) + `"}`,
		`{"text":"§120 Большая"`, `{"text":"Повторение"`, `"callback_data":"n:` + g.ID + `"`} {
		if !strings.Contains(e[0].params["reply_markup"], want) {
			t.Errorf("buttons have no %s: %s", want, e[0].params["reply_markup"])
		}
	}

	// Without reports there is no «Сообщить об ошибке», in either style.
	if got := labels(gradeKeyboard(i18n.RU, g, false)); len(got) != 1 || got[0] != i18n.RU.Back {
		t.Errorf("without reports = %v", got)
	}
	if _, kb := gradeView(i18n.RU, g, false); strings.Contains(strings.Join(callbacks(kb), " "), cbReportGrade) {
		t.Error("«Сообщить об ошибке» without reports")
	}
}

// btn is how the renderer writes a link that opens a note.
func btn(id, label string) string {
	return `<tg-button type="callback_data" style="link" data="a:` + id + `">` + label + `</tg-button>`
}

func itoa(n int) string { return strconv.Itoa(n) }

// callbacksOf lists the callback data of a keyboard sent as JSON.
func callbacksOf(t *testing.T, markup string) []string {
	t.Helper()
	var kb models.InlineKeyboardMarkup
	if err := json.Unmarshal([]byte(markup), &kb); err != nil {
		t.Fatal(err)
	}
	return callbacks(&kb)
}

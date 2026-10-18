package bot

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/Mimic890/terakoya/internal/i18n"
)

// sameIDs compares deleted message IDs (as sent) with tracked ones.
func sameIDs(got []string, want []int) bool {
	w := make([]string, len(want))
	for i, id := range want {
		w[i] = strconv.Itoa(id)
	}
	sort.Strings(got)
	sort.Strings(w)
	return strings.Join(got, ",") == strings.Join(w, ",")
}

// dialogMsgs returns the messages the current dialog tracks.
func (h *harness) dialogMsgs() []int {
	h.t.Helper()
	d, _ := h.bot.loadDialog(h.ctx, h.user.ID, h.user.ID)
	if d == nil {
		h.t.Fatal("no dialog")
	}
	return d.p.Msgs
}

func TestCancelFixText(t *testing.T) {
	for _, how := range []string{"button", "/cancel"} {
		t.Run(how, func(t *testing.T) {
			h := newHarness(t, nil)
			note := h.id(triangles)
			h.tapOn(500, cbArticle+note)
			origin := h.tg.lastID("sendRichMessage")
			h.tg.take()

			// Menu, the plain text (two parts for a long note or one), the
			// instructions, the user's text and photo, and the bot's
			// answers to them.
			h.tapOn(origin, cbSuggest+note)
			h.tap(cbFix)
			h.say("исправленный текст")
			h.photo("pic1", "")
			h.tg.take()
			msgs := h.dialogMsgs()
			if len(msgs) < 6 {
				t.Fatalf("tracked %v", msgs)
			}

			if how == "button" {
				h.tap(cbCancel)
			} else {
				h.say("/cancel")
				msgs = append(msgs, h.msgID) // the command goes too
			}
			calls := h.tg.take()
			if s := texts(calls, "sendMessage"); len(s) != 0 {
				t.Errorf("cancel said %q", s)
			}
			// The note is back, in place of the old one.
			if r := richs(calls); len(r) != 1 || !strings.Contains(r[0], "# Треугольники") {
				t.Errorf("note = %q", r)
			}
			deleted := ids(calls, "deleteMessage")
			if !sameIDs(deleted, append(msgs, origin)) {
				t.Errorf("deleted %v, want %v and the old note %d", deleted, msgs, origin)
			}
			if d, _ := h.bot.loadDialog(h.ctx, 42, 42); d != nil {
				t.Error("the dialog survived")
			}
		})
	}
}

func TestCancelReportAndNewNote(t *testing.T) {
	h := newHarness(t, nil)

	// «Сообщить об ошибке» goes back to its note.
	h.tapOn(500, cbSuggest+h.id(triangles))
	h.tap(cbReport)
	h.say("опечатка")
	h.tg.take()
	h.tap(cbCancel)
	if r := richs(h.tg.take()); len(r) != 1 || !strings.Contains(r[0], "# Треугольники") {
		t.Errorf("report: %q", r)
	}

	// «Добавить теорему…» goes back to the topic's main page.
	main := h.id(vmain)
	h.tapOn(600, cbSuggest+main)
	h.tap(cbAddNote)
	h.tap(cbKind + "0")
	h.say("Новая теорема")
	h.tg.take()
	h.tap(cbCancel)
	calls := h.tg.take()
	if r := richs(calls); len(r) != 1 || !strings.Contains(r[0], "# Параллельность прямой и плоскости") {
		t.Errorf("new note: %q", r)
	}
	if !contains(ids(calls, "deleteMessage"), "600") {
		t.Errorf("old main page not replaced: %v", ids(calls, "deleteMessage"))
	}
}

func TestCancelNewTopic(t *testing.T) {
	h := newHarness(t, nil)
	g := h.bot.catalog().NoteByPath(triangles).Grade()
	h.tapOn(700, cbGrade+g.ID)
	list := h.tg.lastID("sendRichMessage")
	h.tg.take()

	h.tapOn(list, cbAddTopic+g.ID)
	h.say("Углы")
	h.say("Текст темы")
	h.tg.take()
	msgs := h.dialogMsgs()
	h.tap(cbCancel)
	calls := h.tg.take()
	if r := richs(calls); len(r) != 1 || !strings.HasPrefix(r[0], "# Геометрия · 7 класс") {
		t.Errorf("topic list = %q", r)
	}
	if !sameIDs(ids(calls, "deleteMessage"), append(msgs, list)) {
		t.Errorf("deleted %v, want %v and the old list %d", ids(calls, "deleteMessage"), msgs, list)
	}
	if len(texts(calls, "sendMessage")) != 0 {
		t.Errorf("said %q", texts(calls, "sendMessage"))
	}
}

func TestCancelAccessQuestion(t *testing.T) {
	h := newHarness(t, nil)
	h.as(boris)
	note := h.id(triangles)

	// From a note: the question replaces the menu; «Отмена» removes it and
	// brings the note back.
	h.tapOn(500, cbSuggest+note)
	h.tap(cbFix)
	calls := h.tg.take()
	e := to(calls, "editMessageText", boris.ID)
	if len(e) != 1 || e[0].params["text"] != i18n.RU.AccessNeeded || !strings.Contains(e[0].params["reply_markup"], `"x"`) {
		t.Fatalf("question = %+v", calls)
	}
	msgs := h.dialogMsgs()
	h.tap(cbCancel)
	calls = h.tg.take()
	if r := richs(calls); len(r) != 1 || !strings.Contains(r[0], "# Треугольники") {
		t.Errorf("note = %q", r)
	}
	if !sameIDs(ids(calls, "deleteMessage"), append(msgs, 500)) {
		t.Errorf("deleted %v, want %v and 500", ids(calls, "deleteMessage"), msgs)
	}

	// From «Добавить тему»: back to the topic list.
	g := h.bot.catalog().NoteByPath(triangles).Grade()
	h.tapOn(800, cbAddTopic+g.ID)
	if m := to(h.tg.take(), "sendMessage", boris.ID); len(m) != 1 || m[0].params["text"] != i18n.RU.AccessNeeded {
		t.Fatalf("question = %+v", m)
	}
	h.tap(cbCancel)
	if r := richs(h.tg.take()); len(r) != 1 || !strings.HasPrefix(r[0], "# Геометрия · 7 класс") {
		t.Errorf("topic list = %q", r)
	}

	// After «Отправить запрос» the question is no dialog any more.
	h.tapOn(500, cbSuggest+note)
	h.tap(cbFix)
	h.tap(cbRequest + note)
	if d, _ := h.bot.loadDialog(h.ctx, boris.ID, boris.ID); d != nil {
		t.Error("the dialog survived the request")
	}
}

func TestCancelWhenTheNoteIsGone(t *testing.T) {
	h := newHarness(t, nil)
	note := h.id(triangles)
	h.tapOn(500, cbSuggest+note)
	h.tap(cbReport)
	h.tg.take()

	// The note leaves the vault: back to its topic list.
	os.RemoveAll(filepath.Join(h.bot.cfg.RepoDir, filepath.Dir(triangles)))
	if err := h.bot.Reload(h.ctx); err != nil {
		t.Fatal(err)
	}
	h.tap(cbCancel)
	if r := richs(h.tg.take()); len(r) != 1 || !strings.HasPrefix(r[0], "# Геометрия · 7 класс") {
		t.Errorf("fallback = %q", r)
	}

	// And with the whole grade gone, the subject list.
	h.tapOn(500, cbSuggest+h.id(vmain))
	h.tap(cbReport)
	h.tg.take()
	os.RemoveAll(filepath.Join(h.bot.cfg.RepoDir, "Геометрия", "10 класс"))
	os.RemoveAll(filepath.Join(h.bot.cfg.RepoDir, "Геометрия", "7 класс"))
	h.bot.Reload(h.ctx)
	h.tap(cbCancel)
	calls := h.tg.take()
	if s := texts(calls, "sendMessage"); len(s) != 1 || (s[0] != i18n.RU.ChooseSubject && s[0] != i18n.RU.NoSubjects) {
		t.Errorf("subject list = %q (%+v)", s, calls)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

package bot

import (
	"strings"
	"testing"

	"github.com/Mimic890/terakoya/internal/i18n"
	"github.com/Mimic890/terakoya/internal/render"
	"github.com/Mimic890/terakoya/internal/settings"
)

// listsHarness has Геометрия with 7 класс (5 topics) and 10 класс
// (1 topic), and Алгебра with 8 класс (no topics).
func listsHarness(t *testing.T) *harness {
	return newHarness(t, map[string]string{
		"Геометрия/7 класс/02 Углы/Углы.md":             "# Углы\n",
		"Геометрия/7 класс/03 Окружность/Окружность.md": "# Окружность\n",
		"Геометрия/7 класс/04 Площадь/Площадь.md":       "# Площадь\n",
		"Геометрия/7 класс/05 Подобие/Подобие.md":       "# Подобие\n",
		"Алгебра/8 класс/.keep/x.md":                    "", // a grade without topics
	})
}

func TestPlurals(t *testing.T) {
	for n, want := range map[int]string{0: "классов", 1: "класс", 2: "класса", 4: "класса", 5: "классов", 11: "классов",
		12: "классов", 21: "класс", 22: "класса", 111: "классов", 101: "класс"} {
		if got := plural(n, i18n.RU.GradeForms); got != want {
			t.Errorf("%d %s, want %s", n, got, want)
		}
	}
	for n, want := range map[int]string{1: "тема", 3: "темы", 5: "тем", 14: "тем", 23: "темы"} {
		if got := plural(n, i18n.RU.TopicForms); got != want {
			t.Errorf("%d %s, want %s", n, got, want)
		}
	}
}

func TestListMarkdown(t *testing.T) {
	h := listsHarness(t)
	cat := h.bot.catalog()
	geo, alg := cat.Subjects[1], cat.Subjects[0]
	if geo.Name != "Геометрия" || alg.Name != "Алгебра" {
		t.Fatalf("subjects = %s, %s", geo.Name, alg.Name)
	}
	link := func(label, data string) string { return "[" + label + "](" + render.CallbackLink(data) + ")" }

	// Without custom emoji: no emoji and no space in front.
	want := "# Предметы\n\n" +
		link("Алгебра · 1 класс", "s:"+alg.ID) + "\n" +
		link("Геометрия · 2 класса", "s:"+geo.ID) + "\n"
	if got := h.bot.subjectsMarkdown(cat); got != want {
		t.Errorf("subjects =\n%s\nwant\n%s", got, want)
	}
	want = "# Геометрия\n\n" +
		link("7 класс · 5 тем", "g:"+geo.Grades[0].ID) + "\n" +
		link("10 класс · 1 тема", "g:"+geo.Grades[1].ID) + "\n"
	if got := h.bot.gradesMarkdown(geo); got != want {
		t.Errorf("grades =\n%s\nwant\n%s", got, want)
	}
	if got := h.bot.gradesMarkdown(alg); got != "# Алгебра\n\n"+link("8 класс · 0 тем", "g:"+alg.Grades[0].ID)+"\n" {
		t.Errorf("grades of Алгебра =\n%s", got)
	}

	// With one for Геометрия: on its line, in its heading, and in its
	// topic lists; Алгебра has none and no space either.
	h.tg.emoji = []string{"301"}
	h.bot.settings.Update(func(s *settings.Settings) { s.Emoji.Subjects["Геометрия"] = "301" })
	icon := "![⭐](tg://emoji?id=301) "
	if got := h.bot.subjectsMarkdown(cat); !strings.Contains(got, "\n"+link("Алгебра · 1 класс", "s:"+alg.ID)+"\n"+icon+link("Геометрия · 2 класса", "s:"+geo.ID)+"\n") {
		t.Errorf("subjects with an emoji =\n%s", got)
	}
	if got := h.bot.gradesMarkdown(geo); !strings.HasPrefix(got, "# "+icon+"Геометрия\n\n") {
		t.Errorf("grades with an emoji =\n%s", got)
	}
	if got := h.bot.topicListMarkdown(geo.Grades[0]); !strings.HasPrefix(got, "# "+icon+"Геометрия · 7 класс\n\n") {
		t.Errorf("topics with an emoji =\n%s", got)
	}
	if got := h.bot.gradesMarkdown(alg); !strings.HasPrefix(got, "# Алгебра\n\n") {
		t.Errorf("grades without an emoji =\n%s", got)
	}
}

func TestRichListNavigation(t *testing.T) {
	h := listsHarness(t)
	geo := h.bot.catalog().Subjects[1]
	g := geo.Grades[1]

	// /start: the subject list, a rich message with only «Закрыть».
	h.say("/start")
	calls := h.tg.take()
	subjects := lastRich(t, calls)
	// One line per subject: the renderer keeps line breaks as hard breaks.
	if r := richs(calls); len(r) != 1 || r[0] != "# Предметы\n\n"+
		`<tg-button type="callback_data" style="link" data="s:`+h.bot.catalog().Subjects[0].ID+`">Алгебра · 1 класс</tg-button>  `+"\n"+
		`<tg-button type="callback_data" style="link" data="s:`+geo.ID+`">Геометрия · 2 класса</tg-button>`+"\n" {
		t.Errorf("subjects = %q", r)
	}
	if got := callbacksOf(t, subjects.params["reply_markup"]); strings.Join(got, " ") != cbClose {
		t.Errorf("under the subjects = %v", got)
	}

	// A subject replaces the list: a new rich message, the old one goes.
	list := h.tg.lastID("sendRichMessage")
	h.tapOn(list, cbSubject+geo.ID)
	calls = h.tg.take()
	grades := lastRich(t, calls)
	if !strings.HasPrefix(richs(calls)[0], "# Геометрия\n\n") || len(to(calls, "editMessageText", 42)) != 0 ||
		!contains(ids(calls, "deleteMessage"), itoa(list)) {
		t.Errorf("grades = %+v", calls)
	}
	// [Сообщить об ошибке] [« Назад] in one row.
	if got := callbacksOf(t, grades.params["reply_markup"]); strings.Join(got, " ") != cbReportSubject+geo.ID+" "+cbHome ||
		!strings.Contains(grades.params["reply_markup"], `[[{"text":"Сообщить об ошибке","callback_data":"m:`+geo.ID+`"},{"text":"« Назад","callback_data":"h"}]]`) {
		t.Errorf("under the grades = %s", grades.params["reply_markup"])
	}

	// A grade, then «Назад» back to the grades, then to the subjects.
	list = h.tg.lastID("sendRichMessage")
	h.tapOn(list, cbGrade+g.ID)
	if r := richs(h.tg.take()); len(r) != 1 || !strings.HasPrefix(r[0], "# Геометрия · 10 класс") {
		t.Errorf("topics = %q", r)
	}
	h.tapOn(h.tg.lastID("sendRichMessage"), cbSubject+geo.ID)
	if r := richs(h.tg.take()); len(r) != 1 || !strings.HasPrefix(r[0], "# Геометрия\n\n") {
		t.Errorf("back to the grades = %q", r)
	}
	h.tapOn(h.tg.lastID("sendRichMessage"), cbHome)
	if r := richs(h.tg.take()); len(r) != 1 || !strings.HasPrefix(r[0], "# Предметы\n\n") {
		t.Errorf("back to the subjects = %q", r)
	}
	// «Закрыть» removes it.
	list = h.tg.lastID("sendRichMessage")
	h.tapOn(list, cbClose)
	if got := ids(h.tg.take(), "deleteMessage"); !contains(got, itoa(list)) {
		t.Errorf("close deleted %v", got)
	}
}

func TestSubjectReport(t *testing.T) {
	h := listsHarness(t)
	geo := h.bot.catalog().Subjects[1]
	h.say(i18n.RU.KeySubjects)
	h.tapOn(h.tg.lastID("sendRichMessage"), cbSubject+geo.ID)
	h.tg.take()
	h.tapOn(h.tg.lastID("sendRichMessage"), cbReportSubject+geo.ID)
	wantPrompt(t, h.tg.take(), "Геометрия. Опишите, что не так или чего не хватает. Например: «нет 11 класса». Можно приложить фото или скриншот.", "Отмена")
	// «Отмена» goes back to the grade list.
	h.tap(cbCancel)
	if r := richs(h.tg.take()); len(r) != 1 || !strings.HasPrefix(r[0], "# Геометрия\n\n") {
		t.Errorf("cancel = %q", r)
	}

	h.tapOn(h.tg.lastID("sendRichMessage"), cbReportSubject+geo.ID)
	h.say("нет 11 класса")
	h.tap(cbSend)
	if s := texts(h.tg.take(), "editMessageText"); len(s) != 1 || s[0] != i18n.RU.SubmittedReport {
		t.Errorf("outcome = %q", s)
	}
	if len(h.host.issues) != 1 || h.host.issues[0] != "Missing material: Геометрия" {
		t.Fatalf("issues = %q", h.host.issues)
	}
	if body := h.host.bodies[7]; !strings.Contains(body, "```text\nнет 11 класса\n```") || !strings.Contains(body, "- Grade: [`Геометрия`](") {
		t.Errorf("body:\n%s", body)
	}
	if got := h.bot.historyMarkdown(h.ctx, 42); !strings.Contains(got, "- Геометрия — не хватает материала, ") {
		t.Errorf("history:\n%s", got)
	}
}

func TestButtonLists(t *testing.T) {
	h := listsHarness(t)
	geo := h.bot.catalog().Subjects[1]
	h.tg.emoji = []string{"301"}
	h.bot.settings.Update(func(s *settings.Settings) {
		s.ListStyle = settings.ListButtons
		s.Emoji.Subjects["Геометрия"] = "301"
	})

	// /start: a text menu with a button per subject, the emoji on its
	// button, and «Закрыть».
	h.say("/start")
	calls := h.tg.take()
	menu := last(t, calls)
	if menu.params["text"] != i18n.RU.ChooseSubject || len(richs(calls)) != 0 {
		t.Fatalf("subjects = %+v", calls)
	}
	for _, want := range []string{`{"text":"Алгебра","callback_data":"s:`, `{"text":"Геометрия","icon_custom_emoji_id":"301","callback_data":"s:` + geo.ID + `"}`,
		`[{"text":"Закрыть","callback_data":"c"}]`} {
		if !strings.Contains(menu.params["reply_markup"], want) {
			t.Errorf("subject buttons have no %s: %s", want, menu.params["reply_markup"])
		}
	}
	// A subject: the same message, edited, with grade buttons and the
	// report and «Назад» row.
	id := h.bot.msgs.menu(h.ctx, 42)
	h.tapOn(id, cbSubject+geo.ID)
	e := to(h.tg.take(), "editMessageText", 42)
	if len(e) != 1 || e[0].params["message_id"] != itoa(id) || e[0].params["text"] != "Геометрия\n\nВыберите класс:" ||
		!strings.Contains(e[0].params["reply_markup"], `[{"text":"7 класс","callback_data":"g:`) ||
		!strings.Contains(e[0].params["reply_markup"], `[{"text":"Сообщить об ошибке","callback_data":"m:`+geo.ID+`"},{"text":"« Назад","callback_data":"h"}]`) {
		t.Errorf("grades = %+v", e)
	}
	// A grade: the topics, edited in place too; «Назад» there and here.
	h.tapOn(id, cbGrade+geo.Grades[1].ID)
	if e := to(h.tg.take(), "editMessageText", 42); len(e) != 1 || !strings.HasPrefix(e[0].params["text"], "Геометрия · 10 класс") {
		t.Errorf("topics = %+v", e)
	}
	h.tapOn(id, cbSubject+geo.ID)
	h.tapOn(id, cbHome)
	if e := to(h.tg.take(), "editMessageText", 42); len(e) != 2 || e[1].params["text"] != i18n.RU.ChooseSubject {
		t.Errorf("back = %+v", e)
	}
}

func TestReloadDropsPinsOfUnknownNotes(t *testing.T) {
	h := listsHarness(t)
	keep := h.id(triangles)
	h.store.Pin(h.ctx, 42, keep, 10)
	h.store.Pin(h.ctx, 42, "an-old-id", 10)
	if err := h.bot.Reload(h.ctx); err != nil {
		t.Fatal(err)
	}
	if ids, _ := h.store.Pins(h.ctx, 42); len(ids) != 1 || ids[0] != keep {
		t.Errorf("pins = %v", ids)
	}
}

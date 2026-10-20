package bot

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Mimic890/terakoya/internal/i18n"
	"github.com/Mimic890/terakoya/internal/store"
)

// searched is the Markdown of the search results among the calls, or "".
func searched(calls []call) string {
	for _, c := range calls {
		if c.method == "sendRichMessage" && strings.Contains(c.params["rich_message"], "# Поиск: «") {
			return c.params["rich_message"]
		}
	}
	return ""
}

// resultsMsg is the ID of the message with the results the calls sent.
func (h *harness) resultsMsg(calls []call) int {
	h.t.Helper()
	if searched(calls) == "" {
		h.t.Fatal("no results sent")
	}
	// The results are the last rich message sent.
	return h.tg.lastID("sendRichMessage")
}

func deleted(calls []call) []string {
	var out []string
	for _, c := range calls {
		if c.method == "deleteMessage" {
			out = append(out, c.params["message_id"])
		}
	}
	return out
}

func TestTextIsASearch(t *testing.T) {
	h := newHarness(t, nil)
	main := h.id(vtopic + "/Параллельность прямой и плоскости.md")
	h.say("Параллельность")
	calls := h.tg.take()
	md := searched(calls)
	for _, want := range []string{
		"# Поиск: «Параллельность»", "Найдено: ",
		`data=\"sa:` + main + `\"`, "Тема · Параллельность прямой и плоскости",
		"Геометрия · 10 класс · §5 Параллельность прямой и плоскости", "**",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("results lack %q:\n%s", want, md)
		}
	}
	// The query goes after the results are out.
	if d := deleted(calls); len(d) != 1 || d[0] != strconv.Itoa(h.msgID) || calls[len(calls)-1].method != "deleteMessage" {
		t.Errorf("deleted = %v, calls = %v", d, calls)
	}
	// /search with a query does the same.
	h.say("/search треугольник")
	if md := searched(h.tg.take()); !strings.Contains(md, "Треугольники") {
		t.Errorf("/search = %s", md)
	}
	// Recorded for /admin, normalized, without who searched.
	top, _ := h.store.TopSearches(h.ctx, time.Now().Add(-time.Hour), 10)
	var logged []string
	for _, c := range top {
		logged = append(logged, c.Query)
	}
	sort.Strings(logged)
	if strings.Join(logged, ",") != "параллельность,треугольник" || top[0].Results == 0 {
		t.Errorf("logged = %v", top)
	}
	// A message without a word is not a query.
	h.say("👍")
	if s := texts(h.tg.take(), "sendMessage"); len(s) != 1 || s[0] != i18n.RU.UnknownCommand {
		t.Errorf("emoji = %q", s)
	}
}

func TestSearchCommandAlone(t *testing.T) {
	h := newHarness(t, nil)
	h.say("/search")
	calls := h.tg.take()
	wantPrompt(t, calls, i18n.RU.SearchPrompt, "Отмена")
	prompt := h.tg.lastID("sendMessage")
	h.say("треугольники")
	query := h.msgID
	calls = h.tg.take()
	if md := searched(calls); !strings.Contains(md, "Треугольники") {
		t.Fatalf("results = %s", md)
	}
	// In the background, in any order.
	d := deleted(calls)
	sort.Strings(d)
	if strings.Join(d, ",") != fmt.Sprintf("%d,%d", prompt, query) {
		t.Errorf("deleted %s, want the prompt %d and the query %d", d, prompt, query)
	}
	if d, _ := h.bot.loadDialog(h.ctx, anya.ID, anya.ID); d != nil {
		t.Error("the dialog stayed")
	}
	// «Отмена» of /search only goes.
	h.say("/search")
	h.tg.take()
	h.tap(cbCancel)
	if s := searched(h.tg.take()); s != "" {
		t.Errorf("cancel searched: %s", s)
	}
}

func TestTextInADialogIsNotASearch(t *testing.T) {
	h := newHarness(t, nil)
	h.tap(cbReport + h.id(triangles))
	h.tg.take()
	h.say("опечатка во втором абзаце")
	calls := h.tg.take()
	if md := searched(calls); md != "" {
		t.Errorf("searched in a dialog: %s", md)
	}
	wantPrompt(t, calls, i18n.RU.ReportReceived, "Отправить")
	if top, _ := h.store.TopSearches(h.ctx, time.Time{}, 10); len(top) != 0 {
		t.Errorf("logged = %v", top)
	}
}

func TestOpenAResultAndBack(t *testing.T) {
	h := newHarness(t, nil)
	h.say("треугольник")
	results := h.resultsMsg(h.tg.take())
	id := h.id(triangles)
	h.tapOn(results, cbSearchOpen+id)
	calls := h.tg.take()
	note := calls[0]
	for _, c := range calls {
		if c.method == "sendRichMessage" {
			note = c
		}
	}
	if !strings.Contains(note.params["reply_markup"], `"bs:`+id+`"`) {
		t.Errorf("note keyboard = %s", note.params["reply_markup"])
	}
	if d := deleted(calls); len(d) != 1 || d[0] != strconv.Itoa(results) {
		t.Errorf("results not replaced: %v", d)
	}
	// Pinning keeps «Назад» to the results.
	noteID := h.tg.lastID("sendRichMessage")
	h.tapWith(noteID, cbPin+id, note.params["reply_markup"])
	if c := h.tg.take(); !strings.Contains(c[len(c)-1].params["reply_markup"], `"bs:`+id+`"`) {
		t.Errorf("after pinning = %+v", c)
	}
	h.tapOn(noteID, cbBackSearch+id)
	calls = h.tg.take()
	if md := searched(calls); !strings.Contains(md, "# Поиск: «треугольник»") {
		t.Errorf("back = %+v", calls)
	}
	// After a restart, «Назад» goes back as from the catalog.
	h.bot.searches = nil
	h.tapOn(noteID, cbBackSearch+id)
	if md := searched(h.tg.take()); md != "" {
		t.Errorf("back without results = %s", md)
	}
}

func TestSearchPages(t *testing.T) {
	extra := map[string]string{}
	for i := 1; i <= 11; i++ {
		extra[fmt.Sprintf("Геометрия/8 класс/%02d Медианы %d/Медианы %d.md", i, i, i)] =
			fmt.Sprintf("# Медианы %d\n\nМедиана соединяет вершину с серединой стороны.\n", i)
	}
	h := newHarness(t, extra)
	h.say("медиана")
	calls := h.tg.take()
	md := searched(calls)
	if !strings.Contains(md, "Найдено: 11 · страница 1 из 2") || strings.Count(md, `data=\"sa:`) != 8 {
		t.Errorf("page 1:\n%s", md)
	}
	var markup string
	for _, c := range calls {
		if c.method == "sendRichMessage" {
			markup = c.params["reply_markup"]
		}
	}
	if !strings.Contains(markup, `"sp:1"`) || strings.Contains(markup, `"sp:0"`) {
		t.Errorf("page 1 buttons = %s", markup)
	}
	h.tapOn(h.resultsMsg(calls), cbSearchPage+"1")
	calls = h.tg.take()
	md = searched(calls)
	if !strings.Contains(md, "страница 2 из 2") || strings.Count(md, `data=\"sa:`) != 3 {
		t.Errorf("page 2:\n%s", md)
	}
	for _, c := range calls {
		if c.method == "sendRichMessage" && (!strings.Contains(c.params["reply_markup"], `"sp:0"`) ||
			strings.Contains(c.params["reply_markup"], `"sp:2"`)) {
			t.Errorf("page 2 buttons = %s", c.params["reply_markup"])
		}
	}
	// Paging is not a new search.
	if top, _ := h.store.TopSearches(h.ctx, time.Time{}, 10); len(top) != 1 || top[0].Count != 1 {
		t.Errorf("logged = %v", top)
	}
	// After a restart, the pages are gone.
	h.bot.searches = nil
	h.tapOn(1, cbSearchPage+"0")
	if c := h.tg.take(); len(c) != 1 || c[0].params["text"] != i18n.RU.SearchExpired {
		t.Errorf("expired = %+v", c)
	}
}

func TestNothingFound(t *testing.T) {
	h := newHarness(t, nil)
	h.say("треугольник 11 класс")
	calls := h.tg.take()
	md := searched(calls)
	if !strings.Contains(md, "Ничего не нашлось") || !strings.Contains(md, "Возможно, вы имели в виду") {
		t.Errorf("nothing found:\n%s", md)
	}
	var markup string
	for _, c := range calls {
		if c.method == "sendRichMessage" {
			markup = c.params["reply_markup"]
		}
	}
	subject := h.bot.catalog().Subjects[0].ID
	for _, want := range []string{`"text":"треугольник","callback_data":"ss:0"`, `"sr:` + subject + `"`, `"c"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("buttons lack %s: %s", want, markup)
		}
	}
	if top, _ := h.store.MissingSearches(h.ctx, time.Time{}, 10); len(top) != 1 || top[0].Query != "треугольник 11 класс" {
		t.Errorf("missing = %v", top)
	}
	results := h.resultsMsg(calls)

	// The query offered searches again.
	h.tapOn(results, cbSearchSuggest+"0")
	if md := searched(h.tg.take()); !strings.Contains(md, "# Поиск: «треугольник»") || !strings.Contains(md, "Треугольники") {
		t.Errorf("suggestion:\n%s", md)
	}

	// «Сообщить, что не хватает материала»: the report starts with the
	// query and can be sent as it is.
	h.say("пифагор")
	calls = h.tg.take()
	h.tapOn(h.resultsMsg(calls), cbSearchReport+subject)
	wantPrompt(t, h.tg.take(), fmt.Sprintf(i18n.RU.SearchReportPrompt, "Геометрия", "пифагор"), "Отправить", "Отмена")
	// «Отмена» returns to the results.
	h.tap(cbCancel)
	if md := searched(h.tg.take()); !strings.Contains(md, "# Поиск: «пифагор»") {
		t.Error("cancel did not return to the results")
	}
	h.tapOn(h.tg.lastID("sendRichMessage"), cbSearchReport+subject)
	h.say("нужна теорема Пифагора для 8 класса")
	h.tap(cbSend)
	if len(h.host.issues) != 1 {
		t.Fatalf("issues = %v", h.host.issues)
	}
	body := h.host.bodies[h.host.number]
	if !strings.Contains(body, "По запросу «пифагор» ничего не нашлось.") || !strings.Contains(body, "нужна теорема Пифагора") {
		t.Errorf("issue body:\n%s", body)
	}
}

func TestSearchReportChoosesTheSubject(t *testing.T) {
	h := newHarness(t, map[string]string{"Алгебра/7 класс/01 Степени/Степени.md": "# Степени\n\nСтепень числа.\n"})
	h.say("логарифм")
	calls := h.tg.take()
	h.tapOn(h.resultsMsg(calls), cbSearchReport)
	c := h.tg.take()
	choice := last(t, c)
	if choice.params["text"] != i18n.RU.SearchReportChoose || !strings.Contains(choice.params["reply_markup"], `"sp:0"`) {
		t.Fatalf("choice = %+v", c)
	}
	alg := h.bot.catalog().Subjects[0]
	if !strings.Contains(choice.params["reply_markup"], `"sr:`+alg.ID+`"`) {
		t.Errorf("no %s: %s", alg.Name, choice.params["reply_markup"])
	}
	h.tapOn(h.tg.lastID("sendMessage"), cbSearchReport+alg.ID)
	wantPrompt(t, h.tg.take(), fmt.Sprintf(i18n.RU.SearchReportPrompt, "Алгебра", "логарифм"), "Отправить")
}

func TestSearchIsRateLimited(t *testing.T) {
	h := newHarness(t, nil)
	h.bot.limit = newLimiter(2, time.Hour)
	h.say("треугольник")
	h.say("прямая")
	h.tg.take()
	h.say("плоскость")
	if c := h.tg.take(); len(c) != 0 {
		t.Errorf("over the limit = %+v", c)
	}
	if top, _ := h.store.TopSearches(h.ctx, time.Time{}, 10); len(top) != 2 {
		t.Errorf("logged = %v", top)
	}
}

func TestSearchInAdminStats(t *testing.T) {
	h := newHarness(t, nil)
	h.say("треугольник")
	h.say("трапеция")
	h.say("трапеция")
	s, err := h.bot.AdminStats(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(s.Searches) != "[{трапеция 2 0} {треугольник 1 1}]" || fmt.Sprint(s.Missing) != "[{трапеция 2 0}]" {
		t.Errorf("searches = %v, missing = %v", s.Searches, s.Missing)
	}
	md := h.bot.statsMarkdown(s)
	for _, want := range []string{"## Чаще всего ищут — 7 дней\n\n1\\. «трапеция» — 2  \n2\\. «треугольник» — 1\n",
		"## Ищут, но не находят — 180 дней\n\n1\\. «трапеция» — 2\n"} {
		if !strings.Contains(md, want) {
			t.Errorf("stats lack %q:\n%s", want, md)
		}
	}
	// Cleaned up with the rest after 180 days.
	h.store.SetClock(func() time.Time { return time.Now().Add(store.KeepSearches + time.Hour) })
	if c, _ := h.store.Cleanup(h.ctx); c.Searches != 3 {
		t.Errorf("cleaned = %+v", c)
	}
}

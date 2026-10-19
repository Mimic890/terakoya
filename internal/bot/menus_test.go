package bot

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/content"
	"github.com/Mimic890/terakoya/internal/i18n"
	"github.com/Mimic890/terakoya/internal/render"
	"github.com/Mimic890/terakoya/internal/store"
	"github.com/Mimic890/terakoya/internal/suggest"
)

// lastRich returns the last sendRichMessage call.
func lastRich(t *testing.T, calls []call) call {
	t.Helper()
	for i := len(calls) - 1; i >= 0; i-- {
		if calls[i].method == "sendRichMessage" {
			return calls[i]
		}
	}
	t.Fatalf("no note sent: %+v", calls)
	return call{}
}

func firstCall(t *testing.T, calls []call, method string) call {
	t.Helper()
	for _, c := range calls {
		if c.method == method {
			return c
		}
	}
	t.Fatalf("no %s: %+v", method, calls)
	return call{}
}

func TestStartSetsKeyboard(t *testing.T) {
	h := newHarness(t, nil)
	h.say("/start")
	calls := h.tg.take()
	sent := 0
	for _, c := range calls {
		if c.method != "sendMessage" {
			continue
		}
		sent++
		if sent == 1 {
			if c.params["text"] != "Привет, Аня!\nЭто terakoya — школьный справочник: теоремы, аксиомы, задачи и доказательства по классам.\nНашли ошибку или чего-то не хватает? Сообщите — за принятые правки растёт ранг." {
				t.Errorf("greeting = %q", c.params["text"])
			}
			kb := c.params["reply_markup"]
			for _, want := range []string{`"keyboard"`, `"is_persistent":true`, `"resize_keyboard":true`,
				`"Предметы"`, `"Закреплённые"`, `"Аккаунт"`} {
				if !strings.Contains(kb, want) {
					t.Errorf("keyboard has no %s: %s", want, kb)
				}
			}
		}
		if sent == 2 && c.params["text"] != i18n.RU.ChooseSubject {
			t.Errorf("menu = %q", c.params["text"])
		}
	}
	if sent != 2 {
		t.Errorf("sent %d messages", sent)
	}
}

func TestKeyButtons(t *testing.T) {
	h := newHarness(t, nil)
	h.say("/start")
	h.tg.take()
	menu := h.bot.msgs.lastMenu(h.ctx, 42)
	if menu == 0 {
		t.Fatal("no menu")
	}

	// The menu is the last message: it is edited in place, and the tap is
	// deleted.
	h.say(i18n.RU.KeyPinned)
	calls := h.tg.take()
	if got := ids(calls, "deleteMessage"); len(got) != 1 || got[0] != fmt.Sprint(h.msgID) {
		t.Errorf("deleted %v, want the tap %d", got, h.msgID)
	}
	e := to(calls, "editMessageText", 42)
	if len(e) != 1 || e[0].params["message_id"] != fmt.Sprint(menu) || e[0].params["text"] != "Закреплённые\n\n"+i18n.RU.PinnedEmpty {
		t.Errorf("pins = %+v", calls)
	}
	if len(texts(calls, "sendMessage")) != 0 {
		t.Error("a new menu was sent")
	}

	// With a note below the menu, the old menu goes and a new one comes.
	h.tapOn(menu, cbHome)
	h.tg.take()
	h.bot.sendArticle(h.ctx, 42, h.bot.catalog().NoteByPath(triangles), false)
	h.tg.take()
	h.say(i18n.RU.KeySubjects)
	calls = h.tg.take()
	if got := texts(calls, "sendMessage"); len(got) != 1 || got[0] != i18n.RU.ChooseSubject {
		t.Errorf("subjects = %q", got)
	}
	if got := ids(calls, "deleteMessage"); len(got) != 2 || got[1] != fmt.Sprint(menu) {
		t.Errorf("deleted %v, want the tap and the old menu %d", got, menu)
	}
	if len(to(calls, "editMessageText", 42)) != 0 {
		t.Error("the old menu was edited")
	}

	// The keyboard does not disturb a dialog: the tap is not taken as the
	// description.
	h.tap("e:" + h.id(triangles))
	h.tg.take()
	h.say(i18n.RU.KeyAccount)
	if d := h.bot.mustDialog(t); d.ses.State != stateReport || d.p.Text != "" {
		t.Errorf("dialog = %s %q", d.ses.State, d.p.Text)
	}
}

func TestPins(t *testing.T) {
	h := newHarness(t, nil)
	note := h.id(triangles)

	h.tapOn(500, "a:"+note)
	calls := h.tg.take()
	kb := lastRich(t, calls).params["reply_markup"]
	if !strings.Contains(kb, `"Закрепить"`) || !strings.Contains(kb, `"p:`+note+`"`) {
		t.Fatalf("note keyboard = %s", kb)
	}
	noteMsg := h.tg.lastID("sendRichMessage")

	h.tapOn(noteMsg, cbPin+note)
	calls = h.tg.take()
	if got := toasts(calls); len(got) != 1 || got[0] != i18n.RU.PinDone {
		t.Errorf("toast = %q", got)
	}
	r := firstCall(t, calls, "editMessageReplyMarkup")
	if !strings.Contains(r.params["reply_markup"], `"Открепить"`) || !strings.Contains(r.params["reply_markup"], `"b:`+note+`"`) {
		t.Errorf("flipped = %s", r.params["reply_markup"])
	}

	// The list opens the note in its place; «Назад» comes back to the list.
	h.store.Pin(h.ctx, 42, "vanished", maxPins)
	text, kbd := h.bot.pinsView(h.ctx, 42)
	if text != i18n.RU.PinnedTitle || strings.Join(callbacks(kbd), " ") != cbFromPins+note+" "+cbClose ||
		labels(kbd)[0] != "Треугольники" {
		t.Errorf("list = %q %v %v", text, callbacks(kbd), labels(kbd))
	}
	if pins, _ := h.store.Pins(h.ctx, 42); len(pins) != 1 {
		t.Errorf("the vanished pin stayed: %v", pins)
	}
	h.tapOn(700, cbFromPins+note)
	calls = h.tg.take()
	kb = lastRich(t, calls).params["reply_markup"]
	if !strings.Contains(kb, `"bp:`+note+`"`) || !strings.Contains(kb, `"Открепить"`) {
		t.Errorf("note from pins = %s", kb)
	}
	if got := ids(calls, "deleteMessage"); len(got) != 1 || got[0] != "700" {
		t.Errorf("list not replaced: %v", got)
	}
	noteMsg = h.tg.lastID("sendRichMessage")
	// Unpinning keeps the way back to the list.
	h.tapWith(noteMsg, cbPin+note, kb)
	r = firstCall(t, h.tg.take(), "editMessageReplyMarkup")
	if !strings.Contains(r.params["reply_markup"], `"bp:`+note+`"`) || !strings.Contains(r.params["reply_markup"], `"Закрепить"`) {
		t.Errorf("unpinned = %s", r.params["reply_markup"])
	}
	h.tapOn(noteMsg, cbBackPins+note)
	calls = h.tg.take()
	if got := texts(calls, "sendMessage"); len(got) != 1 || got[0] != "Закреплённые\n\n"+i18n.RU.PinnedEmpty {
		t.Errorf("back = %q", got)
	}

	// The limit.
	for i := range maxPins {
		h.store.Pin(h.ctx, 42, fmt.Sprintf("n%d", i), maxPins)
	}
	h.tapOn(noteMsg, cbPin+note)
	calls = h.tg.take()
	if got := toasts(calls); len(got) != 1 || got[0] != fmt.Sprintf(i18n.RU.PinLimit, maxPins) {
		t.Errorf("limit toast = %q", got)
	}
	if ok, _ := h.store.Pinned(h.ctx, 42, note); ok {
		t.Error("pinned over the limit")
	}
}

func TestAccount(t *testing.T) {
	h := newHarness(t, nil)
	h.setRanks("Novice:0,Pupil:1,Student:3,Scholar:15")
	want := "# Аня\n\nРанг: **Novice**  \nДо следующего ранга: ещё 1 принятая правка  \nСледующий ранг: **Pupil**\n\nПринято правок: 0  \nНа рассмотрении: 0"
	if got := h.bot.accountMarkdown(h.ctx, &anya); got != want {
		t.Errorf("account =\n%s\nwant\n%s", got, want)
	}

	// Accepted reports count; a row of another kind, which early versions
	// wrote, does not.
	for n, kind := range []string{store.KindReport, store.KindReport, "edit"} {
		h.store.CreateSuggestion(h.ctx, &store.Suggestion{ShortID: fmt.Sprint(n), TgID: 42, Kind: kind, Issue: n + 1})
	}
	for _, n := range []int{1, 3} {
		sg, _ := h.store.SuggestionByNumber(h.ctx, n)
		h.store.ResolveSuggestion(h.ctx, sg.ID, store.StatusMerged)
	}
	if got := h.bot.accountMarkdown(h.ctx, &anya); !strings.Contains(got, "Ранг: **Pupil**  \nДо следующего ранга: ещё 2 принятые правки  \nСледующий ранг: **Student**\n\nПринято правок: 1  \nНа рассмотрении: 1") {
		t.Errorf("after one = %s", got)
	}
	h.setRanks("Novice:0,Pupil:1,Student:6")
	if got := h.bot.accountMarkdown(h.ctx, &anya); !strings.Contains(got, "ещё 5 принятых правок  \nСледующий ранг: **Student**\n") {
		t.Errorf("five left = %s", got)
	}
	h.setRanks("Novice:0,Pupil:1")
	got := h.bot.accountMarkdown(h.ctx, &anya)
	if !strings.Contains(got, "Ранг: **Pupil**  \nЭто высший ранг\n\n") || strings.Contains(got, "Следующий ранг") || strings.Contains(got, "Уровень прав") {
		t.Errorf("top rank = %s", got)
	}
	// Only admins see their level; names are literal.
	admin := models.User{ID: ivan.ID, FirstName: "*Иван*"}
	if got := h.bot.accountMarkdown(h.ctx, &admin); !strings.HasPrefix(got, "# \\*Иван\\*\n") ||
		!strings.HasSuffix(got, "На рассмотрении: 0  \nУровень прав: admin") {
		t.Errorf("admin = %s", got)
	}
}

func TestAccountAndHistoryMessages(t *testing.T) {
	h := newHarness(t, nil)
	h.say("/start")
	h.tg.take()

	// «Аккаунт» is a rich message that replaces the subject list.
	menu := h.bot.msgs.menu(h.ctx, 42)
	h.say(i18n.RU.KeyAccount)
	calls := h.tg.take()
	acc := lastRich(t, calls)
	if !strings.HasPrefix(acc.params["rich_message"], `{"markdown":"# Аня`) ||
		!strings.Contains(acc.params["reply_markup"], `"Мои правки","callback_data":"hl"`) ||
		!strings.Contains(acc.params["reply_markup"], `"Закрыть","callback_data":"c"`) {
		t.Fatalf("account = %+v", acc.params)
	}
	if got := ids(calls, "deleteMessage"); len(got) != 2 || got[1] != fmt.Sprint(menu) {
		t.Errorf("deleted %v, want the tap and the menu %d", got, menu)
	}
	accID := h.tg.lastID("sendRichMessage")

	// The list replaces it; «Назад» brings it back.
	h.store.CreateSuggestion(h.ctx, &store.Suggestion{ShortID: "a", TgID: 42, Kind: store.KindReport, Issue: 1,
		ArticlePath: triangles, Title: "Треугольники"})
	h.tapOn(accID, cbHistory)
	calls = h.tg.take()
	list := lastRich(t, calls)
	if !strings.Contains(list.params["rich_message"], `data=\"a:`+h.id(triangles)+`\"`) ||
		!strings.Contains(list.params["reply_markup"], `"callback_data":"ac"`) {
		t.Errorf("list = %+v", list.params)
	}
	if got := ids(calls, "deleteMessage"); len(got) != 1 || got[0] != fmt.Sprint(accID) {
		t.Errorf("deleted %v, want the account %d", got, accID)
	}
	listID := h.tg.lastID("sendRichMessage")
	h.tapOn(listID, cbAccount)
	calls = h.tg.take()
	if got := ids(calls, "deleteMessage"); len(got) != 1 || got[0] != fmt.Sprint(listID) {
		t.Errorf("deleted %v, want the list %d", got, listID)
	}

	// A text menu never edits a rich one: it replaces it.
	accID = h.tg.lastID("sendRichMessage")
	h.say(i18n.RU.KeySubjects)
	calls = h.tg.take()
	if len(to(calls, "editMessageText", 42)) != 0 || len(texts(calls, "sendMessage")) != 1 {
		t.Errorf("subjects = %+v", calls)
	}
	if got := ids(calls, "deleteMessage"); len(got) != 2 || got[1] != fmt.Sprint(accID) {
		t.Errorf("deleted %v, want the tap and the account %d", got, accID)
	}
}

func TestHistory(t *testing.T) {
	h := newHarness(t, nil)
	if got := h.bot.historyMarkdown(h.ctx, 42); got != "# Мои правки\n\n## На рассмотрении\n\nПока нет\n\n---\n\n## Приняты\n\nПока нет\n\n---\n\n## Отклонены\n\nПока нет\n" {
		t.Errorf("empty = %q", got)
	}
	add := func(n int, kind, p, title, status, comment string) {
		sg := &store.Suggestion{ShortID: fmt.Sprint(n), TgID: 42, Kind: kind, Issue: n, ArticlePath: p, Title: title}
		h.store.CreateSuggestion(h.ctx, sg)
		if status != store.StatusOpen {
			h.store.ResolveSuggestion(h.ctx, sg.ID, status)
		}
		if comment != "" {
			h.store.SetSuggestionComment(h.ctx, sg.ID, comment)
		}
	}
	add(1, store.KindReport, triangles, "Треугольники", store.StatusMerged, "")
	add(2, store.KindReport, "Геометрия/10 класс", "Геометрия · 10 класс", store.StatusOpen, "")
	add(3, store.KindReport, vtopic+"/задачи/Новая [задача].md", "Новая [задача]", store.StatusClosed, "Нужен\nчертёж $")
	add(4, store.KindReport, vmain, "Параллельность прямой и плоскости", store.StatusOpen, "")
	add(5, store.KindReport, "Геометрия/7 класс", "Геометрия · 7 класс", store.StatusMerged, "")
	got := h.bot.historyMarkdown(h.ctx, 42)
	today := h.bot.date(time.Now())
	for _, want := range []string{
		"## На рассмотрении\n\n- [Параллельность прямой и плоскости](" + suggest.LinkTarget(vmain) + ") — ошибка, " + today +
			"\n- Геометрия · 10 класс — не хватает материала, " + today + "\n\n---\n\n## Приняты",
		"## Приняты\n\n- Геометрия · 7 класс — не хватает материала, " + today +
			"\n- [Треугольники](" + suggest.LinkTarget(triangles) + ") — ошибка, " + today + "\n",
		"## Отклонены\n\n- Новая \\[задача\\] — ошибка, " + today + "\n  Комментарий: «Нужен чертёж \\$»\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("history has no\n%s\n---\n%s", want, got)
		}
	}
	// Rendered, the links are buttons.
	n := &content.Note{Title: "Мои правки", Body: got}
	md := render.Convert(h.bot.catalog(), n, render.Options{}).Parts[0].Markdown
	if !strings.Contains(md, `data="a:`+h.id(triangles)+`">Треугольники</tg-button>`) {
		t.Errorf("rendered = %s", md)
	}
	// Two dividers, between the three sections.
	if strings.Count(md, "\n---\n") != 2 || strings.HasSuffix(strings.TrimSpace(md), "---") {
		t.Errorf("dividers in %s", md)
	}
}

func TestPlural(t *testing.T) {
	forms := i18n.RU.Reports
	one, few, many := "принятая правка", "принятые правки", "принятых правок"
	for n, want := range map[int]string{0: many, 1: one, 2: few, 4: few, 5: many, 11: many,
		12: many, 21: one, 22: few, 111: many, 101: one} {
		if got := plural(n, forms); got != want {
			t.Errorf("plural(%d) = %s, want %s", n, got, want)
		}
	}
}

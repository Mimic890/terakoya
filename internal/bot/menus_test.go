package bot

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Mimic890/terakoya/internal/i18n"
	"github.com/Mimic890/terakoya/internal/ranks"
	"github.com/Mimic890/terakoya/internal/store"
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
			if c.params["text"] != "Привет, Аня! Это Теракоя — школьный справочник, который пишут сами пользователи." {
				t.Errorf("greeting = %q", c.params["text"])
			}
			kb := c.params["reply_markup"]
			for _, want := range []string{`"keyboard"`, `"is_persistent":true`, `"resize_keyboard":true`,
				`"📚 Предметы"`, `"📌 Закреплённые"`, `"👤 Аккаунт"`} {
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
	if len(e) != 1 || e[0].params["message_id"] != fmt.Sprint(menu) || e[0].params["text"] != "📌 Закреплённые\n\n"+i18n.RU.PinnedEmpty {
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
	// edited text.
	h.tap("e:" + h.id(triangles))
	h.tap(cbFix)
	h.tg.take()
	h.say(i18n.RU.KeyAccount)
	if d := h.bot.mustDialog(t); d.ses.State != stateEditText || d.p.Text != "" {
		t.Errorf("dialog = %s %q", d.ses.State, d.p.Text)
	}
}

func TestPins(t *testing.T) {
	h := newHarness(t, nil)
	note := h.id(triangles)

	h.tapOn(500, "a:"+note)
	calls := h.tg.take()
	kb := lastRich(t, calls).params["reply_markup"]
	if !strings.Contains(kb, `"📌 Закрепить"`) || !strings.Contains(kb, `"p:`+note+`"`) {
		t.Fatalf("note keyboard = %s", kb)
	}
	noteMsg := h.tg.lastID("sendRichMessage")

	h.tapOn(noteMsg, cbPin+note)
	calls = h.tg.take()
	if got := toasts(calls); len(got) != 1 || got[0] != i18n.RU.PinDone {
		t.Errorf("toast = %q", got)
	}
	r := firstCall(t, calls, "editMessageReplyMarkup")
	if !strings.Contains(r.params["reply_markup"], `"📌 Открепить"`) || !strings.Contains(r.params["reply_markup"], `"b:`+note+`"`) {
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
	if !strings.Contains(kb, `"bp:`+note+`"`) || !strings.Contains(kb, `"📌 Открепить"`) {
		t.Errorf("note from pins = %s", kb)
	}
	if got := ids(calls, "deleteMessage"); len(got) != 1 || got[0] != "700" {
		t.Errorf("list not replaced: %v", got)
	}
	noteMsg = h.tg.lastID("sendRichMessage")
	// Unpinning keeps the way back to the list.
	h.tapWith(noteMsg, cbPin+note, kb)
	r = firstCall(t, h.tg.take(), "editMessageReplyMarkup")
	if !strings.Contains(r.params["reply_markup"], `"bp:`+note+`"`) || !strings.Contains(r.params["reply_markup"], `"📌 Закрепить"`) {
		t.Errorf("unpinned = %s", r.params["reply_markup"])
	}
	h.tapOn(noteMsg, cbBackPins+note)
	calls = h.tg.take()
	if got := texts(calls, "sendMessage"); len(got) != 1 || got[0] != "📌 Закреплённые\n\n"+i18n.RU.PinnedEmpty {
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
	h.bot.cfg.Ladder, _ = ranks.Parse("Novice:0,Pupil:1,Student:3")
	text, kb := h.bot.accountView(h.ctx, &anya)
	want := "👤 Аня\n\nРанг: Novice\nПринято правок: 0\nДо ранга Pupil: ещё 1 ▱▱▱▱▱▱▱\nПредложения на рассмотрении: 0\nПравка: разрешена"
	if text != want {
		t.Errorf("account =\n%s\nwant\n%s", text, want)
	}
	if strings.Join(callbacks(kb), " ") != cbClose {
		t.Errorf("buttons = %v", callbacks(kb))
	}

	for n := 1; n <= 2; n++ {
		h.store.CreateSuggestion(h.ctx, &store.Suggestion{ShortID: fmt.Sprint(n), TgID: 42, Kind: store.KindEdit, PRNumber: n})
	}
	sg, _ := h.store.SuggestionByNumber(h.ctx, 1)
	h.store.ResolveSuggestion(h.ctx, sg.ID, store.StatusMerged)
	text, _ = h.bot.accountView(h.ctx, &anya)
	if !strings.Contains(text, "Ранг: Pupil\nПринято правок: 1\nДо ранга Student: ещё 2 ▱▱▱▱▱▱▱\nПредложения на рассмотрении: 1") {
		t.Errorf("after one = %s", text)
	}

	// The others: no access yet, then pending; the admin.
	text, kb = h.bot.accountView(h.ctx, &boris)
	if !strings.HasSuffix(text, "Правка: нет доступа") || strings.Join(callbacks(kb), " ") != cbRequestAcc+" "+cbClose {
		t.Errorf("boris = %q %v", text, callbacks(kb))
	}
	h.as(boris)
	h.tapOn(800, cbRequestAcc)
	calls := h.tg.take()
	if got := toasts(calls); len(got) != 1 || got[0] != i18n.RU.AccessRequested {
		t.Errorf("toast = %q", got)
	}
	if e := to(calls, "editMessageText", boris.ID); len(e) != 1 || !strings.HasSuffix(e[0].params["text"], "Правка: запрос на рассмотрении") ||
		strings.Contains(e[0].params["reply_markup"], cbRequestAcc) {
		t.Errorf("refreshed = %+v", e)
	}
	if text, _ = h.bot.accountView(h.ctx, &ivan); !strings.HasSuffix(text, "Правка: администратор") {
		t.Errorf("admin = %q", text)
	}
}

func TestProgress(t *testing.T) {
	for _, c := range []struct {
		done, total int
		want        string
	}{{0, 1, "▱▱▱▱▱▱▱"}, {1, 2, "▰▰▰▱▱▱▱"}, {2, 2, "▰▰▰▰▰▰▰"}, {5, 0, "▱▱▱▱▱▱▱"}} {
		if got := progress(c.done, c.total); got != c.want {
			t.Errorf("progress(%d, %d) = %s, want %s", c.done, c.total, got, c.want)
		}
	}
}

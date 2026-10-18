package bot

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/i18n"
)

// sleeps records the waits of a broadcast instead of waiting.
type sleeps struct {
	mu sync.Mutex
	d  []time.Duration
}

func (s *sleeps) sleep(_ context.Context, d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d = append(s.d, d)
}

func broadcastHarness(t *testing.T) (*harness, *sleeps) {
	h := newHarness(t, nil)
	sl := &sleeps{}
	h.bot.sleep = sl.sleep
	olga := models.User{ID: 44, FirstName: "Ольга"}
	for _, u := range []models.User{boris, olga, ivan} {
		h.store.TouchUser(h.ctx, u.ID, u.Username, u.FirstName)
	}
	h.as(ivan)
	return h, sl
}

// richTo returns the rich messages sent to a chat.
func richTo(calls []call, chatID int64) []string {
	var out []string
	for _, c := range to(calls, "sendRichMessage", chatID) {
		out = append(out, c.params["rich_message"])
	}
	return out
}

func TestBroadcastPreviewAndEdit(t *testing.T) {
	h, sl := broadcastHarness(t)
	h.say("/broadcast")
	wantPrompt(t, h.tg.take(), i18n.RU.BroadcastAsk, "Отмена")

	text := "Привет всем"
	h.send(&models.Message{Text: text, Entities: []models.MessageEntity{ent(text, "всем", models.MessageEntityTypeBold)}})
	calls := h.tg.take()
	if p := richTo(calls, ivan.ID); len(p) != 1 || p[0] != `{"markdown":"Привет **всем**"}` {
		t.Errorf("preview = %q", p)
	}
	firstPreview := h.tg.lastID("sendRichMessage")
	wantPrompt(t, calls, "Отправить всем? Получателей: 3", "Да, отправить", "Изменить", "Нет")
	confirm := h.bot.mustDialogOf(t, ivan.ID).p.PromptID

	// «Изменить» takes the preview and the question away and asks again.
	h.tapOn(confirm, cbBroadcastEdit)
	calls = h.tg.take()
	if got := ids(calls, "deleteMessage"); !sameIDs(got, []int{firstPreview, confirm}) {
		t.Errorf("deleted %v, want the preview %d and the question %d", got, firstPreview, confirm)
	}
	wantPrompt(t, calls, i18n.RU.BroadcastAsk, "Отмена")

	caption := "Новый текст"
	h.send(&models.Message{Photo: []models.PhotoSize{{FileID: "thumb"}, {FileID: "big"}}, Caption: caption,
		CaptionEntities: []models.MessageEntity{ent(caption, "Новый", models.MessageEntityTypeItalic)}})
	calls = h.tg.take()
	want := `{"markdown":"![](tg://photo?id=p)\n\n*Новый* текст","media":[{"id":"p","media":{"type":"photo","media":"big"}}]}`
	if p := richTo(calls, ivan.ID); len(p) != 1 || p[0] != want {
		t.Errorf("preview with photo =\n%q\nwant\n%q", p, want)
	}
	preview := h.tg.lastID("sendRichMessage")
	d := h.bot.mustDialogOf(t, ivan.ID)
	msgs := d.p.Msgs

	h.tapOn(d.p.PromptID, cbBroadcastYes)
	calls = h.tg.take()
	// Everyone but the admin gets exactly the preview.
	for _, id := range []int64{anya.ID, boris.ID, 44} {
		if got := richTo(calls, id); len(got) != 1 || got[0] != want {
			t.Errorf("to %d: %q", id, got)
		}
	}
	if got := richTo(calls, ivan.ID); len(got) != 0 {
		t.Errorf("the admin got it: %q", got)
	}
	// The dialog goes, except the preview, which stays as the record.
	if got := ids(calls, "deleteMessage"); !sameIDs(got, without(msgs, []int{preview})) {
		t.Errorf("deleted %v, want %v but %d", got, msgs, preview)
	}
	if e := to(calls, "editMessageText", ivan.ID); len(e) != 1 || e[0].params["text"] != "Рассылка завершена. Доставлено: 3, заблокировали бота: 0, не доставлено: 0." {
		t.Errorf("report = %+v", e)
	}
	if s := texts(to(calls, "sendMessage", ivan.ID), "sendMessage"); len(s) != 1 || s[0] != "Рассылка: отправлено 0 из 3…" {
		t.Errorf("progress = %q", s)
	}
	all, _ := h.store.Broadcasts(h.ctx)
	if len(all) != 1 || all[0].AdminID != ivan.ID || all[0].Photo != "big" || all[0].Markdown != "*Новый* текст" ||
		all[0].Recipients != 3 || all[0].Sent != 3 || all[0].Finished.IsZero() {
		t.Errorf("stored = %+v", all)
	}
	// Under 25 a second: a 40 ms gap between sends.
	if len(sl.d) != 2 || sl.d[0] != 40*time.Millisecond || sl.d[1] != 40*time.Millisecond {
		t.Errorf("waits = %v", sl.d)
	}
	if d, _ := h.bot.loadDialog(h.ctx, ivan.ID, ivan.ID); d != nil {
		t.Error("the dialog survived")
	}
}

func TestBroadcastRateLimitAndBlocked(t *testing.T) {
	h, sl := broadcastHarness(t)
	limited := false
	h.tg.fail = func(method string, p map[string]string) (int, int) {
		switch {
		case method != "sendRichMessage":
		case p["chat_id"] == fmt.Sprint(boris.ID) && !limited:
			limited = true
			return 429, 3
		case p["chat_id"] == "44":
			return 403, 0
		}
		return 0, 0
	}
	h.say("/broadcast")
	h.say("Важная новость")
	h.tap(cbBroadcastYes)
	calls := h.tg.take()

	// Борис is retried after the wait Telegram asked for; Ольга blocked
	// the bot.
	if got := richTo(calls, boris.ID); len(got) != 1 {
		t.Errorf("boris got %d", len(got))
	}
	found := false
	for _, d := range sl.d {
		found = found || d == 3*time.Second
	}
	if !found {
		t.Errorf("waits = %v, want a 3 s one", sl.d)
	}
	if e := to(calls, "editMessageText", ivan.ID); len(e) != 1 || e[0].params["text"] != "Рассылка завершена. Доставлено: 2, заблокировали бота: 1, не доставлено: 0." {
		t.Errorf("report = %+v", e)
	}
	// From now on she is skipped.
	if ids, _ := h.bot.recipients(h.ctx); len(ids) != 2 {
		t.Errorf("recipients = %v", ids)
	}
	h.say("/broadcast")
	h.say("Ещё одна")
	if s := texts(h.tg.take(), "sendMessage"); !containsText(s, "Отправить всем? Получателей: 2") {
		t.Errorf("second = %q", s)
	}
	// Coming back unblocks her.
	h.store.TouchUser(h.ctx, 44, "", "Ольга")
	if ids, _ := h.bot.recipients(h.ctx); len(ids) != 3 {
		t.Errorf("after she came back = %v", ids)
	}
}

func TestBroadcastProgress(t *testing.T) {
	h, _ := broadcastHarness(t)
	h.bot.progressEvery = 0 // after every message
	h.say("/broadcast")
	h.say("Новость")
	h.tap(cbBroadcastYes)
	var got []string
	for _, c := range to(h.tg.take(), "editMessageText", ivan.ID) {
		got = append(got, c.params["text"])
	}
	want := []string{"Рассылка: отправлено 1 из 3…", "Рассылка: отправлено 2 из 3…", "Рассылка: отправлено 3 из 3…",
		"Рассылка завершена. Доставлено: 3, заблокировали бота: 0, не доставлено: 0."}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("progress = %q", got)
	}
}

func TestBroadcastNoAndOthers(t *testing.T) {
	h, _ := broadcastHarness(t)
	h.say("/broadcast")
	h.say("Черновик")
	h.send(&models.Message{Sticker: &models.Sticker{FileID: "s"}})
	h.tg.take()
	msgs := h.bot.mustDialogOf(t, ivan.ID).p.Msgs
	// «Нет» cleans up like any «Отмена» and sends nothing.
	h.tap(cbCancel)
	calls := h.tg.take()
	if got := ids(calls, "deleteMessage"); !sameIDs(got, msgs) || len(texts(calls, "sendMessage")) != 0 {
		t.Errorf("no: deleted %v, want %v; %+v", got, msgs, calls)
	}
	for _, id := range []int64{anya.ID, boris.ID} {
		if len(richTo(calls, id)) != 0 {
			t.Errorf("%d got it", id)
		}
	}

	// For others /broadcast does nothing but go.
	h.as(anya)
	h.say("/broadcast")
	if c := h.tg.take(); len(c) != 1 || c[0].method != "deleteMessage" {
		t.Errorf("non-admin = %+v", c)
	}
	if d, _ := h.bot.loadDialog(h.ctx, anya.ID, anya.ID); d != nil {
		t.Error("a non-admin got a broadcast dialog")
	}
}

func containsText(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (b *Bot) mustDialogOf(t *testing.T, userID int64) *dialog {
	t.Helper()
	d, _ := b.loadDialog(context.Background(), userID, userID)
	if d == nil {
		t.Fatal("no dialog")
	}
	return d
}

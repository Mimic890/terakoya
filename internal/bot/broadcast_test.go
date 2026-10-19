package bot

import (
	"context"
	"errors"
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

func TestBroadcastOneAtATime(t *testing.T) {
	h, _ := broadcastHarness(t)
	h.bot.broadcasting.Lock() // another one is being sent
	h.say("/broadcast")
	h.say("Новость")
	h.tg.take()
	h.tap(cbBroadcastYes)
	calls := h.tg.take()
	if s := texts(calls, "sendMessage"); len(s) != 1 || s[0] != i18n.RU.BroadcastBusy {
		t.Errorf("busy = %q", s)
	}
	if len(richTo(calls, anya.ID)) != 0 {
		t.Error("sent while another broadcast runs")
	}
	if _, err := h.bot.Broadcast(h.ctx, "x", nil); !errors.Is(err, ErrBroadcastBusy) {
		t.Errorf("socket = %v", err)
	}
	// The dialog stays; once the other one is done, «Да» works.
	h.bot.broadcasting.Unlock()
	h.tap(cbBroadcastYes)
	if got := richTo(h.tg.take(), anya.ID); len(got) != 1 {
		t.Errorf("after the other one: %q", got)
	}
}

func TestBroadcastPausesAndResumes(t *testing.T) {
	h, _ := broadcastHarness(t)
	more := []int64{45, 46, 47}
	for _, id := range more {
		h.store.TouchUser(h.ctx, id, "", "Ученик")
	}
	// Shutdown begins after the second recipient.
	sent := 0
	h.bot.sleep = func(context.Context, time.Duration) {
		if sent++; sent == 2 {
			h.bot.stopNow()
		}
	}
	h.say("/broadcast")
	h.say("Новость")
	h.tap(cbBroadcastYes)
	calls := h.tg.take()
	got := map[int64]int{}
	for _, id := range append([]int64{anya.ID, boris.ID, 44}, more...) {
		got[id] = len(richTo(calls, id))
	}
	if got[anya.ID] != 1 || got[boris.ID] != 1 || got[44]+got[45]+got[46]+got[47] != 0 {
		t.Fatalf("before shutdown = %v", got)
	}
	open, _ := h.store.UnfinishedBroadcasts(h.ctx)
	if len(open) != 1 || open[0].Sent != 2 || open[0].Recipients != 6 {
		t.Fatalf("unfinished = %+v", open)
	}
	// It stopped right after Ольга was marked as being sent to, as if the
	// process died there: she is not sent to again.
	h.store.MarkRecipient(h.ctx, open[0].ID, 44, "sending")

	// The next start.
	h.bot.stopCtx, h.bot.stopNow = context.WithCancel(context.Background())
	h.bot.sleep = func(context.Context, time.Duration) {}
	h.bot.resumeBroadcasts(h.ctx)
	calls = h.tg.take()
	for _, id := range []int64{anya.ID, boris.ID, 44} {
		if n := len(richTo(calls, id)); n != 0 {
			t.Errorf("%d got a second copy", id)
		}
	}
	for _, id := range more {
		if n := len(richTo(calls, id)); n != 1 {
			t.Errorf("%d got %d", id, n)
		}
	}
	if e := to(calls, "editMessageText", ivan.ID); len(e) == 0 ||
		e[len(e)-1].params["text"] != "Рассылка завершена. Доставлено: 5, заблокировали бота: 0, не доставлено: 1." {
		t.Errorf("progress after resume = %+v", e)
	}
	if open, _ := h.store.UnfinishedBroadcasts(h.ctx); len(open) != 0 {
		t.Errorf("still unfinished = %+v", open)
	}
}

func TestDrain(t *testing.T) {
	h := newHarness(t, nil)
	release := make(chan struct{})
	finished := false
	h.bot.later(h.ctx, func(context.Context) {
		<-release
		finished = true
	})
	go func() {
		time.Sleep(20 * time.Millisecond)
		close(release)
	}()
	h.bot.drain(5 * time.Second)
	if !finished || !h.bot.stopping() {
		t.Errorf("drain returned before the work finished")
	}
	// After that, work runs right away.
	ran := false
	h.bot.later(h.ctx, func(context.Context) { ran = true })
	if !ran {
		t.Error("later after drain did not run")
	}
	// Work that does not finish is given up on after the grace period.
	h2 := newHarness(t, nil)
	h2.bot.later(h2.ctx, func(context.Context) { select {} })
	start := time.Now()
	h2.bot.drain(50 * time.Millisecond)
	if time.Since(start) > 2*time.Second {
		t.Error("drain waited too long")
	}
}

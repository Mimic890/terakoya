package bot

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestPanicsAreRecovered(t *testing.T) {
	h := newHarness(t, nil)
	var logs bytes.Buffer
	h.bot.log = slog.New(slog.NewTextHandler(&logs, nil))
	// A broken catalog makes /start panic.
	h.bot.mu.Lock()
	cat := h.bot.cat
	h.bot.cat = nil
	h.bot.mu.Unlock()
	h.say("/start")
	h.bot.later(h.ctx, func(context.Context) { panic("in the background") })
	h.bot.bg.Wait()
	for _, want := range []string{`msg="panic recovered" in=update`, `in="background work" panic="in the background"`, "stack="} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log misses %q:\n%s", want, logs.String())
		}
	}
	// The bot goes on.
	h.bot.mu.Lock()
	h.bot.cat = cat
	h.bot.mu.Unlock()
	h.tg.take()
	h.say("/start")
	if len(texts(h.tg.take(), "sendMessage")) == 0 {
		t.Error("no answer after a panic")
	}
}

func TestBlockedOnForbidden(t *testing.T) {
	h := newHarness(t, nil)
	var logs bytes.Buffer
	h.bot.log = slog.New(slog.NewTextHandler(&logs, nil))
	h.tg.fail = func(method string, p map[string]string) (int, int) {
		if p["chat_id"] == "42" {
			return 403, 0
		}
		return 0, 0
	}
	// A report outcome, a note and a menu to someone who blocked the bot.
	h.bot.send(h.ctx, 42, "Ошибку исправили", nil)
	h.bot.sendArticle(h.ctx, 42, h.bot.catalog().NoteByPath(triangles), cbBack)
	if ids, _ := h.bot.recipients(h.ctx); containsID(ids, 42) {
		t.Errorf("not marked blocked: %v", ids)
	}
	if strings.Contains(logs.String(), "level=ERROR") || !strings.Contains(logs.String(), `level=INFO msg="user has blocked the bot" user=42`) {
		t.Errorf("log:\n%s", logs.String())
	}
	// No «could not send» message after a note to them.
	calls := h.tg.take()
	if len(texts(calls, "sendMessage (failed)")) != 1 || len(texts(calls, "sendRichMessage (failed)")) == 0 {
		t.Errorf("attempts = %+v", calls)
	}
}

func containsID(ids []int64, id int64) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

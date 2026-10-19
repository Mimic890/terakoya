package bot

import (
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	l := newLimiter(3, time.Second)
	now := time.Unix(1000, 0)
	for i := 0; i < 3; i++ {
		if !l.allow(1, now) {
			t.Fatalf("burst %d refused", i)
		}
	}
	if l.allow(1, now) {
		t.Error("over the burst allowed")
	}
	if !l.allow(2, now) {
		t.Error("another user refused")
	}
	// One token a second comes back, up to the burst.
	if !l.allow(1, now.Add(time.Second)) || l.allow(1, now.Add(time.Second)) {
		t.Error("refill after a second")
	}
	later := now.Add(time.Hour)
	for i := 0; i < 3; i++ {
		if !l.allow(1, later) {
			t.Fatalf("after an hour, %d refused", i)
		}
	}
	if l.allow(1, later) {
		t.Error("refilled over the burst")
	}
	var none *limiter
	if !none.allow(1, now) {
		t.Error("nil limiter refused")
	}
}

func TestLimiterPrunes(t *testing.T) {
	l := newLimiter(2, time.Second)
	now := time.Unix(1000, 0)
	for id := int64(0); id < maxBuckets; id++ {
		l.allow(id, now)
	}
	l.allow(-1, now.Add(time.Minute))
	if len(l.users) != 1 {
		t.Errorf("%d buckets after pruning", len(l.users))
	}
}

func TestRateLimit(t *testing.T) {
	h := newHarness(t, nil)
	h.bot.limit = newLimiter(3, time.Hour)
	for i := 0; i < 3; i++ {
		h.tap(cbTop)
	}
	h.tg.take()
	h.tap(cbTop)
	calls := h.tg.take()
	if len(calls) != 1 || calls[0].method != "answerCallbackQuery" {
		t.Errorf("over the limit = %+v", calls)
	}
	h.say("/top")
	if calls := h.tg.take(); len(calls) != 0 {
		t.Errorf("message over the limit = %+v", calls)
	}
	// Admins are not limited.
	h.as(ivan)
	for i := 0; i < 5; i++ {
		h.tap(cbTop)
	}
	if n := len(texts(h.tg.take(), "sendRichMessage")); n != 5 {
		t.Errorf("admin got %d tops", n)
	}
}

package bot

import (
	"context"
	"testing"
	"time"
)

func TestCleanupLoopRunsOnStart(t *testing.T) {
	h := newHarness(t, nil)
	// A note sent eight days ago.
	h.store.SetClock(func() time.Time { return time.Now().Add(-8 * 24 * time.Hour) })
	h.store.SetMessageGroup(h.ctx, 42, []int{1, 2})
	h.store.SetClock(time.Now)
	ctx, cancel := context.WithCancel(h.ctx)
	done := make(chan struct{})
	go func() { h.bot.cleanupLoop(ctx); close(done) }()
	var group []int
	for i := 0; i < 100; i++ {
		if group, _ = h.store.MessageGroup(h.ctx, 42, 2); len(group) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	if len(group) != 1 {
		t.Errorf("old group still known: %v", group)
	}
}

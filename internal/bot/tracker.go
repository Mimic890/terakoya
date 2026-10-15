package bot

import (
	"context"
	"log/slog"

	"github.com/Mimic890/terakoya/internal/store"
)

// tracker remembers the latest messages of each private chat and which of
// them is the navigation menu, so the bot can tell whether the menu is still
// the last message and can be edited in place. The state is kept in the
// database and survives restarts. Failures are logged and treated as
// "unknown", which only means a new menu is sent instead of an edit.
type tracker struct {
	store *store.Store
	log   *slog.Logger
}

func (t *tracker) seen(ctx context.Context, chatID int64, msgID int) {
	t.check(t.store.ChatMessageSeen(ctx, chatID, msgID))
}

func (t *tracker) setMenu(ctx context.Context, chatID int64, msgID int) {
	t.check(t.store.SetChatMenu(ctx, chatID, msgID))
}

func (t *tracker) gone(ctx context.Context, chatID int64, msgID int) {
	t.check(t.store.ChatMessageGone(ctx, chatID, msgID))
}

func (t *tracker) dropMenu(ctx context.Context, chatID int64, msgID int) {
	t.check(t.store.DropChatMenu(ctx, chatID, msgID))
}

// lastMenu returns the menu message ID if it is the last message in the
// chat, or 0.
func (t *tracker) lastMenu(ctx context.Context, chatID int64) int {
	id, err := t.store.LastChatMenu(ctx, chatID)
	t.check(err)
	return id
}

func (t *tracker) check(err error) {
	if err != nil {
		t.log.Error("message tracking", "err", err)
	}
}

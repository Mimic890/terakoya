package bot

import (
	"context"
	"log/slog"
	"sync"

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

	// kinds remembers which bottom-keyboard menu each chat's menu is, so a
	// second tap on the same button can be ignored. It is kept in memory
	// only: after a restart the menu is just shown again.
	mu    sync.Mutex
	kinds map[int64]menuKind
}

type menuKind struct {
	msg  int
	kind string
}

func (t *tracker) seen(ctx context.Context, chatID int64, msgID int) {
	t.check(t.store.ChatMessageSeen(ctx, chatID, msgID))
}

func (t *tracker) setMenu(ctx context.Context, chatID int64, msgID int) {
	t.forgetKind(chatID)
	t.check(t.store.SetChatMenu(ctx, chatID, msgID, false))
}

// setRichMenu marks a rich message as the menu; it is replaced, not
// edited, when the next menu comes.
func (t *tracker) setRichMenu(ctx context.Context, chatID int64, msgID int) {
	t.forgetKind(chatID)
	t.check(t.store.SetChatMenu(ctx, chatID, msgID, true))
}

func (t *tracker) forgetKind(chatID int64) {
	t.mu.Lock()
	delete(t.kinds, chatID)
	t.mu.Unlock()
}

// setKind records that the chat's menu msgID shows the bottom-keyboard
// menu kind.
func (t *tracker) setKind(chatID int64, msgID int, kind string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.kinds == nil {
		t.kinds = map[int64]menuKind{}
	}
	t.kinds[chatID] = menuKind{msgID, kind}
}

// showing reports whether the chat's last message is the menu kind.
func (t *tracker) showing(ctx context.Context, chatID int64, kind string) bool {
	t.mu.Lock()
	k, ok := t.kinds[chatID]
	t.mu.Unlock()
	return ok && k.kind == kind && k.msg == t.lastMenu(ctx, chatID)
}

func (t *tracker) isRich(ctx context.Context, chatID int64, msgID int) bool {
	rich, err := t.store.RichMenu(ctx, chatID, msgID)
	t.check(err)
	return rich
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

// menu returns the menu message ID, last in the chat or not, or 0.
func (t *tracker) menu(ctx context.Context, chatID int64) int {
	id, err := t.store.ChatMenu(ctx, chatID)
	t.check(err)
	return id
}

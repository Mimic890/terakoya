package bot

import (
	"context"
	"time"

	"github.com/Mimic890/terakoya/internal/store"
)

// cleanupEvery is how often old rows are removed from the database.
const cleanupEvery = 24 * time.Hour

// cleanupLoop removes rows that are no longer of use, on start and then
// daily.
func (b *Bot) cleanupLoop(ctx context.Context) {
	t := time.NewTicker(cleanupEvery)
	defer t.Stop()
	for {
		func() {
			defer b.recovered("cleanup")
			c, err := b.store.Cleanup(ctx)
			switch {
			case err != nil && ctx.Err() == nil:
				b.log.Error("database cleanup", "err", err)
			case c != (store.Cleaned{}):
				b.log.Info("database cleanup", "message_parts", c.MessageParts, "sessions", c.Sessions, "note_opens", c.NoteOpens)
			}
		}()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

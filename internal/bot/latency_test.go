package bot

import (
	"testing"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/i18n"
)

// TestKeyButtonLatency times bottom-keyboard taps with every Telegram call
// taking rtt.
func TestKeyButtonLatency(t *testing.T) {
	const rtt = 100 * time.Millisecond
	for _, c := range []struct {
		name  string
		setup func(h *harness) // what is on screen before the tap
		key   string
	}{
		{"subjects, menu last", func(h *harness) { h.say(i18n.RU.KeyPinned) }, i18n.RU.KeySubjects},
		{"subjects, note below the menu", func(h *harness) {
			h.bot.sendArticle(h.ctx, 42, h.bot.catalog().NoteByPath(triangles), false)
		}, i18n.RU.KeySubjects},
		{"account over subjects", func(h *harness) {}, i18n.RU.KeyAccount},
		{"subjects over account", func(h *harness) { h.say(i18n.RU.KeyAccount) }, i18n.RU.KeySubjects},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, nil)
			h.say("/start")
			c.setup(h)
			h.tg.take()
			h.tg.delay = rtt
			u := h.user
			h.msgID++
			m := &models.Message{ID: h.msgID, From: &u, Chat: models.Chat{ID: u.ID, Type: "private"}, Text: c.key}
			start := time.Now()
			h.bot.handle(h.ctx, nil, &models.Update{Message: m})
			took := time.Since(start)
			h.bot.bg.Wait()
			h.tg.delay = 0
			// The user waits for one call: the new menu (or nothing, for
			// the menu already shown).
			if took >= 2*rtt {
				t.Errorf("%s: %v, more than one round trip", c.name, took)
			}
			t.Logf("%s: handler took %v (%.1f round trips)", c.name, took.Round(time.Millisecond), float64(took)/float64(rtt))
		})
	}
}

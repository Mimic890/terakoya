package bot

import (
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/Mimic890/terakoya/internal/i18n"
	"github.com/Mimic890/terakoya/internal/settings"
)

// foreground records the Telegram methods the update handler itself calls,
// leaving out work it starts in the background.
type foreground struct {
	mu      sync.Mutex
	methods []string
}

func (f *foreground) RoundTrip(r *http.Request) (*http.Response, error) {
	if !inBackground(r.Context()) {
		f.mu.Lock()
		f.methods = append(f.methods, r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:])
		f.mu.Unlock()
	}
	return http.DefaultTransport.RoundTrip(r)
}

// TestKeyButtonCalls checks that a tap on the bottom keyboard makes the
// user wait for one Telegram call, the new menu; deleting the tap and the
// old menu happens in the background. It counts calls, not time, so a
// slow machine cannot fail it.
func TestKeyButtonCalls(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(h *harness) // what is on screen before the tap
		key   string
		menu  string // the call that shows the new menu
	}{
		// The subject list is a rich message, sent anew.
		{"subjects over pinned", func(h *harness) { h.say(i18n.RU.KeyPinned) }, i18n.RU.KeySubjects, "sendRichMessage"},
		{"subjects, note below the menu", func(h *harness) {
			h.bot.sendArticle(h.ctx, 42, h.bot.catalog().NoteByPath(triangles), false)
		}, i18n.RU.KeySubjects, "sendRichMessage"},
		{"account over subjects", func(h *harness) {}, i18n.RU.KeyAccount, "sendRichMessage"},
		{"subjects over account", func(h *harness) { h.say(i18n.RU.KeyAccount) }, i18n.RU.KeySubjects, "sendRichMessage"},
		// A text menu replaces a rich one rather than editing it.
		{"settings over subjects", func(h *harness) {}, i18n.RU.KeySettings, "sendMessage"},
		{"settings over pinned", func(h *harness) { h.say(i18n.RU.KeyPinned) }, i18n.RU.KeySettings, "editMessageText"},
		// In the button style, the subject list is edited in place.
		{"subject buttons, menu last", func(h *harness) {
			h.bot.settings.Update(func(s *settings.Settings) { s.ListStyle = settings.ListButtons })
			h.say(i18n.RU.KeyPinned)
		}, i18n.RU.KeySubjects, "editMessageText"},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, nil)
			h.say("/start")
			c.setup(h)
			h.tg.take()
			fg := &foreground{}
			h.bot.traffic.Next = fg
			t.Cleanup(func() { h.bot.traffic.Next = nil })
			h.say(c.key)
			calls := h.tg.take()
			if len(fg.methods) != 1 || fg.methods[0] != c.menu {
				t.Errorf("the handler called %v, want only %s", fg.methods, c.menu)
			}
			// The tap itself goes, in the background.
			if !contains(ids(calls, "deleteMessage"), itoa(h.msgID)) {
				t.Errorf("tap not deleted: %+v", calls)
			}
		})
	}
}

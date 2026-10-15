package bot

import "sync"

// maxTracked bounds the message IDs remembered per chat.
const maxTracked = 32

// tracker remembers the latest messages of each private chat and which of
// them is the navigation menu, so the bot can tell whether the menu is still
// the last message and can be edited in place. It lives in memory; after a
// restart the bot simply sends a new menu.
type tracker struct {
	mu    sync.Mutex
	chats map[int64]*chatLog
}

type chatLog struct {
	menu int
	ids  []int // live message IDs in ascending order
}

func newTracker() *tracker { return &tracker{chats: map[int64]*chatLog{}} }

func (t *tracker) chat(id int64) *chatLog {
	c := t.chats[id]
	if c == nil {
		c = &chatLog{}
		t.chats[id] = c
	}
	return c
}

// seen records a message sent by the bot or the user.
func (t *tracker) seen(chatID int64, msgID int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c := t.chat(chatID)
	i := len(c.ids)
	for i > 0 && c.ids[i-1] >= msgID {
		if c.ids[i-1] == msgID {
			return
		}
		i--
	}
	c.ids = append(c.ids, 0)
	copy(c.ids[i+1:], c.ids[i:])
	c.ids[i] = msgID
	if len(c.ids) > maxTracked {
		c.ids = c.ids[len(c.ids)-maxTracked:]
	}
}

// setMenu marks a message as the current navigation menu.
func (t *tracker) setMenu(chatID int64, msgID int) {
	t.seen(chatID, msgID)
	t.mu.Lock()
	t.chat(chatID).menu = msgID
	t.mu.Unlock()
}

// gone forgets a deleted message.
func (t *tracker) gone(chatID int64, msgID int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c := t.chat(chatID)
	for i, id := range c.ids {
		if id == msgID {
			c.ids = append(c.ids[:i], c.ids[i+1:]...)
			break
		}
	}
	if c.menu == msgID {
		c.menu = 0
	}
}

// dropMenu stops treating a message as the menu while keeping it as a live
// message, e.g. after its keyboard was removed.
func (t *tracker) dropMenu(chatID int64, msgID int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if c := t.chat(chatID); c.menu == msgID {
		c.menu = 0
	}
}

// lastMenu returns the menu message ID if it is the last message in the
// chat, or 0.
func (t *tracker) lastMenu(chatID int64) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	c := t.chat(chatID)
	if c.menu == 0 || len(c.ids) == 0 || c.ids[len(c.ids)-1] != c.menu {
		return 0
	}
	return c.menu
}

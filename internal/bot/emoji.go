package bot

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode/utf16"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/settings"
)

// Custom emoji replace the plain ones where settings.yml sets them: as
// button icons (icon_custom_emoji_id), as custom_emoji entities in text
// messages and as ![…](tg://emoji?id=…) in rich messages. They need a bot
// owner with Telegram Premium. IDs Telegram does not know are not used; if
// a message with custom emoji is rejected but goes through without them,
// the bot stops using custom emoji, logs it once, and tries again only
// when the settings change.
type emojiSet struct {
	log *slog.Logger

	mu    sync.Mutex
	e     settings.Emoji
	ranks map[string]string // by rank title
	off   bool
}

func newEmojiSet(log *slog.Logger) *emojiSet {
	return &emojiSet{log: log, ranks: map[string]string{}}
}

// set puts the emoji of new settings in use, and tries custom emoji again
// if they were turned off.
func (e *emojiSet) set(s settings.Settings) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.e = s.Clone().Emoji
	e.ranks = map[string]string{}
	for _, r := range s.Ranks {
		e.ranks[r.Title] = r.Emoji
	}
	e.off = false
}

// ids lists every configured custom emoji ID.
func (e *emojiSet) ids() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
	add := func(id string) {
		if id != "" && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	for _, id := range []string{e.e.KeyboardSubjects, e.e.KeyboardPinned, e.e.KeyboardAccount, e.e.KeyboardSettings,
		e.e.Pin, e.e.Greeting} {
		add(id)
	}
	for _, id := range e.e.Top {
		add(id)
	}
	for _, id := range e.ranks {
		add(id)
	}
	for _, id := range e.e.Subjects {
		add(id)
	}
	return out
}

// check asks Telegram about the configured IDs and stops using those it
// does not know (the settings file keeps them). If Telegram cannot be
// asked, the IDs are kept.
func (e *emojiSet) check(ctx context.Context, api *tg.Bot) {
	ids := e.ids()
	if len(ids) == 0 {
		return
	}
	stickers, err := api.GetCustomEmojiStickers(ctx, &tg.GetCustomEmojiStickersParams{CustomEmojiIDs: ids})
	if err != nil {
		e.log.Warn("cannot check custom emoji", "err", err)
		return
	}
	known := map[string]bool{}
	for _, s := range stickers {
		known[s.CustomEmojiID] = true
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	drop := func(id *string, slot string) {
		if *id != "" && !known[*id] {
			e.log.Warn("unknown custom emoji; using the plain one", "slot", slot, "id", *id)
			*id = ""
		}
	}
	drop(&e.e.KeyboardSubjects, "keyboard_subjects")
	drop(&e.e.KeyboardPinned, "keyboard_pinned")
	drop(&e.e.KeyboardAccount, "keyboard_account")
	drop(&e.e.KeyboardSettings, "keyboard_settings")
	for i := range e.e.Top {
		drop(&e.e.Top[i], fmt.Sprintf("top %d", i+1))
	}
	drop(&e.e.Pin, "pin")
	drop(&e.e.Greeting, "greeting")
	for title, id := range e.ranks {
		drop(&id, "rank "+title)
		e.ranks[title] = id
	}
	for name, id := range e.e.Subjects {
		drop(&id, "subject "+name)
		e.e.Subjects[name] = id
	}
	e.log.Info("custom emoji", "configured", len(ids), "known", len(known))
}

// get returns what pick chooses from the emoji in use, or "" while
// custom emoji are off.
func (e *emojiSet) get(pick func(*emojiSet) string) string {
	if e == nil {
		return ""
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.off {
		return ""
	}
	return pick(e)
}

func (e *emojiSet) Subjects() string {
	return e.get(func(e *emojiSet) string { return e.e.KeyboardSubjects })
}
func (e *emojiSet) Pinned() string {
	return e.get(func(e *emojiSet) string { return e.e.KeyboardPinned })
}
func (e *emojiSet) Account() string {
	return e.get(func(e *emojiSet) string { return e.e.KeyboardAccount })
}
func (e *emojiSet) Settings() string {
	return e.get(func(e *emojiSet) string { return e.e.KeyboardSettings })
}
func (e *emojiSet) Pin() string      { return e.get(func(e *emojiSet) string { return e.e.Pin }) }
func (e *emojiSet) Greeting() string { return e.get(func(e *emojiSet) string { return e.e.Greeting }) }

// Subject is the emoji of a subject's button, by its name.
func (e *emojiSet) Subject(name string) string {
	return e.get(func(e *emojiSet) string { return e.e.Subjects[name] })
}

// Medal is the emoji of a place of the top.
func (e *emojiSet) Medal(place int) string {
	return e.get(func(e *emojiSet) string { return e.e.Medal(place) })
}

// Rank is the emoji of a rank, by its title.
func (e *emojiSet) Rank(title string) string {
	return e.get(func(e *emojiSet) string { return e.ranks[title] })
}

// rejected turns custom emoji off after Telegram refused a message with
// them that it accepted without.
func (e *emojiSet) rejected(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.off {
		e.off = true
		e.log.Warn("Telegram rejected custom emoji; using plain emoji until restart", "err", err)
	}
}

// withPlain sends with send, and if Telegram refuses a keyboard with
// custom emoji icons, once more without them.
func (b *Bot) withPlain(kb *models.InlineKeyboardMarkup, send func(*models.InlineKeyboardMarkup) error) error {
	err := send(kb)
	if err == nil {
		return nil
	}
	plain, ok := b.plainInline(kb)
	if !ok {
		return err
	}
	if err2 := send(plain); err2 != nil {
		return err2
	}
	b.emoji.rejected(err)
	return nil
}

// Plain versions, for the retry without custom emoji: buttons just lose
// their icons; there is no plain emoji to put back.

func (b *Bot) plainInline(kb *models.InlineKeyboardMarkup) (*models.InlineKeyboardMarkup, bool) {
	if kb == nil {
		return nil, false
	}
	changed := false
	out := &models.InlineKeyboardMarkup{InlineKeyboard: make([][]models.InlineKeyboardButton, len(kb.InlineKeyboard))}
	for i, r := range kb.InlineKeyboard {
		out.InlineKeyboard[i] = append([]models.InlineKeyboardButton{}, r...)
		for j, btn := range r {
			if btn.IconCustomEmojiID != "" {
				out.InlineKeyboard[i][j].IconCustomEmojiID = ""
				changed = true
			}
		}
	}
	return out, changed
}

var richEmoji = regexp.MustCompile(`!\[[^\]]*\]\(tg://emoji\?id=\d+\) ?`)

// plainMarkdown drops the custom emoji of a rich message.
func plainMarkdown(md string) (string, bool) {
	out := richEmoji.ReplaceAllString(md, "")
	return out, out != md
}

// richEmojiRef is a custom emoji in a rich message; alt is shown where
// custom emoji are not supported.
func richEmojiRef(id, alt string) string {
	if id == "" {
		return ""
	}
	return "![" + alt + "](tg://emoji?id=" + id + ") "
}

// emojiText is a text message with custom emoji: plain strings and emoji
// to put between them.
type emojiText []any

// emojiAt is a custom emoji in an emojiText; alt is the character it
// covers.
//
// The spacing around it (before, after) is written only with the emoji,
// so text without one has no stray spaces.
type emojiAt struct{ id, alt, before, after string }

// build returns the text with custom_emoji entities, or with plain set,
// the text without the emoji.
func (t emojiText) build(plain bool) (string, []models.MessageEntity) {
	var sb strings.Builder
	var ents []models.MessageEntity
	n := 0 // UTF-16 length so far
	write := func(s string) {
		sb.WriteString(s)
		n += len(utf16.Encode([]rune(s)))
	}
	for _, p := range t {
		switch v := p.(type) {
		case string:
			write(v)
		case emojiAt:
			if v.id == "" || plain {
				continue
			}
			write(v.before)
			l := len(utf16.Encode([]rune(v.alt)))
			ents = append(ents, models.MessageEntity{Type: models.MessageEntityTypeCustomEmoji, Offset: n, Length: l, CustomEmojiID: v.id})
			write(v.alt + v.after)
		}
	}
	return sb.String(), ents
}

// sendEmojiText sends a text message with custom emoji, falling back to
// the plain text.
func (b *Bot) sendEmojiText(ctx context.Context, chatID int64, t emojiText, kb models.ReplyMarkup) *models.Message {
	text, ents := t.build(false)
	p := &tg.SendMessageParams{ChatID: chatID, Text: text, Entities: ents, ReplyMarkup: kb}
	msg, err := b.api.SendMessage(ctx, p)
	if err != nil && (len(ents) > 0 || hasIcons(kb)) {
		first := err
		p.Text, p.Entities = t.build(true)
		p.ReplyMarkup = b.plainMarkup(kb)
		if msg, err = b.api.SendMessage(ctx, p); err == nil {
			b.emoji.rejected(fmt.Errorf("sendMessage: %w", first))
		}
	}
	if err != nil {
		b.log.Error("send message", "chat", chatID, "err", err)
		return nil
	}
	b.msgs.seen(ctx, chatID, msg.ID)
	return msg
}

func hasIcons(kb models.ReplyMarkup) bool {
	switch k := kb.(type) {
	case *models.InlineKeyboardMarkup:
		for _, r := range k.InlineKeyboard {
			for _, btn := range r {
				if btn.IconCustomEmojiID != "" {
					return true
				}
			}
		}
	case *models.ReplyKeyboardMarkup:
		for _, r := range k.Keyboard {
			for _, btn := range r {
				if btn.IconCustomEmojiID != "" {
					return true
				}
			}
		}
	}
	return false
}

// plainMarkup is a keyboard without icons.
func (b *Bot) plainMarkup(kb models.ReplyMarkup) models.ReplyMarkup {
	switch k := kb.(type) {
	case *models.InlineKeyboardMarkup:
		out, _ := b.plainInline(k)
		return out
	case *models.ReplyKeyboardMarkup:
		out := *k
		out.Keyboard = make([][]models.KeyboardButton, len(k.Keyboard))
		for i, r := range k.Keyboard {
			out.Keyboard[i] = append([]models.KeyboardButton{}, r...)
			for j, btn := range r {
				if btn.IconCustomEmojiID != "" {
					out.Keyboard[i][j].IconCustomEmojiID = ""
				}
			}
		}
		return &out
	}
	return kb
}

// onEmojiIDs answers an admin's message with custom emoji with their IDs,
// to fill in the TERAKOYA_EMOJI_* settings. It reports false for other
// messages.
func (b *Bot) onEmojiIDs(ctx context.Context, m *models.Message) bool {
	if !b.cfg.IsAdmin(m.From.ID) {
		return false
	}
	text, ents := m.Text, m.Entities
	if text == "" {
		text, ents = m.Caption, m.CaptionEntities
	}
	u := utf16.Encode([]rune(text))
	var reply emojiText
	var codes []int // indexes of the IDs in reply, to format them as code
	seen := map[string]bool{}
	for _, e := range ents {
		if e.Type != models.MessageEntityTypeCustomEmoji || seen[e.CustomEmojiID] || e.Offset+e.Length > len(u) {
			continue
		}
		seen[e.CustomEmojiID] = true
		alt := string(utf16.Decode(u[e.Offset : e.Offset+e.Length]))
		if len(reply) > 0 {
			reply = append(reply, "\n")
		}
		codes = append(codes, len(reply)+1)
		reply = append(reply, emojiAt{id: e.CustomEmojiID, alt: alt, after: " "}, e.CustomEmojiID)
	}
	if len(reply) == 0 {
		return false
	}
	out, entities := reply.build(false)
	// The IDs as code, so a tap copies them.
	n := 0
	for i, p := range reply {
		switch v := p.(type) {
		case string:
			l := len(utf16.Encode([]rune(v)))
			if slices.Contains(codes, i) {
				entities = append(entities, models.MessageEntity{Type: models.MessageEntityTypeCode, Offset: n, Length: l})
			}
			n += l
		case emojiAt:
			n += len(utf16.Encode([]rune(v.before + v.alt + v.after)))
		}
	}
	msg, err := b.api.SendMessage(ctx, &tg.SendMessageParams{ChatID: m.Chat.ID, Text: out, Entities: entities})
	if err != nil {
		b.log.Error("send emoji IDs", "err", err)
		return true
	}
	b.msgs.seen(ctx, m.Chat.ID, msg.ID)
	return true
}

package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/settings"
	"github.com/Mimic890/terakoya/internal/store"
)

// /emoji_setup walks an admin through every place a custom emoji can go,
// or through one with /emoji_setup <slot>. Answers are kept in the dialog
// session and written to settings.yml at the end; «Отмена» changes
// nothing.

const (
	stateEmojiWait    = "emoji_wait"    // waiting for a custom emoji
	stateEmojiConfirm = "emoji_confirm" // «Сохранить?»

	cbEmoji        = "ew:"
	cbEmojiSkip    = "ew:s"
	cbEmojiClear   = "ew:c"
	cbEmojiCancel  = "ew:x"
	cbEmojiYes     = "ew:y"
	cbEmojiAnother = "ew:o"
)

// emojiSlot is a place for a custom emoji.
type emojiSlot struct {
	key, short, where, hint string
}

// emojiSlots lists the places in wizard order: the bottom keyboard, the
// pin button, the greeting, each subject of the vault, each rank, and the
// medals of the top up to top.medals.
func (b *Bot) emojiSlots(s settings.Settings) []emojiSlot {
	return b.slotsUpTo(s, s.Top.Medals)
}

// allSlots are the wizard's places plus every medal, which /emoji_setup
// top:<n> sets even beyond top.medals.
func (b *Bot) allSlots(s settings.Settings) []emojiSlot {
	return b.slotsUpTo(s, settings.MaxMedals)
}

func (b *Bot) slotsUpTo(s settings.Settings, medals int) []emojiSlot {
	t := b.t
	out := []emojiSlot{
		{"keyboard_subjects", t.EmojiShortSubjects, t.EmojiWhereSubjects, ""},
		{"keyboard_pinned", t.EmojiShortPinned, t.EmojiWherePinned, ""},
		{"keyboard_account", t.EmojiShortAccount, t.EmojiWhereAccount, ""},
		{"keyboard_settings", t.EmojiShortSettings, t.EmojiWhereSettings, ""},
		{"pin", t.EmojiShortPin, t.EmojiWherePin, ""},
		{"greeting", t.EmojiShortGreeting, t.EmojiWhereGreeting, t.EmojiHintGreeting},
	}
	for _, subj := range b.catalog().Subjects {
		out = append(out, emojiSlot{"subject:" + subj.Name, fmt.Sprintf(t.EmojiShortSubject, subj.Name),
			fmt.Sprintf(t.EmojiWhereSubject, subj.Name), ""})
	}
	for _, r := range s.Ranks {
		out = append(out, emojiSlot{"rank:" + r.Title, fmt.Sprintf(t.EmojiShortRank, r.Title),
			fmt.Sprintf(t.EmojiWhereRank, r.Title, r.From, plural(r.From, t.AcceptedFrom)), t.EmojiHintRank})
	}
	for n := 1; n <= medals; n++ {
		out = append(out, emojiSlot{fmt.Sprintf("top:%d", n), fmt.Sprintf(t.EmojiShortMedal, n),
			fmt.Sprintf(t.EmojiWhereMedal, n), fmt.Sprintf(t.EmojiHintMedal, n)})
	}
	return out
}

func (b *Bot) emojiSlot(s settings.Settings, key string) (emojiSlot, bool) {
	for _, slot := range b.allSlots(s) {
		if strings.EqualFold(slot.key, key) {
			return slot, true
		}
	}
	return emojiSlot{}, false
}

// slotID reads a slot's emoji from settings.
func slotID(s settings.Settings, key string) string {
	switch {
	case key == "keyboard_subjects":
		return s.Emoji.KeyboardSubjects
	case key == "keyboard_pinned":
		return s.Emoji.KeyboardPinned
	case key == "keyboard_account":
		return s.Emoji.KeyboardAccount
	case key == "keyboard_settings":
		return s.Emoji.KeyboardSettings
	case key == "pin":
		return s.Emoji.Pin
	case key == "greeting":
		return s.Emoji.Greeting
	case strings.HasPrefix(key, "subject:"):
		return s.Emoji.Subjects[strings.TrimPrefix(key, "subject:")]
	case strings.HasPrefix(key, "top:"):
		return s.Emoji.Medal(medalPlace(key))
	case strings.HasPrefix(key, "rank:"):
		for _, r := range s.Ranks {
			if r.Title == strings.TrimPrefix(key, "rank:") {
				return r.Emoji
			}
		}
	}
	return ""
}

// setSlot writes a slot's emoji into settings.
func setSlot(s *settings.Settings, key, id string) {
	switch {
	case key == "keyboard_subjects":
		s.Emoji.KeyboardSubjects = id
	case key == "keyboard_pinned":
		s.Emoji.KeyboardPinned = id
	case key == "keyboard_account":
		s.Emoji.KeyboardAccount = id
	case key == "keyboard_settings":
		s.Emoji.KeyboardSettings = id
	case key == "pin":
		s.Emoji.Pin = id
	case key == "greeting":
		s.Emoji.Greeting = id
	case strings.HasPrefix(key, "subject:"):
		s.Emoji.Subjects[strings.TrimPrefix(key, "subject:")] = id
	case strings.HasPrefix(key, "top:"):
		if n := medalPlace(key); n >= 1 && n <= settings.MaxMedals {
			s.Emoji.SetMedal(n, id)
		}
	case strings.HasPrefix(key, "rank:"):
		for i := range s.Ranks {
			if s.Ranks[i].Title == strings.TrimPrefix(key, "rank:") {
				s.Ranks[i].Emoji = id
			}
		}
	}
}

// medalPlace is the place of a top:<n> slot.
func medalPlace(key string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(key, "top:"))
	return n
}

// emojiWizard is the session payload of /emoji_setup.
type emojiWizard struct {
	Slots  []string          `json:"slots"`
	I      int               `json:"i"`
	Values map[string]string `json:"values"` // the answers; "" removes the emoji
	// Pending is the emoji waiting for «Сохранить?», and Alt the character
	// it was sent as.
	Pending  string `json:"pending,omitempty"`
	Alt      string `json:"alt,omitempty"`
	PromptID int    `json:"prompt_id,omitempty"`
	// Msgs are all messages of the wizard, which «Отмена» deletes; the
	// key is the one suggestion dialogs use, so /cancel finds them too.
	Msgs []int `json:"msgs,omitempty"`
}

type wizard struct {
	ses    store.Session
	w      emojiWizard
	chatID int64
}

func (b *Bot) saveWizard(ctx context.Context, wz *wizard) {
	data, _ := json.Marshal(wz.w)
	wz.ses.Payload = string(data)
	wz.ses.ExpiresAt = time.Now().Add(sessionTTL)
	if err := b.store.PutSession(ctx, wz.ses); err != nil {
		b.log.Error("put session", "err", err)
	}
}

// loadWizard returns the admin's /emoji_setup session, or nil.
func (b *Bot) loadWizard(ctx context.Context, userID, chatID int64) *wizard {
	ses, err := b.store.Session(ctx, userID)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) && !errors.Is(err, store.ErrExpired) {
			b.log.Error("session", "err", err)
		}
		return nil
	}
	if ses.State != stateEmojiWait && ses.State != stateEmojiConfirm {
		return nil
	}
	wz := &wizard{ses: ses, chatID: chatID}
	if err := json.Unmarshal([]byte(ses.Payload), &wz.w); err != nil {
		b.log.Error("session payload", "err", err)
		b.store.DeleteSession(ctx, userID)
		return nil
	}
	return wz
}

// startEmojiSetup handles /emoji_setup [slot].
func (b *Bot) startEmojiSetup(ctx context.Context, m *models.Message, arg string) {
	s := b.settings.Get()
	var keys []string
	if arg != "" {
		slot, ok := b.emojiSlot(s, arg)
		if !ok {
			var all []string
			for _, slot := range b.allSlots(s) {
				all = append(all, slot.key)
			}
			b.send(ctx, m.Chat.ID, fmt.Sprintf(b.t.EmojiUnknownSlot, strings.Join(all, "\n")), nil)
			return
		}
		keys = []string{slot.key}
	} else {
		for _, slot := range b.emojiSlots(s) {
			keys = append(keys, slot.key)
		}
	}
	// Like any new dialog, this one replaces what was going on.
	if d, _ := b.loadDialog(ctx, m.From.ID, m.Chat.ID); d != nil {
		b.dropButtons(ctx, d)
	}
	wz := &wizard{chatID: m.Chat.ID, ses: store.Session{TgID: m.From.ID, State: stateEmojiWait},
		w: emojiWizard{Slots: keys, Values: map[string]string{}}}
	b.emojiStep(ctx, wz)
}

func (b *Bot) dropWizardButtons(ctx context.Context, wz *wizard) {
	if wz.w.PromptID != 0 {
		b.dropButtonsOf(ctx, &models.Message{ID: wz.w.PromptID, Chat: models.Chat{ID: wz.chatID}})
		wz.w.PromptID = 0
	}
}

// emojiStep asks for the current slot's emoji, or finishes after the last.
func (b *Bot) emojiStep(ctx context.Context, wz *wizard) {
	b.dropWizardButtons(ctx, wz)
	if wz.w.I >= len(wz.w.Slots) {
		b.finishEmojiSetup(ctx, wz)
		return
	}
	s := b.settings.Get()
	key := wz.w.Slots[wz.w.I]
	slot, ok := b.emojiSlot(s, key)
	if !ok { // a subject or rank that went away meanwhile
		wz.w.I++
		b.emojiStep(ctx, wz)
		return
	}
	text := emojiText{fmt.Sprintf(b.t.EmojiStep, wz.w.I+1, len(wz.w.Slots), slot.where)}
	if slot.hint != "" {
		text = append(text, "\n"+slot.hint)
	}
	text = append(text, "\n\n")
	text = append(text, b.emojiNow(s, slot)...)
	text = append(text, "\n\n"+b.t.EmojiAsk)
	kb := keyboard(row(button(b.t.EmojiSkip, cbEmojiSkip), button(b.t.EmojiClear, cbEmojiClear), button(b.t.Cancel, cbEmojiCancel)))
	wz.ses.State = stateEmojiWait
	if msg := b.sendEmojiText(ctx, wz.chatID, text, kb); msg != nil {
		wz.w.PromptID = msg.ID
		wz.w.Msgs = append(wz.w.Msgs, msg.ID)
	}
	b.saveWizard(ctx, wz)
}

// emojiNow describes a slot's current emoji.
func (b *Bot) emojiNow(s settings.Settings, slot emojiSlot) emojiText {
	if id := slotID(s, slot.key); id != "" {
		return emojiText{b.t.EmojiNow, emojiAt{id: id, alt: placeholder}}
	}
	return emojiText{b.t.EmojiNowNone}
}

// placeholder is the character a custom emoji entity covers; it shows
// only where custom emoji cannot be drawn.
const placeholder = "⭐"

// onEmojiWizardMessage takes the admin's answer to a step. It reports
// false when no /emoji_setup is going on.
func (b *Bot) onEmojiWizardMessage(ctx context.Context, m *models.Message) bool {
	if strings.HasPrefix(m.Text, "/") || !b.cfg.IsAdmin(m.From.ID) {
		return false
	}
	unlock := b.userLocks.lock(m.From.ID)
	defer unlock()
	wz := b.loadWizard(ctx, m.From.ID, m.Chat.ID)
	if wz == nil {
		return false
	}
	wz.w.Msgs = append(wz.w.Msgs, m.ID)
	id, alt := firstCustomEmoji(m)
	if id == "" {
		if msg := b.send(ctx, m.Chat.ID, b.t.EmojiNeedCustom, nil); msg != nil {
			wz.w.Msgs = append(wz.w.Msgs, msg.ID)
		}
		b.saveWizard(ctx, wz)
		return true
	}
	b.dropWizardButtons(ctx, wz)
	wz.w.Pending, wz.w.Alt = id, alt
	wz.ses.State = stateEmojiConfirm
	kb := keyboard(row(button(b.t.Yes, cbEmojiYes), button(b.t.EmojiOther, cbEmojiAnother), button(b.t.Cancel, cbEmojiCancel)))
	if msg := b.sendEmojiText(ctx, m.Chat.ID, emojiText{emojiAt{id: id, alt: alt, after: " "}, b.t.EmojiSaveQuestion}, kb); msg != nil {
		wz.w.PromptID = msg.ID
		wz.w.Msgs = append(wz.w.Msgs, msg.ID)
	}
	b.saveWizard(ctx, wz)
	return true
}

// firstCustomEmoji finds the first custom emoji of a message, in its text
// or caption, and the character it is sent as.
func firstCustomEmoji(m *models.Message) (id, alt string) {
	text, ents := m.Text, m.Entities
	if text == "" {
		text, ents = m.Caption, m.CaptionEntities
	}
	u := utf16.Encode([]rune(text))
	for _, e := range ents {
		if e.Type == models.MessageEntityTypeCustomEmoji && e.CustomEmojiID != "" && e.Offset+e.Length <= len(u) {
			return e.CustomEmojiID, string(utf16.Decode(u[e.Offset : e.Offset+e.Length]))
		}
	}
	return "", ""
}

// onEmojiWizardCallback handles the wizard's buttons.
func (b *Bot) onEmojiWizardCallback(ctx context.Context, q *models.CallbackQuery, msg *models.Message) {
	if !b.cfg.IsAdmin(q.From.ID) {
		b.answer(ctx, q, "", false)
		return
	}
	unlock := b.userLocks.lock(q.From.ID)
	defer unlock()
	wz := b.loadWizard(ctx, q.From.ID, msg.Chat.ID)
	if wz == nil || wz.w.PromptID != msg.ID {
		b.answer(ctx, q, b.t.Expired, true)
		b.dropButtonsOf(ctx, msg)
		return
	}
	b.answer(ctx, q, "", false)
	key := wz.w.Slots[wz.w.I]
	switch {
	case q.Data == cbEmojiCancel:
		// Nothing is saved, and the wizard's messages go.
		if err := b.store.DeleteSession(ctx, q.From.ID); err != nil {
			b.log.Error("delete session", "err", err)
		}
		b.dropMessages(ctx, msg.Chat.ID, wz.w.Msgs)
		return
	case q.Data == cbEmojiSkip && wz.ses.State == stateEmojiWait:
		wz.w.I++
	case q.Data == cbEmojiClear && wz.ses.State == stateEmojiWait:
		wz.w.Values[key] = ""
		wz.w.I++
	case q.Data == cbEmojiYes && wz.ses.State == stateEmojiConfirm:
		wz.w.Values[key] = wz.w.Pending
		wz.w.I++
	case q.Data == cbEmojiAnother && wz.ses.State == stateEmojiConfirm:
	default:
		return
	}
	wz.w.Pending, wz.w.Alt = "", ""
	b.emojiStep(ctx, wz)
}

// finishEmojiSetup writes the answers to settings.yml and sums up.
func (b *Bot) finishEmojiSetup(ctx context.Context, wz *wizard) {
	if err := b.store.DeleteSession(ctx, wz.ses.TgID); err != nil {
		b.log.Error("delete session", "err", err)
	}
	if len(wz.w.Values) > 0 {
		err := b.settings.Update(func(s *settings.Settings) {
			for key, id := range wz.w.Values {
				setSlot(s, key, id)
			}
		})
		if err != nil {
			b.log.Error("save settings", "err", err)
			b.send(ctx, wz.chatID, fmt.Sprintf(b.t.EmojiSaveFailed, err), nil)
			return
		}
		b.log.Info("custom emoji saved", "admin", wz.ses.TgID, "changed", len(wz.w.Values))
	}
	s := b.settings.Get()
	text := emojiText{b.t.EmojiSaved}
	for _, key := range wz.w.Slots {
		slot, ok := b.emojiSlot(s, key)
		if !ok {
			continue
		}
		text = append(text, "\n"+slot.short+": ")
		if id := slotID(s, key); id != "" {
			text = append(text, emojiAt{id: id, alt: placeholder})
		} else {
			text = append(text, b.t.EmojiNone)
		}
	}
	b.sendEmojiText(ctx, wz.chatID, text, nil)
}

// setCommands fills the command menu: the usual commands for everyone,
// and the admin ones only in the admins' chats.
func (b *Bot) setCommands(ctx context.Context) {
	common := []models.BotCommand{
		{Command: "start", Description: b.t.CmdStart},
		{Command: "search", Description: b.t.CmdSearch},
		{Command: "download", Description: b.t.CmdDownload},
		{Command: "top", Description: b.t.CmdTop},
		{Command: "ranks", Description: b.t.CmdRanks},
		{Command: "cancel", Description: b.t.CmdCancel},
	}
	if _, err := b.api.SetMyCommands(ctx, &tg.SetMyCommandsParams{Commands: common}); err != nil {
		b.log.Warn("set commands", "err", err)
	}
	admin := append(append([]models.BotCommand{}, common...),
		models.BotCommand{Command: "emoji_setup", Description: b.t.CmdEmojiSetup},
		models.BotCommand{Command: "broadcast", Description: b.t.CmdBroadcast},
		models.BotCommand{Command: "nick_reset", Description: b.t.CmdNickReset},
		models.BotCommand{Command: "admin", Description: b.t.CmdAdmin},
	)
	for _, id := range b.cfg.Admins {
		_, err := b.api.SetMyCommands(ctx, &tg.SetMyCommandsParams{Commands: admin,
			Scope: &models.BotCommandScopeChat{ChatID: id}})
		if err != nil {
			// The admin has not opened the bot yet.
			b.log.Warn("set admin commands", "admin", id, "err", err)
		}
	}
}

// onAdminCommand handles /emoji_setup, /broadcast, /nick_reset and
// /admin. It
// reports
// false for other commands; users who are not admins get no answer at
// all.
func (b *Bot) onAdminCommand(ctx context.Context, m *models.Message, cmd, arg string) bool {
	switch cmd {
	case "/emoji_setup", "/broadcast", "/nick_reset", "/admin":
	default:
		return false
	}
	if !b.cfg.IsAdmin(m.From.ID) {
		return true
	}
	switch cmd {
	case "/emoji_setup":
		b.startEmojiSetup(ctx, m, arg)
	case "/broadcast":
		b.startBroadcast(ctx, m.Chat.ID, m.From.ID)
	case "/nick_reset":
		b.nickReset(ctx, m, arg)
	case "/admin":
		b.showAdmin(ctx, m.Chat.ID)
	}
	return true
}

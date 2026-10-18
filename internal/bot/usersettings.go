package bot

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/nick"
	"github.com/Mimic890/terakoya/internal/store"
)

// «Настройки» on the bottom keyboard: the nickname and whether the user
// shows up in the top.

const (
	stateNick = "nick_wait" // waiting for a nickname

	cbSettings  = "st:"
	cbNickTG    = "st:t" // «Имя из Telegram»
	cbNickOwn   = "st:n" // «Свой ник»
	cbToggleTop = "st:h" // show or hide in the top
)

// settingsView is the settings menu with the current values.
func (b *Bot) settingsView(ctx context.Context, from *models.User) (string, *models.InlineKeyboardMarkup) {
	u, err := b.store.TouchUser(ctx, from.ID, from.Username, from.FirstName)
	if err != nil {
		b.log.Error("touch user", "err", err)
	}
	shown, toggle := b.t.Yes, b.t.TopHide
	if u.HideTop {
		shown, toggle = b.t.SettingsNo, b.t.TopShow
	}
	text := b.t.SettingsTitle + "\n\n" + fmt.Sprintf(b.t.SettingsNick, u.DisplayName()) + "\n" +
		fmt.Sprintf(b.t.SettingsTop, strings.ToLower(shown))
	return text, keyboard(
		row(button(b.t.NickFromTelegram, cbNickTG), button(b.t.NickOwn, cbNickOwn)),
		row(button(toggle, cbToggleTop)),
		row(button(b.t.Close, cbClose)),
	)
}

// onSettingsCallback handles the settings menu's buttons.
func (b *Bot) onSettingsCallback(ctx context.Context, q *models.CallbackQuery, msg *models.Message) {
	switch q.Data {
	case cbNickTG:
		if err := b.store.SetNickname(ctx, q.From.ID, "", ""); err != nil {
			b.log.Error("reset nickname", "err", err)
		}
	case cbToggleTop:
		u, err := b.store.TouchUser(ctx, q.From.ID, q.From.Username, q.From.FirstName)
		if err == nil {
			err = b.store.SetHideTop(ctx, q.From.ID, !u.HideTop)
		}
		if err != nil {
			b.log.Error("toggle top", "err", err)
		}
	case cbNickOwn:
		b.answer(ctx, q, "", false)
		b.startNick(ctx, q, msg)
		return
	}
	b.answer(ctx, q, "", false)
	text, kb := b.settingsView(ctx, &q.From)
	b.editMenu(ctx, msg.Chat.ID, msg.ID, text, kb)
	b.msgs.setKind(msg.Chat.ID, msg.ID, b.t.KeySettings)
}

// startNick asks for a nickname; the settings menu stays above.
func (b *Bot) startNick(ctx context.Context, q *models.CallbackQuery, menu *models.Message) {
	unlock := b.userLocks.lock(q.From.ID)
	defer unlock()
	if d, _ := b.loadDialog(ctx, q.From.ID, menu.Chat.ID); d != nil {
		b.dropButtons(ctx, d)
	}
	d := &dialog{chatID: menu.Chat.ID, ses: store.Session{TgID: q.From.ID, State: stateNick},
		p: sessionPayload{OriginMsg: menu.ID}}
	b.prompt(ctx, d, b.t.NickAsk, keyboard(cancelRow(b.t.Cancel)))
}

// onNickMessage checks a nickname. A refused one gets the reason and the
// question again; an accepted one ends the dialog, its messages go, and
// the settings menu shows the new name.
func (b *Bot) onNickMessage(ctx context.Context, d *dialog, m *models.Message) {
	name := m.Text
	if name == "" {
		b.prompt(ctx, d, b.t.NickChars+" "+b.t.NickAgain, keyboard(cancelRow(b.t.Cancel)))
		return
	}
	if reason := b.nickProblem(ctx, m.From.ID, name); reason != "" {
		b.prompt(ctx, d, reason+" "+b.t.NickAgain, keyboard(cancelRow(b.t.Cancel)))
		return
	}
	err := b.store.SetNickname(ctx, m.From.ID, name, nick.Key(name))
	if errors.Is(err, store.ErrNicknameTaken) {
		b.prompt(ctx, d, b.t.NickTaken+" "+b.t.NickAgain, keyboard(cancelRow(b.t.Cancel)))
		return
	}
	if err != nil {
		b.log.Error("set nickname", "err", err)
		b.prompt(ctx, d, b.t.SubmitFailed, keyboard(cancelRow(b.t.Cancel)))
		return
	}
	b.log.Info("nickname set", "user", m.From.ID)
	b.endDialog(ctx, d)
	b.dropMessages(ctx, d.chatID, d.p.Msgs)
	text, kb := b.settingsView(ctx, m.From)
	if d.p.OriginMsg != 0 {
		b.editMenu(ctx, d.chatID, d.p.OriginMsg, text, kb)
	} else {
		b.replaceMenu(ctx, d.chatID, text, kb)
	}
	if id := b.msgs.menu(ctx, d.chatID); id != 0 {
		b.msgs.setKind(d.chatID, id, b.t.KeySettings)
	}
}

// nickProblem says why a user may not take a nickname, or "".
func (b *Bot) nickProblem(ctx context.Context, userID int64, name string) string {
	// Nobody may look like the bot or an admin, except an admin like
	// themselves.
	reserved := []string{b.cfg.BotUsername}
	for _, id := range b.cfg.Admins {
		if id == userID {
			continue
		}
		if a, err := b.store.User(ctx, id); err == nil {
			reserved = append(reserved, a.FirstName, a.Username)
		}
	}
	switch nick.Check(name, reserved) {
	case nick.Length:
		return fmt.Sprintf(b.t.NickLength, nick.MinLen, nick.MaxLen)
	case nick.Chars:
		return b.t.NickChars
	case nick.Spaces:
		return b.t.NickSpaces
	case nick.Banned:
		return b.t.NickBanned
	case nick.Reserved:
		return b.t.NickReserved
	}
	taken, err := b.store.NicknameTaken(ctx, userID, nick.Key(name))
	if err != nil {
		b.log.Error("nickname taken", "err", err)
	}
	if taken {
		return b.t.NickTaken
	}
	return ""
}

// nickReset handles /nick_reset <tg id or @username>.
func (b *Bot) nickReset(ctx context.Context, m *models.Message, arg string) {
	if arg == "" {
		b.send(ctx, m.Chat.ID, b.t.NickResetUsage, nil)
		return
	}
	var u store.User
	var err error
	if id, perr := strconv.ParseInt(arg, 10, 64); perr == nil {
		u, err = b.store.User(ctx, id)
	} else {
		u, err = b.store.UserByUsername(ctx, arg)
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		b.send(ctx, m.Chat.ID, b.t.NickResetUnknown, nil)
		return
	case err != nil:
		b.log.Error("find user", "err", err)
		b.send(ctx, m.Chat.ID, b.t.SubmitFailed, nil)
		return
	}
	if u.Nickname == "" {
		b.send(ctx, m.Chat.ID, fmt.Sprintf(b.t.NickResetNone, u.DisplayName()), nil)
		return
	}
	if err := b.store.SetNickname(ctx, u.TgID, "", ""); err != nil {
		b.log.Error("reset nickname", "err", err)
		b.send(ctx, m.Chat.ID, b.t.SubmitFailed, nil)
		return
	}
	b.log.Info("nickname reset", "user", u.TgID, "admin", m.From.ID)
	old := u.Nickname
	u.Nickname = ""
	b.send(ctx, m.Chat.ID, fmt.Sprintf(b.t.NickResetDone, old, u.DisplayName()), nil)
	b.send(ctx, u.TgID, b.t.NickResetNotice, nil)
}

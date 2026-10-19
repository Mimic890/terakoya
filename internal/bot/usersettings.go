package bot

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/nick"
	"github.com/Mimic890/terakoya/internal/store"
)

// «Настройки» on the bottom keyboard has two screens: the main one, with
// the nickname and whether the user shows up in the top, and «Ник», which
// chooses between the Telegram name and an own nickname. Both are one
// menu message, edited in place.

const (
	stateNick = "nick_wait" // waiting for a nickname

	cbSettings     = "st:"
	cbSettingsMain = "st:m" // back to the main screen
	cbNickMenu     = "st:k" // the «Ник» screen
	cbNickTG       = "st:t" // «Из Telegram»
	cbNickOwn      = "st:n" // «Свой ник»
	cbToggleTop    = "st:h" // show or hide in the top

	// kindNick tells the «Ник» screen from the main one, so that the
	// «Настройки» key brings the main one back.
	kindNick = "settings:nick"
)

// check marks the active choice on a button.
const check = " ✓"

// settingsView is the main screen.
func (b *Bot) settingsView(u store.User) (string, *models.InlineKeyboardMarkup) {
	shown, toggle := b.t.SettingsYes, b.t.TopShown+check
	if u.HideTop {
		shown, toggle = b.t.SettingsNo, b.t.TopHidden
	}
	text := b.t.SettingsTitle + "\n\n" + fmt.Sprintf(b.t.SettingsNick, u.AddressName()) + "\n" +
		fmt.Sprintf(b.t.SettingsTop, shown)
	return text, keyboard(
		row(button(b.t.NickButton, cbNickMenu)),
		row(button(toggle, cbToggleTop)),
		row(button(b.t.Close, cbClose)),
	)
}

// nickView is the «Ник» screen.
func (b *Bot) nickView(u store.User) (string, *models.InlineKeyboardMarkup) {
	now, tg, own := fmt.Sprintf(b.t.NickNowTelegram, u.AddressName()), b.t.NickFromTelegram+check, b.t.NickOwn
	if u.Nickname != "" {
		now, tg, own = fmt.Sprintf(b.t.NickNowOwn, u.Nickname), b.t.NickFromTelegram, b.t.NickOwn+check
	}
	text := b.t.NickTitle + "\n\n" + now + "\n" + b.t.NickWhere
	return text, keyboard(
		row(button(tg, cbNickTG), button(own, cbNickOwn)),
		row(button(b.t.Back, cbSettingsMain)),
	)
}

// user records the user and returns their row.
func (b *Bot) user(ctx context.Context, from *models.User) store.User {
	u, err := b.store.TouchUser(ctx, from.ID, from.Username, from.FirstName)
	if err != nil {
		b.log.Error("touch user", "err", err)
		return store.User{TgID: from.ID, Username: from.Username, FirstName: from.FirstName}
	}
	return u
}

// showSettings shows the main screen in place of the current menu.
func (b *Bot) showSettings(ctx context.Context, chatID int64, from *models.User) {
	text, kb := b.settingsView(b.user(ctx, from))
	b.replaceMenu(ctx, chatID, text, kb)
}

// onSettingsCallback handles the buttons of both screens.
func (b *Bot) onSettingsCallback(ctx context.Context, q *models.CallbackQuery, msg *models.Message) {
	u := b.user(ctx, &q.From)
	nickScreen := false
	switch q.Data {
	case cbToggleTop:
		if err := b.store.SetHideTop(ctx, u.TgID, !u.HideTop); err != nil {
			b.log.Error("toggle top", "err", err)
		}
	case cbNickMenu:
		nickScreen = true
	case cbNickTG:
		nickScreen = true
		if u.Nickname == "" {
			b.answer(ctx, q, "", false) // already the Telegram name
			return
		}
		if err := b.store.SetNickname(ctx, u.TgID, "", ""); err != nil {
			b.log.Error("reset nickname", "err", err)
		}
	case cbNickOwn:
		// Also when it is the active choice: the user types a new one.
		b.answer(ctx, q, "", false)
		b.startNick(ctx, q, msg, u)
		return
	}
	b.answer(ctx, q, "", false)
	b.showSettingsScreen(ctx, msg.Chat.ID, msg.ID, u.TgID, nickScreen)
}

// showSettingsScreen edits a settings menu into one of its screens, with
// the user's current values.
func (b *Bot) showSettingsScreen(ctx context.Context, chatID int64, msgID int, userID int64, nickScreen bool) {
	u, err := b.store.User(ctx, userID)
	if err != nil {
		b.log.Error("load user", "err", err)
	}
	text, kb, kind := "", (*models.InlineKeyboardMarkup)(nil), b.t.KeySettings
	if nickScreen {
		text, kb = b.nickView(u)
		kind = kindNick
	} else {
		text, kb = b.settingsView(u)
	}
	if msgID != 0 {
		b.editMenu(ctx, chatID, msgID, text, kb)
	} else {
		b.replaceMenu(ctx, chatID, text, kb)
	}
	if id := b.msgs.menu(ctx, chatID); id != 0 {
		b.msgs.setKind(chatID, id, kind)
	}
}

// startNick asks for a nickname, naming the current one if there is one;
// the «Ник» screen stays above.
func (b *Bot) startNick(ctx context.Context, q *models.CallbackQuery, menu *models.Message, u store.User) {
	unlock := b.userLocks.lock(q.From.ID)
	defer unlock()
	if d, _ := b.loadDialog(ctx, q.From.ID, menu.Chat.ID); d != nil {
		b.dropButtons(ctx, d)
	}
	d := &dialog{chatID: menu.Chat.ID, ses: store.Session{TgID: q.From.ID, State: stateNick},
		p: sessionPayload{OriginMsg: menu.ID}}
	ask := b.t.NickAsk
	if u.Nickname != "" {
		ask = fmt.Sprintf(b.t.NickAskNew, u.Nickname)
	}
	b.prompt(ctx, d, ask, keyboard(cancelRow(b.t.Cancel)))
}

// onNickMessage checks a nickname. A refused one gets the reason and the
// question again; an accepted one ends the dialog, its messages go, and
// the «Ник» screen shows the new name.
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
	b.showSettingsScreen(ctx, d.chatID, d.p.OriginMsg, m.From.ID, true)
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
	old, u, err := b.ResetNickname(ctx, arg, m.From.ID)
	switch {
	case errors.Is(err, ErrNoUser):
		b.send(ctx, m.Chat.ID, b.t.NickResetUnknown, nil)
	case errors.Is(err, ErrNoNickname):
		found, _ := b.AdminUser(ctx, arg)
		b.send(ctx, m.Chat.ID, fmt.Sprintf(b.t.NickResetNone, found.Name), nil)
	case err != nil:
		b.log.Error("reset nickname", "err", err)
		b.send(ctx, m.Chat.ID, b.t.SubmitFailed, nil)
	default:
		b.send(ctx, m.Chat.ID, fmt.Sprintf(b.t.NickResetDone, old, u.Name), nil)
	}
}

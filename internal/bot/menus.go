package bot

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/content"
	"github.com/Mimic890/terakoya/internal/store"
)

// The keyboard at the bottom of the chat opens the subject list, the pinned
// notes and the account; pins are toggled under every note.

// maxPins is the most notes one user can pin.
const maxPins = 30

const (
	cbPin       = "p:"  // p:<note id>, toggles the pin
	cbFromPins  = "pa:" // pa:<note id>, opens a note from the pinned list
	cbBackPins  = "bp:" // bp:<note id>, «Назад» of a note opened from the list
	progressLen = 7
)

func (b *Bot) replyKeyboard() *models.ReplyKeyboardMarkup {
	return &models.ReplyKeyboardMarkup{
		Keyboard: [][]models.KeyboardButton{
			{{Text: b.t.KeySubjects}, {Text: b.t.KeyPinned}},
			{{Text: b.t.KeyAccount}},
		},
		IsPersistent:   true,
		ResizeKeyboard: true,
	}
}

// greet sends the greeting, which carries the bottom keyboard: a message
// cannot have both that keyboard and inline buttons.
func (b *Bot) greet(ctx context.Context, chatID int64, from *models.User) {
	msg, err := b.api.SendMessage(ctx, &tg.SendMessageParams{
		ChatID: chatID, Text: fmt.Sprintf(b.t.Greeting, from.FirstName), ReplyMarkup: b.replyKeyboard(),
	})
	if err != nil {
		b.log.Error("send greeting", "err", err)
		return
	}
	b.msgs.seen(ctx, chatID, msg.ID)
}

// onKeyButton handles a tap on the bottom keyboard, which arrives as the
// button's text. It reports false for any other message.
func (b *Bot) onKeyButton(ctx context.Context, m *models.Message) bool {
	var text string
	var kb *models.InlineKeyboardMarkup
	switch m.Text {
	case b.t.KeySubjects:
		text, kb = b.homeView(ctx, m.From)
	case b.t.KeyPinned:
		text, kb = b.pinsView(ctx, m.From.ID)
	case b.t.KeyAccount:
		text, kb = b.accountView(ctx, m.From)
	default:
		return false
	}
	// The tap would otherwise stay in the chat as the user's message.
	b.deleteMessage(ctx, m.Chat.ID, m.ID)
	b.replaceMenu(ctx, m.Chat.ID, text, kb)
	return true
}

// replaceMenu shows a menu: in place when the current menu is the last
// message, and otherwise as a new one, removing the old menu.
func (b *Bot) replaceMenu(ctx context.Context, chatID int64, text string, kb *models.InlineKeyboardMarkup) {
	if id := b.msgs.lastMenu(ctx, chatID); id != 0 {
		b.editMenu(ctx, chatID, id, text, kb)
		return
	}
	if id := b.msgs.menu(ctx, chatID); id != 0 {
		b.retire(ctx, &models.Message{ID: id, Chat: models.Chat{ID: chatID}})
	}
	b.sendMenu(ctx, chatID, text, kb)
}

// pinsView lists the user's pinned notes, newest first. Pins of notes no
// longer in the vault are dropped.
func (b *Bot) pinsView(ctx context.Context, tgID int64) (string, *models.InlineKeyboardMarkup) {
	ids, err := b.store.Pins(ctx, tgID)
	if err != nil {
		b.log.Error("pins", "err", err)
	}
	cat := b.catalog()
	var rows [][]models.InlineKeyboardButton
	for _, id := range ids {
		n := cat.Note(id)
		if n == nil {
			if err := b.store.Unpin(ctx, tgID, id); err != nil {
				b.log.Error("unpin", "err", err)
			}
			continue
		}
		rows = append(rows, row(button(n.Title, cbFromPins+n.ID)))
	}
	text := b.t.PinnedTitle
	if len(rows) == 0 {
		text += "\n\n" + b.t.PinnedEmpty
	}
	return text, keyboard(append(rows, row(button(b.t.Close, cbClose)))...)
}

// togglePin pins or unpins a note and flips the button in place.
func (b *Bot) togglePin(ctx context.Context, q *models.CallbackQuery, msg *models.Message, n *content.Note) {
	pinned, err := b.store.Pinned(ctx, q.From.ID, n.ID)
	if err != nil {
		b.log.Error("pinned", "err", err)
		b.answer(ctx, q, b.t.SubmitFailed, true)
		return
	}
	toast := b.t.UnpinDone
	if pinned {
		err = b.store.Unpin(ctx, q.From.ID, n.ID)
	} else {
		toast = b.t.PinDone
		err = b.store.Pin(ctx, q.From.ID, n.ID, maxPins)
	}
	if errors.Is(err, store.ErrPinLimit) {
		b.answer(ctx, q, fmt.Sprintf(b.t.PinLimit, maxPins), true)
		return
	}
	if err != nil {
		b.log.Error("toggle pin", "err", err)
		b.answer(ctx, q, b.t.SubmitFailed, true)
		return
	}
	b.answer(ctx, q, toast, false)
	fromPins := false
	if msg.ReplyMarkup != nil {
		for _, d := range callbacks(msg.ReplyMarkup) {
			fromPins = fromPins || strings.HasPrefix(d, cbBackPins)
		}
	}
	_, err = b.api.EditMessageReplyMarkup(ctx, &tg.EditMessageReplyMarkupParams{
		ChatID: msg.Chat.ID, MessageID: msg.ID, ReplyMarkup: articleKeyboard(b.t, n, !pinned, fromPins),
	})
	if err != nil {
		b.log.Debug("flip pin button", "err", err)
	}
}

// accountView shows the user's rank, progress and edit access.
func (b *Bot) accountView(ctx context.Context, from *models.User) (string, *models.InlineKeyboardMarkup) {
	u, err := b.store.TouchUser(ctx, from.ID, from.Username, from.FirstName)
	if err != nil {
		b.log.Error("touch user", "err", err)
	}
	open, _, err := b.store.SuggestQuota(ctx, from.ID)
	if err != nil {
		b.log.Error("suggest quota", "err", err)
	}
	merged := u.MergedCount
	rank := b.cfg.Ladder.For(merged)
	lines := []string{
		fmt.Sprintf(b.t.AccountTitle, from.FirstName),
		"",
		fmt.Sprintf(b.t.AccountRank, rank.Title),
		fmt.Sprintf(b.t.AccountAccepted, merged),
	}
	if next, ok := b.cfg.Ladder.Next(rank); ok {
		lines = append(lines, fmt.Sprintf(b.t.AccountNext, next.Title, next.From-merged,
			progress(merged-rank.From, next.From-rank.From)))
	} else {
		lines = append(lines, b.t.AccountTop)
	}
	lines = append(lines, fmt.Sprintf(b.t.AccountOpen, open))

	var rows [][]models.InlineKeyboardButton
	state := b.t.AccessStateNone
	switch a, err := b.store.Access(ctx, from.ID); {
	case b.cfg.IsAdmin(from.ID):
		state = b.t.AccessStateAdmin
	case err == nil && a.Status == store.AccessGranted:
		state = b.t.AccessStateOn
	case err == nil && a.Status == store.AccessPending:
		state = b.t.AccessStatePending
	default:
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			b.log.Error("access", "err", err)
		}
		if _, until := b.accessBlock(ctx, from.ID); !until.IsZero() {
			state = fmt.Sprintf(b.t.AccessStateDenied, b.date(until))
		} else if len(b.cfg.Admins) > 0 {
			rows = append(rows, row(button(b.t.RequestAccess, cbRequestAcc)))
		}
	}
	lines = append(lines, fmt.Sprintf(b.t.AccountAccess, state))
	return strings.Join(lines, "\n"), keyboard(append(rows, row(button(b.t.Close, cbClose)))...)
}

// progress draws done out of total as a bar of progressLen cells.
func progress(done, total int) string {
	filled := 0
	if total > 0 {
		filled = done * progressLen / total
	}
	filled = max(0, min(filled, progressLen))
	return strings.Repeat("▰", filled) + strings.Repeat("▱", progressLen-filled)
}

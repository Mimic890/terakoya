package bot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/content"
	"github.com/Mimic890/terakoya/internal/render"
	"github.com/Mimic890/terakoya/internal/store"
	"github.com/Mimic890/terakoya/internal/suggest"
)

// The keyboard at the bottom of the chat opens the subject list, the pinned
// notes and the account; pins are toggled under every note.

// maxPins is the most notes one user can pin.
const maxPins = 30

const (
	cbPin      = "p:"  // p:<note id>, toggles the pin
	cbFromPins = "pa:" // pa:<note id>, opens a note from the pinned list
	cbBackPins = "bp:" // bp:<note id>, «Назад» of a note opened from the list
	cbHistory  = "hl"  // «Мои сообщения»
	cbAccount  = "ac"  // back to «Аккаунт»
	cbClose    = "c"   // removes the message it is pressed in
)

func (b *Bot) replyKeyboard() *models.ReplyKeyboardMarkup {
	key := func(icon, label string) models.KeyboardButton {
		return models.KeyboardButton{Text: label, IconCustomEmojiID: icon}
	}
	return &models.ReplyKeyboardMarkup{
		Keyboard: [][]models.KeyboardButton{
			{key(b.emoji.Subjects(), b.t.KeySubjects), key(b.emoji.Pinned(), b.t.KeyPinned)},
			{key(b.emoji.Account(), b.t.KeyAccount), key(b.emoji.Settings(), b.t.KeySettings)},
		},
		IsPersistent:   true,
		ResizeKeyboard: true,
	}
}

// addressName is how the bot addresses a user: the nickname, else the
// Telegram first name, never the @username. The user is recorded on the
// way, so their latest Telegram name counts.
func (b *Bot) addressName(ctx context.Context, from *models.User) string {
	u, err := b.store.TouchUser(ctx, from.ID, from.Username, from.FirstName)
	if err != nil {
		b.log.Error("touch user", "err", err)
		return store.User{Username: from.Username, FirstName: from.FirstName}.AddressName()
	}
	return u.AddressName()
}

// greet sends the greeting, which carries the bottom keyboard: a message
// cannot have both that keyboard and inline buttons.
func (b *Bot) greet(ctx context.Context, chatID int64, from *models.User) {
	b.sendEmojiText(ctx, chatID, emojiText{
		fmt.Sprintf(b.t.Greeting, b.addressName(ctx, from)), emojiAt{id: b.emoji.Greeting(), alt: "👋", before: " "}, "\n" + b.t.GreetingText,
	}, b.replyKeyboard())
}

// keyButton tells which bottom button a message is, or "". Keyboards sent
// by earlier versions stay on screens and send their old texts.
func (b *Bot) keyButton(text string) string {
	for i, label := range []string{b.t.KeySubjects, b.t.KeyPinned, b.t.KeyAccount} {
		if text == label || text == b.t.KeyOld[i] {
			return label
		}
	}
	if text == b.t.KeySettings {
		return text
	}
	return ""
}

// onKeyButton handles a tap on the bottom keyboard, which arrives as the
// button's text. It reports false for any other message. The new menu
// goes out first; the tap and the old menu are deleted in the background.
// Tapping the button of the menu already last in the chat only deletes
// the tap.
func (b *Bot) onKeyButton(ctx context.Context, m *models.Message) bool {
	key := b.keyButton(m.Text)
	if key == "" {
		return false
	}
	start := time.Now()
	chatID := m.Chat.ID
	// The tap would otherwise stay in the chat as the user's message. It
	// is forgotten at once, so the menu above it counts as the last
	// message again.
	b.msgs.gone(ctx, chatID, m.ID)
	b.later(ctx, func(ctx context.Context) {
		if _, err := b.api.DeleteMessage(ctx, &tg.DeleteMessageParams{ChatID: chatID, MessageID: m.ID}); err != nil {
			b.log.Debug("delete key tap", "err", err)
		}
	})
	if b.msgs.showing(ctx, chatID, key) {
		b.log.Debug("key button: menu already shown", "button", key)
		return true
	}
	switch key {
	case b.t.KeySubjects:
		text, kb := b.homeView(ctx, m.From)
		b.replaceMenu(ctx, chatID, text, kb)
	case b.t.KeyPinned:
		text, kb := b.pinsView(ctx, m.From.ID)
		b.replaceMenu(ctx, chatID, text, kb)
	case b.t.KeyAccount:
		b.showAccount(ctx, chatID, m.From)
	case b.t.KeySettings:
		text, kb := b.settingsView(ctx, m.From)
		b.replaceMenu(ctx, chatID, text, kb)
	}
	if id := b.msgs.menu(ctx, chatID); id != 0 {
		b.msgs.setKind(chatID, id, key)
	}
	b.log.Info("key button", "button", key, "ms", time.Since(start).Milliseconds())
	return true
}

// replaceMenu shows a menu: in place when the current menu is the last
// message, and otherwise as a new one; the old menu is removed afterwards,
// in the background.
func (b *Bot) replaceMenu(ctx context.Context, chatID int64, text string, kb *models.InlineKeyboardMarkup) {
	if id := b.msgs.lastMenu(ctx, chatID); id != 0 {
		b.editMenu(ctx, chatID, id, text, kb)
		return
	}
	old := b.msgs.menu(ctx, chatID)
	b.sendMenu(ctx, chatID, text, kb)
	if old != 0 {
		b.retireLater(ctx, chatID, old)
	}
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
	err = b.withPlain(articleKeyboard(b.t, n, !pinned, fromPins, b.emoji.Pin()), func(kb *models.InlineKeyboardMarkup) error {
		_, err := b.api.EditMessageReplyMarkup(ctx, &tg.EditMessageReplyMarkupParams{
			ChatID: msg.Chat.ID, MessageID: msg.ID, ReplyMarkup: kb,
		})
		return err
	})
	if err != nil {
		b.log.Debug("flip pin button", "err", err)
	}
}

// accountMarkdown is the account page: rank, how far the next one is,
// accepted and open reports, and for admins their level.
func (b *Bot) accountMarkdown(ctx context.Context, from *models.User) string {
	u, err := b.store.TouchUser(ctx, from.ID, from.Username, from.FirstName)
	if err != nil {
		b.log.Error("touch user", "err", err)
	}
	open, _, err := b.store.SuggestQuota(ctx, from.ID)
	if err != nil {
		b.log.Error("suggest quota", "err", err)
	}
	accepted := u.Accepted
	ladder := b.ladder()
	rank := ladder.For(accepted)
	icon := richEmojiRef(b.emoji.Rank(rank.Title), "🏅")
	next := []string{b.t.AccountTop}
	if r, ok := ladder.Next(rank); ok {
		left := r.From - accepted
		next = []string{
			fmt.Sprintf(b.t.AccountNext, left, plural(left, b.t.Reports)) + "  ",
			fmt.Sprintf(b.t.AccountNextRank, richEmojiRef(b.emoji.Rank(r.Title), "🏅")+"**"+escapeMarkdown(r.Title)+"**"),
		}
	}
	// Two trailing spaces break the line within a paragraph.
	lines := []string{
		"# " + escapeMarkdown(u.AddressName()),
		"",
		fmt.Sprintf(b.t.AccountRank, icon+"**"+escapeMarkdown(rank.Title)+"**") + "  ",
	}
	lines = append(lines, next...)
	lines = append(lines,
		"",
		fmt.Sprintf(b.t.AccountAccepted, accepted)+"  ",
		fmt.Sprintf(b.t.AccountOpen, open),
	)
	if b.cfg.IsAdmin(from.ID) {
		lines[len(lines)-1] += "  "
		lines = append(lines, b.t.AccountAdmin)
	}
	return strings.Join(lines, "\n")
}

func (b *Bot) accountKeyboard() *models.InlineKeyboardMarkup {
	return keyboard(row(button(b.t.History, cbHistory), button(b.t.Top, cbTop)), row(button(b.t.Close, cbClose)))
}

// historyMarkdown lists the user's reports, and the edits and topics they
// suggested in earlier versions: open, accepted and rejected, newest
// first. Titles of notes still in the vault are links.
func (b *Bot) historyMarkdown(ctx context.Context, tgID int64) string {
	all, err := b.store.UserSuggestions(ctx, tgID)
	if err != nil {
		b.log.Error("user suggestions", "err", err)
	}
	cat := b.catalog()
	sections := []struct {
		title, status string
	}{
		{b.t.HistoryPending, store.StatusOpen},
		{b.t.HistoryAccepted, store.StatusMerged},
		{b.t.HistoryRejected, store.StatusClosed},
	}
	var sb strings.Builder
	sb.WriteString("# " + b.t.History + "\n")
	for i, sec := range sections {
		if i > 0 {
			// A divider between sections, none after the last.
			sb.WriteString("\n---\n")
		}
		sb.WriteString("\n## " + sec.title + "\n\n")
		n := 0
		for _, sg := range all {
			if sg.Status != sec.status {
				continue
			}
			n++
			title := escapeMarkdown(sg.Title)
			if note := cat.NoteByPath(sg.ArticlePath); note != nil {
				title = "[" + strings.NewReplacer("[", "", "]", "").Replace(sg.Title) + "](" + suggest.LinkTarget(note.Path) + ")"
			}
			when := sg.CreatedAt
			if !sg.ResolvedAt.IsZero() {
				when = sg.ResolvedAt
			}
			fmt.Fprintf(&sb, "- "+b.t.HistoryItem+"\n", title, b.kindLabel(sg), b.date(when))
			if sg.Status == store.StatusClosed && sg.Comment != "" {
				sb.WriteString("  " + escapeMarkdown(fmt.Sprintf(b.t.ReviewComment, oneLine(sg.Comment))) + "\n")
			}
		}
		if n == 0 {
			sb.WriteString(b.t.HistoryEmpty + "\n")
		}
	}
	return sb.String()
}

func (b *Bot) historyKeyboard() *models.InlineKeyboardMarkup {
	return keyboard(row(button(b.t.Back, cbAccount), button(b.t.Close, cbClose)))
}

// kindLabel names what a suggestion was. A report about a grade names its
// folder rather than a note. A new topic's page lies right in the topic
// folder (subject/grade/topic/page.md); a new note is deeper.
func (b *Bot) kindLabel(sg store.Suggestion) string {
	switch {
	case sg.Kind == store.KindEdit:
		return b.t.KindEditLabel
	case sg.Kind == store.KindReport && isGradeReport(sg):
		return b.t.KindGradeLabel
	case sg.Kind == store.KindReport:
		return b.t.KindReportLabel
	case strings.Count(sg.ArticlePath, "/") <= 3:
		return b.t.KindTopicLabel
	}
	return b.t.KindNoteLabel
}

// showRich sends a menu as rich messages, split like a long note, with the
// buttons under the last part, and removes the previous menu.
func (b *Bot) showRich(ctx context.Context, chatID int64, title, md string, kb *models.InlineKeyboardMarkup) {
	old := b.msgs.menu(ctx, chatID)
	n := &content.Note{Title: title, Body: md}
	res := render.Convert(b.catalog(), n, render.Options{Continued: b.t.Continued})
	var sent []int
	for i, part := range res.Parts {
		p := &tg.SendRichMessageParams{ChatID: chatID, RichMessage: models.InputRichMessage{Markdown: part.Markdown}}
		var partKB *models.InlineKeyboardMarkup
		if i == len(res.Parts)-1 {
			partKB = kb
			p.ReplyMarkup = kb
		}
		msg, err := b.api.SendRichMessage(ctx, p)
		if err != nil {
			// Once more without custom emoji, in the text or on buttons.
			md, inText := plainMarkdown(part.Markdown)
			plainKB, onButtons := b.plainInline(partKB)
			if inText || onButtons {
				p.RichMessage.Markdown = md
				p.ReplyMarkup = nil
				if plainKB != nil {
					p.ReplyMarkup = plainKB
				}
				var err2 error
				if msg, err2 = b.api.SendRichMessage(ctx, p); err2 == nil {
					b.emoji.rejected(err)
					err = nil
				}
			}
		}
		if err != nil {
			b.log.Error("send rich menu", "title", title, "err", err)
			b.log.Debug("rejected markdown", "markdown", part.Markdown)
			break
		}
		b.msgs.seen(ctx, chatID, msg.ID)
		sent = append(sent, msg.ID)
	}
	if len(sent) == 0 {
		b.send(ctx, chatID, b.t.SendFailed, nil)
		return
	}
	if err := b.store.SetMessageGroup(ctx, chatID, sent); err != nil {
		b.log.Error("store message group", "err", err)
	}
	b.msgs.setRichMenu(ctx, chatID, sent[len(sent)-1])
	if old != 0 {
		b.retireLater(ctx, chatID, old)
	}
}

func (b *Bot) showAccount(ctx context.Context, chatID int64, from *models.User) {
	b.showRich(ctx, chatID, b.addressName(ctx, from), b.accountMarkdown(ctx, from), b.accountKeyboard())
}

func (b *Bot) showHistory(ctx context.Context, chatID int64, from *models.User) {
	b.showRich(ctx, chatID, b.t.History, b.historyMarkdown(ctx, from.ID), b.historyKeyboard())
}

// isGradeReport tells a report about a grade, filed under the grade
// folder, from one about a note.
func isGradeReport(sg store.Suggestion) bool {
	return sg.Kind == store.KindReport && !strings.HasSuffix(sg.ArticlePath, ".md")
}

// date formats a day as «25 октября».
func (b *Bot) date(t time.Time) string {
	return fmt.Sprintf("%d %s", t.Day(), b.t.Months[t.Month()-1])
}

// plural picks the form of a word for n: forms are for 1, 2–4 and 5 or
// more («сообщение», «сообщения», «сообщений»).
func plural(n int, forms [3]string) string {
	n %= 100
	switch {
	case n >= 11 && n <= 14:
		return forms[2]
	case n%10 == 1:
		return forms[0]
	case n%10 >= 2 && n%10 <= 4:
		return forms[1]
	}
	return forms[2]
}

var markdownSpecial = strings.NewReplacer(
	`\`, `\\`, "*", `\*`, "_", `\_`, "[", `\[`, "]", `\]`, "`", "\\`", "$", `\$`,
	"#", `\#`, "<", `\<`, ">", `\>`, "~", `\~`, "|", `\|`,
)

// escapeMarkdown makes text from users or reviewers literal.
func escapeMarkdown(s string) string { return markdownSpecial.Replace(s) }

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

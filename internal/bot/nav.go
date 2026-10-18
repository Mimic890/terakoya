package bot

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/content"
	"github.com/Mimic890/terakoya/internal/i18n"
	"github.com/Mimic890/terakoya/internal/render"
	"github.com/Mimic890/terakoya/internal/settings"
	"github.com/Mimic890/terakoya/internal/suggest"
)

// Callback data prefixes. Subjects, grades and notes are named by their
// 11-character IDs, so every payload fits into 64 bytes.
const (
	cbHome     = "h"
	cbSubject  = "s:"                         // s:<subject id>
	cbGrade    = "g:"                         // g:<grade id>, the topic list
	cbArticle  = render.ArticleCallbackPrefix // a:<note id>, replaces the message it is pressed in
	cbBack     = "b:"                         // b:<note id>
	cbDownload = "d:"                         // d:<note id>
	cbSuggest  = "e:"                         // e:<note id>
	cbAddTopic = "n:"                         // n:<grade id>
)

func (b *Bot) onCallback(ctx context.Context, q *models.CallbackQuery) {
	msg := q.Message.Message
	if msg == nil {
		b.answer(ctx, q, "", false)
		return
	}
	cat := b.catalog()
	data := q.Data
	note := func(prefix string) *content.Note { return cat.Note(strings.TrimPrefix(data, prefix)) }
	switch {
	case data == cbHome:
		b.answer(ctx, q, "", false)
		text, kb := b.homeView(ctx, &q.From)
		b.editMenu(ctx, msg.Chat.ID, msg.ID, text, kb)

	case strings.HasPrefix(data, cbSubject):
		s := cat.Subject(strings.TrimPrefix(data, cbSubject))
		if s == nil {
			b.notFound(ctx, q, msg)
			return
		}
		b.answer(ctx, q, "", false)
		text, kb := subjectView(b.t, s)
		b.editMenu(ctx, msg.Chat.ID, msg.ID, text, kb)

	case strings.HasPrefix(data, cbGrade):
		g := cat.Grade(strings.TrimPrefix(data, cbGrade))
		if g == nil {
			b.notFound(ctx, q, msg)
			return
		}
		b.answer(ctx, q, "", false)
		b.showGrade(ctx, msg.Chat.ID, msg, g)

	case strings.HasPrefix(data, cbArticle):
		n := note(cbArticle)
		if n == nil {
			b.answer(ctx, q, b.t.NotFound, true)
			return
		}
		b.answer(ctx, q, "", false)
		if b.sendArticle(ctx, msg.Chat.ID, n, false) {
			// The note replaces the message it was opened from: the topic
			// list, or every part of the note that linked to it.
			b.retire(ctx, msg)
		}

	case strings.HasPrefix(data, cbOpen):
		// From a notification or a dialog: keep the message it came from.
		n := note(cbOpen)
		if n == nil {
			b.answer(ctx, q, b.t.NotFound, true)
			return
		}
		b.answer(ctx, q, "", false)
		b.sendArticle(ctx, msg.Chat.ID, n, false)

	case strings.HasPrefix(data, cbBack):
		b.answer(ctx, q, "", false)
		n := note(cbBack)
		switch {
		case n != nil && !n.IsMain() && n.Topic.Main != nil:
			// From a theorem or a task to its topic's main page.
			if b.sendArticle(ctx, msg.Chat.ID, n.Topic.Main, false) {
				b.retire(ctx, msg)
			}
		case n != nil:
			b.retire(ctx, msg)
			b.showGrade(ctx, msg.Chat.ID, nil, n.Grade())
		default:
			b.retire(ctx, msg)
			text, kb := b.homeView(ctx, &q.From)
			b.showMenu(ctx, msg.Chat.ID, text, kb)
		}

	case strings.HasPrefix(data, cbDownload):
		n := note(cbDownload)
		if n == nil {
			b.answer(ctx, q, b.t.NotFound, true)
			return
		}
		b.answer(ctx, q, "", false)
		b.sendMarkdownFile(ctx, msg.Chat.ID, n)

	case strings.HasPrefix(data, cbSuggest):
		n := note(cbSuggest)
		if n == nil {
			b.answer(ctx, q, b.t.NotFound, true)
			return
		}
		b.startSuggestion(ctx, q, msg.Chat.ID, n)

	case strings.HasPrefix(data, cbAddTopic):
		g := cat.Grade(strings.TrimPrefix(data, cbAddTopic))
		if g == nil {
			b.answer(ctx, q, b.t.NotFound, true)
			return
		}
		b.startNewTopic(ctx, q, msg.Chat.ID, g)

	case strings.HasPrefix(data, cbPin):
		n := note(cbPin)
		if n == nil {
			b.answer(ctx, q, b.t.NotFound, true)
			return
		}
		b.togglePin(ctx, q, msg, n)

	case strings.HasPrefix(data, cbFromPins):
		n := note(cbFromPins)
		if n == nil {
			b.answer(ctx, q, b.t.NotFound, true)
			text, kb := b.pinsView(ctx, q.From.ID)
			b.editMenu(ctx, msg.Chat.ID, msg.ID, text, kb)
			return
		}
		b.answer(ctx, q, "", false)
		if b.sendArticle(ctx, msg.Chat.ID, n, true) {
			b.retire(ctx, msg)
		}

	case strings.HasPrefix(data, cbBackPins):
		b.answer(ctx, q, "", false)
		b.retire(ctx, msg)
		text, kb := b.pinsView(ctx, q.From.ID)
		b.showMenu(ctx, msg.Chat.ID, text, kb)

	case data == cbClose:
		b.answer(ctx, q, "", false)
		b.retire(ctx, msg)

	case strings.HasPrefix(data, cbRequest):
		b.requestAccess(ctx, q, msg, strings.TrimPrefix(data, cbRequest))

	case data == cbHistory:
		b.answer(ctx, q, "", false)
		b.showHistory(ctx, msg.Chat.ID, &q.From)

	case data == cbAccount:
		b.answer(ctx, q, "", false)
		b.showAccount(ctx, msg.Chat.ID, &q.From)

	case strings.HasPrefix(data, cbEmoji):
		b.onEmojiWizardCallback(ctx, q, msg)

	case strings.HasPrefix(data, cbAllow), strings.HasPrefix(data, cbDeny):
		b.decideAccess(ctx, q, msg, data)

	case data == cbCancel:
		b.answer(ctx, q, "", false)
		b.retire(ctx, msg)
		b.cancelSuggestion(ctx, msg.Chat.ID, q.From.ID)

	case data == cbFix, data == cbReport, data == cbSend, data == cbAddNote,
		strings.HasPrefix(data, cbSection), strings.HasPrefix(data, cbKind), strings.HasPrefix(data, cbParent):
		b.onDialogCallback(ctx, q, msg)

	default:
		b.answer(ctx, q, "", false)
	}
}

func (b *Bot) notFound(ctx context.Context, q *models.CallbackQuery, msg *models.Message) {
	b.answer(ctx, q, b.t.NotFound, true)
	text, kb := b.homeView(ctx, &q.From)
	b.editMenu(ctx, msg.Chat.ID, msg.ID, text, kb)
}

func (b *Bot) homeView(ctx context.Context, from *models.User) (string, *models.InlineKeyboardMarkup) {
	if _, err := b.store.TouchUser(ctx, from.ID, from.Username, from.FirstName); err != nil {
		b.log.Error("touch user", "err", err)
	}
	return homeView(b.t, b.catalog(), b.emoji.Subject)
}

// homeView is the subject list; icon gives a subject's custom emoji. The
// greeting and the rank are elsewhere: in the /start message and under
// «Аккаунт».
func homeView(t i18n.Strings, cat *content.Catalog, icon func(subject string) string) (string, *models.InlineKeyboardMarkup) {
	if len(cat.Subjects) == 0 {
		return t.NoSubjects, keyboard()
	}
	var rows [][]models.InlineKeyboardButton
	for _, s := range cat.Subjects {
		btn := button(s.Name, cbSubject+s.ID)
		btn.IconCustomEmojiID = icon(s.Name)
		rows = append(rows, row(btn))
	}
	return t.ChooseSubject, keyboard(rows...)
}

func subjectView(t i18n.Strings, s *content.Subject) (string, *models.InlineKeyboardMarkup) {
	var rows [][]models.InlineKeyboardButton
	var cur []models.InlineKeyboardButton
	for _, g := range s.Grades {
		cur = append(cur, button(g.Title(), cbGrade+g.ID))
		if len(cur) == 3 {
			rows, cur = append(rows, cur), nil
		}
	}
	if len(cur) > 0 {
		rows = append(rows, cur)
	}
	rows = append(rows, row(button(t.Back, cbHome)))
	return fmt.Sprintf(t.ChooseGrade, s.Name), keyboard(rows...)
}

// showGrade shows a grade's topic list in the style settings.yml sets:
// a rich message with a link per topic, or a button per topic. In the
// button style, msg is the menu to edit; nil replaces the current menu.
// A topic opens in place of the list either way.
func (b *Bot) showGrade(ctx context.Context, chatID int64, msg *models.Message, g *content.Grade) {
	addTopic := b.host != nil
	if b.settings.Get().TopicList() == settings.TopicListButtons {
		text, kb := gradeView(b.t, g, addTopic)
		if msg != nil {
			b.editMenu(ctx, chatID, msg.ID, text, kb)
		} else {
			b.showMenu(ctx, chatID, text, kb)
		}
		return
	}
	header := fmt.Sprintf(b.t.TopicsHeader, g.Subject.Name, g.Title())
	b.showRich(ctx, chatID, header, b.topicListMarkdown(g), gradeKeyboard(b.t, g, addTopic))
}

// topicLabel names a topic in the list: «§5 Параллельность…» from the
// folder number without its leading zeros, or the title alone.
func topicLabel(tp *content.Topic) string {
	if !tp.HasNumber {
		return tp.Title()
	}
	return fmt.Sprintf("§%d %s", tp.Number, tp.Title())
}

// topicListMarkdown is the list style: the heading with the subject's
// custom emoji, if any, and a line per topic that opens it.
func (b *Bot) topicListMarkdown(g *content.Grade) string {
	var sb strings.Builder
	sb.WriteString("# " + richEmojiRef(b.emoji.Subject(g.Subject.Name), placeholder) +
		escapeMarkdown(fmt.Sprintf(b.t.TopicsHeader, g.Subject.Name, g.Title())) + "\n\n")
	if len(g.Topics) == 0 {
		sb.WriteString(b.t.NoTopics + "\n")
	}
	// Each line is a link to the topic's main page, which the renderer
	// turns into an in-text button; lines stay lines, as in Obsidian.
	for _, tp := range g.Topics {
		label := strings.NewReplacer("[", "", "]", "").Replace(topicLabel(tp))
		sb.WriteString("[" + label + "](" + suggest.LinkTarget(tp.Main.Path) + ")\n")
	}
	return sb.String()
}

// gradeKeyboard goes under the topic list: «Добавить тему» when
// suggestions are on, and «Назад».
func gradeKeyboard(t i18n.Strings, g *content.Grade, addTopic bool) *models.InlineKeyboardMarkup {
	var rows [][]models.InlineKeyboardButton
	if addTopic {
		rows = append(rows, row(button(t.AddTopic, cbAddTopic+g.ID)))
	}
	return keyboard(append(rows, row(button(t.Back, cbSubject+g.Subject.ID)))...)
}

// gradeView is the button style of the topic list.
func gradeView(t i18n.Strings, g *content.Grade, addTopic bool) (string, *models.InlineKeyboardMarkup) {
	text := fmt.Sprintf(t.TopicsHeader, g.Subject.Name, g.Title()) + "\n\n" + t.ChooseTopic
	if len(g.Topics) == 0 {
		text += "\n\n" + t.NoTopics
	}
	var rows [][]models.InlineKeyboardButton
	for _, tp := range g.Topics {
		rows = append(rows, row(button(topicLabel(tp), cbArticle+tp.Main.ID)))
	}
	rows = append(rows, gradeKeyboard(t, g, addTopic).InlineKeyboard...)
	return text, keyboard(rows...)
}

// articleKeyboard goes under the last part of every note. The notes of a
// topic are reached through the links on its main page. A note opened
// from the pinned list goes back to the list.
// pinIcon is the custom emoji of the pin button, or "" for the plain one.
func articleKeyboard(t i18n.Strings, n *content.Note, pinned, fromPins bool, pinIcon string) *models.InlineKeyboardMarkup {
	back := cbBack
	if fromPins {
		back = cbBackPins
	}
	label := t.Pin
	if pinned {
		label = t.Unpin
	}
	pin := button(label, cbPin+n.ID)
	pin.IconCustomEmojiID = pinIcon
	return keyboard(
		row(button(t.Back, back+n.ID)),
		row(button(t.SuggestEdit, cbSuggest+n.ID), button(t.DownloadMD, cbDownload+n.ID)),
		row(pin),
	)
}

func callbacks(kb *models.InlineKeyboardMarkup) []string {
	var out []string
	for _, r := range kb.InlineKeyboard {
		for _, b := range r {
			out = append(out, b.CallbackData)
		}
	}
	return out
}

func button(text, data string) models.InlineKeyboardButton {
	return models.InlineKeyboardButton{Text: text, CallbackData: data}
}

func row(btns ...models.InlineKeyboardButton) []models.InlineKeyboardButton { return btns }

func keyboard(rows ...[]models.InlineKeyboardButton) *models.InlineKeyboardMarkup {
	if rows == nil {
		rows = [][]models.InlineKeyboardButton{}
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

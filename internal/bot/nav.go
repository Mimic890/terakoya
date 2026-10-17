package bot

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/content"
	"github.com/Mimic890/terakoya/internal/i18n"
	"github.com/Mimic890/terakoya/internal/ranks"
	"github.com/Mimic890/terakoya/internal/render"
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
		text, kb := gradeView(b.t, g)
		b.editMenu(ctx, msg.Chat.ID, msg.ID, text, kb)

	case strings.HasPrefix(data, cbArticle):
		n := note(cbArticle)
		if n == nil {
			b.answer(ctx, q, b.t.NotFound, true)
			return
		}
		b.answer(ctx, q, "", false)
		if b.sendArticle(ctx, msg.Chat.ID, n) {
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
		b.sendArticle(ctx, msg.Chat.ID, n)

	case strings.HasPrefix(data, cbBack):
		b.answer(ctx, q, "", false)
		n := note(cbBack)
		switch {
		case n != nil && !n.IsMain() && n.Topic.Main != nil:
			// From a theorem or a task to its topic's main page.
			if b.sendArticle(ctx, msg.Chat.ID, n.Topic.Main) {
				b.retire(ctx, msg)
			}
		case n != nil:
			b.retire(ctx, msg)
			text, kb := gradeView(b.t, n.Grade())
			b.showMenu(ctx, msg.Chat.ID, text, kb)
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
	merged := 0
	if u, err := b.store.TouchUser(ctx, from.ID, from.Username, from.FirstName); err != nil {
		b.log.Error("touch user", "err", err)
	} else {
		merged = u.MergedCount
	}
	return homeView(b.t, b.catalog(), b.cfg.Ladder, from.FirstName, merged)
}

func homeView(t i18n.Strings, cat *content.Catalog, ladder ranks.Ladder, name string, merged int) (string, *models.InlineKeyboardMarkup) {
	var sb strings.Builder
	fmt.Fprintf(&sb, t.Greeting, name)
	sb.WriteString("\n\n")
	fmt.Fprintf(&sb, t.RankLine, ladder.For(merged).Title, merged)
	sb.WriteString("\n\n")
	if len(cat.Subjects) == 0 {
		sb.WriteString(t.NoSubjects)
		return sb.String(), keyboard()
	}
	sb.WriteString(t.ChooseSubject)
	var rows [][]models.InlineKeyboardButton
	for _, s := range cat.Subjects {
		rows = append(rows, row(button(s.Name, cbSubject+s.ID)))
	}
	return sb.String(), keyboard(rows...)
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

// gradeView is the topic list of a grade.
func gradeView(t i18n.Strings, g *content.Grade) (string, *models.InlineKeyboardMarkup) {
	text := fmt.Sprintf(t.TopicsHeader, g.Subject.Name, g.Title()) + "\n\n" + t.ChooseTopic
	if len(g.Topics) == 0 {
		text += "\n\n" + t.NoTopics
	}
	var rows [][]models.InlineKeyboardButton
	for _, tp := range g.Topics {
		rows = append(rows, row(button(tp.Title(), cbArticle+tp.Main.ID)))
	}
	rows = append(rows,
		row(button(t.AddTopic, cbAddTopic+g.ID)),
		row(button(t.Back, cbSubject+g.Subject.ID)),
	)
	return text, keyboard(rows...)
}

// articleKeyboard goes under the last part of every note. The notes of a
// topic are reached through the links on its main page.
func articleKeyboard(t i18n.Strings, n *content.Note) *models.InlineKeyboardMarkup {
	return keyboard(
		row(button(t.Back, cbBack+n.ID)),
		row(button(t.SuggestEdit, cbSuggest+n.ID), button(t.DownloadMD, cbDownload+n.ID)),
	)
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

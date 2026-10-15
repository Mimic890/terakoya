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

// Callback data prefixes. Each payload must fit into 64 bytes.
const (
	cbHome     = "h"
	cbSubject  = "s:"                         // s:<subject>
	cbGrade    = "g:"                         // g:<subject>:<grade>
	cbArticle  = render.ArticleCallbackPrefix // a:<id>, from the topic list or a [[link]] in an article
	cbBack     = "b:"                         // b:<id>, leaves an article for its topic list
	cbDownload = "d:"                         // d:<id>
	cbSuggest  = "e:"                         // e:<id>
	cbAddTopic = "n:"                         // n:<subject>:<grade>
)

func gradeCallback(g *content.Grade) string {
	return cbGrade + g.Subject.Dir + ":" + g.Dir
}

func (b *Bot) onCallback(ctx context.Context, q *models.CallbackQuery) {
	msg := q.Message.Message
	if msg == nil {
		b.answer(ctx, q, "", false)
		return
	}
	cat := b.catalog()
	data := q.Data
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
		g := findGrade(cat, strings.TrimPrefix(data, cbGrade))
		if g == nil {
			b.notFound(ctx, q, msg)
			return
		}
		b.answer(ctx, q, "", false)
		text, kb := gradeView(b.t, g)
		b.editMenu(ctx, msg.Chat.ID, msg.ID, text, kb)

	case strings.HasPrefix(data, cbArticle):
		a := cat.Article(strings.TrimPrefix(data, cbArticle))
		if a == nil {
			b.answer(ctx, q, b.t.NotFound, true)
			return
		}
		b.answer(ctx, q, "", false)
		if b.sendArticle(ctx, msg.Chat.ID, a) {
			// The article replaces the message it was opened from: the topic
			// list, or the article that linked to it.
			b.retire(ctx, msg)
		}

	case strings.HasPrefix(data, cbBack):
		b.answer(ctx, q, "", false)
		b.retire(ctx, msg)
		var text string
		var kb *models.InlineKeyboardMarkup
		if a := cat.Article(strings.TrimPrefix(data, cbBack)); a != nil {
			text, kb = gradeView(b.t, a.Grade)
		} else {
			text, kb = b.homeView(ctx, &q.From)
		}
		b.showMenu(ctx, msg.Chat.ID, text, kb)

	case strings.HasPrefix(data, cbDownload):
		a := cat.Article(strings.TrimPrefix(data, cbDownload))
		if a == nil {
			b.answer(ctx, q, b.t.NotFound, true)
			return
		}
		b.answer(ctx, q, "", false)
		b.sendMarkdownFile(ctx, msg.Chat.ID, a)

	case strings.HasPrefix(data, cbSuggest), strings.HasPrefix(data, cbAddTopic):
		b.answer(ctx, q, b.t.ComingSoon, true)

	default:
		b.answer(ctx, q, "", false)
	}
}

func (b *Bot) notFound(ctx context.Context, q *models.CallbackQuery, msg *models.Message) {
	b.answer(ctx, q, b.t.NotFound, true)
	text, kb := b.homeView(ctx, &q.From)
	b.editMenu(ctx, msg.Chat.ID, msg.ID, text, kb)
}

func findGrade(cat *content.Catalog, ref string) *content.Grade {
	subject, grade, ok := strings.Cut(ref, ":")
	if !ok {
		return nil
	}
	s := cat.Subject(subject)
	if s == nil {
		return nil
	}
	return s.Grade(grade)
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
		rows = append(rows, row(button(s.Label(), cbSubject+s.Dir)))
	}
	return sb.String(), keyboard(rows...)
}

func subjectView(t i18n.Strings, s *content.Subject) (string, *models.InlineKeyboardMarkup) {
	var rows [][]models.InlineKeyboardButton
	var cur []models.InlineKeyboardButton
	for _, g := range s.Grades {
		cur = append(cur, button(gradeTitle(t, g), gradeCallback(g)))
		if len(cur) == 3 {
			rows, cur = append(rows, cur), nil
		}
	}
	if len(cur) > 0 {
		rows = append(rows, cur)
	}
	rows = append(rows, row(button(t.Back, cbHome)))
	return fmt.Sprintf(t.ChooseGrade, s.Label()), keyboard(rows...)
}

func gradeView(t i18n.Strings, g *content.Grade) (string, *models.InlineKeyboardMarkup) {
	text := fmt.Sprintf(t.TopicsHeader, g.Subject.Label(), gradeTitle(t, g))
	if g.Notes != "" {
		text += "\n" + g.Notes
	}
	text += "\n\n" + t.ChooseTopic
	if len(g.Articles) == 0 {
		text += "\n\n" + t.NoTopics
	}
	var rows [][]models.InlineKeyboardButton
	for _, a := range g.Articles {
		rows = append(rows, row(button(a.Title, cbArticle+a.ID)))
	}
	rows = append(rows,
		row(button(t.AddTopic, cbAddTopic+g.Subject.Dir+":"+g.Dir)),
		row(button(t.Back, cbSubject+g.Subject.Dir)),
	)
	return text, keyboard(rows...)
}

func articleKeyboard(t i18n.Strings, a *content.Article) *models.InlineKeyboardMarkup {
	return keyboard(
		row(button(t.Back, cbBack+a.ID)),
		row(button(t.SuggestEdit, cbSuggest+a.ID), button(t.DownloadMD, cbDownload+a.ID)),
	)
}

func gradeTitle(t i18n.Strings, g *content.Grade) string {
	if g.Title != "" {
		return g.Title
	}
	return fmt.Sprintf(t.GradeTitle, g.Dir)
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

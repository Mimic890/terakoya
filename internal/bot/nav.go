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
	cbReport   = "e:"                         // e:<note id>, «Сообщить об ошибке» under a note
	// n:<grade id>, «Сообщить об ошибке» under a topic list, for something
	// missing in the grade; earlier versions had «Добавить тему» here.
	cbReportGrade = "n:"
	// m:<subject id>, «Сообщить об ошибке» under a subject's grade list.
	cbReportSubject = "m:"
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
		b.showSubjects(ctx, msg.Chat.ID, msg, &q.From)

	case strings.HasPrefix(data, cbSubject):
		s := cat.Subject(strings.TrimPrefix(data, cbSubject))
		if s == nil {
			b.notFound(ctx, q, msg)
			return
		}
		b.answer(ctx, q, "", false)
		b.showSubject(ctx, msg.Chat.ID, msg, s)

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
			b.showSubjects(ctx, msg.Chat.ID, nil, &q.From)
		}

	case strings.HasPrefix(data, cbDownload):
		n := note(cbDownload)
		if n == nil {
			b.answer(ctx, q, b.t.NotFound, true)
			return
		}
		b.answer(ctx, q, "", false)
		b.sendMarkdownFile(ctx, msg.Chat.ID, n)

	case strings.HasPrefix(data, cbReport):
		n := note(cbReport)
		if n == nil {
			b.answer(ctx, q, b.t.NotFound, true)
			return
		}
		b.startReport(ctx, q, msg.Chat.ID, n)

	case strings.HasPrefix(data, cbReportSubject):
		sub := cat.Subject(strings.TrimPrefix(data, cbReportSubject))
		if sub == nil {
			b.answer(ctx, q, b.t.NotFound, true)
			return
		}
		b.startSubjectReport(ctx, q, msg.Chat.ID, sub)

	case strings.HasPrefix(data, cbReportGrade):
		g := cat.Grade(strings.TrimPrefix(data, cbReportGrade))
		if g == nil {
			b.answer(ctx, q, b.t.NotFound, true)
			return
		}
		b.startGradeReport(ctx, q, msg.Chat.ID, g)

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

	case data == cbHistory:
		b.answer(ctx, q, "", false)
		b.showHistory(ctx, msg.Chat.ID, &q.From)

	case data == cbAccount:
		b.answer(ctx, q, "", false)
		b.showAccount(ctx, msg.Chat.ID, &q.From)

	case data == cbTop:
		b.answer(ctx, q, "", false)
		b.showTop(ctx, msg.Chat.ID, &q.From)

	case data == cbRanks:
		b.answer(ctx, q, "", false)
		b.showRanks(ctx, msg.Chat.ID, &q.From)

	case strings.HasPrefix(data, cbEmoji):
		b.onEmojiWizardCallback(ctx, q, msg)

	case strings.HasPrefix(data, cbAdmin):
		b.onAdminCallback(ctx, q, msg)

	case strings.HasPrefix(data, cbSettings):
		b.onSettingsCallback(ctx, q, msg)

	case strings.HasPrefix(data, cbBroadcast):
		b.onBroadcastCallback(ctx, q, msg)

	case data == cbCancel:
		b.answer(ctx, q, "", false)
		// A dialog's «Отмена» takes all its messages with it; a stale one
		// just goes.
		if !b.cancelDialog(ctx, msg.Chat.ID, q.From.ID) {
			b.retire(ctx, msg)
		}

	case data == cbSend:
		b.onDialogCallback(ctx, q, msg)

	default:
		// Buttons of earlier versions, such as «Исправить текст», and
		// stale ones lose their keyboard.
		b.answer(ctx, q, "", false)
		if msg.ReplyMarkup != nil && len(msg.ReplyMarkup.InlineKeyboard) > 0 {
			b.dropButtonsOf(ctx, msg)
		}
	}
}

func (b *Bot) notFound(ctx context.Context, q *models.CallbackQuery, msg *models.Message) {
	b.answer(ctx, q, b.t.NotFound, true)
	b.showSubjects(ctx, msg.Chat.ID, msg, &q.From)
}

// richLists reports whether subjects, grades and topics are listed as
// rich messages, a line per item, rather than as buttons (list_style in
// settings.yml).
func (b *Bot) richLists() bool { return b.settings.Get().List() != settings.ListButtons }

// showSubjects shows the subject list. In the button style, msg is the
// menu to edit, or nil to replace the current menu; a rich list always
// replaces it. from, if known, is recorded as a user.
func (b *Bot) showSubjects(ctx context.Context, chatID int64, msg *models.Message, from *models.User) {
	if from != nil {
		if _, err := b.store.TouchUser(ctx, from.ID, from.Username, from.FirstName); err != nil {
			b.log.Error("touch user", "err", err)
		}
	}
	cat := b.catalog()
	if b.richLists() {
		b.showRich(ctx, chatID, b.t.SubjectsTitle, b.subjectsMarkdown(cat), keyboard(row(button(b.t.Close, cbClose))))
		return
	}
	text, kb := homeView(b.t, cat, b.emoji.Subject)
	b.showButtons(ctx, chatID, msg, text, kb)
}

// showSubject shows a subject's grade list, like showSubjects.
func (b *Bot) showSubject(ctx context.Context, chatID int64, msg *models.Message, s *content.Subject) {
	report := b.host != nil
	if b.richLists() {
		b.showRich(ctx, chatID, s.Name, b.gradesMarkdown(s), subjectKeyboard(b.t, s, report))
		return
	}
	text, kb := subjectView(b.t, s, report)
	b.showButtons(ctx, chatID, msg, text, kb)
}

// showButtons shows a button menu in place of msg, or of the current menu
// when msg is nil.
func (b *Bot) showButtons(ctx context.Context, chatID int64, msg *models.Message, text string, kb *models.InlineKeyboardMarkup) {
	if msg != nil {
		b.editMenu(ctx, chatID, msg.ID, text, kb)
		return
	}
	b.replaceMenu(ctx, chatID, text, kb)
}

// homeView is the button style of the subject list, with each subject's
// custom emoji (icon) on its button. The greeting and the rank are
// elsewhere: in the /start message and under «Аккаунт».
func homeView(t i18n.Strings, cat *content.Catalog, icon func(subject string) string) (string, *models.InlineKeyboardMarkup) {
	closeRow := row(button(t.Close, cbClose))
	if len(cat.Subjects) == 0 {
		return t.NoSubjects, keyboard(closeRow)
	}
	var rows [][]models.InlineKeyboardButton
	for _, s := range cat.Subjects {
		btn := button(s.Name, cbSubject+s.ID)
		btn.IconCustomEmojiID = icon(s.Name)
		rows = append(rows, row(btn))
	}
	return t.ChooseSubject, keyboard(append(rows, closeRow)...)
}

// subjectsMarkdown is the list style of the subject list: «Предметы» and
// a line per subject with its emoji, if any, and its number of grades,
// which opens it.
func (b *Bot) subjectsMarkdown(cat *content.Catalog) string {
	var sb strings.Builder
	sb.WriteString("# " + escapeMarkdown(b.t.SubjectsTitle) + "\n\n")
	if len(cat.Subjects) == 0 {
		sb.WriteString(escapeMarkdown(b.t.NoSubjects) + "\n")
	}
	for _, s := range cat.Subjects {
		n := len(s.Grades)
		label := fmt.Sprintf("%s · %d %s", s.Name, n, plural(n, b.t.GradeForms))
		sb.WriteString(richEmojiRef(b.emoji.Subject(s.Name), placeholder) + menuLink(label, cbSubject+s.ID) + "\n")
	}
	return sb.String()
}

// gradesMarkdown is the list style of a subject's grades: the subject
// with its emoji, if any, and a line per grade with its number of topics,
// which opens it.
func (b *Bot) gradesMarkdown(s *content.Subject) string {
	var sb strings.Builder
	sb.WriteString("# " + richEmojiRef(b.emoji.Subject(s.Name), placeholder) + escapeMarkdown(s.Name) + "\n\n")
	if len(s.Grades) == 0 {
		sb.WriteString(escapeMarkdown(b.t.NoGrades) + "\n")
	}
	for _, g := range s.Grades {
		n := len(g.Topics)
		label := fmt.Sprintf("%s · %d %s", g.Title(), n, plural(n, b.t.TopicForms))
		sb.WriteString(menuLink(label, cbGrade+g.ID) + "\n")
	}
	return sb.String()
}

// menuLink is a line of a rich menu that sends data when tapped.
func menuLink(label, data string) string {
	return "[" + strings.NewReplacer("[", "", "]", "").Replace(label) + "](" + render.CallbackLink(data) + ")"
}

// subjectKeyboard goes under a subject's grade list: «Сообщить об ошибке»
// when reports are on, for something wrong or missing in the subject, and
// «Назад» to the subjects.
func subjectKeyboard(t i18n.Strings, s *content.Subject, report bool) *models.InlineKeyboardMarkup {
	r := row(button(t.Back, cbHome))
	if report {
		r = row(button(t.ReportError, cbReportSubject+s.ID), button(t.Back, cbHome))
	}
	return keyboard(r)
}

// subjectView is the button style of a subject's grade list.
func subjectView(t i18n.Strings, s *content.Subject, report bool) (string, *models.InlineKeyboardMarkup) {
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
	rows = append(rows, subjectKeyboard(t, s, report).InlineKeyboard...)
	return fmt.Sprintf(t.ChooseGrade, s.Name), keyboard(rows...)
}

// showGrade shows a grade's topic list in the style settings.yml sets:
// a rich message with a link per topic, or a button per topic. In the
// button style, msg is the menu to edit; nil replaces the current menu.
// A topic opens in place of the list either way.
func (b *Bot) showGrade(ctx context.Context, chatID int64, msg *models.Message, g *content.Grade) {
	report := b.host != nil
	if !b.richLists() {
		text, kb := gradeView(b.t, g, report)
		if msg != nil {
			b.editMenu(ctx, chatID, msg.ID, text, kb)
		} else {
			b.showMenu(ctx, chatID, text, kb)
		}
		return
	}
	header := fmt.Sprintf(b.t.TopicsHeader, g.Subject.Name, g.Title())
	b.showRich(ctx, chatID, header, b.topicListMarkdown(g), gradeKeyboard(b.t, g, report))
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

// gradeKeyboard goes under the topic list: «Сообщить об ошибке» when
// reports are on, and «Назад».
func gradeKeyboard(t i18n.Strings, g *content.Grade, report bool) *models.InlineKeyboardMarkup {
	var rows [][]models.InlineKeyboardButton
	if report {
		rows = append(rows, row(button(t.ReportError, cbReportGrade+g.ID)))
	}
	return keyboard(append(rows, row(button(t.Back, cbSubject+g.Subject.ID)))...)
}

// gradeView is the button style of the topic list.
func gradeView(t i18n.Strings, g *content.Grade, report bool) (string, *models.InlineKeyboardMarkup) {
	text := fmt.Sprintf(t.TopicsHeader, g.Subject.Name, g.Title()) + "\n\n" + t.ChooseTopic
	if len(g.Topics) == 0 {
		text += "\n\n" + t.NoTopics
	}
	var rows [][]models.InlineKeyboardButton
	for _, tp := range g.Topics {
		rows = append(rows, row(button(topicLabel(tp), cbArticle+tp.Main.ID)))
	}
	rows = append(rows, gradeKeyboard(t, g, report).InlineKeyboard...)
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
		row(button(t.ReportError, cbReport+n.ID), button(t.DownloadMD, cbDownload+n.ID)),
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

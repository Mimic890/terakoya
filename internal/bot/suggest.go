package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/content"
	"github.com/Mimic890/terakoya/internal/githost"
	"github.com/Mimic890/terakoya/internal/gitrepo"
	"github.com/Mimic890/terakoya/internal/plain"
	"github.com/Mimic890/terakoya/internal/store"
	"github.com/Mimic890/terakoya/internal/suggest"
)

// Dialog states, stored in the session.
const (
	stateMenu          = "suggest_menu"           // «Исправить текст» or «Сообщить об ошибке»
	stateSection       = "suggest_section"        // which part of a long article
	stateEditText      = "suggest_edit"           // waiting for the edited text
	stateEditConfirm   = "suggest_edit_confirm"   // text received, «Отправить»
	stateReport        = "suggest_report"         // waiting for a description
	stateReportConfirm = "suggest_report_confirm" // description received, «Отправить»
	stateKind          = "suggest_kind"           // «Добавить…»: which kind of note
	stateParent        = "suggest_parent"         // a corollary: of which note
	stateNewTitle      = "suggest_new_title"
	stateNewText       = "suggest_new_text"
	stateNewImages     = "suggest_new_images" // text received, «Готово»
)

// Callback data of the dialogs.
const (
	cbFix     = "ef"
	cbReport  = "er"
	cbSection = "es:" // es:<n>
	cbSend    = "ok"
	cbCancel  = "x"
	cbOpen    = "o:" // o:<id>, opens a note without replacing the message
	cbAddNote = "ea"
	cbKind    = "ek:" // ek:<n>, index into the kind list
	cbParent  = "ep:" // ep:<note id>
)

const (
	sessionTTL = time.Hour
	maxImages  = 10
	// sectionChars is the longest plain text sent as one part of an
	// article; Telegram messages, including the user's reply, hold 4096.
	sectionChars = 3500
	// glueWindow: Telegram splits a long pasted text into several messages
	// sent within a second or two; they are joined back.
	glueWindow    = 2
	maxTitleRunes = 80
	maxLabelRunes = 40
)

var downloadClient = &http.Client{Timeout: time.Minute}

// sessionPayload is the JSON stored with a suggestion session.
type sessionPayload struct {
	NoteID  string `json:"note_id,omitempty"`
	GradeID string `json:"grade_id,omitempty"`
	// Kind and ParentID describe a new note of a topic (the main page is
	// NoteID); for a new topic Kind is empty.
	Kind     string `json:"kind,omitempty"`
	ParentID string `json:"parent_id,omitempty"`
	ShortID  string `json:"short_id"`
	// Raw is the article as the user saw it; edits are merged into it.
	Raw     string `json:"raw,omitempty"`
	Section int    `json:"section,omitempty"`
	// Links remembers the titles shown for [[links]], so the plain text
	// compared on submission is the one the user got.
	Links    map[string]string `json:"links,omitempty"`
	Text     string            `json:"text,omitempty"`
	TextDate int               `json:"text_date,omitempty"`
	Title    string            `json:"title,omitempty"`
	Images   []pendingImage    `json:"images,omitempty"`
	PromptID int               `json:"prompt_id,omitempty"`
}

type pendingImage struct {
	FileID  string `json:"file_id"`
	Ext     string `json:"ext"`
	Caption string `json:"caption,omitempty"`
}

// dialog is a loaded session.
type dialog struct {
	ses    store.Session
	p      sessionPayload
	chatID int64
}

// userLocks serializes the handling of one user's updates, so pictures sent
// as an album do not overwrite each other in the session.
type userLocks struct {
	mu    sync.Mutex
	locks map[int64]*sync.Mutex
}

func (u *userLocks) lock(id int64) func() {
	u.mu.Lock()
	if u.locks == nil {
		u.locks = map[int64]*sync.Mutex{}
	}
	l := u.locks[id]
	if l == nil {
		l = &sync.Mutex{}
		u.locks[id] = l
	}
	u.mu.Unlock()
	l.Lock()
	return l.Unlock
}

func cancelRow(t string) []models.InlineKeyboardButton { return row(button(t, cbCancel)) }

// canSuggest checks that suggestions are on and the user may make one now,
// and returns the content commit they are looking at.
func (b *Bot) canSuggest(ctx context.Context, q *models.CallbackQuery) (string, bool) {
	if b.host == nil || !b.repo.Managed() {
		b.answer(ctx, q, b.t.SuggestDisabled, true)
		return "", false
	}
	if msg := b.quotaProblem(ctx, q.From.ID); msg != "" {
		b.answer(ctx, q, msg, true)
		return "", false
	}
	b.mu.RLock()
	base := b.sha
	b.mu.RUnlock()
	if base == "" {
		b.answer(ctx, q, b.t.SuggestDisabled, true)
		return "", false
	}
	if _, err := b.store.TouchUser(ctx, q.From.ID, q.From.Username, q.From.FirstName); err != nil {
		b.log.Error("touch user", "err", err)
	}
	return base, true
}

// startSuggestion opens the «Предложить правку» menu for a note. Under a
// main page it also offers to add a theorem, a task and so on.
func (b *Bot) startSuggestion(ctx context.Context, q *models.CallbackQuery, chatID int64, n *content.Note) {
	unlock := b.userLocks.lock(q.From.ID)
	defer unlock()
	base, ok := b.canSuggest(ctx, q)
	if !ok {
		return
	}
	b.answer(ctx, q, "", false)
	d := &dialog{chatID: chatID,
		ses: store.Session{TgID: q.From.ID, State: stateMenu, BaseSHA: base, ArticlePath: n.Path},
		p:   sessionPayload{NoteID: n.ID, ShortID: suggest.ShortID(), Raw: string(n.Raw)}}
	rows := [][]models.InlineKeyboardButton{row(button(b.t.FixText, cbFix)), row(button(b.t.ReportError, cbReport))}
	text := b.t.SuggestMenu
	if n.IsMain() {
		rows = append(rows, row(button(b.t.AddNote, cbAddNote)))
		text = b.t.SuggestMenuMain
	}
	b.prompt(ctx, d, text, keyboard(append(rows, cancelRow(b.t.Cancel))...))
}

// startNewTopic asks for the title of a new topic in grade g.
func (b *Bot) startNewTopic(ctx context.Context, q *models.CallbackQuery, chatID int64, g *content.Grade) {
	unlock := b.userLocks.lock(q.From.ID)
	defer unlock()
	base, ok := b.canSuggest(ctx, q)
	if !ok {
		return
	}
	b.answer(ctx, q, "", false)
	d := &dialog{chatID: chatID,
		ses: store.Session{TgID: q.From.ID, State: stateNewTitle, BaseSHA: base},
		p:   sessionPayload{GradeID: g.ID, ShortID: suggest.ShortID()}}
	b.prompt(ctx, d, fmt.Sprintf(b.t.NewTitlePrompt, g.Subject.Name, g.Title()), keyboard(cancelRow(b.t.Cancel)))
}

// quotaProblem returns a message if the user may not suggest right now.
func (b *Bot) quotaProblem(ctx context.Context, tgID int64) string {
	open, last, err := b.store.SuggestQuota(ctx, tgID)
	if err != nil {
		b.log.Error("suggest quota", "err", err)
		return b.t.SubmitFailed
	}
	if open >= int(b.cfg.SuggestMaxOpen) {
		return fmt.Sprintf(b.t.TooManyOpen, open)
	}
	if !last.IsZero() && time.Until(last.Add(b.cfg.SuggestCooldown)) > 0 {
		return b.t.TooOften
	}
	return ""
}

// prompt sends the next question of a dialog and saves the session. The
// previous question loses its buttons, so only one «Отмена» is live.
func (b *Bot) prompt(ctx context.Context, d *dialog, text string, kb *models.InlineKeyboardMarkup) {
	b.dropButtons(ctx, d)
	if msg := b.send(ctx, d.chatID, text, kb); msg != nil {
		d.p.PromptID = msg.ID
	}
	b.save(ctx, d)
}

// note answers without buttons; the current question stays live.
func (b *Bot) note(ctx context.Context, d *dialog, text string) {
	b.send(ctx, d.chatID, text, nil)
}

func (b *Bot) dropButtons(ctx context.Context, d *dialog) {
	if d.p.PromptID == 0 {
		return
	}
	_, err := b.api.EditMessageReplyMarkup(ctx, &tg.EditMessageReplyMarkupParams{
		ChatID: d.chatID, MessageID: d.p.PromptID, ReplyMarkup: keyboard(),
	})
	if err != nil {
		b.log.Debug("drop prompt buttons", "err", err)
	}
	d.p.PromptID = 0
}

func (b *Bot) save(ctx context.Context, d *dialog) {
	data, _ := json.Marshal(d.p)
	d.ses.Payload = string(data)
	d.ses.ExpiresAt = time.Now().Add(sessionTTL)
	if err := b.store.PutSession(ctx, d.ses); err != nil {
		b.log.Error("put session", "err", err)
	}
}

// loadDialog returns the user's session. expired is set when it just
// timed out.
func (b *Bot) loadDialog(ctx context.Context, userID, chatID int64) (d *dialog, expired bool) {
	ses, err := b.store.Session(ctx, userID)
	switch {
	case errors.Is(err, store.ErrExpired):
		return nil, true
	case errors.Is(err, store.ErrNotFound):
		return nil, false
	case err != nil:
		b.log.Error("session", "err", err)
		return nil, false
	}
	d = &dialog{ses: ses, chatID: chatID}
	if err := json.Unmarshal([]byte(ses.Payload), &d.p); err != nil {
		b.log.Error("session payload", "err", err)
		b.store.DeleteSession(ctx, userID)
		return nil, false
	}
	return d, false
}

// onDialogCallback handles the buttons of an open dialog.
func (b *Bot) onDialogCallback(ctx context.Context, q *models.CallbackQuery, msg *models.Message) {
	unlock := b.userLocks.lock(q.From.ID)
	defer unlock()
	d, expired := b.loadDialog(ctx, q.From.ID, msg.Chat.ID)
	if d == nil || d.p.PromptID != 0 && d.p.PromptID != msg.ID {
		// A button of a finished or replaced dialog.
		text := ""
		if expired || d == nil {
			text = b.t.Expired
		}
		b.answer(ctx, q, text, text != "")
		b.dropButtonsOf(ctx, msg)
		return
	}
	b.answer(ctx, q, "", false)
	data := q.Data
	switch {
	case data == cbFix && d.ses.State == stateMenu:
		b.chooseFix(ctx, d)
	case data == cbReport && d.ses.State == stateMenu:
		d.ses.State = stateReport
		b.prompt(ctx, d, b.t.ReportPrompt, keyboard(cancelRow(b.t.Cancel)))
	case strings.HasPrefix(data, cbSection) && d.ses.State == stateSection:
		n, err := strconv.Atoi(strings.TrimPrefix(data, cbSection))
		if err != nil {
			return
		}
		b.sendSection(ctx, d, n)
	case data == cbSend && d.ses.State == stateEditConfirm:
		b.submitEdit(ctx, d, &q.From)
	case data == cbSend && d.ses.State == stateReportConfirm:
		b.submitReport(ctx, d, &q.From)
	case data == cbSend && d.ses.State == stateNewImages:
		b.submitNew(ctx, d, &q.From)
	case data == cbAddNote && d.ses.State == stateMenu:
		b.chooseKind(ctx, d)
	case strings.HasPrefix(data, cbKind) && d.ses.State == stateKind:
		i, err := strconv.Atoi(strings.TrimPrefix(data, cbKind))
		if kinds := b.kinds(d); err == nil && i >= 0 && i < len(kinds) {
			b.onKind(ctx, d, kinds[i])
		}
	case strings.HasPrefix(data, cbParent) && d.ses.State == stateParent:
		if n := b.catalog().Note(strings.TrimPrefix(data, cbParent)); n != nil {
			d.p.ParentID = n.ID
			b.askNoteTitle(ctx, d)
		}
	}
}

// kinds lists the kinds a new note of the dialog's topic can have: the
// usual ones, then other kind folders the topic already has.
func (b *Bot) kinds(d *dialog) []string {
	out := append([]string{}, content.Kinds...)
	n := b.dialogNote(d)
	if n == nil {
		return out
	}
	known := map[string]bool{}
	for _, k := range out {
		known[k] = true
	}
	for _, x := range n.Topic.Notes {
		if !known[x.Kind] {
			known[x.Kind] = true
			out = append(out, x.Kind)
		}
	}
	return out
}

// chooseKind asks which kind of note to add to the topic.
func (b *Bot) chooseKind(ctx context.Context, d *dialog) {
	var rows [][]models.InlineKeyboardButton
	var cur []models.InlineKeyboardButton
	for i, k := range b.kinds(d) {
		cur = append(cur, button(content.KindLabel(k), cbKind+strconv.Itoa(i)))
		if len(cur) == 2 {
			rows, cur = append(rows, cur), nil
		}
	}
	if len(cur) > 0 {
		rows = append(rows, cur)
	}
	d.ses.State = stateKind
	b.prompt(ctx, d, b.t.ChooseKind, keyboard(append(rows, cancelRow(b.t.Cancel))...))
}

// onKind continues with the title, or for a corollary first asks which
// note it belongs to.
func (b *Bot) onKind(ctx context.Context, d *dialog, kind string) {
	d.p.Kind = kind
	main := b.dialogNote(d)
	if kind != "следствия" || main == nil {
		b.askNoteTitle(ctx, d)
		return
	}
	var rows [][]models.InlineKeyboardButton
	for _, n := range main.Topic.Notes {
		if n.Kind == "следствия" {
			continue
		}
		rows = append(rows, row(button(shorten(n.KindLabel()+": "+n.Title, 60), cbParent+n.ID)))
	}
	if len(rows) == 0 {
		b.askNoteTitle(ctx, d)
		return
	}
	d.ses.State = stateParent
	b.prompt(ctx, d, b.t.ChooseParent, keyboard(append(rows, cancelRow(b.t.Cancel))...))
}

func (b *Bot) askNoteTitle(ctx context.Context, d *dialog) {
	main := b.dialogNote(d)
	if main == nil {
		b.cancel(ctx, d, b.t.NotFound)
		return
	}
	d.ses.State = stateNewTitle
	b.prompt(ctx, d, fmt.Sprintf(b.t.NewNoteTitlePrompt, content.KindLabel(d.p.Kind), main.Topic.Title()), keyboard(cancelRow(b.t.Cancel)))
}

func shorten(s string, max int) string {
	if r := []rune(s); len(r) > max {
		return strings.TrimSpace(string(r[:max-1])) + "…"
	}
	return s
}

func (b *Bot) dropButtonsOf(ctx context.Context, msg *models.Message) {
	b.dropButtons(ctx, &dialog{chatID: msg.Chat.ID, p: sessionPayload{PromptID: msg.ID}})
}

// dialogNote returns the session's note in the current catalog.
func (b *Bot) dialogNote(d *dialog) *content.Note {
	cat := b.catalog()
	if n := cat.NoteByPath(d.ses.ArticlePath); n != nil {
		return n
	}
	return cat.Note(d.p.NoteID)
}

// sections splits the session's note for editing, without its back-link
// block, which is kept as it is. The [[link]] titles are recorded on first
// use, so later calls render the same text.
func (b *Bot) sections(d *dialog) (body string, secs []plain.Section) {
	body, _ = content.SplitBacklink(string(content.CleanSource([]byte(d.p.Raw))))
	cat, a := b.catalog(), b.dialogNote(d)
	record := d.p.Links == nil
	if record {
		d.p.Links = map[string]string{}
	}
	opt := plain.Options{LinkTitle: func(target string) string {
		if !record {
			return d.p.Links[target]
		}
		title := ""
		if a != nil {
			if to, err := cat.ResolveNote(a, content.Link{Target: target, Wiki: true}); err == nil {
				title = to.Title
			}
		}
		d.p.Links[target] = title
		return title
	}}
	return body, plain.Sections(body, sectionChars, opt)
}

// chooseFix starts «Исправить текст»: a section list for long articles,
// the text itself otherwise.
func (b *Bot) chooseFix(ctx context.Context, d *dialog) {
	_, secs := b.sections(d)
	if len(secs) == 1 {
		b.sendSection(ctx, d, 0)
		return
	}
	var rows [][]models.InlineKeyboardButton
	for i, s := range secs {
		rows = append(rows, row(button(b.sectionLabel(s), cbSection+strconv.Itoa(i))))
	}
	rows = append(rows, cancelRow(b.t.Cancel))
	d.ses.State = stateSection
	b.prompt(ctx, d, b.t.ChooseSection, keyboard(rows...))
}

func (b *Bot) sectionLabel(s plain.Section) string {
	label := s.Title
	if label == "" {
		label = b.t.SectionStart
	}
	if r := []rune(label); len(r) > maxLabelRunes {
		label = strings.TrimSpace(string(r[:maxLabelRunes-1])) + "…"
	}
	if s.Part > 0 {
		label = fmt.Sprintf(b.t.SectionPart, label, s.Part)
	}
	return label
}

// sendSection sends the plain text of section n to copy and edit.
func (b *Bot) sendSection(ctx context.Context, d *dialog, n int) {
	_, secs := b.sections(d)
	if n < 0 || n >= len(secs) {
		return
	}
	b.dropButtons(ctx, d)
	d.p.Section = n
	d.ses.State = stateEditText
	text := secs[n].Plain()
	instructions := b.t.EditInstructions
	if text == "" {
		instructions = b.t.EditEmptySection
	}
	for _, part := range splitMessage(text, 4000) {
		b.send(ctx, d.chatID, part, nil)
	}
	b.prompt(ctx, d, instructions, keyboard(cancelRow(b.t.Cancel)))
}

// splitMessage cuts text into pieces of at most max runes at line breaks.
func splitMessage(text string, max int) []string {
	var out []string
	for utf8.RuneCountInString(text) > max {
		r := []rune(text)
		cut := strings.LastIndex(string(r[:max]), "\n")
		if cut <= 0 {
			cut = len(string(r[:max]))
		}
		out = append(out, strings.TrimSpace(text[:cut]))
		text = strings.TrimSpace(text[cut:])
	}
	if text != "" {
		out = append(out, text)
	}
	return out
}

// onSessionMessage handles a message while the user has an open dialog.
// It reports false if there is none.
func (b *Bot) onSessionMessage(ctx context.Context, m *models.Message) bool {
	unlock := b.userLocks.lock(m.From.ID)
	defer unlock()
	d, expired := b.loadDialog(ctx, m.From.ID, m.Chat.ID)
	if expired {
		b.send(ctx, m.Chat.ID, b.t.Expired, nil)
		return true
	}
	if d == nil {
		return false
	}

	text := strings.TrimSpace(m.Text)
	img, isImage, problem := b.imageOf(m)
	switch {
	case problem != "":
		b.note(ctx, d, problem)
		return true
	case !isImage && text == "":
		b.note(ctx, d, b.t.OnlyTextAndImages)
		return true
	}

	switch d.ses.State {
	case stateMenu, stateSection:
		b.note(ctx, d, b.t.UseButtons)

	case stateEditText, stateEditConfirm:
		if isImage {
			if b.addImage(ctx, d, img) {
				if d.ses.State == stateEditText {
					b.prompt(ctx, d, fmt.Sprintf(b.t.EditImageFirst, len(d.p.Images)), keyboard(cancelRow(b.t.Cancel)))
				} else {
					b.prompt(ctx, d, fmt.Sprintf(b.t.ImageAdded, len(d.p.Images)), b.sendKeyboard(b.t.Send))
				}
			}
			return true
		}
		if d.ses.State == stateEditConfirm && m.Date-d.p.TextDate <= glueWindow {
			text = b.glue(d, d.p.Text, text)
		}
		d.p.Text, d.p.TextDate = text, m.Date
		b.onEditText(ctx, d)

	case stateReport, stateReportConfirm:
		if isImage {
			if !b.addImage(ctx, d, img) {
				return true
			}
		} else {
			d.p.Text = strings.TrimSpace(d.p.Text + "\n\n" + text)
		}
		d.ses.State = stateReportConfirm
		b.prompt(ctx, d, b.t.ReportReceived, b.sendKeyboard(b.t.Send))

	case stateNewTitle:
		if isImage {
			b.note(ctx, d, b.t.NewTitleFirst)
			return true
		}
		b.onNewTitle(ctx, d, text)

	case stateNewText:
		if isImage {
			if b.addImage(ctx, d, img) {
				b.prompt(ctx, d, fmt.Sprintf(b.t.NewImageFirst, len(d.p.Images)), keyboard(cancelRow(b.t.Cancel)))
			}
			return true
		}
		d.p.Text, d.p.TextDate = text, m.Date
		d.ses.State = stateNewImages
		b.prompt(ctx, d, b.t.NewImagesPrompt, b.sendKeyboard(b.t.Done))

	case stateNewImages:
		if isImage {
			if b.addImage(ctx, d, img) {
				b.prompt(ctx, d, fmt.Sprintf(b.t.NewImageAdded, len(d.p.Images)), b.sendKeyboard(b.t.Done))
			}
			return true
		}
		sep := "\n\n"
		if m.Date-d.p.TextDate <= glueWindow {
			sep = "\n" // the rest of a text Telegram split
		}
		d.p.Text, d.p.TextDate = d.p.Text+sep+text, m.Date
		b.prompt(ctx, d, b.t.NewMoreText, b.sendKeyboard(b.t.Done))

	default:
		b.store.DeleteSession(ctx, m.From.ID)
		return false
	}
	return true
}

func (b *Bot) sendKeyboard(label string) *models.InlineKeyboardMarkup {
	return keyboard(row(button(label, cbSend)), cancelRow(b.t.Cancel))
}

// imageOf extracts a picture from a photo or an image file. A message that
// is neither reports isImage false; an unusable picture returns a problem.
func (b *Bot) imageOf(m *models.Message) (img pendingImage, isImage bool, problem string) {
	caption := strings.TrimSpace(m.Caption)
	switch {
	case len(m.Photo) > 0:
		ph := m.Photo[len(m.Photo)-1]
		if int64(ph.FileSize) > b.cfg.ImageMaxBytes {
			return img, true, b.t.ImageTooLarge
		}
		return pendingImage{FileID: ph.FileID, Ext: ".jpg", Caption: caption}, true, ""
	case m.Document != nil:
		ext := suggest.ImageExt(m.Document.FileName, m.Document.MimeType)
		if ext == "" {
			return img, false, b.t.OnlyTextAndImages
		}
		if m.Document.FileSize > b.cfg.ImageMaxBytes {
			return img, true, b.t.ImageTooLarge
		}
		return pendingImage{FileID: m.Document.FileID, Ext: ext, Caption: caption}, true, ""
	case m.Sticker != nil, m.Video != nil, m.Voice != nil, m.Audio != nil, m.Animation != nil, m.VideoNote != nil:
		return img, false, b.t.OnlyTextAndImages
	}
	return img, false, ""
}

func (b *Bot) addImage(ctx context.Context, d *dialog, img pendingImage) bool {
	if len(d.p.Images) >= maxImages {
		b.note(ctx, d, fmt.Sprintf(b.t.TooManyImages, maxImages))
		return false
	}
	d.p.Images = append(d.p.Images, img)
	return true
}

// glue joins two parts of a text Telegram split. The split drops the
// whitespace at the cut, so it is unknown whether it fell between
// paragraphs, between lines or inside a line; the join that matches the
// original section best wins.
func (b *Bot) glue(d *dialog, first, second string) string {
	body, secs := b.sections(d)
	if d.p.Section >= len(secs) {
		return first + "\n\n" + second
	}
	best, bestDist := "", -1
	for _, sep := range []string{"\n\n", "\n", " "} {
		text := first + sep + second
		if dist := plain.Merge(body, secs[d.p.Section], text).Distance(); bestDist < 0 || dist < bestDist {
			best, bestDist = text, dist
		}
	}
	return best
}

// onEditText checks the edited text against the section and asks to send.
func (b *Bot) onEditText(ctx context.Context, d *dialog) {
	if int64(len(d.p.Text)) > b.cfg.SuggestMaxBytes {
		d.ses.State = stateEditText
		b.prompt(ctx, d, b.t.TextTooLong, keyboard(cancelRow(b.t.Cancel)))
		return
	}
	body, secs := b.sections(d)
	if d.p.Section >= len(secs) {
		b.cancel(ctx, d, b.t.SubmitFailed)
		return
	}
	r := plain.Merge(body, secs[d.p.Section], d.p.Text)
	switch {
	case r.Unchanged:
		d.ses.State = stateEditText
		b.prompt(ctx, d, b.t.EditUnchanged, keyboard(cancelRow(b.t.Cancel)))
	case r.Shrunk:
		d.ses.State = stateEditConfirm
		b.prompt(ctx, d, b.t.EditShrunk, b.sendKeyboard(b.t.SendAnyway))
	default:
		d.ses.State = stateEditConfirm
		b.prompt(ctx, d, b.t.EditReceived, b.sendKeyboard(b.t.Send))
	}
}

// onNewTitle checks the title of a new topic or note. Its file name must
// not be used anywhere in the vault yet.
func (b *Bot) onNewTitle(ctx context.Context, d *dialog, title string) {
	title = strings.Join(strings.Fields(title), " ")
	if utf8.RuneCountInString(title) > maxTitleRunes {
		b.prompt(ctx, d, b.t.NewTitleTooLong, keyboard(cancelRow(b.t.Cancel)))
		return
	}
	name := content.SafeFileName(title)
	if name == "" {
		b.prompt(ctx, d, b.t.NameInvalid, keyboard(cancelRow(b.t.Cancel)))
		return
	}
	cat := b.catalog()
	for _, p := range cat.FileNamed(name + ".md") {
		if n := cat.NoteByPath(p); n != nil {
			b.prompt(ctx, d, fmt.Sprintf(b.t.NewTitleTaken, n.Title), keyboard(
				row(button(fmt.Sprintf(b.t.OpenNamed, shorten(n.Title, 40)), cbOpen+n.ID)),
				cancelRow(b.t.Cancel),
			))
			return
		}
	}
	if d.p.Kind == "" && cat.Grade(d.p.GradeID) == nil || d.p.Kind != "" && b.dialogNote(d) == nil {
		b.cancel(ctx, d, b.t.NotFound)
		return
	}
	d.p.Title = title
	d.ses.State = stateNewText
	b.prompt(ctx, d, b.t.NewTextPrompt, keyboard(cancelRow(b.t.Cancel)))
}

// cancel ends the dialog with a message.
func (b *Bot) cancel(ctx context.Context, d *dialog, text string) {
	b.dropButtons(ctx, d)
	if err := b.store.DeleteSession(ctx, d.ses.TgID); err != nil {
		b.log.Error("delete session", "err", err)
	}
	b.send(ctx, d.chatID, text, nil)
}

// cancelSuggestion handles «Отмена» and /cancel.
func (b *Bot) cancelSuggestion(ctx context.Context, chatID, userID int64) {
	unlock := b.userLocks.lock(userID)
	defer unlock()
	d, _ := b.loadDialog(ctx, userID, chatID)
	if d == nil {
		b.send(ctx, chatID, b.t.NothingToCancel, nil)
		return
	}
	b.cancel(ctx, d, b.t.Cancelled)
}

// submitting checks the rate limits again and shows «Отправляю…». It
// returns the message to replace with the outcome, or 0 if refused.
func (b *Bot) submitting(ctx context.Context, d *dialog) int {
	if msg := b.quotaProblem(ctx, d.ses.TgID); msg != "" {
		b.cancel(ctx, d, msg)
		return 0
	}
	b.dropButtons(ctx, d)
	b.save(ctx, d)
	msg := b.send(ctx, d.chatID, b.t.Submitting, nil)
	if msg == nil {
		return 0
	}
	return msg.ID
}

// finish replaces «Отправляю…» with the outcome and ends the dialog on
// success. On failure the dialog stays, so the user can press «Отправить»
// again.
func (b *Bot) finish(ctx context.Context, d *dialog, msgID int, err error, done string) {
	text := done
	if err != nil {
		b.log.Error("submit suggestion", "user", d.ses.TgID, "state", d.ses.State, "err", err)
		text = b.t.SubmitFailed
		b.prompt(ctx, d, text, b.sendKeyboard(b.t.Send))
		b.deleteMessage(ctx, d.chatID, msgID)
		return
	}
	if err := b.store.DeleteSession(ctx, d.ses.TgID); err != nil {
		b.log.Error("delete session", "err", err)
	}
	if _, err := b.api.EditMessageText(ctx, &tg.EditMessageTextParams{ChatID: d.chatID, MessageID: msgID, Text: text}); err != nil {
		b.log.Debug("edit message", "err", err)
		b.send(ctx, d.chatID, text, nil)
	}
}

func (b *Bot) deleteMessage(ctx context.Context, chatID int64, msgID int) {
	if _, err := b.api.DeleteMessage(ctx, &tg.DeleteMessageParams{ChatID: chatID, MessageID: msgID}); err == nil {
		b.msgs.gone(ctx, chatID, msgID)
	}
}

func submitter(u *models.User) suggest.Submitter {
	return suggest.Submitter{TgID: u.ID, Username: u.Username, FirstName: u.FirstName}
}

// submitEdit merges the edited text into the article as the user saw it
// and opens a pull request from the commit they saw.
func (b *Bot) submitEdit(ctx context.Context, d *dialog, from *models.User) {
	a := b.dialogNote(d)
	if a == nil {
		b.cancel(ctx, d, b.t.NotFound)
		return
	}
	body, secs := b.sections(d)
	if d.p.Section >= len(secs) {
		b.cancel(ctx, d, b.t.SubmitFailed)
		return
	}
	sec := secs[d.p.Section]
	merged := plain.Merge(body, sec, d.p.Text)
	_, tail := content.SplitBacklink(string(content.CleanSource([]byte(d.p.Raw))))
	data := body[:sec.Start] + merged.Source + body[sec.End:] + tail
	res, err := suggest.Edit(b.catalog(), a, []byte(data), b.cfg.SuggestMaxBytes)
	var prob *suggest.Problem
	if errors.As(err, &prob) && prob.Code == suggest.TooLarge {
		d.ses.State = stateEditText
		b.prompt(ctx, d, b.t.TextTooLong, keyboard(cancelRow(b.t.Cancel)))
		return
	}
	msgID := b.submitting(ctx, d)
	if msgID == 0 {
		return
	}
	if err == nil {
		section := ""
		if len(secs) > 1 {
			section = b.sectionLabel(sec)
		}
		ch := suggest.Change{Kind: suggest.KindEdit, Title: res.Title, Section: section, Text: d.p.Text,
			Merge: &merged, Issues: res.Issues}
		_, err = b.openPullRequest(ctx, d, from, a.Topic.Path(), res, ch)
	}
	b.finish(ctx, d, msgID, err, b.t.SubmittedEdit)
}

// submitNew opens a pull request with a new topic, or a new note of a
// topic together with the main page that links it.
func (b *Bot) submitNew(ctx context.Context, d *dialog, from *models.User) {
	cat := b.catalog()
	var res *suggest.Result
	var err error
	var topicDir string
	if d.p.Kind == "" {
		g := cat.Grade(d.p.GradeID)
		if g == nil {
			b.cancel(ctx, d, b.t.NotFound)
			return
		}
		res, err = suggest.NewTopic(cat, g, d.p.Title, d.p.Text, b.cfg.SuggestMaxBytes)
		if err == nil {
			topicDir = path.Dir(res.Path)
		}
	} else {
		main := b.dialogNote(d)
		if main == nil {
			b.cancel(ctx, d, b.t.NotFound)
			return
		}
		res, err = suggest.NewNote(cat, main.Topic, d.p.Kind, cat.Note(d.p.ParentID), d.p.Title, d.p.Text, b.cfg.SuggestMaxBytes)
		topicDir = main.Topic.Path()
	}
	var prob *suggest.Problem
	if errors.As(err, &prob) {
		switch prob.Code {
		case suggest.TooLarge:
			b.prompt(ctx, d, b.t.TextTooLong, b.sendKeyboard(b.t.Done))
			return
		case suggest.TakenName:
			// Someone added a note with this name meanwhile.
			d.ses.State = stateNewTitle
			b.prompt(ctx, d, b.t.NameTaken, keyboard(cancelRow(b.t.Cancel)))
			return
		}
	}
	msgID := b.submitting(ctx, d)
	if msgID == 0 {
		return
	}
	done := fmt.Sprintf(b.t.SubmittedNew, d.p.Title)
	if err == nil {
		ch := suggest.Change{Kind: suggest.KindNew, NoteKind: d.p.Kind, Title: res.Title, Text: d.p.Text, Issues: res.Issues}
		_, err = b.openPullRequest(ctx, d, from, topicDir, res, ch)
		if d.p.Kind != "" {
			done = fmt.Sprintf(b.t.SubmittedNote, d.p.Title)
		}
	}
	b.finish(ctx, d, msgID, err, done)
}

// openPullRequest commits the files and the pictures (into the topic's
// _assets) on a new branch whose parent is the commit the user saw, and
// opens a pull request.
func (b *Bot) openPullRequest(ctx context.Context, d *dialog, from *models.User, topicDir string,
	res *suggest.Result, ch suggest.Change) (githost.PR, error) {
	articlePath, err := suggest.RepoPath(res.Path)
	if err != nil {
		return githost.PR{}, err
	}
	ch.RepoPath, ch.BaseSHA, ch.ShortID, ch.By = articlePath, d.ses.BaseSHA, d.p.ShortID, submitter(from)
	branch := suggest.Branch(from.ID, res.Path, ch.ShortID)

	var changes []gitrepo.Change
	for _, f := range res.Files() {
		if _, err := suggest.RepoPath(f.Path); err != nil {
			return githost.PR{}, err
		}
		changes = append(changes, gitrepo.Change{Path: f.Path, Data: f.Data})
		if f.Path != articlePath {
			ch.Files = append(ch.Files, f.Path)
		}
	}
	pics, err := b.pictureChanges(ctx, d, topicDir+"/"+content.AssetsDir, res.Path)
	if err != nil {
		return githost.PR{}, err
	}
	for _, p := range pics {
		changes = append(changes, p.change)
		ch.Pictures = append(ch.Pictures, suggest.Picture{Path: p.change.Path, Caption: p.caption,
			URL: b.host.RawURL(branch, p.change.Path)})
	}

	if err := b.commit(ctx, d, branch, ch, changes); err != nil {
		return githost.PR{}, err
	}
	pr, err := b.host.OpenPR(ctx, githost.NewPR{Title: ch.PRTitle(), Body: ch.PRBody(), Head: branch, Base: b.cfg.RepoBranch})
	if err != nil {
		return githost.PR{}, err
	}
	kind := store.KindEdit
	if ch.Kind == suggest.KindNew {
		kind = store.KindNew
	}
	if err := b.record(ctx, d, from, kind, articlePath, res.Title, branch, pr); err != nil {
		return githost.PR{}, err
	}
	b.log.Info("suggestion submitted", "user", from.ID, "branch", branch, "pr", pr.Number, "base", d.ses.BaseSHA)
	return pr, nil
}

type picture struct {
	change  gitrepo.Change
	caption string
}

// pictureChanges downloads the user's pictures and places them in the
// topic's _assets folder.
func (b *Bot) pictureChanges(ctx context.Context, d *dialog, assets, articlePath string) ([]picture, error) {
	var out []picture
	for i, img := range d.p.Images {
		name := suggest.ImageName(articlePath, d.p.ShortID, i+1, img.Ext)
		rel, err := suggest.RepoPath(assets + "/" + name)
		if err != nil {
			return nil, err
		}
		data, err := b.download(ctx, img.FileID, b.cfg.ImageMaxBytes)
		if err != nil {
			return nil, fmt.Errorf("download picture %d: %w", i+1, err)
		}
		out = append(out, picture{gitrepo.Change{Path: rel, Data: data}, img.Caption})
	}
	return out, nil
}

func (b *Bot) commit(ctx context.Context, d *dialog, branch string, ch suggest.Change, changes []gitrepo.Change) error {
	author := gitrepo.Author{Name: ch.By.Handle(), Email: fmt.Sprintf("tg-%d@users.terakoya.invalid", ch.By.TgID)}
	committer := gitrepo.Author{Name: b.cfg.GitAuthorName, Email: b.cfg.GitAuthorEmail}
	_, err := b.repo.CommitAndPush(ctx, d.ses.BaseSHA, branch, ch.CommitMessage(), author, committer, changes)
	return err
}

func (b *Bot) record(ctx context.Context, d *dialog, from *models.User, kind, articlePath, title, branch string, pr githost.PR) error {
	sg := &store.Suggestion{ShortID: d.p.ShortID, TgID: from.ID, Kind: kind, ArticlePath: articlePath, Title: title,
		BaseSHA: d.ses.BaseSHA, Branch: branch, PRNumber: pr.Number, PRURL: pr.URL}
	if err := b.store.CreateSuggestion(ctx, sg); err != nil {
		return fmt.Errorf("store suggestion %d: %w", pr.Number, err)
	}
	return nil
}

// submitReport opens an issue with the user's description. Pictures become
// issue attachments, or, on hosts without an attachment API, files on a
// report/ branch.
func (b *Bot) submitReport(ctx context.Context, d *dialog, from *models.User) {
	a := b.dialogNote(d)
	if a == nil {
		b.cancel(ctx, d, b.t.NotFound)
		return
	}
	msgID := b.submitting(ctx, d)
	if msgID == 0 {
		return
	}
	b.finish(ctx, d, msgID, b.openIssue(ctx, d, from, a), b.t.SubmittedReport)
}

func (b *Bot) openIssue(ctx context.Context, d *dialog, from *models.User, a *content.Note) error {
	articlePath, err := suggest.RepoPath(d.ses.ArticlePath)
	if err != nil {
		return err
	}
	ch := suggest.Change{Kind: suggest.KindReport, Title: a.Title, RepoPath: articlePath, BaseSHA: d.ses.BaseSHA,
		ShortID: d.p.ShortID, By: submitter(from), Text: d.p.Text, AcceptLabel: b.cfg.AcceptedLabel,
		ArticleURL: b.host.FileURL(d.ses.BaseSHA, articlePath), CurrentURL: b.host.FileURL(b.cfg.RepoBranch, articlePath)}
	issue, err := b.host.OpenIssue(ctx, ch.PRTitle(), ch.IssueBody())
	if err != nil {
		return err
	}
	branch := ""
	if len(d.p.Images) > 0 {
		pics, err := b.pictureChanges(ctx, d, a.Topic.Path()+"/"+content.AssetsDir, d.ses.ArticlePath)
		if err != nil {
			return err
		}
		branch, err = b.attachPictures(ctx, d, issue.Number, &ch, pics)
		if err != nil {
			return err
		}
		if err := b.host.EditIssue(ctx, issue.Number, ch.IssueBody()); err != nil {
			return err
		}
	}
	if err := b.record(ctx, d, from, store.KindReport, articlePath, a.Title, branch, issue); err != nil {
		return err
	}
	b.log.Info("error report submitted", "user", from.ID, "issue", issue.Number, "pictures", len(d.p.Images))
	return nil
}

// attachPictures uploads pictures to the issue and adds them to ch. It
// returns the branch used when the host cannot take attachments.
func (b *Bot) attachPictures(ctx context.Context, d *dialog, number int, ch *suggest.Change, pics []picture) (string, error) {
	for _, p := range pics {
		name := p.change.Path[strings.LastIndex(p.change.Path, "/")+1:]
		u, err := b.host.Attach(ctx, number, name, p.change.Data)
		if errors.Is(err, githost.ErrUnsupported) {
			return b.picturesOnBranch(ctx, d, ch, pics)
		}
		if err != nil {
			return "", err
		}
		ch.Pictures = append(ch.Pictures, suggest.Picture{Caption: p.caption, URL: u})
	}
	return "", nil
}

func (b *Bot) picturesOnBranch(ctx context.Context, d *dialog, ch *suggest.Change, pics []picture) (string, error) {
	branch := "report/" + strings.TrimPrefix(suggest.Branch(ch.By.TgID, d.ses.ArticlePath, ch.ShortID), "suggest/")
	var changes []gitrepo.Change
	ch.Pictures = nil
	for _, p := range pics {
		changes = append(changes, p.change)
		ch.Pictures = append(ch.Pictures, suggest.Picture{Path: p.change.Path, Caption: p.caption, URL: b.host.RawURL(branch, p.change.Path)})
	}
	return branch, b.commit(ctx, d, branch, *ch, changes)
}

// download fetches a Telegram file, refusing anything over max bytes.
func (b *Bot) download(ctx context.Context, fileID string, max int64) ([]byte, error) {
	f, err := b.api.GetFile(ctx, &tg.GetFileParams{FileID: fileID})
	if err != nil {
		return nil, err
	}
	if f.FileSize > max {
		return nil, fmt.Errorf("file is %d bytes, limit %d", f.FileSize, max)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.api.FileDownloadLink(f), nil)
	if err != nil {
		return nil, err
	}
	resp, err := downloadClient.Do(req)
	if err != nil {
		// The URL contains the bot token; keep it out of errors and logs.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return nil, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("file is larger than %d bytes", max)
	}
	return data, nil
}

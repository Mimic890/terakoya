package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/content"
	"github.com/Mimic890/terakoya/internal/githost"
	"github.com/Mimic890/terakoya/internal/gitrepo"
	"github.com/Mimic890/terakoya/internal/store"
	"github.com/Mimic890/terakoya/internal/suggest"
)

// «Сообщить об ошибке» under a note, or under a topic list for something
// missing in the grade: the user describes it, may add pictures, and the
// report becomes an issue.

// Dialog states, stored in the session.
const (
	stateReport        = "suggest_report"         // waiting for a description
	stateReportConfirm = "suggest_report_confirm" // description received, «Отправить»
)

// Callback data of the dialogs.
const (
	cbSend   = "ok"
	cbCancel = "x"
	cbOpen   = "o:" // o:<id>, opens a note without replacing the message
)

const (
	sessionTTL = time.Hour
	maxImages  = 10
)

var downloadClient = &http.Client{Timeout: time.Minute}

// sessionPayload is the JSON stored with a report session. A report is
// about the note NoteID, or about the grade GradeID.
type sessionPayload struct {
	NoteID   string         `json:"note_id,omitempty"`
	GradeID  string         `json:"grade_id,omitempty"`
	ShortID  string         `json:"short_id"`
	Text     string         `json:"text,omitempty"`
	Images   []pendingImage `json:"images,omitempty"`
	PromptID int            `json:"prompt_id,omitempty"`
	// Msgs are all messages of the dialog, the bot's and the user's, which
	// «Отмена» deletes. OriginMsg is the note or list the dialog started
	// from, which is replaced when the user goes back to it.
	Msgs      []int `json:"msgs,omitempty"`
	OriginMsg int   `json:"origin_msg,omitempty"`
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
	ended  bool // the session was deleted
}

// track adds a message to the dialog.
func (d *dialog) track(msg *models.Message) {
	if msg != nil {
		d.p.Msgs = append(d.p.Msgs, msg.ID)
	}
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

// canReport checks that reports are on and the user may send one now,
// and returns the content commit they are looking at.
func (b *Bot) canReport(ctx context.Context, q *models.CallbackQuery) (string, bool) {
	if b.host == nil || !b.repo.Managed() {
		b.answer(ctx, q, b.t.ReportDisabled, true)
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
		b.answer(ctx, q, b.t.ReportDisabled, true)
		return "", false
	}
	if _, err := b.store.TouchUser(ctx, q.From.ID, q.From.Username, q.From.FirstName); err != nil {
		b.log.Error("touch user", "err", err)
	}
	return base, true
}

// startReport asks what is wrong with a note.
func (b *Bot) startReport(ctx context.Context, q *models.CallbackQuery, chatID int64, n *content.Note) {
	unlock := b.userLocks.lock(q.From.ID)
	defer unlock()
	base, ok := b.canReport(ctx, q)
	if !ok {
		return
	}
	b.answer(ctx, q, "", false)
	d := &dialog{chatID: chatID,
		ses: store.Session{TgID: q.From.ID, State: stateReport, BaseSHA: base, ArticlePath: n.Path},
		p:   sessionPayload{NoteID: n.ID, ShortID: suggest.ShortID(), OriginMsg: q.Message.Message.ID}}
	b.prompt(ctx, d, b.t.ReportPrompt, keyboard(cancelRow(b.t.Cancel)))
}

// startGradeReport asks what is missing in grade g.
func (b *Bot) startGradeReport(ctx context.Context, q *models.CallbackQuery, chatID int64, g *content.Grade) {
	unlock := b.userLocks.lock(q.From.ID)
	defer unlock()
	base, ok := b.canReport(ctx, q)
	if !ok {
		return
	}
	b.answer(ctx, q, "", false)
	d := &dialog{chatID: chatID,
		ses: store.Session{TgID: q.From.ID, State: stateReport, BaseSHA: base},
		p:   sessionPayload{GradeID: g.ID, ShortID: suggest.ShortID(), OriginMsg: q.Message.Message.ID}}
	b.prompt(ctx, d, fmt.Sprintf(b.t.GradeReportPrompt, g.Subject.Name, g.Title()), keyboard(cancelRow(b.t.Cancel)))
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
		d.track(msg)
	}
	b.save(ctx, d)
}

// note answers without buttons; the current question stays live.
func (b *Bot) note(ctx context.Context, d *dialog, text string) {
	d.track(b.send(ctx, d.chatID, text, nil))
	b.save(ctx, d)
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

// onDialogCallback handles «Отправить» of an open dialog.
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
	if q.Data == cbSend && d.ses.State == stateReportConfirm {
		b.submitReport(ctx, d, &q.From)
	}
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
	if d.ses.State != stateReport && d.ses.State != stateReportConfirm {
		// A dialog of an earlier version, such as an edit.
		b.endDialog(ctx, d)
		return false
	}
	// The user's replies belong to the dialog too; whatever happens below,
	// the dialog is saved with them unless it ended.
	d.track(m)
	defer func() {
		if !d.ended {
			b.save(ctx, d)
		}
	}()

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
	if isImage {
		if !b.addImage(ctx, d, img) {
			return true
		}
	} else {
		d.p.Text = strings.TrimSpace(d.p.Text + "\n\n" + text)
	}
	d.ses.State = stateReportConfirm
	b.prompt(ctx, d, b.t.ReportReceived, b.sendKeyboard(b.t.Send))
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

// cancel ends the dialog with a message, when the bot has to stop it.
func (b *Bot) cancel(ctx context.Context, d *dialog, text string) {
	b.dropButtons(ctx, d)
	b.endDialog(ctx, d)
	b.send(ctx, d.chatID, text, nil)
}

func (b *Bot) endDialog(ctx context.Context, d *dialog) {
	if err := b.store.DeleteSession(ctx, d.ses.TgID); err != nil {
		b.log.Error("delete session", "err", err)
	}
	d.ended = true
}

// cancelDialog handles «Отмена» and /cancel in any dialog, /emoji_setup
// included: every message of the dialog goes, and the user is back where
// it started. Without a dialog, there is nothing to do.
// It reports whether there was a dialog.
func (b *Bot) cancelDialog(ctx context.Context, chatID, userID int64) bool {
	unlock := b.userLocks.lock(userID)
	defer unlock()
	d, _ := b.loadDialog(ctx, userID, chatID)
	if d == nil {
		return false
	}
	b.endDialog(ctx, d)
	b.dropMessages(ctx, chatID, d.p.Msgs)
	b.goBack(ctx, d)
	return true
}

// dropMessages deletes messages in the background, all at once. They are
// forgotten right away, so a menu above them counts as the last message
// again.
func (b *Bot) dropMessages(ctx context.Context, chatID int64, ids []int) {
	for _, id := range ids {
		b.msgs.gone(ctx, chatID, id)
		b.later(ctx, func(ctx context.Context) {
			if _, err := b.api.DeleteMessage(ctx, &tg.DeleteMessageParams{ChatID: chatID, MessageID: id}); err != nil {
				b.log.Debug("delete dialog message", "msg", id, "err", err)
			}
		})
	}
}

// goBack returns the user to where a cancelled dialog started: its note,
// re-sent in place of the old one, or the topic list for a report about
// the grade. A note that left the vault falls back to its topic list, then to
// the subject list. A dialog without an origin (/emoji_setup) only goes.
func (b *Bot) goBack(ctx context.Context, d *dialog) {
	cat := b.catalog()
	var g *content.Grade
	switch {
	case d.p.NoteID != "" || d.ses.ArticlePath != "":
		if n := b.dialogNote(d); n != nil {
			if b.sendArticle(ctx, d.chatID, n, false) && d.p.OriginMsg != 0 {
				b.retireLater(ctx, d.chatID, d.p.OriginMsg)
			}
			return
		}
		g = gradeOfPath(cat, d.ses.ArticlePath)
	case d.p.GradeID != "":
		g = cat.Grade(d.p.GradeID)
	default:
		return
	}
	if g != nil {
		b.showGrade(ctx, d.chatID, nil, g)
		return
	}
	text, kb := homeView(b.t, cat, b.emoji.Subject)
	b.replaceMenu(ctx, d.chatID, text, kb)
}

// gradeOfPath finds the grade a vault path lies in, if it still exists.
func gradeOfPath(cat *content.Catalog, p string) *content.Grade {
	for _, s := range cat.Subjects {
		for _, g := range s.Grades {
			if strings.HasPrefix(p, g.Path()+"/") {
				return g
			}
		}
	}
	return nil
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
	b.endDialog(ctx, d)
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

// submitReport opens an issue with the user's description. Pictures become
// issue attachments, or, on hosts without an attachment API, files on a
// report/ branch.
func (b *Bot) submitReport(ctx context.Context, d *dialog, from *models.User) {
	r, assets, ok := b.reportOf(d, from)
	if !ok {
		b.cancel(ctx, d, b.t.NotFound)
		return
	}
	msgID := b.submitting(ctx, d)
	if msgID == 0 {
		return
	}
	b.finish(ctx, d, msgID, b.openIssue(ctx, d, from, r, assets), b.t.SubmittedReport)
}

// reportOf describes the dialog's report and names the folder its pictures
// would go to. It fails if the note or grade left the vault.
func (b *Bot) reportOf(d *dialog, from *models.User) (r suggest.Report, assets string, ok bool) {
	r = suggest.Report{BaseSHA: d.ses.BaseSHA, ShortID: d.p.ShortID, By: submitter(from), Text: d.p.Text,
		AcceptLabel: b.cfg.AcceptedLabel}
	if d.p.GradeID != "" {
		g := b.catalog().Grade(d.p.GradeID)
		if g == nil {
			return r, "", false
		}
		r.Grade, r.Title, r.RepoPath = true, fmt.Sprintf(b.t.TopicsHeader, g.Subject.Name, g.Title()), g.Path()
		return r, g.Path() + "/" + content.AssetsDir, true
	}
	a := b.dialogNote(d)
	if a == nil {
		return r, "", false
	}
	r.Title, r.RepoPath = a.Title, d.ses.ArticlePath
	return r, a.Topic.Path() + "/" + content.AssetsDir, true
}

func (b *Bot) openIssue(ctx context.Context, d *dialog, from *models.User, r suggest.Report, assets string) error {
	if _, err := suggest.RepoPath(r.RepoPath); err != nil {
		return err
	}
	r.ArticleURL, r.CurrentURL = b.host.FileURL(d.ses.BaseSHA, r.RepoPath), b.host.FileURL(b.cfg.RepoBranch, r.RepoPath)
	issue, err := b.host.OpenIssue(ctx, r.IssueTitle(), r.IssueBody())
	if err != nil {
		return err
	}
	branch := ""
	if len(d.p.Images) > 0 {
		pics, err := b.pictureChanges(ctx, d, assets, r.RepoPath)
		if err != nil {
			return err
		}
		branch, err = b.attachPictures(ctx, d, issue.Number, &r, pics)
		if err != nil {
			return err
		}
		if err := b.host.EditIssue(ctx, issue.Number, r.IssueBody()); err != nil {
			return err
		}
	}
	sg := &store.Suggestion{ShortID: d.p.ShortID, TgID: from.ID, Kind: store.KindReport, ArticlePath: r.RepoPath,
		Title: r.Title, BaseSHA: d.ses.BaseSHA, Branch: branch, PRNumber: issue.Number, PRURL: issue.URL}
	if err := b.store.CreateSuggestion(ctx, sg); err != nil {
		return fmt.Errorf("store report %d: %w", issue.Number, err)
	}
	b.log.Info("error report submitted", "user", from.ID, "issue", issue.Number, "grade", r.Grade, "pictures", len(d.p.Images))
	return nil
}

type picture struct {
	change  gitrepo.Change
	caption string
}

// pictureChanges downloads the user's pictures and names them in the
// assets folder of the note's topic, or of the grade.
func (b *Bot) pictureChanges(ctx context.Context, d *dialog, assets, about string) ([]picture, error) {
	var out []picture
	for i, img := range d.p.Images {
		name := suggest.ImageName(about, d.p.ShortID, i+1, img.Ext)
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

// attachPictures uploads pictures to the issue and adds them to r. It
// returns the branch used when the host cannot take attachments.
func (b *Bot) attachPictures(ctx context.Context, d *dialog, number int, r *suggest.Report, pics []picture) (string, error) {
	for _, p := range pics {
		name := p.change.Path[strings.LastIndex(p.change.Path, "/")+1:]
		u, err := b.host.Attach(ctx, number, name, p.change.Data)
		if errors.Is(err, githost.ErrUnsupported) {
			return b.picturesOnBranch(ctx, d, r, pics)
		}
		if err != nil {
			return "", err
		}
		r.Pictures = append(r.Pictures, suggest.Picture{Caption: p.caption, URL: u})
	}
	return "", nil
}

// picturesOnBranch commits the pictures on a report/ branch from the
// commit the user saw.
func (b *Bot) picturesOnBranch(ctx context.Context, d *dialog, r *suggest.Report, pics []picture) (string, error) {
	branch := "report/" + strings.TrimPrefix(suggest.Branch(r.By.TgID, r.RepoPath, r.ShortID), "suggest/")
	var changes []gitrepo.Change
	r.Pictures = nil
	for _, p := range pics {
		changes = append(changes, p.change)
		r.Pictures = append(r.Pictures, suggest.Picture{Path: p.change.Path, Caption: p.caption, URL: b.host.RawURL(branch, p.change.Path)})
	}
	author := gitrepo.Author{Name: r.By.Handle(), Email: fmt.Sprintf("tg-%d@users.terakoya.invalid", r.By.TgID)}
	committer := gitrepo.Author{Name: b.cfg.GitAuthorName, Email: b.cfg.GitAuthorEmail}
	_, err := b.repo.CommitAndPush(ctx, d.ses.BaseSHA, branch, r.CommitMessage(), author, committer, changes)
	return branch, err
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

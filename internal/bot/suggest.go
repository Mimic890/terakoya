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

const (
	stateEdit  = "suggest_edit"
	stateNew   = "suggest_new"
	sessionTTL = time.Hour
	maxImages  = 10
	cbCancel   = "x"
)

var downloadClient = &http.Client{Timeout: time.Minute}

// sessionPayload is the JSON stored with a suggestion session.
type sessionPayload struct {
	ArticleID string         `json:"article_id,omitempty"`
	Grade     string         `json:"grade,omitempty"` // <subject>:<grade>
	Images    []pendingImage `json:"images,omitempty"`
}

type pendingImage struct {
	Name   string `json:"name"`
	FileID string `json:"file_id"`
}

func (p sessionPayload) imageNames() []string {
	var out []string
	for _, img := range p.Images {
		out = append(out, img.Name)
	}
	return out
}

// userLocks serializes the handling of one user's messages, so images sent
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

// startSuggestion opens a session for editing an article or adding a topic
// to a grade, after checking the rate limits.
func (b *Bot) startSuggestion(ctx context.Context, q *models.CallbackQuery, chatID int64, state string, a *content.Article, g *content.Grade) {
	if b.host == nil || !b.repo.Managed() {
		b.answer(ctx, q, b.t.SuggestDisabled, true)
		return
	}
	if msg := b.quotaProblem(ctx, q.From.ID); msg != "" {
		b.answer(ctx, q, msg, true)
		return
	}
	b.mu.RLock()
	base := b.sha
	b.mu.RUnlock()
	if base == "" {
		b.answer(ctx, q, b.t.SuggestDisabled, true)
		return
	}
	b.answer(ctx, q, "", false)

	ses := store.Session{TgID: q.From.ID, State: state, BaseSHA: base, ExpiresAt: time.Now().Add(sessionTTL)}
	var p sessionPayload
	var intro string
	if a != nil {
		ses.ArticlePath = a.Path
		p.ArticleID = a.ID
		b.sendMarkdownFile(ctx, chatID, a)
		intro = fmt.Sprintf(b.t.SuggestEditIntro, a.Title)
	} else {
		p.Grade = g.Subject.Dir + ":" + g.Dir
		intro = fmt.Sprintf(b.t.SuggestNewIntro, g.Subject.Label(), gradeTitle(b.t, g))
	}
	data, _ := json.Marshal(p)
	ses.Payload = string(data)
	if _, err := b.store.TouchUser(ctx, q.From.ID, q.From.Username, q.From.FirstName); err != nil {
		b.log.Error("touch user", "err", err)
	}
	if err := b.store.PutSession(ctx, ses); err != nil {
		b.log.Error("put session", "err", err)
		b.send(ctx, chatID, b.t.SubmitFailed, nil)
		return
	}
	b.send(ctx, chatID, intro, keyboard(row(button(b.t.Cancel, cbCancel))))
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
	if wait := time.Until(last.Add(b.cfg.SuggestCooldown)); !last.IsZero() && wait > 0 {
		return fmt.Sprintf(b.t.TooOften, int(wait.Seconds())+1)
	}
	return ""
}

// onSessionMessage handles a message while the user has an open
// suggestion. It reports false if there is no session.
func (b *Bot) onSessionMessage(ctx context.Context, m *models.Message) bool {
	unlock := b.userLocks.lock(m.From.ID)
	defer unlock()
	ses, err := b.store.Session(ctx, m.From.ID)
	if errors.Is(err, store.ErrNotFound) {
		return false
	}
	if err != nil {
		b.log.Error("session", "err", err)
		return false
	}
	var p sessionPayload
	if err := json.Unmarshal([]byte(ses.Payload), &p); err != nil {
		b.log.Error("session payload", "err", err)
		b.store.DeleteSession(ctx, m.From.ID)
		return false
	}

	switch {
	case m.Document != nil && strings.HasSuffix(strings.ToLower(m.Document.FileName), ".md"):
		if m.Document.FileSize > b.cfg.SuggestMaxBytes {
			b.send(ctx, m.Chat.ID, fmt.Sprintf(b.t.FileTooLarge, b.cfg.SuggestMaxBytes>>10), nil)
			return true
		}
		data, err := b.download(ctx, m.Document.FileID, b.cfg.SuggestMaxBytes)
		if err != nil {
			b.log.Error("download document", "err", err)
			b.send(ctx, m.Chat.ID, b.t.SubmitFailed, nil)
			return true
		}
		b.submit(ctx, m, ses, p, data)

	case m.Document != nil:
		b.addImage(ctx, m, ses, p, m.Document.FileName, m.Document.FileID, m.Document.FileSize)

	case len(m.Photo) > 0:
		ph := m.Photo[len(m.Photo)-1]
		b.addImage(ctx, m, ses, p, "photo.jpg", ph.FileID, int64(ph.FileSize))

	case m.Text != "":
		b.submit(ctx, m, ses, p, []byte(m.Text))

	default:
		b.send(ctx, m.Chat.ID, b.t.ImageUnsupported, nil)
	}
	return true
}

func (b *Bot) addImage(ctx context.Context, m *models.Message, ses store.Session, p sessionPayload, original, fileID string, size int64) {
	name := suggest.ImageName(original, p.imageNames())
	switch {
	case name == "":
		b.send(ctx, m.Chat.ID, b.t.ImageUnsupported, nil)
		return
	case size > b.cfg.ImageMaxBytes:
		b.send(ctx, m.Chat.ID, fmt.Sprintf(b.t.ImageTooLarge, b.cfg.ImageMaxBytes>>20), nil)
		return
	case len(p.Images) >= maxImages:
		b.send(ctx, m.Chat.ID, fmt.Sprintf(b.t.TooManyImages, maxImages), nil)
		return
	}
	p.Images = append(p.Images, pendingImage{Name: name, FileID: fileID})
	data, _ := json.Marshal(p)
	ses.Payload = string(data)
	if err := b.store.PutSession(ctx, ses); err != nil {
		b.log.Error("put session", "err", err)
		b.send(ctx, m.Chat.ID, b.t.SubmitFailed, nil)
		return
	}
	b.send(ctx, m.Chat.ID, fmt.Sprintf(b.t.ImageSaved, name, name), nil)
}

// submit validates the article, commits it on a new branch from the
// session's base commit and opens a pull request.
func (b *Bot) submit(ctx context.Context, m *models.Message, ses store.Session, p sessionPayload, data []byte) {
	cat := b.catalog()
	var res *suggest.Result
	var err error
	var grade *content.Grade
	kind := store.KindEdit
	switch ses.State {
	case stateEdit:
		a := cat.ArticleByPath(ses.ArticlePath)
		if a == nil {
			a = cat.Article(p.ArticleID)
		}
		if a == nil {
			b.store.DeleteSession(ctx, m.From.ID)
			b.send(ctx, m.Chat.ID, b.t.NotFound, nil)
			return
		}
		grade = a.Grade
		res, err = suggest.Edit(cat, a, data, p.imageNames(), b.cfg.SuggestMaxBytes)
	case stateNew:
		kind = store.KindNew
		grade = findGrade(cat, p.Grade)
		if grade == nil {
			b.store.DeleteSession(ctx, m.From.ID)
			b.send(ctx, m.Chat.ID, b.t.NotFound, nil)
			return
		}
		res, err = suggest.New(cat, grade, data, p.imageNames(), b.cfg.SuggestMaxBytes)
	default:
		b.store.DeleteSession(ctx, m.From.ID)
		return
	}
	var prob *suggest.Problem
	if errors.As(err, &prob) {
		b.send(ctx, m.Chat.ID, fmt.Sprintf(b.t.Problems[string(prob.Code)], prob.Args...), nil)
		return
	}
	if err != nil {
		b.log.Error("prepare suggestion", "err", err)
		b.send(ctx, m.Chat.ID, b.t.SubmitFailed, nil)
		return
	}
	if res.Unchanged {
		b.send(ctx, m.Chat.ID, b.t.Unchanged, nil)
		return
	}
	if msg := b.quotaProblem(ctx, m.From.ID); msg != "" {
		b.send(ctx, m.Chat.ID, msg, nil)
		return
	}

	b.send(ctx, m.Chat.ID, b.t.Submitting, nil)
	pr, err := b.openPullRequest(ctx, m.From, ses, p, kind, grade, res)
	if err != nil {
		b.log.Error("submit suggestion", "user", m.From.ID, "path", res.Path, "err", err)
		b.send(ctx, m.Chat.ID, b.t.SubmitFailed, nil)
		return
	}
	if err := b.store.DeleteSession(ctx, m.From.ID); err != nil {
		b.log.Error("delete session", "err", err)
	}
	text := fmt.Sprintf(b.t.Submitted, pr.Number)
	if len(res.Issues) > 0 {
		text += "\n\n" + fmt.Sprintf(b.t.SubmittedIssues, len(res.Issues))
	}
	b.send(ctx, m.Chat.ID, text, nil)
}

func (b *Bot) openPullRequest(ctx context.Context, from *models.User, ses store.Session, p sessionPayload,
	kind string, g *content.Grade, res *suggest.Result) (githost.PR, error) {
	articlePath, err := suggest.RepoPath(b.cfg.ContentSubdir, res.Path)
	if err != nil {
		return githost.PR{}, err
	}
	changes := []gitrepo.Change{{Path: articlePath, Data: res.Data}}
	var images []string
	for _, img := range p.Images {
		// Only images the article refers to are committed.
		if !strings.Contains(string(res.Data), "assets/"+img.Name) {
			continue
		}
		rel, err := suggest.RepoPath(b.cfg.ContentSubdir, g.Subject.Dir+"/"+g.Dir+"/assets/"+img.Name)
		if err != nil {
			return githost.PR{}, err
		}
		data, err := b.download(ctx, img.FileID, b.cfg.ImageMaxBytes)
		if err != nil {
			return githost.PR{}, fmt.Errorf("download image %s: %w", img.Name, err)
		}
		changes = append(changes, gitrepo.Change{Path: rel, Data: data})
		images = append(images, rel)
	}

	by := suggest.Submitter{TgID: from.ID, Username: from.Username, FirstName: from.FirstName}
	ch := suggest.Change{Kind: kind, Title: res.Title, RepoPath: articlePath, Images: images,
		BaseSHA: ses.BaseSHA, ShortID: suggest.ShortID(), Issues: res.Issues, By: by}
	branch := suggest.Branch(from.ID, res.Path, ch.ShortID)
	author := gitrepo.Author{Name: by.Handle(), Email: fmt.Sprintf("tg-%d@users.terakoya.invalid", from.ID)}
	committer := gitrepo.Author{Name: b.cfg.GitAuthorName, Email: b.cfg.GitAuthorEmail}

	if _, err := b.repo.CommitAndPush(ctx, ses.BaseSHA, branch, ch.CommitMessage(), author, committer, changes); err != nil {
		return githost.PR{}, err
	}
	pr, err := b.host.OpenPR(ctx, githost.NewPR{Title: ch.PRTitle(), Body: ch.PRBody(), Head: branch, Base: b.cfg.RepoBranch})
	if err != nil {
		return githost.PR{}, err
	}
	sg := &store.Suggestion{ShortID: ch.ShortID, TgID: from.ID, Kind: kind, ArticlePath: articlePath, Title: res.Title,
		BaseSHA: ses.BaseSHA, Branch: branch, PRNumber: pr.Number, PRURL: pr.URL}
	if err := b.store.CreateSuggestion(ctx, sg); err != nil {
		return githost.PR{}, fmt.Errorf("store suggestion for PR %d: %w", pr.Number, err)
	}
	b.log.Info("suggestion submitted", "user", from.ID, "branch", branch, "pr", pr.Number, "base", ses.BaseSHA)
	return pr, nil
}

// cancelSuggestion ends the user's session.
func (b *Bot) cancelSuggestion(ctx context.Context, chatID, userID int64) {
	if _, err := b.store.Session(ctx, userID); errors.Is(err, store.ErrNotFound) {
		b.send(ctx, chatID, b.t.NothingToCancel, nil)
		return
	}
	if err := b.store.DeleteSession(ctx, userID); err != nil {
		b.log.Error("delete session", "err", err)
	}
	b.send(ctx, chatID, b.t.Cancelled, nil)
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

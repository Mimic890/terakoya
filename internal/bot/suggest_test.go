package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/config"
	"github.com/Mimic890/terakoya/internal/githost"
	"github.com/Mimic890/terakoya/internal/gitrepo"
	"github.com/Mimic890/terakoya/internal/i18n"
	"github.com/Mimic890/terakoya/internal/ranks"
	"github.com/Mimic890/terakoya/internal/store"
	"github.com/Mimic890/terakoya/internal/suggest"
)

// fakeTelegram records Bot API calls and answers them with plausible results.
type fakeTelegram struct {
	mu    sync.Mutex
	calls []call
	next  int
}

type call struct {
	method string
	params map[string]string
}

func (f *fakeTelegram) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/file/") {
		w.Write([]byte("PNG:" + r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]))
		return
	}
	method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	params := map[string]string{}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		r.ParseMultipartForm(1 << 20)
		for k, v := range r.MultipartForm.Value {
			params[k] = v[0]
		}
	} else {
		var raw map[string]any
		json.NewDecoder(r.Body).Decode(&raw)
		for k, v := range raw {
			params[k] = fmt.Sprint(v)
		}
	}
	f.mu.Lock()
	f.calls = append(f.calls, call{method, params})
	f.next++
	id := 1000 + f.next
	f.mu.Unlock()

	var result any = true
	switch method {
	case "sendMessage", "sendDocument", "editMessageReplyMarkup":
		result = map[string]any{"message_id": id, "date": 0, "chat": map[string]any{"id": 42, "type": "private"}}
	case "getFile":
		result = map[string]any{"file_id": params["file_id"], "file_unique_id": "u", "file_size": 7,
			"file_path": "documents/" + params["file_id"]}
	}
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
}

func (f *fakeTelegram) take() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.calls
	f.calls = nil
	return out
}

// fakeHost stands in for GitHub or Forgejo.
type fakeHost struct {
	prs []githost.NewPR
}

func (h *fakeHost) OpenPR(_ context.Context, pr githost.NewPR) (githost.PR, error) {
	h.prs = append(h.prs, pr)
	return githost.PR{Number: 6 + len(h.prs), URL: "https://host/pr"}, nil
}
func (h *fakeHost) LastReviewComment(context.Context, int) (string, error) {
	return "Слишком коротко.", nil
}
func (h *fakeHost) ParseWebhook(http.Header, []byte) (githost.Event, error) {
	return githost.Event{}, nil
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

type harness struct {
	t        *testing.T
	ctx      context.Context
	bot      *Bot
	tg       *fakeTelegram
	host     *fakeHost
	store    *store.Store
	remote   string
	upstream string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	upstream := filepath.Join(root, "upstream")
	git(t, root, "init", "--quiet", "--bare", "--initial-branch=main", remote)
	git(t, root, "clone", "--quiet", remote, upstream)
	if err := os.CopyFS(filepath.Join(upstream, "content"), os.DirFS("../../examples/content")); err != nil {
		t.Fatal(err)
	}
	git(t, upstream, "add", ".")
	git(t, upstream, "commit", "--quiet", "-m", "init")
	git(t, upstream, "push", "--quiet", "origin", "HEAD:main")

	checkout := filepath.Join(root, "checkout")
	repo, err := gitrepo.Ensure(ctx, checkout, remote, "main", "")
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	cfg := config.Default()
	cfg.BotToken, cfg.BotUsername = "123:abc", "terakoya_bot"
	cfg.RepoDir, cfg.RepoURL, cfg.GitHost = checkout, remote, "forgejo"
	cfg.Ladder, _ = ranks.Parse("kōhai:0,senpai:1")

	fake := &fakeTelegram{}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	host := &fakeHost{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	b, err := New(ctx, cfg, st, repo, host, log, tg.WithServerURL(srv.URL), tg.WithSkipGetMe())
	if err != nil {
		t.Fatal(err)
	}
	return &harness{t: t, ctx: ctx, bot: b, tg: fake, host: host, store: st, remote: remote, upstream: upstream}
}

var anya = models.User{ID: 42, Username: "anya", FirstName: "Аня"}

func (h *harness) tap(data string) {
	h.bot.handle(h.ctx, nil, &models.Update{CallbackQuery: &models.CallbackQuery{
		ID: "q", From: anya, Data: data,
		Message: models.MaybeInaccessibleMessage{Message: &models.Message{ID: 100, Chat: models.Chat{ID: 42, Type: "private"}}},
	}})
}

func (h *harness) say(text string) {
	u := anya
	h.bot.handle(h.ctx, nil, &models.Update{Message: &models.Message{
		ID: 101, From: &u, Chat: models.Chat{ID: 42, Type: "private"}, Text: text,
	}})
}

func texts(calls []call, method string) []string {
	var out []string
	for _, c := range calls {
		if c.method == method {
			out = append(out, c.params["text"])
		}
	}
	return out
}

func TestSuggestEditFlow(t *testing.T) {
	h := newHarness(t)
	base := h.bot.repo.HeadSHA(h.ctx)

	h.tap("e:triangles")
	calls := h.tg.take()
	if len(texts(calls, "sendDocument")) != 1 || len(texts(calls, "sendMessage")) != 1 {
		t.Fatalf("expected the .md file and instructions, got %+v", calls)
	}
	ses, err := h.store.Session(h.ctx, 42)
	if err != nil || ses.BaseSHA != base || ses.State != stateEdit {
		t.Fatalf("session = %+v, %v", ses, err)
	}

	// Main moves on while the user is editing.
	os.WriteFile(filepath.Join(h.upstream, "content/geometry/7/02-triangles.md"), []byte("---\ntitle: Треугольники\nid: triangles\n---\nupstream\n"), 0o644)
	git(t, h.upstream, "commit", "--quiet", "-am", "upstream edit")
	git(t, h.upstream, "push", "--quiet", "origin", "HEAD:main")

	h.say("# Треугольники\n\nНовый текст про [[Нет такой темы]].")
	sent := texts(h.tg.take(), "sendMessage")
	if len(sent) != 2 || sent[0] != i18n.RU.Submitting || !strings.Contains(sent[1], "#7") || !strings.Contains(sent[1], "1") {
		t.Fatalf("replies = %q", sent)
	}
	if len(h.host.prs) != 1 {
		t.Fatalf("PRs = %+v", h.host.prs)
	}
	pr := h.host.prs[0]
	if !strings.HasPrefix(pr.Head, "suggest/42-triangles-") || pr.Base != "main" || pr.Title != "Suggest edit: Треугольники" {
		t.Errorf("pr = %+v", pr)
	}
	for _, want := range []string{"`42`", "@anya", "content/geometry/7/02-triangles.md", base, "Нет такой темы"} {
		if !strings.Contains(pr.Body, want) {
			t.Errorf("PR body misses %q:\n%s", want, pr.Body)
		}
	}
	// The branch starts from the commit the user saw, not the newer main.
	if parent := git(t, h.remote, "rev-parse", pr.Head+"^"); parent != base {
		t.Errorf("branch parent = %s, want base %s", parent, base)
	}
	content := git(t, h.remote, "show", pr.Head+":content/geometry/7/02-triangles.md")
	if !strings.HasPrefix(content, "---\ntitle: Треугольники\norder: 2\nid: triangles\n") ||
		!strings.Contains(content, "---\n\n# Треугольники\n\nНовый текст") {
		t.Errorf("committed file = %q", content)
	}
	if _, err := h.store.Session(h.ctx, 42); err == nil {
		t.Error("session not cleared")
	}

	// Rate limit: a second suggestion right away is refused.
	h.tap("e:triangles")
	if alerts := h.tg.take(); len(alerts) != 1 || !strings.Contains(alerts[0].params["text"], "Слишком часто") {
		t.Errorf("rate limit reply = %+v", alerts)
	}

	// Merge webhook: rank goes up and the user is told.
	h.bot.HandleGitEvent(h.ctx, githost.Event{Kind: githost.PRMerged, PRNumber: 7, HeadBranch: pr.Head})
	sent = texts(h.tg.take(), "sendMessage")
	if len(sent) != 1 || !strings.Contains(sent[0], "принята") || !strings.Contains(sent[0], "senpai") {
		t.Errorf("merge notification = %q", sent)
	}
	if u, _ := h.store.User(h.ctx, 42); u.MergedCount != 1 {
		t.Errorf("merged count = %d", u.MergedCount)
	}
	// A repeated delivery changes nothing.
	h.bot.HandleGitEvent(h.ctx, githost.Event{Kind: githost.PRMerged, PRNumber: 7, HeadBranch: pr.Head})
	if calls := h.tg.take(); len(calls) != 0 {
		t.Errorf("repeated delivery sent %+v", calls)
	}

	// Push webhook: the catalog follows main.
	h.bot.HandleGitEvent(h.ctx, githost.Event{Kind: githost.Push, Ref: "refs/heads/main"})
	if got, want := h.bot.sha, git(t, h.remote, "rev-parse", "main"); got != want {
		t.Errorf("after push sha = %s, want %s", got, want)
	}
	if body := h.bot.catalog().Article("triangles").Body; !strings.Contains(body, "upstream") {
		t.Errorf("catalog not reloaded: %q", body)
	}
}

func TestSuggestNewTopicClosed(t *testing.T) {
	h := newHarness(t)
	h.tap("n:geometry:8")
	h.tg.take()

	h.say("Ромб\n\nУ ромба все стороны равны.")
	h.tg.take()
	if len(h.host.prs) != 1 {
		t.Fatalf("PRs = %+v", h.host.prs)
	}
	pr := h.host.prs[0]
	file := git(t, h.remote, "show", pr.Head+":content/geometry/8/02-romb.md")
	if !strings.HasPrefix(file, "---\ntitle: Ромб\norder: 2\n---\n\nРомб") {
		t.Errorf("new file = %q", file)
	}

	h.bot.HandleGitEvent(h.ctx, githost.Event{Kind: githost.PRClosed, PRNumber: 7, HeadBranch: pr.Head})
	sent := texts(h.tg.take(), "sendMessage")
	if len(sent) != 1 || !strings.Contains(sent[0], "отклонена") || !strings.Contains(sent[0], "Слишком коротко.") {
		t.Errorf("close notification = %q", sent)
	}
	if u, _ := h.store.User(h.ctx, 42); u.MergedCount != 0 {
		t.Errorf("merged count = %d", u.MergedCount)
	}
}

func TestSuggestRejectsAndCancels(t *testing.T) {
	h := newHarness(t)
	h.tap("e:triangles")
	h.tg.take()

	h.say("---\ntitle: x\nid: geometry-7-basic-notions\n---\n")
	sent := texts(h.tg.take(), "sendMessage")
	if len(sent) != 1 || !strings.Contains(sent[0], "уже занят") {
		t.Errorf("reply = %q", sent)
	}
	if len(h.host.prs) != 0 {
		t.Error("PR opened for a rejected submission")
	}
	// The session survives a rejection so the user can try again.
	if _, err := h.store.Session(h.ctx, 42); err != nil {
		t.Fatalf("session lost: %v", err)
	}

	h.say("/cancel")
	if sent := texts(h.tg.take(), "sendMessage"); len(sent) != 1 || sent[0] != i18n.RU.Cancelled {
		t.Errorf("cancel reply = %q", sent)
	}
	h.say("просто текст")
	if sent := texts(h.tg.take(), "sendMessage"); len(sent) != 1 || sent[0] != i18n.RU.UnknownCommand {
		t.Errorf("after cancel = %q", sent)
	}
}

func TestSuggestWithImages(t *testing.T) {
	h := newHarness(t)
	h.tap("e:geometry-7-basic-notions")
	h.tg.take()

	u := anya
	send := func(m *models.Message) {
		m.ID, m.From, m.Chat = 102, &u, models.Chat{ID: 42, Type: "private"}
		h.bot.handle(h.ctx, nil, &models.Update{Message: m})
	}
	send(&models.Message{Document: &models.Document{FileID: "fig1", FileName: "Рисунок 1.png", FileSize: 7}})
	send(&models.Message{Photo: []models.PhotoSize{{FileID: "small", Width: 90}, {FileID: "big", Width: 800, FileSize: 7}}})
	send(&models.Message{Document: &models.Document{FileID: "v", FileName: "figure.svg"}})
	sent := texts(h.tg.take(), "sendMessage")
	if len(sent) != 3 || !strings.Contains(sent[0], "assets/risunok-1.png") || !strings.Contains(sent[1], "assets/photo.jpg") ||
		sent[2] != i18n.RU.ImageUnsupported {
		t.Fatalf("replies = %q", sent)
	}

	// Only the referenced image is committed.
	h.say("---\ntitle: Основные понятия\n---\n\n![Рисунок](assets/risunok-1.png)\n")
	h.tg.take()
	if len(h.host.prs) != 1 {
		t.Fatalf("PRs = %+v", h.host.prs)
	}
	head := h.host.prs[0].Head
	if got := git(t, h.remote, "show", head+":content/geometry/7/assets/risunok-1.png"); got != "PNG:fig1" {
		t.Errorf("image = %q", got)
	}
	files := git(t, h.remote, "diff", "--name-only", head+"^", head)
	if files != "content/geometry/7/01-basic-notions.md\ncontent/geometry/7/assets/risunok-1.png" {
		t.Errorf("changed files = %q", files)
	}
	if !strings.Contains(h.host.prs[0].Body, "Image: `content/geometry/7/assets/risunok-1.png`") {
		t.Errorf("PR body = %s", h.host.prs[0].Body)
	}
}

func TestEveryProblemHasText(t *testing.T) {
	for _, c := range []suggest.Code{suggest.TooLarge, suggest.NotUTF8, suggest.Empty, suggest.NoFrontmatter,
		suggest.OpenFrontmatter, suggest.BadFrontmatter, suggest.NoTitle, suggest.BadID, suggest.TakenID,
		suggest.OverLimit, suggest.BadPath} {
		if i18n.RU.Problems[string(c)] == "" {
			t.Errorf("no text for %s", c)
		}
	}
}

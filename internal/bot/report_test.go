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
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tg "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/config"
	"github.com/Mimic890/terakoya/internal/githost"
	"github.com/Mimic890/terakoya/internal/gitrepo"
	"github.com/Mimic890/terakoya/internal/i18n"
	"github.com/Mimic890/terakoya/internal/ranks"
	"github.com/Mimic890/terakoya/internal/settings"
	"github.com/Mimic890/terakoya/internal/store"
)

// fakeTelegram records Bot API calls and answers them with plausible results.
type fakeTelegram struct {
	mu    sync.Mutex
	calls []call
	next  int
	last  map[string]int // the last message ID returned, by method
	// delay, when set, is how long every call takes, like a real round
	// trip to Telegram.
	delay time.Duration
	// reject, when set, makes the calls it returns true for fail.
	reject func(method string, params map[string]string) bool
	// emoji are the custom emoji IDs getCustomEmojiStickers knows.
	emoji []string
}

type call struct {
	method string
	params map[string]string
}

func (f *fakeTelegram) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/file/") {
		w.Write([]byte("IMG:" + r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]))
		return
	}
	method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	f.mu.Lock()
	delay := f.delay
	f.mu.Unlock()
	time.Sleep(delay)
	params := map[string]string{}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		r.ParseMultipartForm(1 << 20)
		for k, v := range r.MultipartForm.Value {
			params[k] = v[0]
		}
	} else {
		var raw map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&raw)
		for k, v := range raw {
			var s string
			if json.Unmarshal(v, &s) == nil {
				params[k] = s
			} else {
				params[k] = string(v)
			}
		}
	}
	f.mu.Lock()
	if f.reject != nil && f.reject(method, params) {
		f.calls = append(f.calls, call{method + " (rejected)", params})
		f.mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": 400, "description": "Bad Request: DOCUMENT_INVALID"})
		return
	}
	f.calls = append(f.calls, call{method, params})
	f.next++
	id := 1000 + f.next
	if f.last == nil {
		f.last = map[string]int{}
	}
	f.last[method] = id
	f.mu.Unlock()

	var result any = true
	switch method {
	case "sendMessage", "editMessageReplyMarkup", "editMessageText", "sendRichMessage":
		result = map[string]any{"message_id": id, "date": 0, "chat": map[string]any{"id": 42, "type": "private"}}
	case "sendDocument":
		result = map[string]any{"message_id": id, "date": 0, "chat": map[string]any{"id": 42, "type": "private"},
			"document": map[string]any{"file_id": fmt.Sprintf("doc%d", id), "file_unique_id": "u"}}
	case "sendPhoto":
		result = map[string]any{"message_id": id, "date": 0, "chat": map[string]any{"id": -100, "type": "supergroup"},
			"photo": []map[string]any{{"file_id": fmt.Sprintf("photo%d", id), "file_unique_id": "u", "width": 800, "height": 600}}}
	case "getCustomEmojiStickers":
		var stickers []map[string]any
		for _, id := range f.emoji {
			if strings.Contains(params["custom_emoji_ids"], `"`+id+`"`) {
				stickers = append(stickers, map[string]any{"file_id": "s" + id, "file_unique_id": "u", "type": "custom_emoji",
					"width": 100, "height": 100, "is_animated": false, "is_video": false, "custom_emoji_id": id})
			}
		}
		result = stickers
	case "getFile":
		result = map[string]any{"file_id": params["file_id"], "file_unique_id": "u", "file_size": 7,
			"file_path": "photos/" + params["file_id"]}
	}
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
}

func (f *fakeTelegram) lastID(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last[method]
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
	mu          sync.Mutex
	issues      []string // titles
	bodies      map[int]string
	attachments []string
	states      map[int]githost.State
	number      int
	// noAttach makes the host one without issue attachments, like GitHub.
	noAttach bool
}

func (h *fakeHost) nextNumber() int {
	if h.number == 0 {
		h.number = 6
	}
	h.number++
	return h.number
}

func (h *fakeHost) OpenIssue(_ context.Context, title, body string) (githost.PR, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.issues = append(h.issues, title)
	n := h.nextNumber()
	if h.bodies == nil {
		h.bodies = map[int]string{}
	}
	h.bodies[n] = body
	return githost.PR{Number: n, URL: "https://host/issues"}, nil
}
func (h *fakeHost) EditIssue(_ context.Context, number int, body string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.bodies[number] = body
	return nil
}
func (h *fakeHost) Attach(_ context.Context, number int, name string, data []byte) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.noAttach {
		return "", githost.ErrUnsupported
	}
	h.attachments = append(h.attachments, name+"="+string(data))
	return fmt.Sprintf("https://host/attachments/%d/%s", number, name), nil
}
func (h *fakeHost) EnsureLabel(context.Context, string, string, string) error { return nil }
func (h *fakeHost) State(_ context.Context, number int, _ bool) (githost.State, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.states[number], nil
}
func (h *fakeHost) setState(number int, st githost.State) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.states == nil {
		h.states = map[int]githost.State{}
	}
	h.states[number] = st
}
func (h *fakeHost) LastReviewComment(context.Context, int) (string, error) {
	return "Слишком коротко.", nil
}
func (h *fakeHost) ParseWebhook(http.Header, []byte) (githost.Event, error) {
	return githost.Event{}, nil
}
func (h *fakeHost) FileURL(ref, path string) string { return "https://host/src/" + ref + "/" + path }
func (h *fakeHost) RawURL(ref, path string) string  { return "https://host/raw/" + ref + "/" + path }

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "core.quotepath=off"}, args...)...)
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
	msgID    int
	user     models.User
}

// newHarness serves examples/vault plus the triangles topic of testdata
// and extra files (vault path → content) from a local bare repository.
func newHarness(t *testing.T, extra map[string]string) *harness {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	upstream := filepath.Join(root, "upstream")
	git(t, root, "init", "--quiet", "--bare", "--initial-branch=main", remote)
	git(t, root, "clone", "--quiet", remote, upstream)
	if err := os.CopyFS(upstream, os.DirFS("../../examples/vault")); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{triangles: readFile(t, "testdata/triangles.md"),
		path.Dir(triangles) + "/_assets/triangle.png": readFile(t, "testdata/triangle.png")}
	for p, data := range extra {
		files[p] = data
	}
	for p, data := range files {
		abs := filepath.Join(upstream, filepath.FromSlash(p))
		os.MkdirAll(filepath.Dir(abs), 0o755)
		if err := os.WriteFile(abs, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
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
	cfg.DataDir = root
	cfg.Admins = []int64{ivan.ID}

	fake := &fakeTelegram{}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	host := &fakeHost{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	b, err := New(ctx, cfg, st, repo, host, log, tg.WithServerURL(srv.URL), tg.WithSkipGetMe())
	if err != nil {
		t.Fatal(err)
	}
	b.media.Raster = fakeRaster{}
	h := &harness{t: t, ctx: ctx, bot: b, tg: fake, host: host, store: st, remote: remote, upstream: upstream, msgID: 100,
		user: anya}
	h.setRanks("kōhai:0,senpai:1")
	// Аня has used the bot before.
	st.TouchUser(ctx, anya.ID, anya.Username, anya.FirstName)
	return h
}

// setRanks replaces the ladder in settings.yml, as "title:from,…".
func (h *harness) setRanks(spec string) {
	h.t.Helper()
	l, err := ranks.Parse(spec)
	if err != nil {
		h.t.Fatal(err)
	}
	err = h.bot.settings.Update(func(s *settings.Settings) {
		s.Ranks = settings.FromLadder(l, nil).Ranks
	})
	if err != nil {
		h.t.Fatal(err)
	}
}

var (
	anya  = models.User{ID: 42, Username: "anya", FirstName: "Аня"}
	ivan  = models.User{ID: 7, Username: "ivan", FirstName: "Иван"} // the admin
	boris = models.User{ID: 43, Username: "boris", FirstName: "Борис"}
)

// as makes the following taps and messages come from u.
func (h *harness) as(u models.User) { h.user = u }

// tap presses a button. Dialog buttons are pressed on the current prompt.
func (h *harness) tap(data string) {
	id := 100
	if d, _ := h.bot.loadDialog(h.ctx, h.user.ID, h.user.ID); d != nil && d.p.PromptID != 0 {
		id = d.p.PromptID
	}
	h.tapOn(id, data)
}

func (h *harness) tapOn(msgID int, data string) { h.tapWith(msgID, data, "") }

// tapWith presses a button of a message whose keyboard, as JSON, Telegram
// sends along.
func (h *harness) tapWith(msgID int, data, markup string) {
	msg := &models.Message{ID: msgID, Chat: models.Chat{ID: h.user.ID, Type: "private"}}
	if markup != "" {
		msg.ReplyMarkup = &models.InlineKeyboardMarkup{}
		if err := json.Unmarshal([]byte(markup), msg.ReplyMarkup); err != nil {
			h.t.Fatal(err)
		}
	}
	h.bot.handle(h.ctx, nil, &models.Update{CallbackQuery: &models.CallbackQuery{
		ID: "q", From: h.user, Data: data,
		Message: models.MaybeInaccessibleMessage{Message: msg},
	}})
	h.bot.bg.Wait() // deletions left to the background
}

// send delivers a message from the user; the clock advances 10 seconds per
// message unless the date is given.
func (h *harness) send(m *models.Message) {
	u := h.user
	h.msgID++
	m.ID, m.From, m.Chat = h.msgID, &u, models.Chat{ID: u.ID, Type: "private"}
	if m.Date == 0 {
		m.Date = h.msgID * 10
	}
	h.bot.handle(h.ctx, nil, &models.Update{Message: m})
	h.bot.bg.Wait() // deletions left to the background
}

func (h *harness) say(text string) { h.send(&models.Message{Text: text}) }

func (h *harness) photo(fileID, caption string) {
	h.send(&models.Message{Photo: []models.PhotoSize{{FileID: "thumb", Width: 90}, {FileID: fileID, Width: 800, FileSize: 7}}, Caption: caption})
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

// last returns the last sendMessage call.
func last(t *testing.T, calls []call) call {
	t.Helper()
	for i := len(calls) - 1; i >= 0; i-- {
		if calls[i].method == "sendMessage" {
			return calls[i]
		}
	}
	t.Fatalf("no message sent: %+v", calls)
	return call{}
}

func wantPrompt(t *testing.T, calls []call, text string, buttons ...string) {
	t.Helper()
	c := last(t, calls)
	if c.params["text"] != text {
		t.Errorf("prompt = %q, want %q", c.params["text"], text)
	}
	for _, b := range buttons {
		if !strings.Contains(c.params["reply_markup"], `"`+b+`"`) {
			t.Errorf("prompt %q has no button %q: %s", text, b, c.params["reply_markup"])
		}
	}
}

func (h *harness) file(ref, path string) string { return git(h.t, h.remote, "show", ref+":"+path) }

// id is the ID of the note at a vault path.
func (h *harness) id(p string) string {
	h.t.Helper()
	n := h.bot.catalog().NoteByPath(p)
	if n == nil {
		h.t.Fatalf("no note %s", p)
	}
	return n.ID
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// fakeRaster turns any SVG into a fake PNG.
type fakeRaster struct{}

func (fakeRaster) Tag() string { return "fake" }
func (fakeRaster) Rasterize(context.Context, string) ([]byte, error) {
	return []byte("\x89PNG"), nil
}

func (b *Bot) mustDialog(t *testing.T) *dialog {
	t.Helper()
	d, _ := b.loadDialog(context.Background(), 42, 42)
	if d == nil {
		t.Fatal("no dialog")
	}
	return d
}

const (
	triangles = "Геометрия/7 класс/01 Треугольники/Треугольники.md"
	vtopic    = "Геометрия/10 класс/05 Параллельность прямой и плоскости"
	vmain     = vtopic + "/Параллельность прямой и плоскости.md"
)

// toasts returns the texts of the callback answers.
func toasts(calls []call) []string {
	var out []string
	for _, c := range calls {
		if c.method == "answerCallbackQuery" {
			out = append(out, c.params["text"])
		}
	}
	return out
}

// to returns the calls of a method made to a chat.
func to(calls []call, method string, chatID int64) []call {
	var out []call
	for _, c := range calls {
		if c.method == method && c.params["chat_id"] == fmt.Sprint(chatID) {
			out = append(out, c)
		}
	}
	return out
}

func TestReport(t *testing.T) {
	h := newHarness(t, nil)
	h.bot.rejectGrace = 0
	base := h.bot.repo.HeadSHA(h.ctx)

	// The button under a note asks right away; no choice comes first.
	h.tapOn(500, cbReport+h.id(triangles))
	calls := h.tg.take()
	wantPrompt(t, calls, i18n.RU.ReportPrompt, "Отмена")
	if len(texts(calls, "sendMessage")) != 1 {
		t.Errorf("messages = %q", texts(calls, "sendMessage"))
	}
	h.say("в теореме опечатка, должно быть 180°")
	wantPrompt(t, h.tg.take(), i18n.RU.ReportReceived, "Отправить", "Отмена")
	h.photo("shot", "вот тут")
	wantPrompt(t, h.tg.take(), i18n.RU.ReportReceived, "Отправить")
	h.tap(cbSend)
	if s := texts(h.tg.take(), "editMessageText"); len(s) != 1 || s[0] != i18n.RU.SubmittedReport {
		t.Errorf("outcome = %q", s)
	}
	if len(h.host.issues) != 1 || h.host.issues[0] != "Error report: Треугольники" {
		t.Fatalf("issues %q", h.host.issues)
	}
	body := h.host.bodies[7]
	for _, want := range []string{"> в теореме опечатка, должно быть 180°", "https://host/src/" + base + "/" + triangles,
		"вот тут", "https://host/attachments/7/treugolniki-", "`accepted` label"} {
		if !strings.Contains(body, want) {
			t.Errorf("issue body misses %q:\n%s", want, body)
		}
	}
	if len(h.host.attachments) != 1 || !strings.HasSuffix(h.host.attachments[0], "=IMG:shot") {
		t.Errorf("attachments = %q", h.host.attachments)
	}
	if branches := git(t, h.remote, "branch", "--list"); strings.Contains(branches, "report/") {
		t.Errorf("branch pushed for a host with attachments: %s", branches)
	}
	if _, err := h.store.Session(h.ctx, 42); err == nil {
		t.Error("session not cleared")
	}

	// Rate limit.
	h.tap(cbReport + h.id(triangles))
	if c := h.tg.take(); len(c) != 1 || c[0].params["text"] != i18n.RU.TooOften {
		t.Errorf("rate limit = %+v", c)
	}

	// Closed without the label: the user hears about it only after the
	// grace period, from the host's state.
	h.host.setState(7, githost.State{Closed: true, ClosedAt: time.Now().Add(-time.Hour)})
	h.bot.HandleGitEvent(h.ctx, githost.Event{Kind: githost.IssueChanged, PRNumber: 7, State: githost.State{Closed: true}})
	var got []string
	for i := 0; i < 100 && len(got) == 0; i++ {
		time.Sleep(10 * time.Millisecond)
		got = texts(h.tg.take(), "sendMessage")
	}
	if len(got) != 1 || !strings.Contains(got[0], "закрыли без исправлений") || !strings.Contains(got[0], "Слишком коротко.") {
		t.Fatalf("rejection = %q", got)
	}

	// The label added afterwards turns it into an accepted report, once,
	// and the rank goes up.
	labeled := githost.Event{Kind: githost.IssueChanged, PRNumber: 7, State: githost.State{Closed: true, Labels: []string{"Accepted"}}}
	h.bot.HandleGitEvent(h.ctx, labeled)
	h.bot.HandleGitEvent(h.ctx, labeled)
	c := to(h.tg.take(), "sendMessage", 42)
	if len(c) != 1 || !strings.HasPrefix(c[0].params["text"], "Ошибку в «Треугольники» исправили. Спасибо, что сообщили! Принято сообщений: 1.\n\nТеперь вы senpai!") ||
		!strings.Contains(c[0].params["reply_markup"], `"o:`+h.id(triangles)+`"`) {
		t.Errorf("late accept = %+v", c)
	}
	if u, _ := h.store.User(h.ctx, 42); u.Accepted != 1 {
		t.Errorf("accepted = %d", u.Accepted)
	}
	// The notification button opens the note and keeps the notification.
	h.tapOn(555, "o:"+h.id(triangles))
	calls = h.tg.take()
	if len(texts(calls, "sendRichMessage")) != 1 || len(texts(calls, "deleteMessage")) != 0 {
		t.Errorf("open = %+v", calls)
	}
}

func TestGradeReport(t *testing.T) {
	h := newHarness(t, nil)
	base := h.bot.repo.HeadSHA(h.ctx)
	g := h.bot.catalog().NoteByPath(triangles).Grade()

	h.tapOn(700, cbReportGrade+g.ID)
	wantPrompt(t, h.tg.take(), fmt.Sprintf(i18n.RU.GradeReportPrompt, "Геометрия", "7 класс"), "Отмена")
	h.say("нет темы про ромб")
	h.tap(cbSend)
	if s := texts(h.tg.take(), "editMessageText"); len(s) != 1 || s[0] != i18n.RU.SubmittedReport {
		t.Errorf("outcome = %q", s)
	}
	if len(h.host.issues) != 1 || h.host.issues[0] != "Missing material: Геометрия · 7 класс" {
		t.Fatalf("issues %q", h.host.issues)
	}
	body := h.host.bodies[7]
	for _, want := range []string{"Missing material reported in Telegram by @anya.", "> нет темы про ромб",
		"- Grade: [`Геометрия/7 класс`](https://host/src/" + base + "/Геометрия/7 класс)"} {
		if !strings.Contains(body, want) {
			t.Errorf("issue body misses %q:\n%s", want, body)
		}
	}
	sg, err := h.store.SuggestionByNumber(h.ctx, 7)
	if err != nil || sg.Kind != store.KindReport || sg.ArticlePath != "Геометрия/7 класс" || sg.Title != "Геометрия · 7 класс" {
		t.Fatalf("stored = %+v, %v", sg, err)
	}
	if got := h.bot.historyMarkdown(h.ctx, 42); !strings.Contains(got, "- Геометрия · 7 класс — не хватает материала, ") {
		t.Errorf("history:\n%s", got)
	}

	h.bot.HandleGitEvent(h.ctx, githost.Event{Kind: githost.IssueChanged, PRNumber: 7,
		State: githost.State{Closed: true, Labels: []string{"accepted"}}})
	c := to(h.tg.take(), "sendMessage", 42)
	if len(c) != 1 || !strings.HasPrefix(c[0].params["text"], "Ваше сообщение про «Геометрия · 7 класс» учли. Спасибо! Принято сообщений: 1.") ||
		strings.Contains(c[0].params["reply_markup"], `"o:`) {
		t.Errorf("accepted = %+v", c)
	}
}

func TestReportPicturesOnBranch(t *testing.T) {
	h := newHarness(t, nil)
	h.host.noAttach = true
	base := h.bot.repo.HeadSHA(h.ctx)
	g := h.bot.catalog().NoteByPath(triangles).Grade()

	h.tap(cbReport + h.id(triangles))
	h.photo("pic1", "после таблицы")
	h.tap(cbSend)
	h.tg.take()
	sg, _ := h.store.SuggestionByNumber(h.ctx, 7)
	if !strings.HasPrefix(sg.Branch, "report/42-treugolniki-") {
		t.Fatalf("branch = %q", sg.Branch)
	}
	if parent := git(t, h.remote, "rev-parse", sg.Branch+"^"); parent != base {
		t.Errorf("branch parent = %s, want %s", parent, base)
	}
	img := path.Dir(triangles) + "/_assets/treugolniki-" + strings.TrimPrefix(sg.Branch, "report/42-treugolniki-") + "-1.jpg"
	if got := h.file(sg.Branch, img); got != "IMG:pic1" {
		t.Errorf("picture = %q", got)
	}
	if body := h.host.bodies[7]; !strings.Contains(body, "`"+img+"` — после таблицы") || !strings.Contains(body, "https://host/raw/"+sg.Branch+"/"+img) {
		t.Errorf("issue body:\n%s", body)
	}

	// A grade's pictures go to the grade's assets.
	h.bot.cfg.SuggestCooldown = 0
	h.tap(cbReportGrade + g.ID)
	h.photo("pic2", "")
	h.tap(cbSend)
	h.tg.take()
	sg, _ = h.store.SuggestionByNumber(h.ctx, 8)
	files := git(t, h.remote, "diff", "--name-only", sg.Branch+"^", sg.Branch)
	if !strings.HasPrefix(files, "Геометрия/7 класс/_assets/") || !strings.HasSuffix(files, "-1.jpg") {
		t.Errorf("grade picture = %q on %q", files, sg.Branch)
	}
}

func TestReportsOpenToEveryone(t *testing.T) {
	h := newHarness(t, nil)
	h.as(boris)
	h.tap(cbReport + h.id(triangles))
	wantPrompt(t, h.tg.take(), i18n.RU.ReportPrompt, "Отмена")
	h.say("опечатка")
	h.tap(cbSend)
	if len(h.host.issues) != 1 {
		t.Fatalf("issues = %q", h.host.issues)
	}

	// The cap on open reports.
	h.bot.cfg.SuggestCooldown, h.bot.cfg.SuggestMaxOpen = 0, 1
	h.tg.take()
	h.tap(cbReport + h.id(triangles))
	if s := toasts(h.tg.take()); len(s) != 1 || s[0] != fmt.Sprintf(i18n.RU.TooManyOpen, 1) {
		t.Errorf("cap = %q", s)
	}
}

func TestDialogEdges(t *testing.T) {
	h := newHarness(t, nil)
	h.tap(cbReport + h.id(triangles))
	first := h.bot.mustDialog(t).p.PromptID
	h.tg.take()

	h.send(&models.Message{Sticker: &models.Sticker{FileID: "s"}})
	h.send(&models.Message{Document: &models.Document{FileID: "d", FileName: "notes.pdf"}})
	if s := texts(h.tg.take(), "sendMessage"); len(s) != 2 || s[0] != i18n.RU.OnlyTextAndImages || s[1] != i18n.RU.OnlyTextAndImages {
		t.Errorf("sticker and pdf = %q", s)
	}
	h.say("опечатка")
	h.tg.take()
	// A button of the replaced prompt does nothing but lose its keyboard.
	h.tapOn(first, cbSend)
	if c := h.tg.take(); len(texts(c, "sendMessage")) != 0 || len(texts(c, "editMessageReplyMarkup")) != 1 || len(h.host.issues) != 0 {
		t.Errorf("stale button = %+v", c)
	}

	h.tap(cbCancel)
	if s := texts(h.tg.take(), "sendMessage"); len(s) != 0 {
		t.Errorf("cancel said %q", s)
	}
	// With no dialog, /cancel only goes itself.
	h.say("/cancel")
	if c := h.tg.take(); len(c) != 1 || c[0].method != "deleteMessage" {
		t.Errorf("second cancel = %+v", c)
	}
	h.say("просто текст")
	if s := texts(h.tg.take(), "sendMessage"); len(s) != 1 || s[0] != i18n.RU.UnknownCommand {
		t.Errorf("no dialog = %q", s)
	}

	// An expired dialog says so.
	h.tap(cbReportGrade + h.bot.catalog().NoteByPath(triangles).Grade().ID)
	h.tg.take()
	d := h.bot.mustDialog(t)
	d.ses.ExpiresAt = time.Now().Add(-time.Minute)
	h.store.PutSession(h.ctx, d.ses)
	h.say("нет темы")
	if s := texts(h.tg.take(), "sendMessage"); len(s) != 1 || s[0] != i18n.RU.Expired {
		t.Errorf("expired = %q", s)
	}
	h.tapOn(d.p.PromptID, cbSend)
	if c := h.tg.take(); len(c) == 0 || c[0].params["text"] != i18n.RU.Expired {
		t.Errorf("button after expiry = %+v", c)
	}
}

// Buttons and dialogs of earlier versions, still on users' screens.
func TestEarlierVersions(t *testing.T) {
	h := newHarness(t, nil)

	// «Исправить текст» of an old menu loses its keyboard.
	h.tapWith(900, "ef", `{"inline_keyboard":[[{"text":"Исправить текст","callback_data":"ef"}]]}`)
	if c := h.tg.take(); len(c) != 2 || c[0].method != "answerCallbackQuery" || c[1].method != "editMessageReplyMarkup" {
		t.Errorf("old button = %+v", c)
	}

	// A text sent into an old edit dialog ends it and is taken as usual.
	h.store.PutSession(h.ctx, store.Session{TgID: 42, State: "suggest_edit", ArticlePath: triangles,
		Payload: `{"note_id":"x","raw":"…"}`, ExpiresAt: time.Now().Add(time.Hour)})
	h.say("исправленный текст")
	if s := texts(h.tg.take(), "sendMessage"); len(s) != 1 || s[0] != i18n.RU.UnknownCommand {
		t.Errorf("old dialog = %q", s)
	}
	if d, _ := h.bot.loadDialog(h.ctx, 42, 42); d != nil {
		t.Error("old dialog survived")
	}

	// An edit's pull request merged after the update: the user hears about
	// it, and it stays out of the rank.
	h.store.TouchUser(h.ctx, 42, "anya", "Аня")
	edit := &store.Suggestion{ShortID: "e1", TgID: 42, Kind: store.KindEdit, ArticlePath: triangles, Title: "Треугольники",
		Branch: "suggest/42-treugolniki-e1", PRNumber: 30}
	h.store.CreateSuggestion(h.ctx, edit)
	h.bot.HandleGitEvent(h.ctx, githost.Event{Kind: githost.PRMerged, PRNumber: 30, HeadBranch: edit.Branch})
	c := to(h.tg.take(), "sendMessage", 42)
	if len(c) != 1 || c[0].params["text"] != "Ваша правка в «Треугольники» принята. Спасибо!" {
		t.Errorf("merge notification = %+v", c)
	}
	if u, _ := h.store.User(h.ctx, 42); u.Accepted != 0 {
		t.Errorf("accepted = %d", u.Accepted)
	}
	if got := h.bot.historyMarkdown(h.ctx, 42); !strings.Contains(got, "— правка, ") {
		t.Errorf("history:\n%s", got)
	}
}

func TestReconcile(t *testing.T) {
	h := newHarness(t, nil)
	h.tap(cbReport + h.id(triangles))
	h.say("опечатка")
	h.tap(cbSend)
	h.tg.take()

	// No webhook: the periodic check finds the accepted report.
	h.bot.reconcile(h.ctx)
	if c := h.tg.take(); len(c) != 0 {
		t.Errorf("open issue reported: %+v", c)
	}
	h.host.setState(7, githost.State{Closed: true, Labels: []string{"accepted"}})
	h.bot.reconcile(h.ctx)
	if s := texts(h.tg.take(), "sendMessage"); len(s) != 1 || !strings.Contains(s[0], "исправили") {
		t.Errorf("reconcile = %q", s)
	}
	if open, _ := h.store.OpenSuggestions(h.ctx); len(open) != 0 {
		t.Errorf("still open: %+v", open)
	}
}

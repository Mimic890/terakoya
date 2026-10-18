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
	prs         []githost.NewPR
	issues      []string // titles
	bodies      map[int]string
	attachments []string
	states      map[int]githost.State
	number      int
}

func (h *fakeHost) nextNumber() int {
	if h.number == 0 {
		h.number = 6
	}
	h.number++
	return h.number
}

func (h *fakeHost) OpenPR(_ context.Context, pr githost.NewPR) (githost.PR, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.prs = append(h.prs, pr)
	return githost.PR{Number: h.nextNumber(), URL: "https://host/pr"}, nil
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
	cfg.Ladder, _ = ranks.Parse("kōhai:0,senpai:1")
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
	// Аня may edit; access itself is tested with other users.
	st.TouchUser(ctx, anya.ID, anya.Username, anya.FirstName)
	st.RequestAccess(ctx, anya.ID, "", "")
	st.DecideAccess(ctx, anya.ID, true, ivan.ID)
	return &harness{t: t, ctx: ctx, bot: b, tg: fake, host: host, store: st, remote: remote, upstream: upstream, msgID: 100,
		user: anya}
}

var (
	anya = models.User{ID: 42, Username: "anya", FirstName: "Аня"}
	ivan = models.User{ID: 7, Username: "ivan", FirstName: "Иван"} // the admin
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

func TestFixText(t *testing.T) {
	h := newHarness(t, nil)
	base := h.bot.repo.HeadSHA(h.ctx)
	orig := h.file("main", triangles)

	h.tap("e:" + h.id(triangles))
	wantPrompt(t, h.tg.take(), i18n.RU.SuggestMenuMain, "Исправить текст", "Сообщить об ошибке", "Добавить теорему, задачу…", "Отмена")

	h.tap(cbFix)
	calls := h.tg.take()
	sent := texts(calls, "sendMessage")
	if len(sent) != 2 || !strings.HasPrefix(sent[0], "Треугольники\n\nТреугольник — фигура") || strings.Contains(sent[0], "**") || strings.Contains(sent[0], "##") || strings.Contains(sent[0], "\\") || strings.Contains(sent[0], "[[") {
		t.Fatalf("plain text = %q", sent)
	}
	wantPrompt(t, calls, i18n.RU.EditInstructions, "Отмена")
	if len(texts(calls, "editMessageReplyMarkup")) != 1 {
		t.Error("the menu kept its buttons")
	}

	// Main moves on while the user is editing.
	os.WriteFile(filepath.Join(h.upstream, triangles), []byte(strings.Replace(orig, "Задача", "Упражнение", 1)), 0o644)
	git(t, h.upstream, "commit", "--quiet", "-am", "upstream edit")
	git(t, h.upstream, "push", "--quiet", "origin", "HEAD:main")

	edited := strings.Replace(sent[0], "равна 180°:", "всегда равна 180°:", 1)
	h.say(edited)
	wantPrompt(t, h.tg.take(), i18n.RU.EditReceived, "Отправить", "Отмена")
	h.photo("pic1", "после таблицы")
	wantPrompt(t, h.tg.take(), fmt.Sprintf(i18n.RU.ImageAdded, 1), "Отправить")

	h.tap(cbSend)
	calls = h.tg.take()
	if s := texts(calls, "sendMessage"); len(s) != 1 || s[0] != i18n.RU.Submitting {
		t.Errorf("messages = %q", s)
	}
	if s := texts(calls, "editMessageText"); len(s) != 1 || s[0] != i18n.RU.SubmittedEdit {
		t.Errorf("outcome = %q", s)
	}
	if len(h.host.prs) != 1 {
		t.Fatalf("PRs = %+v", h.host.prs)
	}
	pr := h.host.prs[0]
	if !strings.HasPrefix(pr.Head, "suggest/42-treugolniki-") || pr.Title != "Suggest edit: Треугольники" {
		t.Errorf("pr = %+v", pr)
	}
	// The branch starts from the commit the user saw, and only the edited
	// words differ from it: the formula keeps its LaTeX.
	if parent := git(t, h.remote, "rev-parse", pr.Head+"^"); parent != base {
		t.Errorf("branch parent = %s, want %s", parent, base)
	}
	if got := h.file(pr.Head, triangles); got != strings.Replace(orig, "равна $180^\\circ$:", "всегда равна $180^\\circ$:", 1) {
		t.Errorf("committed file:\n%s", got)
	}
	short := strings.TrimPrefix(pr.Head, "suggest/42-treugolniki-")
	img := "Геометрия/7 класс/01 Треугольники/_assets/treugolniki-" + short + "-1.jpg"
	if got := h.file(pr.Head, img); got != "IMG:pic1" {
		t.Errorf("picture = %q", got)
	}
	for _, want := range []string{"`42`", "@anya", base, "### To check", "> Сумма углов любого треугольника равна 180°:",
		"`" + img + "` — после таблицы", "https://host/raw/" + pr.Head + "/" + img, "### Text as sent"} {
		if !strings.Contains(pr.Body, want) {
			t.Errorf("PR body misses %q:\n%s", want, pr.Body)
		}
	}
	if _, err := h.store.Session(h.ctx, 42); err == nil {
		t.Error("session not cleared")
	}

	// Rate limit.
	h.tap("e:" + h.id(triangles))
	if c := h.tg.take(); len(c) != 1 || c[0].params["text"] != i18n.RU.TooOften {
		t.Errorf("rate limit = %+v", c)
	}

	// Merged: rank goes up, the user is told and can open the topic.
	h.bot.HandleGitEvent(h.ctx, githost.Event{Kind: githost.PRMerged, PRNumber: 7, HeadBranch: pr.Head})
	c := last(t, h.tg.take())
	if !strings.Contains(c.params["text"], "Ваша правка в «Треугольники» принята") || !strings.Contains(c.params["text"], "senpai") ||
		!strings.Contains(c.params["reply_markup"], `"o:`+h.id(triangles)+`"`) {
		t.Errorf("merge notification = %+v", c.params)
	}
	h.bot.HandleGitEvent(h.ctx, githost.Event{Kind: githost.PRMerged, PRNumber: 7, HeadBranch: pr.Head})
	if calls := h.tg.take(); len(calls) != 0 {
		t.Errorf("repeated delivery sent %+v", calls)
	}
	// The notification button opens the article and keeps the notification.
	h.tapOn(555, "o:"+h.id(triangles))
	calls = h.tg.take()
	if len(texts(calls, "sendRichMessage")) != 1 || len(texts(calls, "deleteMessage")) != 0 {
		t.Errorf("open = %+v", calls)
	}
}

func TestFixTextUnchangedShrunkAndGlued(t *testing.T) {
	h := newHarness(t, nil)
	h.tap("e:" + h.id(triangles))
	h.tg.take()
	h.tap(cbFix)
	plainText := texts(h.tg.take(), "sendMessage")[0]

	h.say(plainText + "\n")
	wantPrompt(t, h.tg.take(), i18n.RU.EditUnchanged, "Отмена")

	h.say("Тут всё неправильно.")
	wantPrompt(t, h.tg.take(), i18n.RU.EditShrunk, "Отправить как есть", "Отмена")

	// Telegram splits a long paste: the parts arrive within a second and
	// are joined back.
	cut := strings.Index(plainText, "\n\nВиды")
	first, second := plainText[:cut], strings.Replace(plainText[cut:], "Задача", "Загадка", 1)
	h.send(&models.Message{Text: first, Date: 5000})
	h.send(&models.Message{Text: second, Date: 5001})
	wantPrompt(t, h.tg.take(), i18n.RU.EditReceived, "Отправить")
	h.tap(cbSend)
	h.tg.take()
	if len(h.host.prs) != 1 {
		t.Fatalf("PRs = %+v", h.host.prs)
	}
	got := h.file(h.host.prs[0].Head, triangles)
	if !strings.Contains(got, "Загадка «на смекалку»: тетрадь стоит \\$2") || !strings.Contains(got, "$$\\angle A") ||
		!strings.Contains(got, "## Виды треугольников") {
		t.Errorf("glued edit:\n%s", got)
	}
}

func TestFixTextLongArticle(t *testing.T) {
	var b strings.Builder
	b.WriteString("# Длинная тема\n\nВступление.\n\n## Первый раздел\n\n")
	for i := 0; i < 12; i++ {
		fmt.Fprintf(&b, "Абзац %d: %s\n\n", i, strings.Repeat("много слов про $x^2$ и треугольники ", 10))
	}
	b.WriteString("## Второй раздел\n\nКороткий **раздел**.\n")
	long := "Геометрия/7 класс/05 Длинная/Длинная.md"
	h := newHarness(t, map[string]string{long: b.String()})

	h.tap("e:" + h.id(long))
	h.tap(cbFix)
	wantPrompt(t, h.tg.take(), i18n.RU.ChooseSection, "Начало", "Первый раздел (1)", "Первый раздел (2)", "Второй раздел")

	_, secs := h.bot.sections(h.bot.mustDialog(t))
	h.tap(fmt.Sprintf("%s%d", cbSection, len(secs)-1))
	sent := texts(h.tg.take(), "sendMessage")
	if len(sent) != 2 || sent[0] != "Второй раздел\n\nКороткий раздел." {
		t.Fatalf("section text = %q", sent)
	}
	h.say("Второй раздел\n\nКороткий, но важный раздел.")
	h.tap(cbSend)
	h.tg.take()
	pr := h.host.prs[0]
	got := h.file(pr.Head, long)
	if got+"\n" != strings.Replace(b.String(), "Короткий **раздел**.", "Короткий, но важный **раздел**.", 1) {
		t.Errorf("committed:\n%s", got)
	}
	if !strings.Contains(pr.Body, "Section: Второй раздел") {
		t.Errorf("PR body:\n%s", pr.Body)
	}
}

func TestReport(t *testing.T) {
	h := newHarness(t, nil)
	h.bot.rejectGrace = 0
	base := h.bot.repo.HeadSHA(h.ctx)

	h.tap("e:" + h.id(triangles))
	h.tap(cbReport)
	wantPrompt(t, h.tg.take(), i18n.RU.ReportPrompt, "Отмена")
	h.say("в теореме опечатка, должно быть 180°")
	wantPrompt(t, h.tg.take(), i18n.RU.ReportReceived, "Отправить", "Отмена")
	h.photo("shot", "вот тут")
	wantPrompt(t, h.tg.take(), i18n.RU.ReportReceived, "Отправить")
	h.tap(cbSend)
	if s := texts(h.tg.take(), "editMessageText"); len(s) != 1 || s[0] != i18n.RU.SubmittedReport {
		t.Errorf("outcome = %q", s)
	}
	if len(h.host.issues) != 1 || h.host.issues[0] != "Error report: Треугольники" || len(h.host.prs) != 0 {
		t.Fatalf("issues %q PRs %d", h.host.issues, len(h.host.prs))
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

	// The label added afterwards turns it into an accepted report, once.
	labeled := githost.Event{Kind: githost.IssueChanged, PRNumber: 7, State: githost.State{Closed: true, Labels: []string{"Accepted"}}}
	h.bot.HandleGitEvent(h.ctx, labeled)
	h.bot.HandleGitEvent(h.ctx, labeled)
	got = texts(h.tg.take(), "sendMessage")
	if len(got) != 1 || !strings.Contains(got[0], "Ошибку в «Треугольники» исправили") || !strings.Contains(got[0], "senpai") {
		t.Errorf("late accept = %q", got)
	}
	if u, _ := h.store.User(h.ctx, 42); u.MergedCount != 1 {
		t.Errorf("merged count = %d", u.MergedCount)
	}
}

func TestNewTopic(t *testing.T) {
	h := newHarness(t, nil)
	g := h.bot.catalog().NoteByPath(triangles).Grade()
	h.tap("n:" + g.ID)
	wantPrompt(t, h.tg.take(), "Новая тема: Геометрия · 7 класс. Как она называется?", "Отмена")

	h.photo("early", "")
	if s := texts(h.tg.take(), "sendMessage"); len(s) != 1 || s[0] != i18n.RU.NewTitleFirst {
		t.Errorf("photo before title = %q", s)
	}
	// A note with this file name exists, in any topic.
	h.say("треугольники")
	wantPrompt(t, h.tg.take(), "«Треугольники» уже есть. Пришлите другое название или откройте то, что есть.",
		"Открыть «Треугольники»", "Отмена")
	h.say("Новая: тема?")
	wantPrompt(t, h.tg.take(), i18n.RU.NewTextPrompt)
	h.tap(cbCancel)
	h.tap("n:" + g.ID)
	h.tg.take()
	h.say(strings.Repeat("очень ", 20) + "длинно")
	wantPrompt(t, h.tg.take(), i18n.RU.NewTitleTooLong)

	h.say("  Ромб ")
	wantPrompt(t, h.tg.take(), i18n.RU.NewTextPrompt, "Отмена")
	h.send(&models.Message{Text: "Ромб — параллелограмм, у которого все стороны равны.", Date: 7000})
	wantPrompt(t, h.tg.take(), i18n.RU.NewImagesPrompt, "Готово", "Отмена")
	h.send(&models.Message{Text: "Диагонали ромба перпендикулярны: AC перпендикулярно BD.", Date: 7100})
	wantPrompt(t, h.tg.take(), i18n.RU.NewMoreText, "Готово")
	h.photo("romb", "после первого абзаца")
	wantPrompt(t, h.tg.take(), fmt.Sprintf(i18n.RU.NewImageAdded, 1), "Готово")
	h.tap(cbSend)
	if s := texts(h.tg.take(), "editMessageText"); len(s) != 1 || s[0] != fmt.Sprintf(i18n.RU.SubmittedNew, "Ромб") {
		t.Errorf("outcome = %q", s)
	}

	pr := h.host.prs[0]
	want := "# Ромб\n\nРомб — параллелограмм, у которого все стороны равны.\n\n" +
		"Диагонали ромба перпендикулярны: AC перпендикулярно BD."
	if got := h.file(pr.Head, "Геометрия/7 класс/02 Ромб/Ромб.md"); got != want {
		t.Errorf("new file:\n%s", got)
	}
	files := git(t, h.remote, "diff", "--name-only", pr.Head+"^", pr.Head)
	if !strings.Contains(files, "Геометрия/7 класс/02 Ромб/_assets/romb-") || pr.Title != "Suggest new topic: Ромб" ||
		!strings.Contains(pr.Body, "после первого абзаца") {
		t.Errorf("files %q PR %+v", files, pr)
	}

	h.bot.HandleGitEvent(h.ctx, githost.Event{Kind: githost.PRClosed, PRNumber: 7, HeadBranch: pr.Head})
	got := texts(h.tg.take(), "sendMessage")
	if len(got) != 1 || !strings.Contains(got[0], "Тему «Ромб» не добавили") || !strings.Contains(got[0], "Слишком коротко.") {
		t.Errorf("close notification = %q", got)
	}
}

func TestDialogEdges(t *testing.T) {
	h := newHarness(t, nil)
	h.tap("e:" + h.id(triangles))
	menu := h.bot.mustDialog(t).p.PromptID
	h.tg.take()

	h.say("просто текст")
	if s := texts(h.tg.take(), "sendMessage"); len(s) != 1 || s[0] != i18n.RU.UseButtons {
		t.Errorf("text in the menu = %q", s)
	}
	h.send(&models.Message{Sticker: &models.Sticker{FileID: "s"}})
	h.send(&models.Message{Document: &models.Document{FileID: "d", FileName: "notes.pdf"}})
	if s := texts(h.tg.take(), "sendMessage"); len(s) != 2 || s[0] != i18n.RU.OnlyTextAndImages || s[1] != i18n.RU.OnlyTextAndImages {
		t.Errorf("sticker and pdf = %q", s)
	}

	h.tap(cbReport)
	h.tg.take()
	// A button of the replaced menu does nothing but lose its keyboard.
	h.tapOn(menu, cbFix)
	if c := h.tg.take(); len(texts(c, "sendMessage")) != 0 || len(texts(c, "editMessageReplyMarkup")) != 1 {
		t.Errorf("stale button = %+v", c)
	}

	h.tap(cbCancel)
	if s := texts(h.tg.take(), "sendMessage"); len(s) != 1 || s[0] != i18n.RU.Cancelled {
		t.Errorf("cancel = %q", s)
	}
	h.say("/cancel")
	if s := texts(h.tg.take(), "sendMessage"); len(s) != 1 || s[0] != i18n.RU.NothingToCancel {
		t.Errorf("second cancel = %q", s)
	}
	h.say("просто текст")
	if s := texts(h.tg.take(), "sendMessage"); len(s) != 1 || s[0] != i18n.RU.UnknownCommand {
		t.Errorf("no dialog = %q", s)
	}

	// An expired dialog says so.
	h.tap("n:" + h.bot.catalog().NoteByPath(triangles).Grade().ID)
	h.tg.take()
	d := h.bot.mustDialog(t)
	d.ses.ExpiresAt = time.Now().Add(-time.Minute)
	h.store.PutSession(h.ctx, d.ses)
	h.say("Ромб")
	if s := texts(h.tg.take(), "sendMessage"); len(s) != 1 || s[0] != i18n.RU.Expired {
		t.Errorf("expired = %q", s)
	}
	h.tapOn(d.p.PromptID, cbSend)
	if c := h.tg.take(); len(c) == 0 || c[0].params["text"] != i18n.RU.Expired {
		t.Errorf("button after expiry = %+v", c)
	}
}

func TestReconcile(t *testing.T) {
	h := newHarness(t, nil)
	h.tap("e:" + h.id(triangles))
	h.tg.take()
	h.tap(cbFix)
	plainText := texts(h.tg.take(), "sendMessage")[0]
	h.say(strings.Replace(plainText, "Задача", "Загадка", 1))
	h.tap(cbSend)
	h.tg.take()

	// No webhook: the periodic check finds the merge.
	h.bot.reconcile(h.ctx)
	if c := h.tg.take(); len(c) != 0 {
		t.Errorf("open PR reported: %+v", c)
	}
	h.host.setState(7, githost.State{Closed: true, Merged: true})
	h.bot.reconcile(h.ctx)
	if s := texts(h.tg.take(), "sendMessage"); len(s) != 1 || !strings.Contains(s[0], "принята") {
		t.Errorf("reconcile = %q", s)
	}
	if open, _ := h.store.OpenSuggestions(h.ctx); len(open) != 0 {
		t.Errorf("still open: %+v", open)
	}
}

func TestGlueMidSentence(t *testing.T) {
	h := newHarness(t, nil)
	h.tap("e:" + h.id(triangles))
	h.tg.take()
	h.tap(cbFix)
	plainText := texts(h.tg.take(), "sendMessage")[0]
	edited := strings.Replace(plainText, "Задача", "Загадка", 1)
	// Cut inside "попарно соединяющих"; Telegram trims the space.
	cut := strings.Index(edited, " соединяющих")
	h.send(&models.Message{Text: edited[:cut], Date: 5000})
	h.send(&models.Message{Text: edited[cut+1:], Date: 5001})
	h.tap(cbSend)
	h.tg.take()
	got := h.file(h.host.prs[0].Head, triangles)
	orig := h.file("main", triangles)
	if got != strings.Replace(orig, "Задача", "Загадка", 1) {
		t.Errorf("glued mid-sentence:\n%s", got)
	}
}

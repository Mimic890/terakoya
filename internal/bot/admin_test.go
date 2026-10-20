package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/i18n"
	"github.com/Mimic890/terakoya/internal/nick"
	"github.com/Mimic890/terakoya/internal/store"
)

// setClock makes the bot and its store see the same time.
func (h *harness) setClock(at *time.Time) {
	clock := func() time.Time { return *at }
	h.bot.clock = clock
	h.store.SetClock(clock)
}

func lastSeen(t *testing.T, h *harness, id int64) time.Time {
	t.Helper()
	u, err := h.store.UserInfo(h.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return u.LastSeen
}

func TestLastSeenThrottled(t *testing.T) {
	h := newHarness(t, nil)
	now := time.Now().Truncate(time.Second)
	h.setClock(&now)
	h.say("просто текст")
	if got := lastSeen(t, h, anya.ID); !got.Equal(now) {
		t.Fatalf("first = %v", got)
	}
	// Within five minutes nothing is written, taps included.
	first := now
	now = now.Add(4 * time.Minute)
	h.say("ещё")
	h.tapOn(100, cbHome)
	if got := lastSeen(t, h, anya.ID); !got.Equal(first) {
		t.Errorf("throttled = %v, want %v", got, first)
	}
	now = now.Add(2 * time.Minute)
	h.tapOn(100, cbHome)
	if got := lastSeen(t, h, anya.ID); !got.Equal(now) {
		t.Errorf("after 6 min = %v, want %v", got, now)
	}
	// A new user is recorded by their first update.
	h.as(boris)
	h.tapOn(100, cbHome)
	if got := lastSeen(t, h, boris.ID); !got.Equal(now) {
		t.Errorf("boris = %v", got)
	}
}

func TestNoteOpensCounted(t *testing.T) {
	h := newHarness(t, nil)
	tri := h.id(triangles)
	h.tapOn(500, cbArticle+tri)
	h.tapOn(501, cbArticle+tri)
	h.tapOn(502, cbArticle+h.id(vmain))
	h.say("/start " + tri) // a deep link opens it too
	top, err := h.store.TopNotes(h.ctx, "", 10)
	if err != nil || len(top) != 2 || top[0].NoteID != tri || top[0].Opens != 3 || top[0].Title != "Треугольники" || top[1].Opens != 1 {
		t.Errorf("opens = %+v, %v", top, err)
	}
}

func TestTrafficCounted(t *testing.T) {
	h := newHarness(t, nil)
	h.say("/start")
	got := h.bot.traffic.Total()
	if got.Calls < 2 || got.Sent == 0 || got.Received == 0 {
		t.Fatalf("traffic = %+v", got)
	}
	h.bot.storeTraffic(h.ctx)
	today, total, err := h.store.TrafficTotals(h.ctx, store.Day(time.Now()))
	if err != nil || today.Calls != got.Calls || total != today {
		t.Errorf("stored = %+v %+v %v, counted %+v", today, total, err, got)
	}
	if h.bot.traffic.Pending() != (store.Traffic{}) {
		t.Error("pending after storing")
	}
}

// adminPanel opens /admin as Иван and returns the panel's Markdown and
// buttons. CPU use is measured without waiting.
func (h *harness) adminPanel() (md, kb string) {
	h.t.Helper()
	h.bot.sleep = func(context.Context, time.Duration) {}
	h.as(ivan)
	h.say("/admin")
	return h.rich(h.tg.take())
}

// rich returns the Markdown and buttons of the last rich message.
func (h *harness) rich(calls []call) (md, kb string) {
	h.t.Helper()
	c := lastRich(h.t, calls)
	var rm struct{ Markdown string }
	json.Unmarshal([]byte(c.params["rich_message"]), &rm)
	return rm.Markdown, c.params["reply_markup"]
}

func TestAdminPanel(t *testing.T) {
	h := newHarness(t, nil)
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "proc", "self"), 0o755)
	os.WriteFile(filepath.Join(root, "proc", "self", "status"), []byte("VmRSS:\t   20480 kB\n"), 0o644)
	os.WriteFile(filepath.Join(root, "proc", "self", "stat"), []byte("1 (terakoya) S 1 1 1 0 -1 0 0 0 0 0 10 5 0 0\n"), 0o644)
	for _, f := range []string{"rx_bytes", "tx_bytes"} {
		os.MkdirAll(filepath.Join(root, "sys", "class", "net", "eth0", "statistics"), 0o755)
		os.WriteFile(filepath.Join(root, "sys", "class", "net", "eth0", "statistics", f), []byte("1048576"), 0o644)
	}
	h.bot.procRoot, h.bot.sysRoot = filepath.Join(root, "proc"), filepath.Join(root, "sys")
	h.tapOn(500, cbArticle+h.id(triangles))
	h.tapOn(501, cbArticle+h.id(triangles))
	h.tapOn(502, cbArticle+h.id(vmain))
	// Counted before titles were kept: left out, never shown as an ID.
	h.store.NoteOpened(h.ctx, "old-id", "", "")
	// Removed from the vault since: named by its last title.
	h.store.NoteOpened(h.ctx, "gone-id", "Старая заметка", "Геометрия/7 класс/01 Старое/Старая заметка.md")
	h.accept(anya, 2)
	h.store.CreateSuggestion(h.ctx, &store.Suggestion{ShortID: "o", TgID: anya.ID, Kind: store.KindReport, Issue: 50})

	md, kb := h.adminPanel()
	for _, want := range []string{"# Admin panel\n\nОбновлено ",
		"## Сервер\n\n| Показатель | Значение |\n|---|---|\n| CPU | 0,0 % одного ядра, ядер: ",
		"| Память | RSS 20 МБ, Go heap ", "| Диск | свободно ", "| Сеть контейнера | получено 1 МБ, отправлено 1 МБ |",
		"| Telegram API сегодня | запросов ", "| Работает | ", "| Версия | dev |",
		"## Хранилище", "| file\\_id в кэше | 0 |", "| Резервные копии | папка latest |\n| Последняя копия | ещё не было |",
		"## Vault", "| Заметок | ", "| Последнее обновление | с запуска не было |",
		"## Пользователи\n\n| Всего | Новых сегодня |",
		"| Всего | 1 | 2 | 0 |",
		"## Документы\n\n| Открытий сегодня | За 7 дней | За 180 дней |\n|---|---|---|\n| 5 | 5 | 5 |",
		"### Чаще всего открывают, 7 дней\n\n| № | Документ | Раздел | Открытий |\n|---|---|---|---|\n| 1 | Треугольники | ",
		"| Старая заметка (удалён) | Геометрия / 7 класс / 01 Старое | 1 |",
		"### Ищут, но не находят, 180 дней\n\nПока нет."} {
		if !strings.Contains(md, want) {
			t.Errorf("panel misses %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "old-id") || strings.Contains(md, "Рассыл") {
		t.Errorf("panel shows an ID or broadcasts:\n%s", md)
	}
	if kb != `{"inline_keyboard":[[{"text":"Пользователи","callback_data":"ad:u:0"},{"text":"Обновить vault","callback_data":"ad:y"}],[{"text":"Обновить","callback_data":"ad:m"}]]}` {
		t.Errorf("buttons = %s", kb)
	}
	// «Обновить» sends the panel anew; the old one goes.
	panel := h.tg.lastID("sendRichMessage")
	h.tapOn(panel, cbAdminMain)
	calls := h.tg.take()
	if md, _ := h.rich(calls); !strings.HasPrefix(md, "# Admin panel") || !contains(ids(calls, "deleteMessage"), fmt.Sprint(panel)) {
		t.Errorf("refresh = %+v", calls)
	}
	// Sizes are reused for a minute.
	first := h.bot.sizes.at
	h.tapOn(panel, cbAdminMain)
	if h.bot.sizes.at != first {
		t.Error("sizes recomputed within a minute")
	}
}

func TestAdminOnlyForAdmins(t *testing.T) {
	h := newHarness(t, nil)
	h.say("/admin")
	if c := h.tg.take(); len(c) != 1 || c[0].method != "deleteMessage" {
		t.Errorf("/admin = %+v", c)
	}
	for _, data := range []string{cbAdminMain, cbAdminUsers + "0", cbAdminSync} {
		h.tapOn(100, data)
		if c := h.tg.take(); len(c) != 1 || c[0].method != "answerCallbackQuery" || c[0].params["text"] != "" {
			t.Errorf("%s = %+v", data, c)
		}
	}
}

func TestAdminUsersTable(t *testing.T) {
	h := newHarness(t, nil)
	now := time.Date(2026, 10, 20, 14, 5, 0, 0, time.Local)
	h.setClock(&now)
	for i := 1; i <= 30; i++ {
		now = now.Add(time.Minute)
		h.store.Seen(h.ctx, int64(1000+i), fmt.Sprintf("user_%d", i), fmt.Sprintf("Ученик %d", i))
	}
	h.store.Seen(h.ctx, 2000, "", "Оля | Ко")
	h.store.SetNickname(h.ctx, 2000, "Звезда", nick.Key("Звезда"))
	h.store.MarkBlocked(h.ctx, 1001)
	_, kb := h.adminPanel()
	if !strings.Contains(kb, `"ad:u:0"`) {
		t.Fatalf("no «Пользователи»: %s", kb)
	}

	// Аня has used the bot but was never seen; Иван opened /admin; 31 more.
	anyaInfo, _ := h.store.UserInfo(h.ctx, anya.ID)
	lastAnya := anyaInfo.FirstSeen
	h.tapOn(100, cbAdminUsers+"0")
	md, kb := h.rich(h.tg.take())
	stamp := now.Format("02.01.2006 15:04")
	for _, want := range []string{"# Users\n\nСтраница 1 из 2, всего пользователей: 33\n\n| № | ID | Username | Имя | Ник | Ранг |",
		// The most recent first; text from users is literal.
		"| 2000 | — | Оля \\| Ко | Звезда | kōhai | 0 | 0 | 0 | " + stamp + " | " + stamp + " | показан | нет |",
		"| 1030 | @user\\_30 | Ученик 30 | — |"} {
		if !strings.Contains(md, want) {
			t.Errorf("page 1 misses %q:\n%s", want, md)
		}
	}
	if n := strings.Count(md, "\n| "); n != 26 { // the head and 25 users
		t.Errorf("page 1 has %d rows", n)
	}
	if kb != `{"inline_keyboard":[[{"text":"Вперёд ›","callback_data":"ad:u:1"}],[{"text":"В админку","callback_data":"ad:m"}]]}` {
		t.Errorf("page 1 buttons = %s", kb)
	}
	h.tapOn(100, cbAdminUsers+"1")
	md, kb = h.rich(h.tg.take())
	if !strings.Contains(md, "Страница 2 из 2") || !strings.Contains(md, "| 33 | ") ||
		!strings.Contains(md, "| 1001 | @user\\_1 | Ученик 1 | — | kōhai | 0 | 0 | 0 | 20.10.2026 14:06 | 20.10.2026 14:06 | показан | да |") || !strings.HasSuffix(md, "| 33 | 42 | @anya | Аня | — | kōhai | 0 | 0 | 0 | "+h.bot.stamp(lastAnya)+" | — | показан | нет |\n") {
		t.Errorf("page 2 =\n%s", md)
	}
	if kb != `{"inline_keyboard":[[{"text":"‹ Назад","callback_data":"ad:u:0"}],[{"text":"В админку","callback_data":"ad:m"}]]}` {
		t.Errorf("page 2 buttons = %s", kb)
	}
	// A page past the end, from an old message, shows the last one.
	h.tapOn(100, cbAdminUsers+"7")
	if md, _ := h.rich(h.tg.take()); !strings.Contains(md, "Страница 2 из 2") {
		t.Errorf("past the end =\n%s", md)
	}
	// Back to the panel.
	h.tapOn(100, cbAdminMain)
	if md, _ := h.rich(h.tg.take()); !strings.HasPrefix(md, "# Admin panel") {
		t.Errorf("back =\n%s", md)
	}
}

func TestAdminUsersOnePage(t *testing.T) {
	h := newHarness(t, nil)
	h.adminPanel()
	h.tapOn(100, cbAdminUsers+"0")
	_, kb := h.rich(h.tg.take())
	if kb != `{"inline_keyboard":[[{"text":"В админку","callback_data":"ad:m"}]]}` {
		t.Errorf("buttons = %s", kb)
	}
}

func TestAdminSync(t *testing.T) {
	h := newHarness(t, nil)
	h.adminPanel()
	var waits []time.Duration
	h.bot.sleep = func(_ context.Context, d time.Duration) { waits = append(waits, d) }
	old := h.bot.repo.HeadSHA(h.ctx)

	// Nothing new: a wait message, the panel anew, and the outcome that
	// goes after a second.
	h.tapOn(100, cbAdminSync)
	calls := h.tg.take()
	sent := texts(to(calls, "sendMessage", ivan.ID), "sendMessage")
	if len(sent) != 2 || sent[0] != "Обновление vault…" || sent[1] != "Vault обновлён, изменений нет, коммит "+old[:7] {
		t.Fatalf("sent = %q", sent)
	}
	md, _ := h.rich(calls)
	if !strings.Contains(md, "| Последнее обновление | ") || strings.Contains(md, "с запуска не было") {
		t.Errorf("panel after sync:\n%s", md)
	}
	if del := ids(calls, "deleteMessage"); len(waits) != 2 || waits[1] != time.Second || len(del) < 2 {
		t.Errorf("waits %v, deleted %v", waits, del)
	}

	// A new note and a broken link upstream: the findings stay.
	note := "Геометрия/7 класс/02 Углы/Углы.md"
	os.MkdirAll(filepath.Join(h.upstream, filepath.Dir(note)), 0o755)
	os.WriteFile(filepath.Join(h.upstream, note), []byte("# Углы\n\nСм. [[Нет такой заметки]].\n"), 0o644)
	git(t, h.upstream, "add", ".")
	git(t, h.upstream, "commit", "--quiet", "-m", "angles")
	git(t, h.upstream, "push", "--quiet", "origin", "HEAD:main")
	newSHA := git(t, h.upstream, "rev-parse", "HEAD")
	notes := len(h.bot.catalog().Notes())
	waits = nil

	h.tapOn(100, cbAdminSync)
	calls = h.tg.take()
	sent = texts(to(calls, "sendMessage", ivan.ID), "sendMessage")
	want := fmt.Sprintf("Vault обновлён: %s → %s\nЗаметок: %d → %d\nПроверка: ошибок 1, предупреждений 0\n• %s:3: error: ",
		old[:7], newSHA[:7], notes, notes+1, note)
	if len(sent) != 2 || !strings.HasPrefix(sent[1], want) {
		t.Errorf("changed = %q\nwant prefix\n%q", sent, want)
	}
	if len(waits) != 1 { // only the CPU measurement
		t.Errorf("waits = %v", waits)
	}
	if md, _ := h.rich(calls); !strings.Contains(md, old[:7]+" → "+newSHA[:7]) {
		t.Errorf("panel after sync:\n%s", md)
	}
}

func TestSyncText(t *testing.T) {
	b := &Bot{t: i18n.RU}
	if got := b.syncText(SyncResult{Err: "fetch: timeout"}); got != "Не удалось обновить vault: fetch: timeout" {
		t.Errorf("error = %q", got)
	}
	r := SyncResult{OldCommit: "aaaaaaaaaa", NewCommit: "bbbbbbbbbb", NotesBefore: 10, NotesAfter: 9, Warnings: 2,
		Issues: []string{"a.md:1: warning: x", "b.md:2: warning: y"}}
	if got := b.syncText(r); got != "Vault обновлён: aaaaaaa → bbbbbbb\nЗаметок: 10 → 9\nПроверка: ошибок 0, предупреждений 2\n• a.md:1: warning: x\n• b.md:2: warning: y" {
		t.Errorf("changed = %q", got)
	}
}

// A search dialog of an earlier version is dropped: the text is searched.
func TestOldAdminSearchDialog(t *testing.T) {
	h := newHarness(t, nil)
	h.store.PutSession(h.ctx, store.Session{TgID: anya.ID, State: "admin_search", Payload: "{}", ExpiresAt: time.Now().Add(time.Hour)})
	h.send(&models.Message{Text: "кто-нибудь"})
	if md := searched(h.tg.take()); !strings.Contains(md, "кто-нибудь") {
		t.Errorf("search = %q", md)
	}
}

func TestSocketBroadcast(t *testing.T) {
	h, _ := broadcastHarness(t)
	h.tg.take()
	r, err := h.bot.Broadcast(h.ctx, "Завтра *обновление*", nil)
	h.bot.bg.Wait()
	if err != nil || r.Recipients != 3 {
		t.Fatalf("started = %+v, %v", r, err)
	}
	calls := h.tg.take()
	// Plain text, sent literally; no progress messages, as there is no chat.
	for _, id := range []int64{anya.ID, boris.ID, 44} {
		if got := richTo(calls, id); len(got) != 1 || got[0] != `{"markdown":"Завтра \\*обновление\\*"}` {
			t.Errorf("to %d: %q", id, got)
		}
	}
	// The first admin gets a test copy first, and no progress messages.
	if got := richTo(calls, ivan.ID); len(got) != 1 || got[0] != `{"markdown":"Завтра \\*обновление\\*"}` {
		t.Errorf("test copy = %q", got)
	}
	if len(to(calls, "sendMessage", ivan.ID))+len(to(calls, "editMessageText", ivan.ID)) != 0 {
		t.Errorf("progress to the admin: %+v", calls)
	}
	all, _ := h.store.Broadcasts(h.ctx)
	if len(all) != 1 || all[0].Sent != 3 || all[0].AdminID != 0 || all[0].Finished.IsZero() {
		t.Errorf("stored = %+v", all)
	}

	// A picture is uploaded once, to the first admin, and that message goes.
	if _, err := h.bot.Broadcast(h.ctx, "", []byte("JPEG")); err != nil {
		t.Fatal(err)
	}
	h.bot.bg.Wait()
	calls = h.tg.take()
	up := to(calls, "sendPhoto", ivan.ID)
	if len(up) != 1 || !contains(ids(calls, "deleteMessage"), fmt.Sprint(h.tg.lastID("sendPhoto"))) {
		t.Errorf("upload = %+v", calls)
	}
	if got := richTo(calls, anya.ID); len(got) != 1 || !strings.Contains(got[0], `"media":"photo`) {
		t.Errorf("picture broadcast = %q", got)
	}
	if _, err := h.bot.Broadcast(h.ctx, " ", nil); err == nil {
		t.Error("an empty broadcast")
	}
}

func TestAdminUsersPages(t *testing.T) {
	h := newHarness(t, nil)
	for i := 0; i < 25; i++ {
		h.store.Seen(h.ctx, int64(100+i), "", "u")
	}
	for i := 25; i < 60; i++ {
		h.store.Seen(h.ctx, int64(100+i), "", "u")
	}
	for page, want := range map[int][2]int{0: {0, 25}, 2: {2, 11}, 9: {2, 11}, -1: {0, 25}} {
		l, err := h.bot.AdminUsers(h.ctx, "", page)
		if err != nil || l.Page != want[0] || len(l.Users) != want[1] || l.Pages != 3 || l.Found != 61 {
			t.Errorf("page %d = page %d, %d users, %d pages, %v", page, l.Page, len(l.Users), l.Pages, err)
		}
	}
	if _, err := h.bot.AdminUser(h.ctx, "@nobody"); err != ErrNoUser {
		t.Errorf("unknown = %v", err)
	}
}

func TestSocketBroadcastTestCopyFails(t *testing.T) {
	h, _ := broadcastHarness(t)
	h.tg.fail = func(method string, p map[string]string) (int, int) {
		if method == "sendRichMessage" && p["chat_id"] == fmt.Sprint(ivan.ID) {
			return 400, 0
		}
		return 0, 0
	}
	h.tg.take()
	if _, err := h.bot.Broadcast(h.ctx, "Новость", nil); err == nil || !strings.Contains(err.Error(), "nothing was sent") {
		t.Fatalf("err = %v", err)
	}
	h.bot.bg.Wait()
	calls := h.tg.take()
	if got := richTo(calls, anya.ID); len(got) != 0 {
		t.Errorf("sent to users: %q", got)
	}
	if all, _ := h.store.Broadcasts(h.ctx); len(all) != 0 {
		t.Errorf("stored = %+v", all)
	}
	// The slot is free again.
	h.tg.fail = nil
	if _, err := h.bot.Broadcast(h.ctx, "Новость", nil); err != nil {
		t.Errorf("after a failed test copy: %v", err)
	}
	h.bot.bg.Wait()
}

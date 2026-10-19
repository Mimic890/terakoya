package bot

import (
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
	if err != nil || len(top) != 2 || top[0] != (store.NoteCount{NoteID: tri, Opens: 3}) || top[1].Opens != 1 {
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

// adminMenu opens /admin as Иван and returns the menu's message ID.
func (h *harness) adminMenu() (int, call) {
	h.t.Helper()
	h.as(ivan)
	h.say("/admin")
	calls := h.tg.take()
	c := firstCall(h.t, calls, "sendMessage")
	return h.tg.lastID("sendMessage"), c
}

// edited returns the text of the last edit of a message.
func edited(t *testing.T, calls []call, msgID int) call {
	t.Helper()
	for i := len(calls) - 1; i >= 0; i-- {
		if calls[i].method == "editMessageText" && calls[i].params["message_id"] == fmt.Sprint(msgID) {
			return calls[i]
		}
	}
	t.Fatalf("message %d not edited: %+v", msgID, calls)
	return call{}
}

func TestAdminMain(t *testing.T) {
	h := newHarness(t, nil)
	h.accept(anya, 1)
	h.store.CreateSuggestion(h.ctx, &store.Suggestion{ShortID: "o", TgID: anya.ID, Kind: store.KindReport, Issue: 50})
	menu, c := h.adminMenu()
	// Аня has used the bot; Иван is seen as he opens /admin.
	if !strings.HasPrefix(c.params["text"], "Админка\n\nПользователей: 2, активны за 24 ч: 1\nПравок на рассмотрении: 1\nVault: ") ||
		!strings.Contains(c.params["text"], "\nРаботает: ") {
		t.Errorf("main = %q", c.params["text"])
	}
	kb := c.params["reply_markup"]
	for _, b := range []string{`[{"text":"Пользователи","callback_data":"ad:u:0"},{"text":"Статистика","callback_data":"ad:s"}]`,
		`[{"text":"Сервер","callback_data":"ad:v"},{"text":"Обновить vault","callback_data":"ad:y"}]`,
		`[{"text":"Рассылка","callback_data":"ad:b"},{"text":"Закрыть","callback_data":"c"}]`} {
		if !strings.Contains(kb, b) {
			t.Errorf("main has no %s: %s", b, kb)
		}
	}
	// «Рассылка» opens /broadcast.
	h.tapOn(menu, cbAdminBcast)
	wantPrompt(t, h.tg.take(), i18n.RU.BroadcastAsk, "Отмена")
}

func TestAdminOnlyForAdmins(t *testing.T) {
	h := newHarness(t, nil)
	h.say("/admin")
	if c := h.tg.take(); len(c) != 1 || c[0].method != "deleteMessage" {
		t.Errorf("/admin = %+v", c)
	}
	for _, data := range []string{cbAdminMain, cbAdminUsers + "0", cbAdminCard + "42", cbAdminResetOK + "42", cbAdminStats,
		cbAdminServer, cbAdminSync, cbAdminBcast, cbAdminFind} {
		h.tapOn(100, data)
		if c := h.tg.take(); len(c) != 1 || c[0].method != "answerCallbackQuery" || c[0].params["text"] != "" {
			t.Errorf("%s = %+v", data, c)
		}
	}
	if d, _ := h.bot.loadDialog(h.ctx, anya.ID, anya.ID); d != nil {
		t.Error("a dialog for a non-admin")
	}
}

func TestAdminUsersPagingSearchAndCard(t *testing.T) {
	h := newHarness(t, nil)
	// Sessions expire by the real clock, so this one starts now.
	now := time.Now().Truncate(time.Second)
	h.setClock(&now)
	for i := 1; i <= 12; i++ {
		now = now.Add(time.Minute)
		h.store.Seen(h.ctx, int64(1000+i), fmt.Sprintf("user%d", i), fmt.Sprintf("Ученик %d", i))
	}
	h.store.Seen(h.ctx, 2000, "", "Ольга")
	h.store.SetNickname(h.ctx, 2000, "Звезда", nick.Key("Звезда"))
	h.store.MarkBlocked(h.ctx, 1001)
	menu, _ := h.adminMenu()

	h.tapOn(menu, cbAdminUsers+"0")
	c := edited(t, h.tg.take(), menu)
	// Аня has used the bot but was not seen; Иван was, opening /admin.
	if !strings.HasPrefix(c.params["text"], "Пользователи\n\nВсего: 15 · новых сегодня: 15 · за 7 дней: 15\nАктивны за 24 ч: 14 · за 7 дней: 14\nЗаблокировали бота: 1") {
		t.Errorf("users = %q", c.params["text"])
	}
	kb := c.params["reply_markup"]
	// The most recent first; display names tell people apart.
	if !strings.Contains(kb, `[{"text":"Звезда","callback_data":"ad:c:2000"}],[{"text":"@user11","callback_data":"ad:c:1011"}]`) ||
		!strings.Contains(kb, `[{"text":"‹","callback_data":"ad:u:0"},{"text":"1/2","callback_data":"ad:-"},{"text":"›","callback_data":"ad:u:1"}]`) ||
		strings.Count(kb, `"ad:c:`) != 10 {
		t.Errorf("page 1 = %s", kb)
	}
	h.tapOn(menu, cbAdminUsers+"1")
	kb = edited(t, h.tg.take(), menu).params["reply_markup"]
	if strings.Count(kb, `"ad:c:`) != 5 || !strings.Contains(kb, `"2/2"`) {
		t.Errorf("page 2 = %s", kb)
	}

	// Search: the question below, then the list shows what it found.
	h.tapOn(menu, cbAdminFind)
	wantPrompt(t, h.tg.take(), i18n.RU.AdminSearchAsk, "Отмена")
	msgs := h.dialogMsgs()
	h.say("ученик 1")
	calls := h.tg.take()
	if got := ids(calls, "deleteMessage"); !sameIDs(got, append(msgs, h.msgID)) {
		t.Errorf("deleted %v", got)
	}
	c = edited(t, calls, menu)
	// «Ученик 1», «Ученик 10» … «Ученик 12».
	if !strings.Contains(c.params["text"], "\n\nПоиск «ученик 1»: найдено 4") || strings.Count(c.params["reply_markup"], `"ad:c:`) != 4 ||
		!strings.Contains(c.params["reply_markup"], `{"text":"Сбросить поиск","callback_data":"ad:ux"}`) || strings.Contains(c.params["reply_markup"], `"‹"`) {
		t.Errorf("found = %+v", c.params)
	}
	for q, want := range map[string]int{"@USER12": 1, "звез": 1, "2000": 1, "нет такого": 0} {
		h.tapOn(menu, cbAdminFind)
		h.say(q)
		c = edited(t, h.tg.take(), menu)
		if got := strings.Count(c.params["reply_markup"], `"ad:c:`); got != want {
			t.Errorf("%q found %d", q, got)
		}
		if want == 0 && !strings.Contains(c.params["text"], "Никого не нашлось.") {
			t.Errorf("nobody = %q", c.params["text"])
		}
	}
	// «Отмена» of a search goes back to the list.
	h.tapOn(menu, cbAdminFind)
	h.tg.take()
	h.tap(cbCancel)
	edited(t, h.tg.take(), menu)
	h.tapOn(menu, cbAdminClear)
	if c := edited(t, h.tg.take(), menu); strings.Contains(c.params["text"], "Поиск") {
		t.Errorf("cleared = %q", c.params["text"])
	}

	// The card, and a nickname reset with a question first.
	h.tapOn(menu, cbAdminCard+"2000")
	c = edited(t, h.tg.take(), menu)
	seen := h.bot.moment(now)
	want := "Звезда\nID 2000\n\nНик: Звезда\nРанг: kōhai\nПравки: принято 0 · на рассмотрении 0 · отклонено 0\n" +
		"Впервые: " + seen + "\nПоследний раз: " + seen + "\nВ топе: показан\nЗаблокировал бота: нет"
	if c.params["text"] != want || c.params["reply_markup"] != `{"inline_keyboard":[[{"text":"Сбросить ник","callback_data":"ad:n:2000"}],[{"text":"« Назад","callback_data":"ad:u:0"}]]}` {
		t.Errorf("card =\n%q %s\nwant\n%q", c.params["text"], c.params["reply_markup"], want)
	}
	h.tapOn(menu, cbAdminReset+"2000")
	c = edited(t, h.tg.take(), menu)
	if c.params["text"] != "Сбросить ник «Звезда» у Ольга? Пользователь получит уведомление." ||
		!strings.Contains(c.params["reply_markup"], `"ad:ny:2000"`) || !strings.Contains(c.params["reply_markup"], `{"text":"Нет","callback_data":"ad:c:2000"}`) {
		t.Errorf("ask = %+v", c.params)
	}
	h.tapOn(menu, cbAdminResetOK+"2000")
	calls = h.tg.take()
	if s := texts(to(calls, "sendMessage", 2000), "sendMessage"); len(s) != 1 || s[0] != i18n.RU.NickResetNotice {
		t.Errorf("notice = %q", s)
	}
	if c := edited(t, calls, menu); !strings.HasPrefix(c.params["text"], "Ольга\nID 2000\n\nНик: нет\n") || strings.Contains(c.params["reply_markup"], "ad:n:") {
		t.Errorf("after reset = %+v", c.params)
	}
	h.tapOn(menu, cbAdminCard+"1001")
	if c := edited(t, h.tg.take(), menu); !strings.Contains(c.params["text"], "\n@user1 · ID 1001\n") || !strings.HasSuffix(c.params["text"], "Заблокировал бота: да") {
		t.Errorf("blocked card = %q", c.params["text"])
	}
}

func TestAdminStats(t *testing.T) {
	h := newHarness(t, nil)
	h.tapOn(500, cbArticle+h.id(triangles))
	h.tapOn(501, cbArticle+h.id(triangles))
	h.tapOn(502, cbArticle+h.id(vmain))
	h.store.NoteOpened(h.ctx, "gone-note")
	h.accept(anya, 2)
	h.store.CreateBroadcast(h.ctx, &store.Broadcast{Markdown: "x"}, []int64{1, 2, 3})
	menu, _ := h.adminMenu()
	h.tapOn(menu, cbAdminStats)
	r := lastRich(t, h.tg.take())
	md := r.params["rich_message"]
	for _, want := range []string{"# Статистика", "Всего: на рассмотрении 0 · принято 2 · отклонено 0",
		"## Чаще всего открывают — 7 дней", "1\\\\. \\u003ctg-button type=\\\"callback_data\\\" style=\\\"link\\\" data=\\\"a:" + h.id(triangles) + "\\\"\\u003eТреугольники\\u003c/tg-button\\u003e — 2",
		"gone-note (удалена) — 1", "Рассылок: 1", "Последняя: "} {
		if !strings.Contains(md, want) {
			t.Errorf("stats miss %q:\n%s", want, md)
		}
	}
	if !strings.Contains(r.params["reply_markup"], `"ad:m"`) {
		t.Errorf("buttons = %s", r.params["reply_markup"])
	}
}

func TestAdminServer(t *testing.T) {
	h := newHarness(t, nil)
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "proc", "self"), 0o755)
	os.WriteFile(filepath.Join(root, "proc", "self", "status"), []byte("VmRSS:\t   20480 kB\n"), 0o644)
	for _, f := range []string{"rx_bytes", "tx_bytes"} {
		os.MkdirAll(filepath.Join(root, "sys", "class", "net", "eth0", "statistics"), 0o755)
		os.WriteFile(filepath.Join(root, "sys", "class", "net", "eth0", "statistics", f), []byte("1048576"), 0o644)
	}
	h.bot.procRoot, h.bot.sysRoot = filepath.Join(root, "proc"), filepath.Join(root, "sys")
	menu, _ := h.adminMenu()

	h.tapOn(menu, cbAdminServer)
	calls := h.tg.take()
	var edits []string
	for _, c := range to(calls, "editMessageText", ivan.ID) {
		edits = append(edits, c.params["text"])
	}
	if len(edits) != 2 || edits[0] != i18n.RU.AdminComputing {
		t.Fatalf("edits = %q", edits)
	}
	for _, want := range []string{"Сервер\n\nПамять: RSS 20 МБ, Go heap ", "Работает: ", "версия dev",
		"Сеть контейнера: получено 1 МБ, отправлено 1 МБ", "Telegram API сегодня: запросов ", "Vault на диске: ",
		"Данные: ", "file_id в кэше: 0", "Vault: коммит ", "Синхронизаций с запуска не было"} {
		if !strings.Contains(edits[1], want) {
			t.Errorf("server misses %q:\n%s", want, edits[1])
		}
	}
	if !strings.Contains(edited(t, calls, menu).params["reply_markup"], `{"text":"Обновить","callback_data":"ad:v"}`) {
		t.Error("no «Обновить»")
	}
	// Sizes are reused for a minute.
	first := h.bot.sizes.at
	h.tapOn(menu, cbAdminServer)
	if h.bot.sizes.at != first {
		t.Error("sizes recomputed within a minute")
	}
}

func TestAdminSync(t *testing.T) {
	h := newHarness(t, nil)
	menu, _ := h.adminMenu()
	old := h.bot.repo.HeadSHA(h.ctx)

	h.tapOn(menu, cbAdminSync)
	calls := h.tg.take()
	e := to(calls, "editMessageText", ivan.ID)
	if len(e) != 2 || e[0].params["text"] != "Обновляю…" || e[1].params["text"] != "Изменений нет, коммит "+old[:7] {
		t.Fatalf("unchanged = %+v", e)
	}

	// A new note and a broken link upstream.
	note := "Геометрия/7 класс/02 Углы/Углы.md"
	os.MkdirAll(filepath.Join(h.upstream, filepath.Dir(note)), 0o755)
	os.WriteFile(filepath.Join(h.upstream, note), []byte("# Углы\n\nСм. [[Нет такой заметки]].\n"), 0o644)
	git(t, h.upstream, "add", ".")
	git(t, h.upstream, "commit", "--quiet", "-m", "angles")
	git(t, h.upstream, "push", "--quiet", "origin", "HEAD:main")
	newSHA := git(t, h.upstream, "rev-parse", "HEAD")
	notes := len(h.bot.catalog().Notes())

	h.tapOn(menu, cbAdminSync)
	got := edited(t, h.tg.take(), menu).params["text"]
	want := fmt.Sprintf("Vault обновлён: %s → %s\nЗаметок: %d → %d\nПроверка: ошибок 1, предупреждений 0\n• %s:3: error: ",
		old[:7], newSHA[:7], notes, notes+1, note)
	if !strings.HasPrefix(got, want) {
		t.Errorf("changed =\n%q\nwant prefix\n%q", got, want)
	}
	if r := h.bot.lastSync; r == nil || r.NewCommit != newSHA {
		t.Errorf("last sync = %+v", r)
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

func TestAdminSearchOnlyForAdmins(t *testing.T) {
	h := newHarness(t, nil)
	h.store.PutSession(h.ctx, store.Session{TgID: anya.ID, State: stateAdminSearch, Payload: "{}", ExpiresAt: time.Now().Add(time.Hour)})
	h.send(&models.Message{Text: "кто-нибудь"})
	if s := texts(h.tg.take(), "sendMessage"); len(s) != 1 || s[0] != i18n.RU.UnknownCommand {
		t.Errorf("non-admin search = %q", s)
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
	for page, want := range map[int][2]int{0: {0, 10}, 2: {2, 6}, 9: {2, 6}, -1: {0, 10}} {
		l, err := h.bot.AdminUsers(h.ctx, "", page)
		if err != nil || l.Page != want[0] || len(l.Users) != want[1] || l.Pages != 3 || l.Found != 26 {
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

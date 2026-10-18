package bot

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/i18n"
	"github.com/Mimic890/terakoya/internal/store"
)

var (
	boris = models.User{ID: 43, Username: "boris", FirstName: "Борис"}
	olga  = models.User{ID: 8, FirstName: "Ольга"} // the second admin, no @username
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

func TestAccessRequestAllowed(t *testing.T) {
	h := newHarness(t, nil)
	h.bot.cfg.Admins = []int64{ivan.ID, olga.ID}
	h.as(ivan)
	h.say("/start") // admins are known users, so their names can be shown
	h.as(olga)
	h.say("/start")
	h.tg.take()
	note := h.id(triangles)

	// Without access, «Исправить текст» turns the menu into the question.
	h.as(boris)
	h.tap("e:" + note)
	menu := h.bot.mustDialogOf(t, boris.ID).p.PromptID
	h.tap(cbFix)
	calls := h.tg.take()
	edits := to(calls, "editMessageText", boris.ID)
	if len(edits) != 1 || edits[0].params["text"] != i18n.RU.AccessNeeded || edits[0].params["message_id"] != fmt.Sprint(menu) ||
		!strings.Contains(edits[0].params["reply_markup"], `"ar:`+note+`"`) || !strings.Contains(edits[0].params["reply_markup"], `"c"`) {
		t.Fatalf("question = %+v", calls)
	}
	if d, _ := h.bot.loadDialog(h.ctx, boris.ID, boris.ID); d != nil {
		t.Error("the dialog went on")
	}

	// «Отправить запрос» goes to every admin.
	h.tapOn(menu, cbRequest+note)
	calls = h.tg.take()
	want := "Запрос на право предлагать правки\nБорис · @boris · ID 43\nПринято правок: 0 · сообщений об ошибках: 0"
	for _, admin := range []int64{ivan.ID, olga.ID} {
		m := to(calls, "sendMessage", admin)
		if len(m) != 1 || m[0].params["text"] != want || !strings.Contains(m[0].params["reply_markup"], `"ay:43"`) ||
			!strings.Contains(m[0].params["reply_markup"], `"an:43"`) {
			t.Errorf("admin %d got %+v", admin, m)
		}
	}
	if e := to(calls, "editMessageText", boris.ID); len(e) != 1 || e[0].params["text"] != i18n.RU.AccessRequested {
		t.Errorf("user sees %+v", e)
	}
	copies, _ := h.store.AccessMessages(h.ctx, boris.ID)
	if len(copies) != 2 {
		t.Fatalf("admin copies = %v", copies)
	}

	// One pending request at a time.
	h.tap("e:" + note)
	h.tap(cbFix)
	if got := toasts(h.tg.take()); got[len(got)-1] != i18n.RU.AccessPending {
		t.Errorf("toasts = %q", got)
	}
	if d := h.bot.mustDialogOf(t, boris.ID); d.ses.State != stateMenu {
		t.Errorf("state = %s; the menu should stay", d.ses.State)
	}

	// Иван allows; both copies show it, and Ольга is too late.
	h.as(ivan)
	h.tapOn(copies[ivan.ID], cbAllow+"43")
	calls = h.tg.take()
	for admin, id := range copies {
		e := to(calls, "editMessageText", admin)
		if len(e) != 1 || e[0].params["message_id"] != fmt.Sprint(id) || e[0].params["text"] != want+"\n\n✅ Разрешено — Иван (@ivan)" {
			t.Errorf("copy of %d = %+v", admin, e)
		}
	}
	m := to(calls, "sendMessage", boris.ID)
	if len(m) != 1 || m[0].params["text"] != i18n.RU.AccessGranted || !strings.Contains(m[0].params["reply_markup"], `"o:`+note+`"`) {
		t.Errorf("user told %+v", m)
	}
	h.as(olga)
	h.tapOn(copies[olga.ID], cbDeny+"43")
	if got := toasts(h.tg.take()); len(got) != 1 || got[0] != "Уже решено: Иван (@ivan)" {
		t.Errorf("late admin = %q", got)
	}

	// Now the edit starts.
	h.as(boris)
	h.tap(cbClose)
	h.tap("e:" + note)
	h.tap(cbFix)
	h.tg.take()
	if d := h.bot.mustDialogOf(t, boris.ID); d.ses.State != stateEditText {
		t.Fatalf("state = %s", d.ses.State)
	}

	// /access lists him; for others the admin commands do not exist.
	h.as(ivan)
	h.say("/access")
	got := texts(h.tg.take(), "sendMessage")
	if len(got) != 1 || !strings.Contains(got[0], "Доступ есть у 2 человек:") ||
		!strings.Contains(got[0], "• Борис (@boris), 43 — с ") || !strings.Contains(got[0], "разрешил(а) Иван (@ivan)") ||
		!strings.Contains(got[0], "Администраторы: Иван (@ivan), Ольга") {
		t.Errorf("/access = %q", got)
	}
	h.as(boris)
	h.say("/access")
	h.say("/revoke @anya")
	// The commands are deleted like any other, and get no answer.
	for _, c := range h.tg.take() {
		if c.method != "deleteMessage" {
			t.Errorf("non-admin got %s %v", c.method, c.params)
		}
	}
	if _, err := h.store.Access(h.ctx, anya.ID); err != nil {
		t.Fatal(err)
	}
	if !h.bot.mayEdit(h.ctx, anya.ID) {
		t.Error("a non-admin revoked access")
	}

	// Revoking ends his open edit at the next step.
	h.as(ivan)
	h.say("/revoke @Boris")
	calls = h.tg.take()
	if got := texts(to(calls, "sendMessage", ivan.ID), "sendMessage"); len(got) != 1 || got[0] != "Доступ отозван: Борис (@boris)." {
		t.Errorf("admin told %q", got)
	}
	if got := texts(to(calls, "sendMessage", boris.ID), "sendMessage"); len(got) != 1 || got[0] != i18n.RU.AccessRevoked {
		t.Errorf("user told %q", got)
	}
	h.say("/revoke 43")
	if got := texts(h.tg.take(), "sendMessage"); len(got) != 1 || got[0] != i18n.RU.RevokeNoAccess {
		t.Errorf("second revoke = %q", got)
	}
	h.say("/revoke 7")
	h.say("/revoke @nobody")
	h.say("/revoke")
	if got := texts(h.tg.take(), "sendMessage"); strings.Join(got, "|") != i18n.RU.RevokeAdmin+"|"+i18n.RU.RevokeUnknown+"|"+i18n.RU.RevokeUsage {
		t.Errorf("revoke errors = %q", got)
	}

	h.as(boris)
	h.say("исправленный текст")
	if got := texts(h.tg.take(), "sendMessage"); len(got) != 1 || got[0] != i18n.RU.AccessGone {
		t.Errorf("after revoke = %q", got)
	}
	if d, _ := h.bot.loadDialog(h.ctx, boris.ID, boris.ID); d != nil {
		t.Error("the dialog survived the revoke")
	}

	// A revoke counts as a denial: no new request for 7 days.
	h.tap("e:" + note)
	h.tap(cbFix)
	until := time.Now().Add(accessCooldown)
	wantText := fmt.Sprintf("Запрос отклонён. Повторить можно с %d %s.", until.Day(), i18n.RU.Months[until.Month()-1])
	if e := to(h.tg.take(), "editMessageText", boris.ID); len(e) != 1 || e[0].params["text"] != wantText ||
		!strings.Contains(e[0].params["reply_markup"], "Понятно") {
		t.Errorf("after revoke = %+v", e)
	}
}

func TestAccessDeniedAndReports(t *testing.T) {
	h := newHarness(t, nil)
	h.as(ivan)
	h.say("/start")
	h.tg.take()
	g := h.bot.catalog().NoteByPath(triangles).Grade()

	// «Добавить тему» asks in a new message, under the topic list.
	h.as(boris)
	h.tapOn(500, cbAddTopic+g.ID)
	calls := h.tg.take()
	if m := to(calls, "sendMessage", boris.ID); len(m) != 1 || m[0].params["text"] != i18n.RU.AccessNeeded ||
		!strings.Contains(m[0].params["reply_markup"], `"ar:"`) {
		t.Fatalf("question = %+v", calls)
	}
	if len(ids(calls, "deleteMessage")) != 0 || len(to(calls, "editMessageText", boris.ID)) != 0 {
		t.Error("the topic list was touched")
	}
	h.tapOn(900, cbRequest)
	copies, _ := h.store.AccessMessages(h.ctx, boris.ID)
	h.tg.take()

	h.as(ivan)
	h.tapOn(copies[ivan.ID], cbDeny+"43")
	calls = h.tg.take()
	if e := to(calls, "editMessageText", ivan.ID); len(e) != 1 || !strings.HasSuffix(e[0].params["text"], "❌ Отклонено — Иван (@ivan)") {
		t.Errorf("copy = %+v", e)
	}
	if got := texts(to(calls, "sendMessage", boris.ID), "sendMessage"); len(got) != 1 || got[0] != i18n.RU.AccessDenied {
		t.Errorf("user told %q", got)
	}
	// A non-admin's tap on an admin button does nothing.
	h.as(boris)
	h.tapOn(copies[ivan.ID], cbAllow+"43")
	if h.bot.mayEdit(h.ctx, boris.ID) {
		t.Fatal("a user allowed himself")
	}
	h.tg.take()

	// Error reports need no access.
	h.tap("e:" + h.id(triangles))
	h.tap(cbReport)
	wantPrompt(t, h.tg.take(), i18n.RU.ReportPrompt, "Отмена")

	// After a week he may ask again.
	h.bot.store.SetClock(func() time.Time { return time.Now().Add(-accessCooldown - time.Hour) })
	h.store.RequestAccess(h.ctx, boris.ID, "", "")
	h.store.DecideAccess(h.ctx, boris.ID, false, ivan.ID)
	h.bot.store.SetClock(time.Now)
	h.tap(cbCancel)
	h.tap("e:" + h.id(triangles))
	h.tap(cbFix)
	if e := to(h.tg.take(), "editMessageText", boris.ID); len(e) != 1 || e[0].params["text"] != i18n.RU.AccessNeeded {
		t.Errorf("after a week = %+v", e)
	}
}

func TestAccessWithoutAdmins(t *testing.T) {
	h := newHarness(t, nil)
	h.bot.cfg.Admins = nil
	h.as(boris)
	h.tap("e:" + h.id(triangles))
	h.tap(cbFix)
	if got := toasts(h.tg.take()); got[len(got)-1] != i18n.RU.AccessNoAdmins {
		t.Errorf("toasts = %q", got)
	}
	if a, err := h.store.Access(h.ctx, anya.ID); err != nil || a.Status != store.AccessGranted {
		t.Fatalf("anya = %+v, %v", a, err)
	}
}

func (b *Bot) mustDialogOf(t *testing.T, userID int64) *dialog {
	t.Helper()
	d, _ := b.loadDialog(t.Context(), userID, userID)
	if d == nil {
		t.Fatal("no dialog")
	}
	return d
}

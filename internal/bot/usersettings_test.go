package bot

import (
	"fmt"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/i18n"
	"github.com/Mimic890/terakoya/internal/nick"
)

// openSettings taps «Настройки» and returns the menu's message ID.
func (h *harness) openSettings() (int, call) {
	h.t.Helper()
	h.say(i18n.RU.KeySettings)
	c := last(h.t, h.tg.take())
	return h.tg.lastID("sendMessage"), c
}

func TestSettingsKeyboard(t *testing.T) {
	h := newHarness(t, nil)
	h.say("/start")
	kb := firstCall(t, h.tg.take(), "sendMessage").params["reply_markup"]
	if !strings.Contains(kb, `[[{"text":"Предметы"},{"text":"Закреплённые"}],[{"text":"Аккаунт"},{"text":"Настройки"}]]`) {
		t.Errorf("keyboard = %s", kb)
	}
}

func TestSettingsMenuAndTopToggle(t *testing.T) {
	h := newHarness(t, nil)
	h.accept(anya, 1)
	menu, c := h.openSettings()
	if c.params["text"] != "Настройки\n\nНик: Аня\nПоказывать меня в топе: да" {
		t.Errorf("menu = %q", c.params["text"])
	}
	for _, b := range []string{`"st:t"`, `"st:n"`, `"Скрыть меня из топа","callback_data":"st:h"`, `"Закрыть","callback_data":"c"`} {
		if !strings.Contains(c.params["reply_markup"], b) {
			t.Errorf("menu has no %s: %s", b, c.params["reply_markup"])
		}
	}
	// A second tap on the key only goes itself.
	h.say(i18n.RU.KeySettings)
	if calls := h.tg.take(); len(calls) != 1 || calls[0].method != "deleteMessage" {
		t.Errorf("repeated tap = %+v", calls)
	}

	h.tapOn(menu, cbToggleTop)
	e := to(h.tg.take(), "editMessageText", anya.ID)
	if len(e) != 1 || e[0].params["text"] != "Настройки\n\nНик: Аня\nПоказывать меня в топе: нет" ||
		!strings.Contains(e[0].params["reply_markup"], `"Показывать меня в топе","callback_data":"st:h"`) {
		t.Fatalf("hidden = %+v", e)
	}
	if got := h.bot.topMarkdown(h.ctx, &boris); strings.Contains(got, "@anya") {
		t.Errorf("others still see her: %q", got)
	}
	if got := h.bot.topMarkdown(h.ctx, &anya); !strings.Contains(got, "Ваше место: 1 — 1 принятое сообщение") {
		t.Errorf("she does not see her place: %q", got)
	}
	h.tapOn(menu, cbToggleTop)
	if e := to(h.tg.take(), "editMessageText", anya.ID); len(e) != 1 || !strings.HasSuffix(e[0].params["text"], ": да") {
		t.Errorf("shown again = %+v", e)
	}
	if got := h.bot.topMarkdown(h.ctx, &boris); !strings.Contains(got, "@anya") {
		t.Errorf("back in the top: %q", got)
	}
}

func TestNickname(t *testing.T) {
	h := newHarness(t, nil)
	h.store.TouchUser(h.ctx, ivan.ID, ivan.Username, ivan.FirstName)
	h.store.TouchUser(h.ctx, boris.ID, boris.Username, boris.FirstName)
	h.store.SetNickname(h.ctx, boris.ID, "Звезда", nick.Key("Звезда"))
	menu, _ := h.openSettings()

	h.tapOn(menu, cbNickOwn)
	wantPrompt(t, h.tg.take(), i18n.RU.NickAsk, "Отмена")
	for _, c := range []struct{ name, reason string }{
		{"ab", "Ник должен быть от 3 до 20 символов."},
		{"Аня!", i18n.RU.NickChars},
		{"Ан  я", i18n.RU.NickSpaces},
		{"Главный Админ", i18n.RU.NickReserved},
		{"terakoya_bot2", i18n.RU.NickReserved},
		{"Иван-2", i18n.RU.NickReserved}, // the admin's name
		{"ivan_fan", i18n.RU.NickReserved},
		{"fuckboy", i18n.RU.NickBanned},
		{"ЗВЕЗДА", i18n.RU.NickTaken},
		{"3вeздa", i18n.RU.NickTaken}, // digit 3, Latin e and a
		{"Зве_зда", i18n.RU.NickTaken},
	} {
		h.say(c.name)
		wantPrompt(t, h.tg.take(), c.reason+" "+i18n.RU.NickAgain, "Отмена")
		if d := h.bot.mustDialog(t); d.ses.State != stateNick {
			t.Fatalf("%s ended the dialog", c.name)
		}
	}
	h.send(&models.Message{Sticker: &models.Sticker{FileID: "s"}})
	h.tg.take()

	msgs := h.dialogMsgs()
	h.say("Ёжик")
	calls := h.tg.take()
	// The dialog goes, and the menu shows the new name.
	if got := ids(calls, "deleteMessage"); !sameIDs(got, append(msgs, h.msgID)) {
		t.Errorf("deleted %v, want %v and the name", got, msgs)
	}
	e := to(calls, "editMessageText", anya.ID)
	if len(e) != 1 || e[0].params["message_id"] != fmt.Sprint(menu) || !strings.HasPrefix(e[0].params["text"], "Настройки\n\nНик: Ёжик\n") {
		t.Errorf("menu = %+v", e)
	}
	if d, _ := h.bot.loadDialog(h.ctx, anya.ID, anya.ID); d != nil {
		t.Error("the dialog survived")
	}
	// The name shows everywhere.
	if got := h.bot.accountMarkdown(h.ctx, &anya); !strings.HasPrefix(got, "# Ёжик\n") {
		t.Errorf("account = %q", got)
	}
	h.accept(anya, 1)
	if got := h.bot.topMarkdown(h.ctx, &boris); !strings.Contains(got, "1\\. Ёжик") {
		t.Errorf("top = %q", got)
	}
	h.bot.cfg.SuggestCooldown = 0
	h.tap(cbReport + h.id(triangles))
	h.say("опечатка")
	h.tap(cbSend)
	h.tg.take()
	if body := h.host.bodies[7]; !strings.Contains(body, "Error reported in Telegram by Ёжик.") || !strings.Contains(body, "- Nickname: Ёжик") ||
		!strings.Contains(body, "- Username: @anya") {
		t.Errorf("issue body:\n%s", body)
	}

	// The admin may look like themselves.
	if got := h.bot.nickProblem(h.ctx, ivan.ID, "Иван-2"); got != "" {
		t.Errorf("admin's own name: %q", got)
	}

	// «Имя из Telegram» goes back to it.
	h.tapOn(menu, cbNickTG)
	if e := to(h.tg.take(), "editMessageText", anya.ID); len(e) != 1 || !strings.Contains(e[0].params["text"], "Ник: Аня\n") {
		t.Errorf("telegram name = %+v", e)
	}

	// «Отмена» cleans up and leaves the menu as it is.
	h.tapOn(menu, cbNickOwn)
	h.say("x")
	h.tg.take()
	msgs = h.dialogMsgs()
	h.tap(cbCancel)
	calls = h.tg.take()
	if got := ids(calls, "deleteMessage"); !sameIDs(got, msgs) || len(to(calls, "editMessageText", anya.ID)) != 0 ||
		len(texts(calls, "sendMessage")) != 0 {
		t.Errorf("cancel = %+v", calls)
	}
}

func TestNickReset(t *testing.T) {
	h := newHarness(t, nil)
	h.store.SetNickname(h.ctx, anya.ID, "Ёжик", nick.Key("Ёжик"))
	h.as(ivan)

	h.say("/nick_reset @Anya")
	calls := h.tg.take()
	if s := texts(to(calls, "sendMessage", ivan.ID), "sendMessage"); len(s) != 1 || s[0] != "Ник «Ёжик» сброшен, теперь это @anya." {
		t.Errorf("admin = %q", s)
	}
	if s := texts(to(calls, "sendMessage", anya.ID), "sendMessage"); len(s) != 1 || s[0] != "Ваш ник сброшен администратором." {
		t.Errorf("user = %q", s)
	}
	if u, _ := h.store.User(h.ctx, anya.ID); u.Nickname != "" {
		t.Errorf("nickname = %q", u.Nickname)
	}
	for arg, want := range map[string]string{
		"":        i18n.RU.NickResetUsage,
		"42":      "У @anya нет своего ника.",
		"@nobody": i18n.RU.NickResetUnknown,
	} {
		h.say(strings.TrimSpace("/nick_reset " + arg))
		if s := texts(h.tg.take(), "sendMessage"); len(s) != 1 || s[0] != want {
			t.Errorf("/nick_reset %s = %q", arg, s)
		}
	}

	// Others get nothing.
	h.store.TouchUser(h.ctx, ivan.ID, ivan.Username, ivan.FirstName)
	h.store.SetNickname(h.ctx, ivan.ID, "Учитель", nick.Key("Учитель"))
	h.as(anya)
	h.say("/nick_reset " + fmt.Sprint(ivan.ID))
	if c := h.tg.take(); len(c) != 1 || c[0].method != "deleteMessage" {
		t.Errorf("non-admin = %+v", c)
	}
	if u, _ := h.store.User(h.ctx, ivan.ID); u.Nickname != "Учитель" {
		t.Error("a non-admin reset a nickname")
	}
}

func TestNamesAddressAndList(t *testing.T) {
	h := newHarness(t, nil)
	h.accept(anya, 1)

	// Addressed by the first name, never the @username…
	h.say("/start")
	if c := firstCall(t, h.tg.take(), "sendMessage"); !strings.HasPrefix(c.params["text"], "Привет, Аня!\n") {
		t.Errorf("greeting = %q", c.params["text"])
	}
	if got := h.bot.accountMarkdown(h.ctx, &anya); !strings.HasPrefix(got, "# Аня\n") {
		t.Errorf("account = %q", got)
	}
	// …but told apart by it in listings.
	if got := h.bot.topMarkdown(h.ctx, &boris); !strings.Contains(got, "1\\. @anya") {
		t.Errorf("top = %q", got)
	}

	// With a nickname, the nickname everywhere.
	h.store.SetNickname(h.ctx, anya.ID, "Ёжик", nick.Key("Ёжик"))
	h.say("/start")
	if c := firstCall(t, h.tg.take(), "sendMessage"); !strings.HasPrefix(c.params["text"], "Привет, Ёжик!\n") {
		t.Errorf("greeting = %q", c.params["text"])
	}
	if got := h.bot.addressName(h.ctx, &anya); got != "Ёжик" {
		t.Errorf("address name = %q", got)
	}
	if got := h.bot.topMarkdown(h.ctx, &boris); !strings.Contains(got, "1\\. Ёжик") {
		t.Errorf("top = %q", got)
	}
}

func TestNicknameLookAlikes(t *testing.T) {
	h := newHarness(t, nil)
	h.store.TouchUser(h.ctx, boris.ID, boris.Username, boris.FirstName)
	h.store.SetNickname(h.ctx, boris.ID, "Маша", nick.Key("Маша"))
	for name, taken := range map[string]bool{
		"Mаша":  true, // Latin M
		"MАША":  true, // Latin M, Cyrillic rest
		"мaшa":  true, // Latin a
		"Ма_ша": true,
		"Ма-ша": true,
		"Ма ша": true,
		"Masha": false, // other letters
		"Маша2": false,
	} {
		got := h.bot.nickProblem(h.ctx, anya.ID, name) == i18n.RU.NickTaken
		if got != taken {
			t.Errorf("%q taken = %v, want %v", name, got, taken)
		}
	}
	// The store refuses it too, whatever slipped past the check.
	if err := h.store.SetNickname(h.ctx, anya.ID, "Mаша", nick.Key("Mаша")); err == nil {
		t.Error("store took a look-alike")
	}
}

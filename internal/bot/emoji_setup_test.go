package bot

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/i18n"
	"github.com/Mimic890/terakoya/internal/settings"
)

// custom sends a custom emoji, as Telegram delivers it.
func (h *harness) custom(id, alt string) {
	h.send(&models.Message{Text: alt, Entities: []models.MessageEntity{
		{Type: models.MessageEntityTypeCustomEmoji, Offset: 0, Length: len([]rune(alt)) * 2, CustomEmojiID: id}}})
}

// wizardTap presses a button of the current /emoji_setup prompt.
func (h *harness) wizardTap(data string) {
	h.t.Helper()
	wz := h.bot.loadWizard(h.ctx, h.user.ID, h.user.ID)
	if wz == nil {
		h.t.Fatal("no wizard")
	}
	h.tapOn(wz.w.PromptID, data)
}

func TestEmojiSetupWizard(t *testing.T) {
	h := newHarness(t, nil)
	h.tg.emoji = []string{"5001", "5002", "5003"}
	h.as(ivan)
	// Two medals: the wizard asks for two places of the top, not five.
	h.bot.settings.Update(func(s *settings.Settings) { s.Top.Medals = 2 })

	h.say("/emoji_setup")
	c := last(t, h.tg.take())
	want := "Шаг 1 из 11. Кнопка «Предметы» в нижней клавиатуре.\n\nСейчас: нет\n\n" + i18n.RU.EmojiAsk
	if c.params["text"] != want {
		t.Errorf("step 1 = %q, want %q", c.params["text"], want)
	}
	for _, b := range []string{`"ew:s"`, `"ew:c"`, `"ew:x"`, `"Пропустить"`, `"Без эмодзи"`, `"Отмена"`} {
		if !strings.Contains(c.params["reply_markup"], b) {
			t.Errorf("step has no %s: %s", b, c.params["reply_markup"])
		}
	}

	// Text or a plain emoji is not an answer.
	h.say("😀")
	if got := texts(h.tg.take(), "sendMessage"); len(got) != 1 || got[0] != i18n.RU.EmojiNeedCustom {
		t.Errorf("plain emoji = %q", got)
	}

	// A custom one is previewed; «Другой» asks again.
	h.custom("5001", "😀")
	c = last(t, h.tg.take())
	if c.params["text"] != "😀 Сохранить?" || !strings.Contains(c.params["entities"], `"custom_emoji_id":"5001"`) ||
		!strings.Contains(c.params["reply_markup"], `"ew:y"`) || !strings.Contains(c.params["reply_markup"], `"ew:o"`) {
		t.Errorf("preview = %+v", c.params)
	}
	h.wizardTap(cbEmojiAnother)
	if c = last(t, h.tg.take()); !strings.HasPrefix(c.params["text"], "Шаг 1 из 11.") {
		t.Errorf("after «Другой» = %q", c.params["text"])
	}
	h.custom("5001", "😀")
	h.wizardTap(cbEmojiYes)
	if c = last(t, h.tg.take()); !strings.HasPrefix(c.params["text"], "Шаг 2 из 11. Кнопка «Закреплённые»") {
		t.Errorf("step 2 = %q", c.params["text"])
	}

	h.wizardTap(cbEmojiSkip)  // Закреплённые
	h.wizardTap(cbEmojiClear) // Аккаунт
	if c = last(t, h.tg.take()); !strings.HasPrefix(c.params["text"], "Шаг 4 из 11. Кнопка «Настройки» в нижней клавиатуре.") {
		t.Errorf("settings step = %q", c.params["text"])
	}
	h.wizardTap(cbEmojiSkip) // Настройки
	h.wizardTap(cbEmojiSkip) // Закрепить
	c = last(t, h.tg.take())
	if !strings.HasPrefix(c.params["text"], "Шаг 6 из 11. Приветствие после /start") ||
		!strings.Contains(c.params["text"], "\n"+i18n.RU.EmojiHintGreeting+"\n\nСейчас: нет") {
		t.Errorf("greeting step = %q", c.params["text"])
	}
	h.wizardTap(cbEmojiSkip) // Приветствие
	if c = last(t, h.tg.take()); !strings.HasPrefix(c.params["text"], "Шаг 7 из 11. Кнопка предмета «Геометрия» в списке предметов.") {
		t.Errorf("subject step = %q", c.params["text"])
	}
	h.wizardTap(cbEmojiSkip) // Геометрия
	if c = last(t, h.tg.take()); !strings.HasPrefix(c.params["text"], "Шаг 8 из 11. Значок ранга kōhai (от 0 принятых правок)") {
		t.Errorf("rank step = %q", c.params["text"])
	}
	h.wizardTap(cbEmojiSkip) // kōhai
	h.custom("5002", "🎓")
	h.wizardTap(cbEmojiYes) // senpai
	if c = last(t, h.tg.take()); c.params["text"] != "Шаг 10 из 11. Медаль 1 места в /top — вместо номера места.\n1 место\n\nСейчас: нет\n\n"+i18n.RU.EmojiAsk {
		t.Errorf("medal step = %q", c.params["text"])
	}
	h.custom("5003", "🥇")
	h.wizardTap(cbEmojiYes)  // top:1
	h.wizardTap(cbEmojiSkip) // top:2

	c = last(t, h.tg.take())
	wantSum := "Готово, сохранено:\nПредметы: ⭐\nЗакреплённые: —\nАккаунт: —\nНастройки: —\nЗакрепить: —\nПриветствие: —\nПредмет «Геометрия»: —\nРанг kōhai: —\nРанг senpai: ⭐\nТоп, 1 место: ⭐\nТоп, 2 место: —"
	if c.params["text"] != wantSum {
		t.Errorf("summary =\n%q\nwant\n%q", c.params["text"], wantSum)
	}
	s, err := settings.Load(h.bot.settings.Path())
	if err != nil {
		t.Fatal(err)
	}
	if s.Emoji.KeyboardSubjects != "5001" || s.Ranks[1].Emoji != "5002" || s.Emoji.KeyboardAccount != "" || s.Emoji.Medal(1) != "5003" {
		t.Errorf("saved = %+v", s)
	}
	if d, _ := h.bot.loadDialog(h.ctx, ivan.ID, ivan.ID); d != nil {
		t.Error("the wizard did not end")
	}
	// In use at once.
	if h.bot.emoji.Subjects() != "5001" || h.bot.emoji.Rank("senpai") != "5002" || h.bot.emoji.Medal(1) != "5003" {
		t.Errorf("in use: %q %q", h.bot.emoji.Subjects(), h.bot.emoji.Rank("senpai"))
	}

	// Without a wizard, custom emoji get their IDs back again.
	h.custom("5002", "🎓")
	if got := texts(h.tg.take(), "sendMessage"); len(got) != 1 || got[0] != "🎓 5002" {
		t.Errorf("ID helper = %q", got)
	}
}

func TestEmojiSetupOneSlotAndCancel(t *testing.T) {
	h := newHarness(t, nil)
	h.as(ivan)

	h.say("/emoji_setup rank:Senpai")
	if c := last(t, h.tg.take()); !strings.HasPrefix(c.params["text"], "Шаг 1 из 1. Значок ранга senpai (от 1 принятой правки)") {
		t.Errorf("one slot = %q", c.params["text"])
	}
	h.wizardTap(cbEmojiClear)
	if c := last(t, h.tg.take()); c.params["text"] != "Готово, сохранено:\nРанг senpai: —" {
		t.Errorf("summary = %q", c.params["text"])
	}

	h.say("/emoji_setup nowhere")
	got := texts(h.tg.take(), "sendMessage")
	if len(got) != 1 || !strings.HasPrefix(got[0], "Нет такого места.") || !strings.Contains(got[0], "\nsubject:Геометрия\n") ||
		!strings.Contains(got[0], "\nrank:senpai\n") || !strings.HasSuffix(got[0], "\ntop:5") {
		t.Errorf("unknown slot = %q", got)
	}

	// A medal beyond top.medals can still be set by its slot.
	h.say("/emoji_setup top:5")
	if c := last(t, h.tg.take()); !strings.HasPrefix(c.params["text"], "Шаг 1 из 1. Медаль 5 места в /top") {
		t.Errorf("top:5 = %q", c.params["text"])
	}
	h.wizardTap(cbEmojiCancel)
	h.tg.take()

	// «Отмена» saves nothing, even answers already given.
	before, _ := os.ReadFile(h.bot.settings.Path())
	h.tg.emoji = []string{"5001"}
	h.say("/emoji_setup")
	h.custom("5001", "😀")
	h.wizardTap(cbEmojiYes)
	h.tg.take()
	msgs := h.bot.loadWizard(h.ctx, ivan.ID, ivan.ID).w.Msgs
	h.wizardTap(cbEmojiCancel)
	calls := h.tg.take()
	// Every message of the wizard goes, the admin's emoji included, and
	// nothing is said.
	if got := ids(calls, "deleteMessage"); len(msgs) != 4 || !sameIDs(got, msgs) || len(texts(calls, "sendMessage")) != 0 {
		t.Errorf("cancel: deleted %v, want %v; calls %+v", got, msgs, calls)
	}
	if after, _ := os.ReadFile(h.bot.settings.Path()); string(after) != string(before) {
		t.Error("cancel changed the settings")
	}
	if h.bot.loadWizard(h.ctx, ivan.ID, ivan.ID) != nil {
		t.Error("the wizard survived «Отмена»")
	}

	// A stale button does nothing.
	h.tapOn(12345, cbEmojiYes)
	if got := toasts(h.tg.take()); len(got) != 1 || got[0] != i18n.RU.Expired {
		t.Errorf("stale = %q", got)
	}
}

func TestEmojiSetupAdminsOnly(t *testing.T) {
	h := newHarness(t, nil)
	h.say("/emoji_setup")
	for _, c := range h.tg.take() {
		if c.method != "deleteMessage" {
			t.Errorf("non-admin got %s %v", c.method, c.params)
		}
	}
	if h.bot.loadWizard(h.ctx, anya.ID, anya.ID) != nil {
		t.Error("a wizard for a non-admin")
	}
	// Nor can they press its buttons.
	h.tapOn(1, cbEmojiYes)
	if got := toasts(h.tg.take()); len(got) != 1 || got[0] != "" {
		t.Errorf("non-admin tap = %q", got)
	}
}

func TestSettingsHandEdit(t *testing.T) {
	h := newHarness(t, nil)
	path := h.bot.settings.Path()
	os.WriteFile(path, []byte("ranks:\n  - {title: Новичок, from: 0}\n  - {title: Знаток, from: 1}\n"), 0o644)
	future := time.Now().Add(time.Minute)
	os.Chtimes(path, future, future)
	h.bot.settings.Check()
	if md := h.bot.accountMarkdown(h.ctx, &anya); !strings.Contains(md, "Ранг: **Новичок**") {
		t.Errorf("account after the edit = %s", md)
	}
	// A broken edit keeps the previous ladder.
	os.WriteFile(path, []byte("ranks:\n  - {title: x, from: 3}\n"), 0o644)
	future = future.Add(time.Minute)
	os.Chtimes(path, future, future)
	h.bot.settings.Check()
	if l := h.bot.ladder(); l[0].Title != "Новичок" {
		t.Errorf("ladder = %+v", l)
	}
}

func TestCommandMenus(t *testing.T) {
	h := newHarness(t, nil)
	h.tg.take()
	h.bot.setCommands(h.ctx)
	calls := h.tg.take()
	if len(calls) != 2 || calls[0].method != "setMyCommands" || calls[1].method != "setMyCommands" {
		t.Fatalf("calls = %+v", calls)
	}
	if all := calls[0].params["commands"]; calls[0].params["scope"] != "" || strings.Contains(all, "emoji_setup") || strings.Contains(all, "broadcast") || strings.Contains(all, "nick_reset") ||
		!strings.Contains(all, `"start"`) || !strings.Contains(all, `"top"`) || !strings.Contains(all, `"ranks"`) {
		t.Errorf("everyone's menu = %v", calls[0].params)
	}
	if admin := calls[1].params; !strings.Contains(admin["scope"], `"chat_id":7`) ||
		!strings.Contains(admin["commands"], `"emoji_setup"`) || !strings.Contains(admin["commands"], `"ranks"`) || !strings.Contains(admin["commands"], `"broadcast"`) || !strings.Contains(admin["commands"], `"nick_reset"`) ||
		strings.Contains(admin["commands"], `"revoke"`) || strings.Contains(admin["commands"], `"access"`) {
		t.Errorf("admin menu = %v", admin)
	}
}

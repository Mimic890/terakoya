package bot

import (
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/Mimic890/terakoya/internal/i18n"
)

// onlyTapDeleted checks that the last tap was deleted and nothing else done.
func onlyTapDeleted(t *testing.T, h *harness, what string) {
	t.Helper()
	calls := h.tg.take()
	if len(calls) != 1 || calls[0].method != "deleteMessage" || calls[0].params["message_id"] != strconv.Itoa(h.msgID) {
		t.Errorf("%s: calls = %+v", what, calls)
	}
}

func TestRepeatedKeyTap(t *testing.T) {
	h := newHarness(t, nil)
	h.say("/start")
	h.tg.take()

	// The first tap shows the list; the second, with it still last, only
	// deletes the tap.
	h.say(i18n.RU.KeySubjects)
	if len(to(h.tg.take(), "editMessageText", 42)) != 1 {
		t.Fatal("subjects not shown")
	}
	h.say(i18n.RU.KeySubjects)
	onlyTapDeleted(t, h, "second «Предметы»")

	// The same for the account, a rich message.
	h.say(i18n.RU.KeyAccount)
	if len(texts(h.tg.take(), "sendRichMessage")) != 1 {
		t.Fatal("account not shown")
	}
	h.say(i18n.RU.KeyAccount)
	onlyTapDeleted(t, h, "second «Аккаунт»")
	// Old keyboards send the old text; it is the same button.
	h.say("👤 Аккаунт")
	onlyTapDeleted(t, h, "old «👤 Аккаунт»")

	// Once something else is below it, the menu is shown again.
	h.bot.sendArticle(h.ctx, 42, h.bot.catalog().NoteByPath(triangles), false)
	h.tg.take()
	h.say(i18n.RU.KeyAccount)
	if len(texts(h.tg.take(), "sendRichMessage")) != 1 {
		t.Error("account not shown again below a note")
	}

	// Navigating inside the subject list changes it: «Предметы» shows the
	// list again.
	h.say(i18n.RU.KeySubjects)
	h.tg.take()
	menu := h.bot.msgs.menu(h.ctx, 42)
	h.tapOn(menu, cbSubject+h.bot.catalog().Subjects[0].ID)
	h.tg.take()
	h.say(i18n.RU.KeySubjects)
	if e := to(h.tg.take(), "editMessageText", 42); len(e) != 1 || e[0].params["text"] != i18n.RU.ChooseSubject {
		t.Errorf("after navigating: %+v", e)
	}
}

// emojiRune matches pictographs and symbols drawn as emoji.
var emojiRune = regexp.MustCompile(`[\x{1F000}-\x{1FAFF}\x{2600}-\x{27BF}\x{2B00}-\x{2BFF}\x{FE0F}]`)

func TestNoEmojiByDefault(t *testing.T) {
	// No UI text has an emoji of its own.
	v := reflect.ValueOf(i18n.RU)
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		name := v.Type().Field(i).Name
		if name == "KeyOld" { // what old keyboards send, only matched
			continue
		}
		var all []string
		switch f.Kind() {
		case reflect.String:
			all = []string{f.String()}
		case reflect.Array:
			for j := 0; j < f.Len(); j++ {
				all = append(all, f.Index(j).String())
			}
		}
		for _, s := range all {
			if emojiRune.MatchString(s) {
				t.Errorf("%s has an emoji: %q", name, s)
			}
		}
	}

	h := newHarness(t, nil)
	h.say("/start")
	calls := h.tg.take()
	greet := firstCall(t, calls, "sendMessage")
	if greet.params["text"] != "Привет, Аня!\nЭто terakoya — школьный справочник: теоремы, аксиомы, задачи и доказательства по классам.\nНашли ошибку или чего-то не хватает? Сообщите — за принятые правки растёт ранг." {
		t.Errorf("greeting = %q", greet.params["text"])
	}
	h.tapOn(500, "a:"+h.id(triangles))
	calls = append(calls, h.tg.take()...)
	h.say(i18n.RU.KeyPinned)
	calls = append(calls, h.tg.take()...)
	h.say(i18n.RU.KeyAccount)
	calls = append(calls, h.tg.take()...)

	// No emoji anywhere, and no label with stray spaces where an emoji
	// would have gone.
	label := regexp.MustCompile(`"text":"([^"]*)"`)
	for _, c := range calls {
		for _, field := range []string{"text", "reply_markup", "rich_message"} {
			s := c.params[field]
			if emojiRune.MatchString(s) {
				t.Errorf("%s %s has an emoji: %s", c.method, field, s)
			}
		}
		for _, m := range label.FindAllStringSubmatch(c.params["reply_markup"], -1) {
			l := m[1]
			if l != strings.TrimFunc(l, unicode.IsSpace) || strings.Contains(l, "  ") {
				t.Errorf("button %q has stray spaces", l)
			}
		}
	}
	for _, want := range []string{`{"text":"Предметы"}`, `{"text":"Закреплённые"}`, `{"text":"Аккаунт"}`} {
		if !strings.Contains(greet.params["reply_markup"], want) {
			t.Errorf("keyboard has no %s: %s", want, greet.params["reply_markup"])
		}
	}
	pin := false
	for _, c := range calls {
		pin = pin || strings.Contains(c.params["reply_markup"], `{"text":"Закрепить","callback_data":"p:`)
	}
	if !pin {
		t.Error("no «Закрепить» button under the note")
	}
	if text, _ := h.bot.pinsView(h.ctx, 42); text != "Закреплённые\n\n"+i18n.RU.PinnedEmpty {
		t.Errorf("pins = %q", text)
	}
}

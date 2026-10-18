package bot

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/Mimic890/terakoya/internal/content"
	"github.com/Mimic890/terakoya/internal/i18n"
	"github.com/Mimic890/terakoya/internal/render"
	"github.com/Mimic890/terakoya/internal/settings"
)

// withEmoji sets custom emoji for every place in settings.yml, as
// Telegram knows them.
func withEmoji(h *harness) {
	h.t.Helper()
	h.tg.emoji = []string{"101", "102", "103", "104", "105", "201", "202", "301"}
	err := h.bot.settings.Update(func(s *settings.Settings) {
		s.Emoji = settings.Emoji{KeyboardSubjects: "101", KeyboardPinned: "102", KeyboardAccount: "103", Pin: "104",
			Greeting: "105", Subjects: map[string]string{"Геометрия": "301"}}
		s.Ranks[0].Emoji, s.Ranks[1].Emoji = "201", "202"
	})
	if err != nil {
		h.t.Fatal(err)
	}
	h.tg.take()
}

func entities(t *testing.T, c call) []models.MessageEntity {
	t.Helper()
	var out []models.MessageEntity
	if c.params["entities"] != "" {
		if err := json.Unmarshal([]byte(c.params["entities"]), &out); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func TestEmojiIDsForAdmins(t *testing.T) {
	h := newHarness(t, nil)
	h.as(ivan)
	// "Вот 😀 и 👍👍": offsets are in UTF-16 units, where 😀 takes two.
	h.send(&models.Message{Text: "Вот 😀 и 👍👍", Entities: []models.MessageEntity{
		{Type: models.MessageEntityTypeCustomEmoji, Offset: 4, Length: 2, CustomEmojiID: "5001"},
		{Type: models.MessageEntityTypeCustomEmoji, Offset: 9, Length: 2, CustomEmojiID: "5002"},
		{Type: models.MessageEntityTypeCustomEmoji, Offset: 11, Length: 2, CustomEmojiID: "5002"},
	}})
	c := last(t, h.tg.take())
	if c.params["text"] != "😀 5001\n👍 5002" {
		t.Errorf("reply = %q", c.params["text"])
	}
	want := []models.MessageEntity{
		{Type: models.MessageEntityTypeCustomEmoji, Offset: 0, Length: 2, CustomEmojiID: "5001"},
		{Type: models.MessageEntityTypeCustomEmoji, Offset: 8, Length: 2, CustomEmojiID: "5002"},
		{Type: models.MessageEntityTypeCode, Offset: 3, Length: 4},
		{Type: models.MessageEntityTypeCode, Offset: 11, Length: 4},
	}
	got := entities(t, c)
	if len(got) != len(want) {
		t.Fatalf("entities = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entity %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	// In a caption too; and for others it is just a message.
	h.send(&models.Message{Caption: "👍", CaptionEntities: []models.MessageEntity{
		{Type: models.MessageEntityTypeCustomEmoji, Offset: 0, Length: 2, CustomEmojiID: "5003"}}})
	if got := texts(h.tg.take(), "sendMessage"); len(got) != 1 || got[0] != "👍 5003" {
		t.Errorf("caption = %q", got)
	}
	h.as(boris)
	h.send(&models.Message{Text: "👍", Entities: []models.MessageEntity{
		{Type: models.MessageEntityTypeCustomEmoji, Offset: 0, Length: 2, CustomEmojiID: "5003"}}})
	if got := texts(h.tg.take(), "sendMessage"); len(got) != 1 || got[0] != i18n.RU.UnknownCommand {
		t.Errorf("non-admin = %q", got)
	}
}

func TestEmojiInUse(t *testing.T) {
	h := newHarness(t, nil)
	withEmoji(h)
	h.say("/start")
	calls := h.tg.take()
	greet := firstCall(t, calls, "sendMessage")
	if greet.params["text"] != "Привет, Аня! 👋\nЭто terakoya — школьный справочник, который пишут сами пользователи." {
		t.Errorf("greeting = %q", greet.params["text"])
	}
	if e := entities(t, greet); len(e) != 1 || e[0].Offset != 13 || e[0].Length != 2 || e[0].CustomEmojiID != "105" {
		t.Errorf("greeting entities = %+v", e)
	}
	kb := greet.params["reply_markup"]
	for _, want := range []string{`{"text":"Предметы","icon_custom_emoji_id":"101"}`, `{"text":"Закреплённые","icon_custom_emoji_id":"102"}`,
		`{"text":"Аккаунт","icon_custom_emoji_id":"103"}`} {
		if !strings.Contains(kb, want) {
			t.Errorf("keyboard has no %s: %s", want, kb)
		}
	}
	if menu := last(t, calls); !strings.Contains(menu.params["reply_markup"], `{"text":"Геометрия","icon_custom_emoji_id":"301","callback_data":"s:`) {
		t.Errorf("subjects = %s", menu.params["reply_markup"])
	}

	// Taps of the new and of old keyboards both work.
	for _, text := range []string{"Закреплённые", "📌 Закреплённые"} {
		if got := h.bot.keyButton(text); got != i18n.RU.KeyPinned {
			t.Errorf("keyButton(%q) = %q", text, got)
		}
	}

	h.tapOn(500, "a:"+h.id(triangles))
	if kb := lastRich(t, h.tg.take()).params["reply_markup"]; !strings.Contains(kb, `{"text":"Закрепить","icon_custom_emoji_id":"104","callback_data":"p:`+h.id(triangles)+`"}`) {
		t.Errorf("note keyboard = %s", kb)
	}

	md := h.bot.accountMarkdown(h.ctx, &anya)
	if !strings.Contains(md, "Ранг: ![🏅](tg://emoji?id=201) **kōhai**") {
		t.Errorf("account = %s", md)
	}
	// The renderer keeps custom emoji, in notes too.
	n := &content.Note{Title: "x", Body: md}
	if out := render.Convert(h.bot.catalog(), n, render.Options{}).Parts[0].Markdown; !strings.Contains(out, "![🏅](tg://emoji?id=201) **kōhai**") {
		t.Errorf("rendered = %s", out)
	}
}

func TestEmojiRejected(t *testing.T) {
	h := newHarness(t, nil)
	withEmoji(h)
	h.tg.reject = func(method string, p map[string]string) bool {
		return strings.Contains(p["reply_markup"], "icon_custom_emoji_id") || strings.Contains(p["entities"], "custom_emoji") ||
			strings.Contains(p["rich_message"], "tg://emoji")
	}
	h.say("/start")
	calls := h.tg.take()
	if calls[0].method != "sendMessage (rejected)" {
		t.Fatalf("first call = %s", calls[0].method)
	}
	greet := calls[1]
	if greet.method != "sendMessage" || greet.params["text"] != "Привет, Аня!\nЭто terakoya — школьный справочник, который пишут сами пользователи." ||
		!strings.Contains(greet.params["reply_markup"], `{"text":"Предметы"}`) || greet.params["entities"] != "" {
		t.Errorf("plain greeting = %+v", greet)
	}
	// From then on no custom emoji are tried.
	for _, c := range calls[2:] {
		if strings.HasSuffix(c.method, "(rejected)") || strings.Contains(c.params["reply_markup"], "icon_custom_emoji_id") {
			t.Errorf("after the fallback: %s %v", c.method, c.params)
		}
	}
	h.tapOn(500, "a:"+h.id(triangles))
	if kb := lastRich(t, h.tg.take()).params["reply_markup"]; !strings.Contains(kb, `{"text":"Закрепить","callback_data":"p:`) {
		t.Errorf("note keyboard = %s", kb)
	}
	if md := h.bot.accountMarkdown(h.ctx, &anya); strings.Contains(md, "tg://emoji") {
		t.Errorf("account = %s", md)
	}
}

func TestEmojiRejectedPerMessage(t *testing.T) {
	// Each kind of send falls back on its own.
	for name, send := range map[string]func(h *harness){
		"note":    func(h *harness) { h.tapOn(500, "a:"+h.id(triangles)) },
		"menu":    func(h *harness) { h.say(i18n.RU.KeySubjects) },
		"account": func(h *harness) { h.say(i18n.RU.KeyAccount) },
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, nil)
			withEmoji(h)
			h.tg.reject = func(method string, p map[string]string) bool {
				return strings.Contains(p["reply_markup"], "icon_custom_emoji_id") || strings.Contains(p["rich_message"], "tg://emoji")
			}
			send(h)
			calls := h.tg.take()
			rejected, sent := 0, 0
			for _, c := range calls {
				switch {
				case strings.HasSuffix(c.method, "(rejected)"):
					rejected++
				case c.method == "sendMessage" || c.method == "sendRichMessage":
					sent++
				}
			}
			if rejected != 1 || sent != 1 {
				t.Errorf("calls = %+v", calls)
			}
			if h.bot.emoji.Pin() != "" {
				t.Error("custom emoji still on")
			}
		})
	}
}

func TestEmojiCheck(t *testing.T) {
	h := newHarness(t, nil)
	h.tg.emoji = []string{"101", "201", "301"}
	e := newEmojiSet(h.bot.log)
	e.set(settings.Settings{
		Ranks: []settings.Rank{{Title: "a", Emoji: "201"}, {Title: "b", From: 1, Emoji: "998"}},
		Emoji: settings.Emoji{KeyboardSubjects: "101", Pin: "999", Subjects: map[string]string{"Геометрия": "301", "Алгебра": "997"}},
	})
	e.check(h.ctx, h.bot.api)
	if e.Subjects() != "101" || e.Pin() != "" || e.Rank("a") != "201" || e.Rank("b") != "" ||
		e.Subject("Геометрия") != "301" || e.Subject("Алгебра") != "" {
		t.Errorf("after check: %+v", e)
	}
}

// TestNoStraySpaces builds every text with an emoji slot without the
// emoji: unset, or dropped after Telegram refused it.
func TestNoStraySpaces(t *testing.T) {
	h := newHarness(t, nil)
	b := h.bot
	texts := map[string]func(id string) emojiText{
		"greeting": func(id string) emojiText {
			return emojiText{fmt.Sprintf(b.t.Greeting, "Аня"), emojiAt{id: id, alt: "👋", before: " "}, "\n" + b.t.GreetingText}
		},
		"rank-up": func(id string) emojiText {
			return emojiText{"Принято.\n\n" + b.t.RankUpPrefix, emojiAt{id: id, alt: "🏅", after: " "}, fmt.Sprintf(b.t.RankUp, "Pupil")}
		},
		"preview": func(id string) emojiText {
			return emojiText{emojiAt{id: id, alt: "😀", after: " "}, b.t.EmojiSaveQuestion}
		},
	}
	want := map[string]string{
		"greeting": "Привет, Аня!\nЭто terakoya — школьный справочник, который пишут сами пользователи.",
		"rank-up":  "Принято.\n\nТеперь вы Pupil!",
		"preview":  "Сохранить?",
	}
	for name, text := range texts {
		for _, c := range []struct {
			id    string
			plain bool
		}{{"", false}, {"77", true}} {
			got, ents := text(c.id).build(c.plain)
			if got != want[name] || len(ents) != 0 {
				t.Errorf("%s (id %q, plain %v) = %q, %v", name, c.id, c.plain, got, ents)
			}
		}
		// With the emoji, the spacing is there.
		if got, _ := text("77").build(false); strings.Contains(got, "  ") || strings.Contains(got, " \n") {
			t.Errorf("%s with emoji = %q", name, got)
		}
	}
	// Rich messages: the account page without a rank emoji.
	if md := b.accountMarkdown(h.ctx, &anya); !strings.Contains(md, "Ранг: **kōhai**  \n") {
		t.Errorf("account = %q", md)
	}
	if md, _ := plainMarkdown("Ранг: " + richEmojiRef("77", "🏅") + "**kōhai**"); md != "Ранг: **kōhai**" {
		t.Errorf("plain account = %q", md)
	}
}

func TestAccountNextRankEmoji(t *testing.T) {
	h := newHarness(t, nil)
	withEmoji(h)
	// kōhai has 201, senpai 202.
	if got := h.bot.accountMarkdown(h.ctx, &anya); !strings.Contains(got,
		"Ранг: ![🏅](tg://emoji?id=201) **kōhai**  \nДо следующего ранга: ещё 1 принятое сообщение  \n"+
			"Следующий ранг: ![🏅](tg://emoji?id=202) **senpai**\n") {
		t.Errorf("account = %s", got)
	}
}

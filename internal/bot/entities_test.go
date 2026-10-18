package bot

import (
	"testing"
	"unicode/utf16"

	"github.com/go-telegram/bot/models"
)

// ent marks the first occurrence of part in text.
func ent(text, part string, typ models.MessageEntityType) models.MessageEntity {
	u := utf16.Encode([]rune(text))
	p := utf16.Encode([]rune(part))
	for i := 0; i+len(p) <= len(u); i++ {
		if string(utf16.Decode(u[i:i+len(p)])) == part {
			return models.MessageEntity{Type: typ, Offset: i, Length: len(p)}
		}
	}
	panic("no " + part)
}

func TestEntitiesMarkdown(t *testing.T) {
	const (
		bold   = models.MessageEntityTypeBold
		italic = models.MessageEntityTypeItalic
	)
	link := func(text, part, url string) models.MessageEntity {
		e := ent(text, part, models.MessageEntityTypeTextLink)
		e.URL = url
		return e
	}
	cases := []struct {
		name, text string
		ents       func(string) []models.MessageEntity
		want       string
	}{
		{"plain and literal", "Цена: 5*3 = 15 [скидка] #важно", nil, `Цена: 5\*3 = 15 \[скидка\] \#важно`},
		{"line breaks", "Первая\nвторая\n\nновый абзац", nil, "Первая  \nвторая\n\nновый абзац"},
		{"bold with edge spaces", "Это важно сегодня", func(s string) []models.MessageEntity {
			return []models.MessageEntity{ent(s, " важно ", bold)}
		}, "Это **важно** сегодня"},
		{"nested", "жирный и курсив", func(s string) []models.MessageEntity {
			return []models.MessageEntity{ent(s, "жирный и курсив", bold), ent(s, "курсив", italic)}
		}, "**жирный и *курсив***"},
		{"crossing", "раз два три", func(s string) []models.MessageEntity {
			return []models.MessageEntity{ent(s, "раз два", bold), ent(s, "два три", italic)}
		}, "**раз *два*** *три*"},
		{"across a line break", "раз\nдва", func(s string) []models.MessageEntity {
			return []models.MessageEntity{ent(s, "раз\nдва", bold)}
		}, "**раз**  \n**два**"},
		{"others", "под зачёрк скрыто", func(s string) []models.MessageEntity {
			return []models.MessageEntity{ent(s, "под", models.MessageEntityTypeUnderline),
				ent(s, "зачёрк", models.MessageEntityTypeStrikethrough), ent(s, "скрыто", models.MessageEntityTypeSpoiler)}
		}, "<u>под</u> ~~зачёрк~~ <tg-spoiler>скрыто</tg-spoiler>"},
		{"code is raw", "Нажмите `a*b` тут", func(s string) []models.MessageEntity {
			return []models.MessageEntity{ent(s, "`a*b`", models.MessageEntityTypeCode)}
		}, "Нажмите `` `a*b` `` тут"},
		{"code", "Команда /top x_y", func(s string) []models.MessageEntity {
			return []models.MessageEntity{ent(s, "/top x_y", models.MessageEntityTypeCode)}
		}, "Команда `/top x_y`"},
		{"link", "Сайт школы (новый)", func(s string) []models.MessageEntity {
			return []models.MessageEntity{link(s, "Сайт школы", "https://x.ru/a (b)")}
		}, "[Сайт школы](https://x.ru/a%20%28b%29) (новый)"},
		{"bold link", "Сайт", func(s string) []models.MessageEntity {
			return []models.MessageEntity{ent(s, "Сайт", bold), link(s, "Сайт", "https://x.ru")}
		}, "[**Сайт**](https://x.ru)"},
		{"custom emoji", "Привет 🎉 всем", func(s string) []models.MessageEntity {
			e := ent(s, "🎉", models.MessageEntityTypeCustomEmoji)
			e.CustomEmojiID = "555"
			return []models.MessageEntity{e, ent(s, "Привет 🎉 всем", bold)}
		}, "**Привет ![🎉](tg://emoji?id=555) всем**"},
		{"lists and headings stay text", "- раз\n1. два\n===", nil, "\\- раз  \n1\\. два  \n\\==="},
		{"code block", "Пример:\nx := 1\ny\nконец", func(s string) []models.MessageEntity {
			e := ent(s, "x := 1\ny\n", models.MessageEntityTypePre)
			e.Language = "go"
			return []models.MessageEntity{e}
		}, "Пример:\n\n```go\nx := 1\ny\n```\n\nконец"},
		{"quote", "Цитата:\nбыть или\nне быть\nвсё", func(s string) []models.MessageEntity {
			return []models.MessageEntity{ent(s, "быть или\nне быть", models.MessageEntityTypeBlockquote), ent(s, "быть", bold)}
		}, "Цитата:\n\n> **быть** или  \n> не быть\n\nвсё"},
	}
	for _, c := range cases {
		var ents []models.MessageEntity
		if c.ents != nil {
			ents = c.ents(c.text)
		}
		if got := entitiesMarkdown(c.text, ents); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

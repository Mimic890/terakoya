package bot

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/go-telegram/bot/models"
)

// entitiesMarkdown turns a message's text and formatting, as Telegram
// delivers them, into rich Markdown that shows the same: bold, italic,
// underline, strikethrough, spoilers, code, links, custom emoji, code
// blocks and quotes. Line breaks stay line breaks; everything else is
// literal text.
func entitiesMarkdown(text string, ents []models.MessageEntity) string {
	u := utf16.Encode([]rune(text))
	var blocks, inline []models.MessageEntity
	for _, e := range ents {
		if e.Offset < 0 || e.Length <= 0 || e.Offset+e.Length > len(u) {
			continue
		}
		switch e.Type {
		case models.MessageEntityTypePre, models.MessageEntityTypeBlockquote, models.MessageEntityTypeExpandableBlockquote:
			blocks = append(blocks, e)
		default:
			inline = append(inline, e)
		}
	}
	sort.SliceStable(blocks, func(i, j int) bool { return blocks[i].Offset < blocks[j].Offset })

	var parts []string
	add := func(s string) {
		if s = strings.Trim(s, "\n"); s != "" {
			parts = append(parts, s)
		}
	}
	at := 0
	for _, bl := range blocks {
		if bl.Offset < at {
			continue // overlaps the previous block
		}
		add(inlineMarkdown(u, at, bl.Offset, inline))
		end := bl.Offset + bl.Length
		if bl.Type == models.MessageEntityTypePre {
			add(codeBlock(string(utf16.Decode(u[bl.Offset:end])), bl.Language))
		} else {
			add(quoteBlock(inlineMarkdown(u, bl.Offset, end, inline)))
		}
		at = end
	}
	add(inlineMarkdown(u, at, len(u), inline))
	return strings.Join(parts, "\n\n")
}

func codeBlock(code, lang string) string {
	fence := "```"
	for strings.Contains(code, fence) {
		fence += "`"
	}
	return fence + lang + "\n" + strings.Trim(code, "\n") + "\n" + fence
}

func quoteBlock(md string) string {
	lines := strings.Split(strings.Trim(md, "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight("> "+l, " ")
		if l != "" && strings.HasSuffix(l, "  ") {
			lines[i] += "  "
		}
	}
	return strings.Join(lines, "\n")
}

// mark is an inline entity being written.
type mark struct {
	e          models.MessageEntity
	open, shut string
	raw        bool // code: its text is not escaped
}

func newMark(e models.MessageEntity, u []uint16) (*mark, bool) {
	switch e.Type {
	case models.MessageEntityTypeBold:
		return &mark{e: e, open: "**", shut: "**"}, true
	case models.MessageEntityTypeItalic:
		return &mark{e: e, open: "*", shut: "*"}, true
	case models.MessageEntityTypeUnderline:
		return &mark{e: e, open: "<u>", shut: "</u>"}, true
	case models.MessageEntityTypeStrikethrough:
		return &mark{e: e, open: "~~", shut: "~~"}, true
	case models.MessageEntityTypeSpoiler:
		return &mark{e: e, open: "<tg-spoiler>", shut: "</tg-spoiler>"}, true
	case models.MessageEntityTypeTextLink:
		url := strings.NewReplacer(" ", "%20", "(", "%28", ")", "%29", "<", "%3C", ">", "%3E").Replace(e.URL)
		return &mark{e: e, open: "[", shut: "](" + url + ")"}, true
	case models.MessageEntityTypeCode:
		code := string(utf16.Decode(u[e.Offset : e.Offset+e.Length]))
		fence := "`"
		for strings.Contains(code, fence) {
			fence += "`"
		}
		// Code that starts or ends with a backtick is padded, as Markdown
		// needs.
		open, shut := fence, fence
		if strings.HasPrefix(code, "`") || strings.HasSuffix(code, "`") {
			open, shut = fence+" ", " "+fence
		}
		return &mark{e: e, open: open, shut: shut, raw: true}, true
	}
	return nil, false
}

// order nests marks: links outside, code inside.
var order = map[models.MessageEntityType]int{
	models.MessageEntityTypeTextLink: 0, models.MessageEntityTypeBold: 1, models.MessageEntityTypeItalic: 2,
	models.MessageEntityTypeUnderline: 3, models.MessageEntityTypeStrikethrough: 4, models.MessageEntityTypeSpoiler: 5,
	models.MessageEntityTypeCode: 6,
}

// inlineMarkdown converts u[from:to]. Marks are closed before each line
// break and opened again after it, and whitespace at their edges moves
// outside them, as Markdown needs.
func inlineMarkdown(u []uint16, from, to int, ents []models.MessageEntity) string {
	var marks []*mark
	emoji := map[int]models.MessageEntity{}
	for _, e := range ents {
		if e.Offset >= to || e.Offset+e.Length <= from {
			continue
		}
		if e.Type == models.MessageEntityTypeCustomEmoji {
			if e.Offset >= from && e.Offset+e.Length <= to {
				emoji[e.Offset] = e
			}
			continue
		}
		if m, ok := newMark(e, u); ok {
			marks = append(marks, m)
		}
	}
	sort.SliceStable(marks, func(i, j int) bool {
		if order[marks[i].e.Type] != order[marks[j].e.Type] {
			return order[marks[i].e.Type] < order[marks[j].e.Type]
		}
		return marks[i].e.Offset < marks[j].e.Offset
	})

	var out strings.Builder
	var stack, pending []*mark
	held := "" // whitespace that goes after the marks closed next
	active := func(m *mark, p int) bool { return m.e.Offset <= p && p < m.e.Offset+m.e.Length }
	// sync closes the marks not active at p and queues the ones to open.
	sync := func(p int, newline bool) {
		keep := 0
		for keep < len(stack) && !newline && active(stack[keep], p) {
			keep++
		}
		for i := len(stack) - 1; i >= keep; i-- {
			out.WriteString(stack[i].shut)
		}
		out.WriteString(held)
		held = ""
		stack = stack[:keep]
		pending = pending[:0]
		if newline {
			return
		}
		for _, m := range marks {
			if active(m, p) && !containsMark(stack, m) {
				pending = append(pending, m)
			}
		}
	}
	// write emits text at the current marks.
	write := func(s string, raw bool) {
		core := strings.TrimLeftFunc(s, unicode.IsSpace)
		lead := s[:len(s)-len(core)]
		trimmed := strings.TrimRightFunc(core, unicode.IsSpace)
		trail := core[len(trimmed):]
		if trimmed == "" {
			held += s
			return
		}
		out.WriteString(held + lead)
		held = ""
		for _, m := range pending {
			out.WriteString(m.open)
			stack = append(stack, m)
		}
		pending = pending[:0]
		if raw {
			out.WriteString(trimmed)
		} else {
			out.WriteString(escapeMarkdown(trimmed))
		}
		held = trail
	}
	inCode := func() bool {
		for _, m := range append(append([]*mark{}, stack...), pending...) {
			if m.raw {
				return true
			}
		}
		return false
	}

	p := from
	for p < to {
		if e, ok := emoji[p]; ok {
			sync(p, false)
			alt := string(utf16.Decode(u[e.Offset : e.Offset+e.Length]))
			write(emojiRef(e.CustomEmojiID, alt), true)
			p = e.Offset + e.Length
			continue
		}
		if u[p] == '\n' {
			sync(p, true)
			out.WriteString("\n")
			p++
			continue
		}
		sync(p, false)
		// The run up to the next boundary: a mark's edge, an emoji or a
		// line break.
		end := p + 1
		for end < to && u[end] != '\n' && !boundary(marks, end) {
			if _, ok := emoji[end]; ok {
				break
			}
			end++
		}
		write(string(utf16.Decode(u[p:end])), inCode())
		p = end
	}
	sync(to, true)
	return breakLines(out.String())
}

func containsMark(list []*mark, m *mark) bool {
	for _, x := range list {
		if x == m {
			return true
		}
	}
	return false
}

func boundary(marks []*mark, p int) bool {
	for _, m := range marks {
		if m.e.Offset == p || m.e.Offset+m.e.Length == p {
			return true
		}
	}
	return false
}

var (
	listStart    = regexp.MustCompile(`^(\s*)([-+])(\s|$)`)
	orderedStart = regexp.MustCompile(`^(\s*\d+)([.)])(\s|$)`)
	ruleLine     = regexp.MustCompile(`^\s*=+\s*$`)
)

// breakLines keeps every line break of the message, which Markdown would
// join into one paragraph, and keeps lines from turning into lists or
// headings.
func breakLines(md string) string {
	lines := strings.Split(md, "\n")
	for i, l := range lines {
		l = listStart.ReplaceAllString(l, `$1\$2$3`)
		l = orderedStart.ReplaceAllString(l, `$1\$2$3`)
		if ruleLine.MatchString(l) {
			l = `\` + strings.TrimLeft(l, " ")
		}
		if l != "" && i+1 < len(lines) && lines[i+1] != "" {
			l += "  "
		}
		lines[i] = l
	}
	return strings.Join(lines, "\n")
}

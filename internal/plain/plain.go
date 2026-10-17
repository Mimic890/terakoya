// Package plain shows articles as plain text that people without any
// Markdown or LaTeX knowledge can copy, edit and send back, and merges their
// edited text into the Markdown source without losing the formatting of the
// parts they did not touch.
package plain

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Mimic890/terakoya/internal/content"
	"github.com/Mimic890/terakoya/internal/render"
)

// Kind is the type of a Markdown block.
type Kind int

const (
	Paragraph Kind = iota
	Heading
	List
	Table
	Quote
	Code
	Math
	Image
)

// Span is a formatted piece of a block (a formula, a link, bold text...)
// with its plain rendering. When edited text still contains the plain
// rendering, the source is put back.
type Span struct {
	Plain, Source string
}

// Block is one Markdown block: a paragraph, heading, list, table, quote,
// code block, display formula or image.
type Block struct {
	Kind  Kind
	Level int // heading level
	// Source is the Markdown of the block; Start and End are its byte
	// offsets in the body.
	Source     string
	Start, End int
	// Plain is the readable text. It never contains blank lines, so blocks
	// can be told apart by the blank lines between them.
	Plain string
	Spans []Span
	// Lines pairs each plain line of a table or list with its source, so
	// untouched rows and items keep their exact Markdown.
	Lines []Span
}

// Options configure the plain rendering.
type Options struct {
	// LinkTitle returns the title of the article a [[link]] points to, or "".
	LinkTitle func(target string) string
}

// Image placeholders shown instead of pictures.
const (
	imageMark      = "[картинка]"
	imageMarkTitle = "[картинка: %s]"
)

var (
	fenceStart  = regexp.MustCompile("^[ \t]{0,3}(`{3,}|~{3,})")
	headingLine = regexp.MustCompile(`^[ \t]{0,3}(#{1,6})(?:[ \t]+(.*?))?[ \t#]*$`)
	listItem    = regexp.MustCompile(`^([ \t]*)([-*+]|\d{1,9}[.)])[ \t]+(?:\[([ xX])\][ \t]+)?(.*)$`)
	tableRule   = regexp.MustCompile(`^\|?[ \t]*:?-+:?[ \t]*(\|[ \t]*:?-+:?[ \t]*)*\|?$`)
)

// Parse splits an article body into blocks.
func Parse(body string, opt Options) []Block {
	lines := strings.Split(body, "\n")
	offs := make([]int, len(lines)+1)
	for i, l := range lines {
		offs[i+1] = offs[i] + len(l) + 1
	}
	var blocks []Block
	for i := 0; i < len(lines); {
		t := strings.TrimSpace(lines[i])
		if t == "" {
			i++
			continue
		}
		start := i
		kind := Paragraph
		switch {
		case fenceStart.MatchString(lines[i]):
			kind = Code
			fence := fenceStart.FindStringSubmatch(lines[i])[1]
			for i++; i < len(lines); i++ {
				if c := strings.TrimSpace(lines[i]); strings.HasPrefix(c, fence[:3]) && strings.Trim(c, fence[:1]) == "" && len(c) >= len(fence) {
					i++
					break
				}
			}
		case strings.HasPrefix(t, "$$"):
			kind = Math
			if t == "$$" || !strings.HasSuffix(t, "$$") || len(t) < 4 {
				for i++; i < len(lines); i++ {
					if strings.HasSuffix(strings.TrimSpace(lines[i]), "$$") {
						break
					}
				}
			}
			i++
		case headingLine.MatchString(lines[i]):
			kind = Heading
			i++
		default:
			for i++; i < len(lines); i++ {
				l := lines[i]
				if strings.TrimSpace(l) == "" || fenceStart.MatchString(l) || headingLine.MatchString(l) ||
					strings.HasPrefix(strings.TrimSpace(l), "$$") {
					break
				}
			}
		}
		if i > len(lines) {
			i = len(lines)
		}
		end := offs[i] - 1 // without the last newline
		if end > len(body) {
			end = len(body)
		}
		blk := Block{Kind: kind, Start: offs[start], End: end, Source: body[offs[start]:end]}
		blk.render(lines[start:i], opt)
		blocks = append(blocks, blk)
	}
	return blocks
}

// render fills in the kind details, the plain text and the spans.
func (b *Block) render(lines []string, opt Options) {
	var out []string
	add := func(s string, spans []Span) {
		out = append(out, s)
		b.Spans = append(b.Spans, spans...)
	}
	switch b.Kind {
	case Heading:
		m := headingLine.FindStringSubmatch(lines[0])
		b.Level = len(m[1])
		add(inline(m[2], opt))
	case Code:
		for _, l := range lines[1:] {
			if fenceStart.MatchString(l) {
				break
			}
			out = append(out, l)
		}
	case Math:
		src := strings.TrimSpace(strings.Join(lines, "\n"))
		src = strings.TrimSuffix(strings.TrimPrefix(src, "$$"), "$$")
		p := Latex(src)
		out = append(out, p)
		b.Spans = append(b.Spans, Span{Plain: p, Source: b.Source})
	default:
		b.Kind = classify(lines)
		switch b.Kind {
		case Image:
			l, _ := imageOnly(lines[0])
			p := imageText(l.Text)
			if l.Wiki {
				p = imageMark
			}
			add(p, []Span{{Plain: p, Source: b.Source}})
		case Table:
			for _, l := range lines {
				t := strings.TrimSpace(l)
				if tableRule.MatchString(t) {
					continue
				}
				var cells []string
				for _, c := range splitRow(t) {
					p, spans := inline(strings.TrimSpace(c), opt)
					cells = append(cells, p)
					b.Spans = append(b.Spans, spans...)
				}
				out = append(out, strings.Join(cells, " | "))
				b.Lines = append(b.Lines, Span{Plain: out[len(out)-1], Source: l})
			}
		case List:
			for _, l := range lines {
				if m := listItem.FindStringSubmatch(l); m != nil {
					marker := "• "
					switch {
					case m[3] == " ":
						marker = "☐ "
					case m[3] != "":
						marker = "☑ "
					case m[2][0] >= '0' && m[2][0] <= '9':
						marker = m[2] + " "
					}
					p, spans := inline(m[4], opt)
					add(marker+p, spans)
					b.Lines = append(b.Lines, Span{Plain: marker + p, Source: l})
					continue
				}
				// A continuation line joins the item above.
				p, spans := inline(strings.TrimSpace(l), opt)
				if len(out) == 0 {
					add(p, spans)
					b.Lines = append(b.Lines, Span{Plain: p, Source: l})
					continue
				}
				out[len(out)-1] += " " + p
				b.Spans = append(b.Spans, spans...)
				last := &b.Lines[len(b.Lines)-1]
				last.Plain, last.Source = out[len(out)-1], last.Source+"\n"+l
			}
		case Quote:
			var text []string
			for _, l := range lines {
				text = append(text, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), ">")))
			}
			// Obsidian shows every line of a paragraph on its own line.
			add(inline(strings.Join(text, "\n"), opt))
		default:
			var text []string
			for _, l := range lines {
				text = append(text, strings.TrimSpace(l))
			}
			add(inline(strings.Join(text, "\n"), opt))
		}
	}
	b.Plain = joinLines(out)
}

// imageOnly parses a line that is just an image.
func imageOnly(line string) (content.Link, bool) {
	t := strings.TrimSpace(line)
	l, ok := content.LinkAt(t)
	return l, ok && l.Image && l.Len == len(t)
}

func classify(lines []string) Kind {
	if _, ok := imageOnly(lines[0]); ok && len(lines) == 1 {
		return Image
	}
	if listItem.MatchString(lines[0]) {
		return List
	}
	all := func(prefix string) bool {
		for _, l := range lines {
			if !strings.HasPrefix(strings.TrimSpace(l), prefix) {
				return false
			}
		}
		return true
	}
	switch {
	case all("|"):
		return Table
	case all(">"):
		return Quote
	}
	return Paragraph
}

// splitRow splits a table row into cells, honoring \| escapes.
func splitRow(row string) []string {
	row = strings.TrimSuffix(strings.TrimPrefix(row, "|"), "|")
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(row); i++ {
		switch {
		case row[i] == '\\' && i+1 < len(row) && row[i+1] == '|':
			cur.WriteString(`\|`)
			i++
		case row[i] == '|':
			cells = append(cells, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(row[i])
		}
	}
	return append(cells, cur.String())
}

// joinLines drops empty lines so a block's plain text has no blank lines.
func joinLines(lines []string) string {
	var kept []string
	for _, l := range lines {
		for _, part := range strings.Split(l, "\n") {
			if strings.TrimSpace(part) != "" {
				kept = append(kept, strings.TrimRight(part, " \t"))
			}
		}
	}
	return strings.Join(kept, "\n")
}

func imageText(alt string) string {
	alt = strings.TrimSpace(alt)
	if alt == "" {
		return imageMark
	}
	return strings.Replace(imageMarkTitle, "%s", alt, 1)
}

// inline renders one line of Markdown as plain text and returns the
// formatted spans in it.
func inline(s string, opt Options) (string, []Span) {
	var b strings.Builder
	var spans []Span
	span := func(src, plain string, inner []Span) {
		b.WriteString(plain)
		spans = append(spans, Span{Plain: plain, Source: src})
		spans = append(spans, inner...)
	}
	for j := 0; j < len(s); {
		ch := s[j]
		rest := s[j:]
		switch {
		case ch == '\\' && j+1 < len(s) && isASCIIPunct(s[j+1]):
			b.WriteByte(s[j+1])
			j += 2

		case ch == '`':
			n := 0
			for n < len(rest) && rest[n] == '`' {
				n++
			}
			end := strings.Index(rest[n:], rest[:n])
			if end < 0 {
				b.WriteString(rest[:n])
				j += n
				continue
			}
			code := strings.TrimSpace(rest[n : n+end])
			span(rest[:n+end+n], code, nil)
			j += n + end + n

		case ch == '$':
			n := render.MathSpan(rest)
			if n == 0 {
				b.WriteByte('$')
				j++
				continue
			}
			d := 1
			if strings.HasPrefix(rest, "$$") {
				d = 2
			}
			// Rows of an inline array or aligned block, on one line.
			span(rest[:n], strings.ReplaceAll(Latex(rest[d:n-d]), "\n", "; "), nil)
			j += n

		case ch == '[' || ch == '!':
			l, ok := content.LinkAt(rest)
			if !ok {
				b.WriteByte(ch)
				j++
				continue
			}
			src := rest[:l.Len]
			switch {
			case l.Image && l.Wiki:
				span(src, imageMark, nil)
			case l.Image:
				span(src, imageText(l.Text), nil)
			case l.Wiki:
				label := l.Text
				target := strings.TrimSuffix(content.FileTarget(l), ".md")
				if label == "" && opt.LinkTitle != nil {
					label = opt.LinkTitle(target)
				}
				if label == "" {
					label = content.TitleFromFile(target)
				}
				span(src, label, nil)
			default:
				p, inner := inline(l.Text, opt)
				span(src, p, inner)
			}
			j += l.Len

		case ch == '*' || ch == '_' || ch == '~':
			n, delim := emphasis(s, j)
			if n == 0 {
				b.WriteByte(ch)
				j++
				continue
			}
			p, inner := inline(s[j+len(delim):j+n-len(delim)], opt)
			span(s[j:j+n], p, inner)
			j += n

		default:
			b.WriteByte(ch)
			j++
		}
	}
	return b.String(), spans
}

// emphasis returns the length of a **bold**, *italic*, __bold__, _italic_
// or ~~strike~~ span starting at s[j], or 0.
func emphasis(s string, j int) (int, string) {
	ch := s[j]
	delim := string(ch)
	if j+1 < len(s) && s[j+1] == ch {
		delim += string(ch)
	} else if ch == '~' {
		return 0, ""
	}
	open := j + len(delim)
	if open >= len(s) || s[open] == ' ' || s[open] == ch {
		return 0, ""
	}
	if ch == '_' && j > 0 && isWordByte(s[j-1]) {
		return 0, ""
	}
	for k := open + 1; k+len(delim) <= len(s); k++ {
		if s[k] == '\\' {
			k++
			continue
		}
		if !strings.HasPrefix(s[k:], delim) || s[k-1] == ' ' {
			continue
		}
		after := k + len(delim)
		if after < len(s) && s[after] == ch {
			continue
		}
		if ch == '_' && after < len(s) && isWordByte(s[after]) {
			continue
		}
		return after - j, delim
	}
	return 0, ""
}

func isASCIIPunct(c byte) bool {
	return c < utf8.RuneSelf && strings.IndexByte("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", c) >= 0
}

// isWordByte reports whether c is part of a word; bytes of multi-byte
// runes count as letters.
func isWordByte(c byte) bool {
	return c >= utf8.RuneSelf || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// Section is a part of an article offered for editing on its own: the text
// under one heading, or a piece of it when it is too long for one message.
type Section struct {
	// Title is the plain heading, or "" for the part before the first
	// heading.
	Title string
	// Part numbers the pieces of a section split for length, from 1; it is
	// 0 when the section is whole.
	Part int
	// Start and End are byte offsets in the body.
	Start, End int
	Blocks     []Block
}

// Plain is the text the user gets to edit.
func (s Section) Plain() string {
	var parts []string
	for _, b := range s.Blocks {
		parts = append(parts, b.Plain)
	}
	return strings.Join(parts, "\n\n")
}

// Sections splits a body for editing. If the plain text of the whole body
// fits in max characters, there is one section. Otherwise the body is split
// at headings of level 2 and below, and sections longer than max are split
// further between blocks.
func Sections(body string, max int, opt Options) []Section {
	blocks := Parse(body, opt)
	if len(blocks) == 0 {
		return []Section{{Start: 0, End: 0}}
	}
	whole := newSection("", 0, blocks)
	if length(whole) <= max {
		return []Section{whole}
	}
	var groups [][]Block
	for i, b := range blocks {
		if i == 0 || b.Kind == Heading && b.Level >= 2 {
			groups = append(groups, nil)
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], b)
	}
	var out []Section
	for _, g := range groups {
		title := ""
		if g[0].Kind == Heading && g[0].Level >= 2 {
			title = g[0].Plain
		}
		sec := newSection(title, 0, g)
		if length(sec) <= max {
			out = append(out, sec)
			continue
		}
		part, cur, size := 1, []Block(nil), 0
		for _, b := range g {
			n := utf8.RuneCountInString(b.Plain) + 2
			if len(cur) > 0 && size+n > max {
				out = append(out, newSection(title, part, cur))
				part, cur, size = part+1, nil, 0
			}
			cur, size = append(cur, b), size+n
		}
		out = append(out, newSection(title, part, cur))
	}
	return out
}

func newSection(title string, part int, blocks []Block) Section {
	return Section{Title: title, Part: part, Start: blocks[0].Start, End: blocks[len(blocks)-1].End, Blocks: blocks}
}

func length(s Section) int { return utf8.RuneCountInString(s.Plain()) }

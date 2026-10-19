// Package render converts vault notes into Telegram Rich Markdown.
//
// The conversion works on the source text so that everything Telegram
// already understands passes through unchanged. It rewrites what Telegram
// cannot render from a vault: block math, local images, links between
// notes, Obsidian's line breaks, stray dollar signs and the back-link block
// at the end of a note. Notes too long for one message are split at
// headings into several parts.
package render

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/Mimic890/terakoya/internal/content"
)

// MaxImageSize is the largest image Telegram accepts as a photo.
const MaxImageSize = 10 << 20

// ImageExts are the picture formats of a vault. SVG is rasterized before
// sending.
var ImageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".svg": true}

// ArticleCallbackPrefix starts the callback_data of buttons that open a
// note; the rest is the note ID.
const ArticleCallbackPrefix = "a:"

// Options configure a conversion.
type Options struct {
	Limits Limits
	// Continued formats the title repeated at the top of every part after
	// the first, e.g. "%s (продолжение)".
	Continued string
	// Callbacks turns links to CallbackLink targets into buttons. It is
	// for menus the bot writes itself, never for notes from the vault.
	Callbacks bool
}

// callbackScheme starts a CallbackLink.
const callbackScheme = "callback:"

// CallbackLink is a link target that, with Options.Callbacks, makes the
// link an in-text button sending data as its callback data.
func CallbackLink(data string) string { return callbackScheme + data }

// Media is a local image referenced from the converted Markdown as
// tg://photo?id=<ID>.
type Media struct {
	ID string
	// SHA256 is the hash of the file as stored (the SVG source for SVGs).
	SHA256  string
	AbsPath string
	Path    string // vault-relative
	SVG     bool
}

// Part is one rich message.
type Part struct {
	Markdown string
	// Media lists each distinct local image once, in order of first use.
	Media []Media
	Stats Stats
}

// Result of a conversion.
type Result struct {
	Parts  []Part
	Issues []content.Issue
}

// Media returns the images of all parts.
func (r *Result) Media() []Media {
	var out []Media
	for _, p := range r.Parts {
		out = append(out, p.Media...)
	}
	return out
}

// MediaID returns the rich message media ID for a file hash.
func MediaID(sha string) string { return "p" + sha[:24] }

// Convert renders a note from cat, split into as many parts as the
// Telegram limits require.
func Convert(cat *content.Catalog, n *content.Note, opts Options) *Result {
	if opts.Limits == (Limits{}) {
		opts.Limits = DefaultLimits
	}
	if opts.Continued == "" {
		opts.Continued = "%s (cont.)"
	}
	body, _ := content.SplitBacklink(n.Body)
	lines := strings.Split(body, "\n")
	src := make([]srcLine, len(lines))
	for i, l := range lines {
		src[i] = srcLine{l, i + 1}
	}
	res := &Result{}
	issues := map[content.Issue]bool{}
	add := func(is []content.Issue) {
		for _, i := range is {
			if !issues[i] {
				issues[i] = true
				res.Issues = append(res.Issues, i)
			}
		}
	}

	conv := func(src []srcLine, first bool) (*converter, []string) {
		if !first {
			head := srcLine{"# " + fmt.Sprintf(opts.Continued, n.Title), 0}
			src = append([]srcLine{head, {"", 0}}, src...)
		}
		c := &converter{cat: cat, note: n, mediaSeen: map[string]bool{}, callbacks: opts.Callbacks}
		c.run(src)
		return c, opts.Limits.Check(c.part.Stats)
	}
	if c, over := conv(src, true); len(over) == 0 {
		res.Parts = []Part{c.part}
		add(c.issues)
		return res
	}

	pieces := split(src, func(p []srcLine) bool {
		_, over := conv(p, false)
		return len(over) == 0
	})
	var cur []srcLine
	flush := func() {
		if len(cur) == 0 {
			return
		}
		c, over := conv(cur, len(res.Parts) == 0)
		add(c.issues)
		for _, msg := range over {
			add([]content.Issue{{Path: n.Path, Line: cur[0].no, Severity: content.Error,
				Msg: msg + "; this block cannot be split further"}})
		}
		res.Parts = append(res.Parts, c.part)
		cur = nil
	}
	for _, p := range pieces {
		if len(cur) > 0 {
			if _, over := conv(join(cur, p), len(res.Parts) == 0); len(over) > 0 {
				flush()
			}
		}
		cur = join(cur, p)
	}
	flush()
	if len(res.Parts) > 1 {
		add([]content.Issue{{Path: n.Path, Severity: content.Info,
			Msg: fmt.Sprintf("too long for one message; sent in %d parts", len(res.Parts))}})
	}
	return res
}

func join(a, b []srcLine) []srcLine {
	out := append([]srcLine{}, a...)
	if len(out) > 0 && len(b) > 0 {
		out = append(out, srcLine{"", 0})
	}
	return append(out, b...)
}

type srcLine struct {
	text string
	no   int // 1-based line in the note, 0 for generated lines
}

type converter struct {
	cat       *content.Catalog
	note      *content.Note
	part      Part
	issues    []content.Issue
	out       []string
	breakable []bool // whether out[i] is ordinary text that may take a line break
	mediaSeen map[string]bool
	mediaRefs int
	callbacks bool
}

func (c *converter) issue(line int, sev content.Severity, format string, args ...any) {
	is := content.Issue{Path: c.note.Path, Line: line, Severity: sev, Msg: fmt.Sprintf(format, args...)}
	for _, prev := range c.issues {
		if prev == is {
			return
		}
	}
	c.issues = append(c.issues, is)
}

func (c *converter) emit(line string, text bool) {
	c.out = append(c.out, line)
	c.breakable = append(c.breakable, text)
}

var (
	fenceOpen = regexp.MustCompile("^([ \t]*(?:>[ \t]?)*[ \t]*)(`{3,}|~{3,})(.*)$")
	// linePrefix matches indentation and blockquote markers.
	linePrefix = regexp.MustCompile(`^[ \t]*(?:>[ \t]?)*[ \t]*`)
	// blockStart matches lines that start a new block rather than continue
	// a paragraph.
	blockStart = regexp.MustCompile(`^[ \t]*(?:[-*+][ \t]|\d+[.)][ \t]|#{1,6}([ \t]|$)|\||` + "```" + `|~~~|\$\$|>)`)
)

func (c *converter) run(src []srcLine) {
	// Leading blank lines carry no content.
	for len(src) > 0 && strings.TrimSpace(src[0].text) == "" {
		src = src[1:]
	}
	var fence string // closing fence marker while inside a code block
	for i := 0; i < len(src); i++ {
		line, lineNo := src[i].text, src[i].no

		if fence != "" {
			c.emit(line, false)
			rest := strings.TrimSpace(linePrefix.ReplaceAllString(line, ""))
			if strings.HasPrefix(rest, fence) && strings.Trim(rest, fence[:1]) == "" {
				fence = ""
			}
			continue
		}
		if m := fenceOpen.FindStringSubmatch(line); m != nil && !(m[2][0] == '`' && strings.Contains(m[3], "`")) {
			fence = m[2]
			c.emit(line, false)
			continue
		}

		prefix := linePrefix.FindString(line)
		body := strings.TrimSpace(line[len(prefix):])

		if strings.HasPrefix(body, "$$") {
			if n := c.blockMath(src[i:], prefix, lineNo); n > 0 {
				i += n - 1
				continue
			}
		}
		if l, ok := content.LinkAt(body); ok && l.Image && l.Len == len(body) {
			c.imageBlock(prefix, l, lineNo)
			continue
		}
		text := body != "" && !strings.HasPrefix(body, "|")
		c.emit(prefix+c.inline(body, lineNo), text)
	}
	c.lineBreaks()
	c.part.Markdown = strings.TrimRight(strings.Join(c.out, "\n"), "\n") + "\n"
	c.part.Stats = Measure(c.part.Markdown)
	c.part.Stats.Media = c.mediaRefs
}

// lineBreaks keeps Obsidian's line breaks: a single newline inside a
// paragraph is a line break there, while Markdown would join the lines.
// Two trailing spaces make it a hard break.
func (c *converter) lineBreaks() {
	for i := 0; i+1 < len(c.out); i++ {
		if !c.breakable[i] || !c.breakable[i+1] {
			continue
		}
		next := c.out[i+1]
		if blockStart.MatchString(next) && !continuesQuote(c.out[i], next) {
			continue
		}
		c.out[i] = strings.TrimRight(c.out[i], " \t") + "  "
	}
}

// continuesQuote reports whether next is the next line of the quote that
// line is in, rather than the start of a new block.
func continuesQuote(line, next string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), ">") && strings.HasPrefix(strings.TrimSpace(next), ">") &&
		!blockStart.MatchString(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(next), ">")))
}

// blockMath converts a $$…$$ block that starts at src[0] into a ```math
// fence and returns the number of lines consumed, or 0 if the block is not
// terminated.
func (c *converter) blockMath(src []srcLine, prefix string, lineNo int) int {
	strip := func(l string) string {
		if strings.HasPrefix(l, prefix) {
			return l[len(prefix):]
		}
		return strings.TrimLeft(l, " \t>")
	}
	first := strings.TrimLeft(strings.TrimSpace(strip(src[0].text))[2:], " \t")
	var lines []string
	n := 0
	if end := strings.Index(first, "$$"); end >= 0 {
		if tail := strings.TrimSpace(first[end+2:]); tail != "" {
			c.issue(lineNo, content.Error, "text after closing $$ on the same line")
		}
		lines = append(lines, first[:end])
		n = 1
	} else {
		lines = append(lines, first)
		for j := 1; j < len(src); j++ {
			l := strip(src[j].text)
			t := strings.TrimRight(l, " \t")
			if strings.HasSuffix(t, "$$") {
				lines = append(lines, strings.TrimSuffix(t, "$$"))
				n = j + 1
				break
			}
			if strings.TrimSpace(l) == "" {
				// A blank line ends a paragraph, so the block cannot continue.
				break
			}
			lines = append(lines, l)
		}
	}
	if n == 0 {
		c.issue(lineNo, content.Error, "unterminated $$ block")
		return 0
	}
	var body []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			body = append(body, strings.TrimRight(l, " \t"))
		}
	}
	if len(body) == 0 {
		c.issue(lineNo, content.Error, "empty $$ block")
	}
	c.emit(prefix+"```math", false)
	for _, l := range dedent(body) {
		c.emit(prefix+l, false)
	}
	c.emit(prefix+"```", false)
	return n
}

func dedent(lines []string) []string {
	min := -1
	for _, l := range lines {
		n := len(l) - len(strings.TrimLeft(l, " \t"))
		if min < 0 || n < min {
			min = n
		}
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l[min:]
	}
	return out
}

func (c *converter) imageBlock(prefix string, l content.Link, lineNo int) {
	if !l.Wiki && content.IsRemote(l.Target) {
		c.mediaRefs++
		c.emit(prefix+"!["+l.Text+"]("+l.Target+")", false)
		return
	}
	media, err := c.localImage(l)
	if err != nil {
		c.issue(lineNo, content.Error, "image %s: %v", content.FileTarget(l), err)
		if l.Text != "" && !l.Wiki {
			c.emit(prefix+escapeText(l.Text), true)
		}
		return
	}
	c.mediaRefs++
	if !c.mediaSeen[media.ID] {
		c.mediaSeen[media.ID] = true
		c.part.Media = append(c.part.Media, media)
	}
	alt := l.Text
	if l.Wiki {
		alt = ""
	}
	ref := "![" + alt + "](tg://photo?id=" + media.ID
	if l.Title != "" {
		ref += ` "` + l.Title + `"`
	}
	c.emit(prefix+ref+")", false)
}

// localImage resolves an image link and hashes the file.
func (c *converter) localImage(l content.Link) (Media, error) {
	rel, err := c.cat.ResolveImage(c.note, l)
	if err != nil {
		return Media{}, err
	}
	ext := strings.ToLower(path.Ext(rel))
	if !ImageExts[ext] {
		return Media{}, fmt.Errorf("unsupported format %q (use svg, png or jpg)", ext)
	}
	abs := c.cat.Root + "/" + rel
	f, err := os.Open(abs)
	if err != nil {
		return Media{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Media{}, err
	}
	if st.Size() > MaxImageSize {
		return Media{}, fmt.Errorf("file is larger than %d MB", MaxImageSize>>20)
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return Media{}, err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	return Media{ID: MediaID(sum), SHA256: sum, AbsPath: abs, Path: rel, SVG: ext == ".svg"}, nil
}

// inline rewrites one line of text outside code blocks.
func (c *converter) inline(s string, lineNo int) string {
	var b strings.Builder
	for j := 0; j < len(s); {
		ch := s[j]
		switch {
		case ch == '\\' && j+1 < len(s):
			b.WriteString(s[j : j+2])
			j += 2

		case ch == '`':
			n := runLen(s[j:], '`')
			if end := closingRun(s[j+n:], n); end >= 0 {
				b.WriteString(s[j : j+n+end+n])
				j += n + end + n
			} else {
				b.WriteString(s[j : j+n])
				j += n
			}

		case ch == '$':
			if end := closingMath(s[j:]); end > 0 {
				b.WriteString(s[j : j+end])
				j += end
				continue
			}
			c.issue(lineNo, content.Warning, "unmatched $; write \\$ for a literal dollar sign")
			b.WriteString(`\$`)
			j++

		case ch == '[' || ch == '!':
			l, ok := content.LinkAt(s[j:])
			if !ok {
				b.WriteByte(ch)
				j++
				continue
			}
			b.WriteString(c.link(l, lineNo))
			j += l.Len

		default:
			b.WriteByte(ch)
			j++
		}
	}
	return b.String()
}

// link renders a link inside a line: a note link becomes a callback button
// that opens the note in place, a web link stays as it is.
func (c *converter) link(l content.Link, lineNo int) string {
	if l.Image && !l.Wiki && strings.HasPrefix(strings.ToLower(l.Target), "tg://emoji?") {
		// A custom emoji, which Telegram draws inline.
		return "![" + l.Text + "](" + l.Target + ")"
	}
	if l.Image {
		c.issue(lineNo, content.Error, "image %s must be on its own line", content.FileTarget(l))
		return escapeText(l.Text)
	}
	if c.callbacks && !l.Wiki && strings.HasPrefix(l.Target, callbackScheme) {
		return button(strings.TrimPrefix(l.Target, callbackScheme), l.Text)
	}
	if !l.Wiki && content.IsRemote(l.Target) {
		return "[" + l.Text + "](" + l.Target + ")"
	}
	label := l.Text
	n, err := c.cat.ResolveNote(c.note, l)
	if err != nil {
		c.issue(lineNo, content.Error, "link to %s: %v", content.FileTarget(l), err)
		if label == "" {
			label = content.TitleFromFile(path.Base(content.FileTarget(l)))
		}
		return escapeText(label)
	}
	if label == "" {
		label = n.Title
	}
	return button(ArticleCallbackPrefix+n.ID, label)
}

// button is an in-text button sending data. Its text is plain: Markdown
// and math markers are dropped.
func button(data, label string) string {
	return `<tg-button type="callback_data" style="link" data="` + html.EscapeString(data) + `">` +
		html.EscapeString(plainLabel(label)) + "</tg-button>"
}

var labelMarks = strings.NewReplacer("**", "", "__", "", "~~", "", "`", "", "$", "", `\`, "")

func plainLabel(s string) string {
	return strings.TrimSpace(labelMarks.Replace(s))
}

func escapeText(s string) string {
	r := strings.NewReplacer(`[`, `\[`, `]`, `\]`)
	return r.Replace(s)
}

func runLen(s string, ch byte) int {
	n := 0
	for n < len(s) && s[n] == ch {
		n++
	}
	return n
}

// closingRun finds a run of exactly n backticks in s.
func closingRun(s string, n int) int {
	for i := 0; i < len(s); {
		if s[i] != '`' {
			i++
			continue
		}
		k := runLen(s[i:], '`')
		if k == n {
			return i
		}
		i += k
	}
	return -1
}

// closingMath returns the length of the $…$ or $$…$$ span at the start of s,
// or 0 if s does not start a valid math span.
func closingMath(s string) int {
	delim := "$"
	if strings.HasPrefix(s, "$$") {
		delim = "$$"
	}
	open := len(delim)
	if open >= len(s) || s[open] == ' ' || s[open] == '\t' {
		return 0
	}
	for i := open; i < len(s); i++ {
		switch {
		case s[i] == '\\':
			i++
		case strings.HasPrefix(s[i:], delim):
			if s[i-1] == ' ' || s[i-1] == '\t' || i == open {
				return 0
			}
			if delim == "$" && i+1 < len(s) && s[i+1] == '$' {
				return 0
			}
			return i + len(delim)
		}
	}
	return 0
}

// MathSpan returns the length of the $…$ or $$…$$ span at the start of s,
// or 0 if s does not start one. It follows the converter's rules.
func MathSpan(s string) int { return closingMath(s) }

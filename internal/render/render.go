// Package render converts article Markdown into Telegram Rich Markdown.
//
// The conversion works on the source text so that everything Telegram
// already understands passes through unchanged. It only rewrites what
// Telegram cannot render from a content repository: block math, local
// images, [[wikilinks]] and stray dollar signs.
package render

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Mimic890/terakoya/internal/content"
)

// MaxImageSize is the largest image Telegram accepts as a photo.
const MaxImageSize = 10 << 20

// ImageExts are the image formats that can be sent as photos.
var ImageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".webp": true}

// ArticleCallbackPrefix starts the callback_data of buttons that open an
// article; the rest is the article ID.
const ArticleCallbackPrefix = "a:"

// Options configure a conversion.
type Options struct {
	Limits Limits
}

// Media is a local image referenced from the converted Markdown as
// tg://photo?id=<ID>.
type Media struct {
	ID      string
	SHA256  string
	AbsPath string
}

// Result of a conversion.
type Result struct {
	Markdown string
	// Media lists each distinct local image once, in order of first use.
	Media  []Media
	Stats  Stats
	Issues []content.Issue
}

// LimitErrors returns the issues caused by Telegram message limits.
func (r *Result) LimitErrors() []content.Issue {
	var out []content.Issue
	for _, i := range r.Issues {
		if strings.HasPrefix(i.Msg, limitPrefix) {
			out = append(out, i)
		}
	}
	return out
}

// MediaID returns the rich message media ID for a file hash.
func MediaID(sha string) string { return "p" + sha[:24] }

// Convert renders an article from cat.
func Convert(cat *content.Catalog, a *content.Article, opts Options) *Result {
	if opts.Limits == (Limits{}) {
		opts.Limits = DefaultLimits
	}
	c := &converter{cat: cat, art: a, opts: opts, res: &Result{}, mediaSeen: map[string]bool{}}
	c.run()
	c.res.Stats = Measure(c.res.Markdown)
	c.res.Stats.Media = c.mediaRefs
	for _, msg := range opts.Limits.Check(c.res.Stats) {
		c.issue(0, content.Error, "%s", msg)
	}
	return c.res
}

type converter struct {
	cat       *content.Catalog
	art       *content.Article
	opts      Options
	res       *Result
	out       []string
	mediaSeen map[string]bool
	mediaRefs int
}

func (c *converter) issue(line int, sev content.Severity, format string, args ...any) {
	is := content.Issue{Path: c.art.Path, Line: line, Severity: sev, Msg: fmt.Sprintf(format, args...)}
	for _, prev := range c.res.Issues {
		if prev == is {
			return
		}
	}
	c.res.Issues = append(c.res.Issues, is)
}

var (
	fenceOpen = regexp.MustCompile("^([ \t]*(?:>[ \t]?)*[ \t]*)(`{3,}|~{3,})(.*)$")
	// linePrefix matches indentation and blockquote markers.
	linePrefix = regexp.MustCompile(`^[ \t]*(?:>[ \t]?)*[ \t]*`)
	imageLine  = regexp.MustCompile(`^!\[([^\]]*)\]\(\s*(<[^>]*>|[^\s)]+)(?:\s+"([^"]*)")?\s*\)$`)
	imageAt    = regexp.MustCompile(`^!\[([^\]]*)\]\(\s*(<[^>]*>|[^\s)]+)(?:\s+"([^"]*)")?\s*\)`)
)

func (c *converter) run() {
	lines := strings.Split(strings.TrimLeft(c.art.Body, "\n"), "\n")
	// Keep line numbers right after trimming leading blank lines.
	first := c.art.BodyLine + strings.Count(c.art.Body, "\n") - strings.Count(strings.TrimLeft(c.art.Body, "\n"), "\n")

	var fence string // closing fence marker while inside a code block
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		lineNo := first + i

		if fence != "" {
			c.out = append(c.out, line)
			rest := strings.TrimSpace(linePrefix.ReplaceAllString(line, ""))
			if strings.HasPrefix(rest, fence) && strings.Trim(rest, fence[:1]) == "" {
				fence = ""
			}
			continue
		}
		if m := fenceOpen.FindStringSubmatch(line); m != nil && !(m[2][0] == '`' && strings.Contains(m[3], "`")) {
			fence = m[2]
			c.out = append(c.out, line)
			continue
		}

		prefix := linePrefix.FindString(line)
		body := strings.TrimSpace(line[len(prefix):])

		if strings.HasPrefix(body, "$$") {
			if n := c.blockMath(lines[i:], prefix, lineNo); n > 0 {
				i += n - 1
				continue
			}
		}
		if m := imageLine.FindStringSubmatch(body); m != nil {
			c.imageBlock(prefix, m, lineNo)
			continue
		}
		c.out = append(c.out, prefix+c.inline(body, lineNo))
	}
	c.res.Markdown = strings.TrimRight(strings.Join(c.out, "\n"), "\n") + "\n"
}

// blockMath converts a $$…$$ block that starts at lines[0] into a ```math
// fence and returns the number of lines consumed, or 0 if the block is not
// terminated.
func (c *converter) blockMath(lines []string, prefix string, lineNo int) int {
	strip := func(l string) string {
		if strings.HasPrefix(l, prefix) {
			return l[len(prefix):]
		}
		return strings.TrimLeft(l, " \t>")
	}
	first := strings.TrimLeft(strings.TrimSpace(strip(lines[0]))[2:], " \t")
	var src []string
	n := 0
	if end := strings.Index(first, "$$"); end >= 0 {
		if tail := strings.TrimSpace(first[end+2:]); tail != "" {
			c.issue(lineNo, content.Error, "text after closing $$ on the same line")
		}
		src = append(src, first[:end])
		n = 1
	} else {
		src = append(src, first)
		for j := 1; j < len(lines); j++ {
			l := strip(lines[j])
			t := strings.TrimRight(l, " \t")
			if strings.HasSuffix(t, "$$") {
				src = append(src, strings.TrimSuffix(t, "$$"))
				n = j + 1
				break
			}
			if strings.TrimSpace(l) == "" {
				// A blank line ends a paragraph, so the block cannot continue.
				break
			}
			src = append(src, l)
		}
	}
	if n == 0 {
		c.issue(lineNo, content.Error, "unterminated $$ block")
		return 0
	}
	var body []string
	for _, l := range src {
		if strings.TrimSpace(l) != "" {
			body = append(body, strings.TrimRight(l, " \t"))
		}
	}
	if len(body) == 0 {
		c.issue(lineNo, content.Error, "empty $$ block")
	}
	c.out = append(c.out, prefix+"```math")
	for _, l := range dedent(body) {
		c.out = append(c.out, prefix+l)
	}
	c.out = append(c.out, prefix+"```")
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

func (c *converter) imageBlock(prefix string, m []string, lineNo int) {
	alt, target, title := m[1], strings.Trim(m[2], "<>"), m[3]
	if isRemote(target) {
		c.mediaRefs++
		c.out = append(c.out, prefix+m[0])
		return
	}
	media, err := c.localImage(target)
	if err != nil {
		c.issue(lineNo, content.Error, "image %s: %v", target, err)
		if alt != "" {
			c.out = append(c.out, prefix+escapeText(alt))
		}
		return
	}
	c.mediaRefs++
	if !c.mediaSeen[media.ID] {
		c.mediaSeen[media.ID] = true
		c.res.Media = append(c.res.Media, media)
	}
	ref := "![" + alt + "](tg://photo?id=" + media.ID
	if title != "" {
		ref += ` "` + title + `"`
	}
	c.out = append(c.out, prefix+ref+")")
}

func isRemote(target string) bool {
	return strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "tg://")
}

// localImage resolves an image path relative to the article and hashes it.
func (c *converter) localImage(target string) (Media, error) {
	p, err := url.PathUnescape(target)
	if err != nil {
		p = target
	}
	if path.IsAbs(p) || strings.Contains(p, "\\") {
		return Media{}, fmt.Errorf("path must be relative to the article")
	}
	rel := path.Clean(path.Join(path.Dir(c.art.Path), p))
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return Media{}, fmt.Errorf("path leaves the content directory")
	}
	if ext := strings.ToLower(path.Ext(rel)); !ImageExts[ext] {
		return Media{}, fmt.Errorf("unsupported format %q (use png, jpg or webp)", ext)
	}
	abs := filepath.Join(c.cat.Root, filepath.FromSlash(rel))
	f, err := os.Open(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return Media{}, fmt.Errorf("file not found")
		}
		return Media{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Media{}, err
	}
	if st.IsDir() {
		return Media{}, fmt.Errorf("is a directory")
	}
	if st.Size() > MaxImageSize {
		return Media{}, fmt.Errorf("file is larger than %d MB", MaxImageSize>>20)
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return Media{}, err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	return Media{ID: MediaID(sum), SHA256: sum, AbsPath: abs}, nil
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

		case strings.HasPrefix(s[j:], "![["):
			end := strings.Index(s[j+3:], "]]")
			if end < 0 {
				b.WriteByte(ch)
				j++
				continue
			}
			c.issue(lineNo, content.Error, "![[%s]] embeds are not supported; use ![alt](assets/file.png)", s[j+3:j+3+end])
			j += 3 + end + 2

		case strings.HasPrefix(s[j:], "[["):
			end := strings.Index(s[j+2:], "]]")
			if end < 0 {
				b.WriteByte(ch)
				j++
				continue
			}
			b.WriteString(c.wikilink(s[j+2:j+2+end], lineNo))
			j += 2 + end + 2

		case ch == '!' && strings.HasPrefix(s[j:], "!["):
			m := imageAt.FindStringSubmatch(s[j:])
			if m == nil {
				b.WriteByte(ch)
				j++
				continue
			}
			c.issue(lineNo, content.Error, "image %s must be on its own line", m[2])
			b.WriteString(escapeText(m[1]))
			j += len(m[0])

		default:
			b.WriteByte(ch)
			j++
		}
	}
	return b.String()
}

func (c *converter) wikilink(inner string, lineNo int) string {
	// Inside tables Obsidian escapes the alias separator as \|.
	inner = strings.ReplaceAll(inner, `\|`, "|")
	target, label, _ := strings.Cut(inner, "|")
	if h := strings.IndexAny(target, "#^"); h >= 0 {
		target = target[:h]
	}
	target = strings.TrimSpace(target)
	label = strings.TrimSpace(label)
	a := c.cat.Resolve(c.art, target)
	if a == nil {
		c.issue(lineNo, content.Error, "link [[%s]] does not match any article", target)
		if label == "" {
			label = target
		}
		return escapeText(label)
	}
	if label == "" {
		label = a.Title
	}
	// An inline callback button styled as a link opens the article in place;
	// a t.me deep link would make the user send a visible /start message.
	return `<tg-button type="callback_data" style="link" data="` + html.EscapeString(ArticleCallbackPrefix+a.ID) + `">` +
		html.EscapeString(label) + "</tg-button>"
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

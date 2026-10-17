package content

import (
	"errors"
	"net/url"
	"path"
	"strings"
)

// Link is a link or embed in a line of Markdown.
type Link struct {
	// Text is the link text, image alt text or wikilink label ("" if none).
	Text string
	// Target is the destination as written: URL-encoded for Markdown
	// links, plain for wikilinks.
	Target string
	Title  string // Markdown link title, rarely used
	Image  bool
	Wiki   bool
	// Len is the length of the whole link in the line.
	Len int
}

// LinkAt parses a Markdown link, image, wikilink or embed starting at
// s[0]: [text](dest), ![alt](dest), [[target|label]] or ![[target]].
// Destinations may contain balanced parentheses, as Obsidian writes
// "Чертёж (задача 3).svg" without encoding them.
func LinkAt(s string) (Link, bool) {
	image := strings.HasPrefix(s, "!")
	rest := s
	if image {
		rest = s[1:]
	}
	off := len(s) - len(rest)
	if strings.HasPrefix(rest, "[[") {
		end := strings.Index(rest, "]]")
		if end < 0 {
			return Link{}, false
		}
		inner := strings.ReplaceAll(rest[2:end], `\|`, "|")
		target, label, _ := strings.Cut(inner, "|")
		return Link{Text: strings.TrimSpace(label), Target: strings.TrimSpace(target), Image: image, Wiki: true,
			Len: off + end + 2}, strings.TrimSpace(target) != ""
	}
	if !strings.HasPrefix(rest, "[") {
		return Link{}, false
	}
	// Link text: brackets may nest.
	depth, i := 0, 0
	for ; i < len(rest); i++ {
		switch rest[i] {
		case '\\':
			i++
		case '[':
			depth++
		case ']':
			depth--
		}
		if depth == 0 {
			break
		}
	}
	if i >= len(rest) || i+1 >= len(rest) || rest[i+1] != '(' {
		return Link{}, false
	}
	text := rest[1:i]
	j := i + 2
	for j < len(rest) && (rest[j] == ' ' || rest[j] == '\t') {
		j++
	}
	var dest string
	if j < len(rest) && rest[j] == '<' {
		end := strings.IndexByte(rest[j:], '>')
		if end < 0 {
			return Link{}, false
		}
		dest = rest[j+1 : j+end]
		j += end + 1
	} else {
		start, parens := j, 0
		for ; j < len(rest); j++ {
			c := rest[j]
			if c == '\\' && j+1 < len(rest) {
				j++
				continue
			}
			if c == '(' {
				parens++
			} else if c == ')' {
				if parens == 0 {
					break
				}
				parens--
			} else if c == ' ' || c == '\t' {
				break
			}
		}
		dest = rest[start:j]
	}
	for j < len(rest) && (rest[j] == ' ' || rest[j] == '\t') {
		j++
	}
	title := ""
	if j < len(rest) && (rest[j] == '"' || rest[j] == '\'') {
		end := strings.IndexByte(rest[j+1:], rest[j])
		if end < 0 {
			return Link{}, false
		}
		title = rest[j+1 : j+1+end]
		j += end + 2
		for j < len(rest) && (rest[j] == ' ' || rest[j] == '\t') {
			j++
		}
	}
	if j >= len(rest) || rest[j] != ')' {
		return Link{}, false
	}
	return Link{Text: text, Target: dest, Title: title, Image: image, Len: off + j + 1}, true
}

// IsRemote reports whether a link target is a URL rather than a file.
func IsRemote(target string) bool {
	t := strings.ToLower(target)
	for _, p := range []string{"http://", "https://", "tg://", "mailto:"} {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

// Resolution errors.
var (
	ErrNotFound  = errors.New("not found")
	ErrAmbiguous = errors.New("ambiguous: several files have this name; write the path")
)

// FileTarget turns a link target into a file reference: URL-decoded
// (Markdown links only), without "#heading", with ".md" added to note
// links that name no file type.
func FileTarget(l Link) string {
	t := l.Target
	if !l.Wiki {
		if d, err := url.PathUnescape(t); err == nil {
			t = d
		}
	}
	if i := strings.IndexAny(t, "#^"); i >= 0 && !l.Image {
		t = t[:i]
	}
	t = strings.TrimSpace(t)
	// "Признак (5.1)" has no extension even though path.Ext sees ".1)".
	if t != "" && !l.Image && !FileExts[strings.ToLower(path.Ext(t))] {
		t += ".md"
	}
	return t
}

// ResolveFile finds the vault file a link in the note at fromRel points
// to, as Obsidian does: a target with a path is taken relative to the note,
// then from the vault root; a bare file name must be unique in the vault.
// For images, the topic's _assets folder (assets, may be "") is tried first.
func (c *Catalog) ResolveFile(fromRel, target, assets string) (string, error) {
	if target == "" {
		return "", ErrNotFound
	}
	if strings.Contains(target, "/") {
		for _, p := range []string{path.Join(path.Dir(fromRel), target), strings.TrimPrefix(target, "/")} {
			if p = path.Clean(p); c.files[p] {
				return p, nil
			}
		}
		return "", ErrNotFound
	}
	if assets != "" {
		if p := assets + "/" + target; c.files[p] {
			return p, nil
		}
	}
	if m := c.byName[target]; len(m) > 0 {
		if len(m) > 1 {
			return "", ErrAmbiguous
		}
		return m[0], nil
	}
	if m := c.byFolded[strings.ToLower(target)]; len(m) > 0 {
		if len(m) > 1 {
			return "", ErrAmbiguous
		}
		return m[0], nil
	}
	return "", ErrNotFound
}

// ResolveNote finds the note a link in from points to.
func (c *Catalog) ResolveNote(from *Note, l Link) (*Note, error) {
	p, err := c.ResolveFile(from.Path, FileTarget(l), "")
	if err != nil {
		return nil, err
	}
	n := c.byPath[p]
	if n == nil {
		return nil, ErrNotFound
	}
	return n, nil
}

// ResolveImage finds the image file a link in from points to.
func (c *Catalog) ResolveImage(from *Note, l Link) (string, error) {
	assets := ""
	if from.Topic != nil {
		assets = from.Topic.Path() + "/_assets"
	}
	return c.ResolveFile(from.Path, FileTarget(l), assets)
}

// Links returns the links of a note body in order, outside code blocks and
// inline code.
func Links(body string) []Link {
	var out []Link
	ScanLines(body, func(_ int, line string, code bool) {
		if code {
			return
		}
		for i := 0; i < len(line); i++ {
			switch line[i] {
			case '\\':
				i++
			case '`':
				n := 1
				for i+n < len(line) && line[i+n] == '`' {
					n++
				}
				if end := strings.Index(line[i+n:], line[i:i+n]); end >= 0 {
					i += n + end + n - 1
				} else {
					i += n - 1
				}
			case '[', '!':
				if l, ok := LinkAt(line[i:]); ok {
					out = append(out, l)
					i += l.Len - 1
				}
			}
		}
	})
	return out
}

// ScanLines calls f for each line of a body with its 1-based number and
// whether it is inside a fenced code block (fence lines included).
func ScanLines(body string, f func(n int, line string, code bool)) {
	fence := ""
	for i, line := range strings.Split(body, "\n") {
		t := strings.TrimLeft(line, " \t>")
		switch {
		case fence != "":
			f(i+1, line, true)
			if strings.HasPrefix(strings.TrimSpace(t), fence) && strings.Trim(strings.TrimSpace(t), fence[:1]) == "" {
				fence = ""
			}
			continue
		case strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~"):
			n := 3
			for n < len(t) && t[n] == t[0] {
				n++
			}
			fence = t[:n]
			f(i+1, line, true)
			continue
		}
		f(i+1, line, false)
	}
}

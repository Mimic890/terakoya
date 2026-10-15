package render

import (
	"fmt"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// Limits are the Telegram rich message limits.
type Limits struct {
	Chars   int
	Blocks  int
	Media   int
	Nesting int
	Columns int
}

// DefaultLimits as documented for sendRichMessage.
var DefaultLimits = Limits{Chars: 32768, Blocks: 500, Media: 50, Nesting: 16, Columns: 20}

// Stats are measured on converted Markdown. They approximate what Telegram
// counts and err on the side of overcounting.
type Stats struct {
	Chars   int
	Blocks  int
	Media   int
	Nesting int
	Columns int
}

const limitPrefix = "message limit: "

// Check returns a message for every exceeded limit.
func (l Limits) Check(s Stats) []string {
	var out []string
	check := func(name string, got, max int) {
		if got > max {
			out = append(out, fmt.Sprintf("%s%s %d exceeds %d", limitPrefix, name, got, max))
		}
	}
	check("characters", s.Chars, l.Chars)
	check("blocks", s.Blocks, l.Blocks)
	check("media", s.Media, l.Media)
	check("nesting depth", s.Nesting, l.Nesting)
	check("table columns", s.Columns, l.Columns)
	return out
}

var parser = goldmark.New(goldmark.WithExtensions(extension.GFM, extension.Footnote)).Parser()

// Measure parses Markdown and counts text characters, blocks, nesting depth
// and table columns. Media is counted by the converter.
func Measure(md string) Stats {
	src := []byte(md)
	doc := parser.Parse(text.NewReader(src))
	var s Stats
	var walk func(n ast.Node, depth int)
	walk = func(n ast.Node, depth int) {
		switch v := n.(type) {
		case *ast.Text:
			s.Chars += utf8.RuneCount(v.Segment.Value(src))
		case *ast.String:
			s.Chars += utf8.RuneCount(v.Value)
		case *ast.FencedCodeBlock, *ast.CodeBlock, *ast.HTMLBlock:
			lines := n.Lines()
			for i := 0; i < lines.Len(); i++ {
				seg := lines.At(i)
				s.Chars += utf8.RuneCount(seg.Value(src))
			}
		case *extast.Table:
			if cols := len(v.Alignments); cols > s.Columns {
				s.Columns = cols
			}
		}
		if n.Kind() != ast.KindDocument {
			if n.Type() == ast.TypeBlock && n.Kind() != extast.KindTableCell {
				s.Blocks++
			}
			if n.Kind() != ast.KindText && n.Kind() != ast.KindString && depth > s.Nesting {
				s.Nesting = depth
			}
		}
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			next := depth + 1
			if n.Kind() == ast.KindDocument {
				next = 1
			}
			walk(c, next)
		}
	}
	walk(doc, 0)
	return s
}

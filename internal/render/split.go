package render

import (
	"regexp"
	"strings"
)

// atom is a block that is never cut: a paragraph, list, table, quote,
// formula or code block, or a heading.
type atom struct {
	lines []srcLine
	level int // heading level, 0 for other blocks
}

var (
	atomHeading = regexp.MustCompile(`^[ \t]{0,3}(#{1,6})([ \t]|$)`)
	atomList    = regexp.MustCompile(`^[ \t]*(?:[-*+]|\d+[.)])[ \t]`)
)

// atoms splits source lines into blocks at blank lines, keeping fenced
// code, $$ formulas and loose lists whole.
func atoms(src []srcLine) []atom {
	var out []atom
	var cur []srcLine
	flush := func() {
		if len(cur) > 0 {
			out = append(out, atom{lines: cur})
			cur = nil
		}
	}
	fence, math := "", false
	for i := 0; i < len(src); i++ {
		t := strings.TrimSpace(linePrefix.ReplaceAllString(src[i].text, ""))
		switch {
		case fence != "":
			cur = append(cur, src[i])
			if strings.HasPrefix(t, fence) && strings.Trim(t, fence[:1]) == "" {
				fence = ""
			}
			continue
		case math:
			cur = append(cur, src[i])
			if strings.HasSuffix(t, "$$") {
				math = false
			}
			continue
		}
		if m := fenceOpen.FindStringSubmatch(src[i].text); m != nil {
			fence = m[2]
			cur = append(cur, src[i])
			continue
		}
		if strings.HasPrefix(t, "$$") && (t == "$$" || !strings.HasSuffix(t[2:], "$$")) {
			math = true
			cur = append(cur, src[i])
			continue
		}
		if m := atomHeading.FindStringSubmatch(src[i].text); m != nil {
			flush()
			out = append(out, atom{lines: []srcLine{src[i]}, level: len(m[1])})
			continue
		}
		if t == "" {
			// A list continues after a blank line if the next line is an
			// item or indented.
			if len(cur) > 0 && atomList.MatchString(cur[0].text) {
				if j := nextText(src, i); j >= 0 && (atomList.MatchString(src[j].text) ||
					strings.HasPrefix(src[j].text, " ") || strings.HasPrefix(src[j].text, "\t")) {
					cur = append(cur, src[i])
					continue
				}
			}
			flush()
			continue
		}
		cur = append(cur, src[i])
	}
	flush()
	return out
}

func nextText(src []srcLine, i int) int {
	for j := i + 1; j < len(src); j++ {
		if strings.TrimSpace(src[j].text) != "" {
			return j
		}
	}
	return -1
}

// split cuts a note into pieces that can be packed into messages: whole
// "##" sections where they fit, oversized ones at "###", and oversized
// "###" sections between blocks. A heading always stays with the block
// after it.
func split(src []srcLine, fits func([]srcLine) bool) [][]srcLine {
	var out [][]srcLine
	var rec func(as []atom, level int)
	rec = func(as []atom, level int) {
		for _, g := range groupAtoms(as, level) {
			lines := flatten(g)
			if level > 3 || fits(lines) {
				out = append(out, lines)
				continue
			}
			rec(g, level+1)
		}
	}
	rec(atoms(src), 2)
	return out
}

// groupAtoms starts a new group at every heading of the given level or
// above (but not "#"). At level 4 and deeper every block is its own group,
// headings glued to the block that follows.
func groupAtoms(as []atom, level int) [][]atom {
	var out [][]atom
	for i := 0; i < len(as); i++ {
		a := as[i]
		start := len(out) == 0
		switch {
		case level > 3:
			start = true
			if a.level > 0 && i+1 < len(as) {
				out = append(out, []atom{a, as[i+1]})
				i++
				continue
			}
		case a.level >= 2 && a.level <= level:
			start = true
		}
		if start && !(len(out) > 0 && onlyHeadings(out[len(out)-1])) {
			out = append(out, nil)
		}
		out[len(out)-1] = append(out[len(out)-1], a)
	}
	return out
}

// onlyHeadings reports whether a group has nothing but headings, like the
// "## Section" line before its first "###": it must not end a message.
func onlyHeadings(g []atom) bool {
	for _, a := range g {
		if a.level == 0 {
			return false
		}
	}
	return true
}

func flatten(as []atom) []srcLine {
	var out []srcLine
	for i, a := range as {
		if i > 0 {
			out = append(out, srcLine{"", 0})
		}
		out = append(out, a.lines...)
	}
	return out
}

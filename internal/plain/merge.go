package plain

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Mode says how edited text was merged into a section.
type Mode int

const (
	// Matched: the text was matched to the section paragraph by paragraph.
	// Untouched paragraphs keep their source.
	Matched Mode = iota
	// Partial: the user sent only the paragraphs they changed; each one
	// replaced the paragraph it resembles and the rest was kept.
	Partial
	// Replaced: the text could not be matched reliably and replaced the
	// whole section.
	Replaced
)

// Change is one place where the user's text differs from the section,
// in plain text. Before is empty for added text, After for removed text.
type Change struct {
	Before, After string
}

// Result is a merged section.
type Result struct {
	// Source is the new Markdown of the section.
	Source    string
	Unchanged bool
	Mode      Mode
	Changes   []Change
	// Notes explain to the reviewer anything unusual about the merge.
	Notes []string
	// Shrunk is set when the text is much shorter than the section and
	// most of the section would be removed: the user probably sent only a
	// fragment and should confirm.
	Shrunk bool
}

// Thresholds of the merge heuristics.
const (
	similarCopy  = 0.6 // a second paste of the section, possibly edited
	similarBlock = 0.5 // an edited paragraph and its original
)

// Merge applies text, the user's edited version of sec.Plain(), to the
// section. body is the article body sec was taken from.
func Merge(body string, sec Section, text string) Result {
	orig := sec.Blocks
	keys := make([]string, len(orig))
	for i, b := range orig {
		keys[i] = normalize(b.Plain)
	}
	user := SplitText(text)
	old := body[sec.Start:sec.End]
	var notes []string

	// The text pasted twice: one copy untouched, the other edited or not.
	if len(user) > len(orig) && len(orig) > 0 {
		if at := findRun(user, keys); at >= 0 {
			rest := append(append([]string{}, user[:at]...), user[at+len(keys):]...)
			if similarity(strings.Join(rest, "\n"), strings.Join(keys, "\n")) >= similarCopy {
				user = rest
				notes = append(notes, "The text was pasted twice; the unchanged copy was dropped.")
			}
		}
	}
	if equal(user, keys) {
		return Result{Source: old, Unchanged: true}
	}

	// Paragraphs repeated more often than in the original make any
	// matching a guess.
	count := map[string]int{}
	for _, k := range keys {
		count[k]--
	}
	for _, u := range user {
		if _, ok := count[u]; ok {
			count[u]++
			if count[u] > 0 {
				return replaceAll(body, sec, user, append(notes, "Some paragraphs appear more often than in the original."))
			}
		}
	}

	ops := diff(keys, user)
	m := &merger{body: body, sec: sec, user: user, keys: keys}
	if r, ok := m.partial(ops, notes); ok {
		return r
	}
	r := m.build(ops)
	r.Notes = append(notes, r.Notes...)
	r.Shrunk = shrunk(orig, user, ops)
	if r.Source == old {
		r.Unchanged = true
	}
	return r
}

// Distance scores how far a merge result is from the original: 0 for
// unchanged, more for every change and for a less reliable merge. It
// helps choose between ways to read the user's text.
func (r Result) Distance() int {
	if r.Unchanged {
		return 0
	}
	d := 1 + len(r.Changes)
	for _, c := range r.Changes {
		d += strings.Count(c.Before, "\n\n") + strings.Count(c.After, "\n\n")
	}
	if r.Shrunk {
		d += 100
	}
	if r.Mode != Matched {
		d += 1000
	}
	return d
}

// SplitText splits text into normalized blocks at blank lines.
func SplitText(text string) []string {
	text = strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(text)
	var out []string
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			out = append(out, strings.Join(cur, "\n"))
			cur = nil
		}
	}
	for _, l := range strings.Split(text, "\n") {
		l = normalizeLine(l)
		if l == "" {
			flush()
			continue
		}
		cur = append(cur, l)
	}
	flush()
	return out
}

func normalize(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = normalizeLine(l); l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// normalizeLine collapses whitespace, including no-break spaces.
func normalizeLine(l string) string {
	return strings.Join(strings.FieldsFunc(l, func(r rune) bool {
		return unicode.IsSpace(r) || r == '\u00a0' || r == '\u200b' || r == '\ufeff'
	}), " ")
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// findRun returns where run occurs in s as a contiguous sequence, or -1.
func findRun(s, run []string) int {
	for i := 0; i+len(run) <= len(s); i++ {
		if equal(s[i:i+len(run)], run) {
			return i
		}
	}
	return -1
}

// op is one step of a block diff.
type op struct {
	kind byte // '=', '-', '+'
	o, u int  // indexes into the original and the user's blocks
}

// diff is a longest-common-subsequence diff of two block lists.
func diff(a, b []string) []op {
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var ops []op
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && a[i] == b[j]:
			ops = append(ops, op{'=', i, j})
			i, j = i+1, j+1
		case j < m && (i == n || lcs[i][j+1] >= lcs[i+1][j]):
			ops = append(ops, op{'+', -1, j})
			j++
		default:
			ops = append(ops, op{'-', i, -1})
			i++
		}
	}
	return ops
}

// shrunk reports whether the text is less than half the section and drops
// at least two of its blocks.
func shrunk(orig []Block, user []string, ops []op) bool {
	removed := 0
	for _, o := range ops {
		if o.kind == '-' {
			removed++
		}
	}
	added := 0
	for _, o := range ops {
		if o.kind == '+' {
			added++
		}
	}
	// Blocks moved elsewhere show up as removed and added; only net
	// removals count.
	return removed-added >= 1 && removed >= 2 && 2*runes(strings.Join(user, "\n")) < runes(plainOf(orig))
}

func plainOf(blocks []Block) string {
	var parts []string
	for _, b := range blocks {
		parts = append(parts, b.Plain)
	}
	return strings.Join(parts, "\n")
}

func runes(s string) int { return utf8.RuneCountInString(s) }

type merger struct {
	body string
	sec  Section
	user []string
	keys []string
}

// piece is a block of the merged section: its Markdown and the index of
// the original block it reproduces, or -1.
type piece struct {
	src  string
	orig int
}

// build merges hunk by hunk: equal blocks keep their source, a block the
// user moved keeps its source too, and edited blocks become the user's
// text with as much of the original formatting restored as possible.
func (m *merger) build(ops []op) Result {
	orig := m.sec.Blocks
	used := make([]bool, len(orig))
	for _, o := range ops {
		if o.kind == '=' {
			used[o.o] = true
		}
	}
	var out []piece
	var changes []Change
	moved := 0
	for i := 0; i < len(ops); {
		if ops[i].kind == '=' {
			out = append(out, piece{orig[ops[i].o].Source, ops[i].o})
			i++
			continue
		}
		var dels, ins []int
		for ; i < len(ops) && ops[i].kind != '='; i++ {
			if ops[i].kind == '-' {
				dels = append(dels, ops[i].o)
			} else {
				ins = append(ins, ops[i].u)
			}
		}
		var spans []Span
		for _, d := range dels {
			spans = append(spans, orig[d].Spans...)
		}
		var before, after []string
		for k, u := range ins {
			if j := m.unusedMatch(m.user[u], used); j >= 0 {
				// Moved from elsewhere in the section.
				used[j] = true
				moved++
				out = append(out, piece{orig[j].Source, j})
				continue
			}
			var like *Block
			if len(dels) == len(ins) {
				like = &orig[dels[k]]
			}
			out = append(out, piece{toMarkdown(m.user[u], like, spans), -1})
			after = append(after, m.user[u])
		}
		for _, d := range dels {
			if !used[d] {
				before = append(before, orig[d].Plain)
			}
		}
		if len(before) > 0 || len(after) > 0 {
			changes = append(changes, Change{Before: strings.Join(before, "\n\n"), After: strings.Join(after, "\n\n")})
		}
	}
	// Blocks counted as removed in one hunk may have been reused as moved
	// in a later one; drop them from the earlier change.
	changes = m.pruneMoved(changes, used)
	r := Result{Source: m.join(out), Mode: Matched, Changes: changes}
	if moved > 0 {
		r.Notes = append(r.Notes, fmt.Sprintf("%d paragraph(s) were moved; their formatting was kept.", moved))
	}
	return r
}

// unusedMatch finds an original block, not yet placed, whose plain text is
// exactly key.
func (m *merger) unusedMatch(key string, used []bool) int {
	for j, k := range m.keys {
		if !used[j] && k == key {
			return j
		}
	}
	return -1
}

func (m *merger) pruneMoved(changes []Change, used []bool) []Change {
	movedPlain := map[string]bool{}
	for j, b := range m.sec.Blocks {
		if used[j] {
			movedPlain[b.Plain] = true
		}
	}
	var out []Change
	for _, c := range changes {
		var keep []string
		for _, p := range strings.Split(c.Before, "\n\n") {
			if p != "" && !movedPlain[p] {
				keep = append(keep, p)
			}
		}
		c.Before = strings.Join(keep, "\n\n")
		if c.Before != "" || c.After != "" {
			out = append(out, c)
		}
	}
	return out
}

// partial handles a user who sent only the paragraphs they edited: if the
// text is short and every new paragraph clearly resembles a different
// original one, in order, only those are replaced.
func (m *merger) partial(ops []op, notes []string) (Result, bool) {
	orig := m.sec.Blocks
	if !shrunk(orig, m.user, ops) {
		return Result{}, false
	}
	equalAt := map[int]int{} // user block -> original block
	for _, o := range ops {
		if o.kind == '=' {
			equalAt[o.u] = o.o
		}
	}
	replace := map[int]int{} // original block -> user block
	next := 0
	for u := range m.user {
		if o, ok := equalAt[u]; ok {
			if o < next {
				return Result{}, false
			}
			next = o + 1
			continue
		}
		best, bestSim := -1, 0.0
		for o := next; o < len(orig); o++ {
			if s := similarity(m.user[u], m.keys[o]); s > bestSim {
				best, bestSim = o, s
			}
		}
		if best < 0 || bestSim < similarBlock {
			return Result{}, false
		}
		replace[best] = u
		next = best + 1
	}
	if len(replace) == 0 {
		return Result{}, false
	}
	var out []piece
	var changes []Change
	for o, b := range orig {
		u, ok := replace[o]
		if !ok {
			out = append(out, piece{b.Source, o})
			continue
		}
		out = append(out, piece{toMarkdown(m.user[u], &orig[o], b.Spans), -1})
		changes = append(changes, Change{Before: b.Plain, After: m.user[u]})
	}
	return Result{
		Source: m.join(out), Mode: Partial, Changes: changes,
		Notes: append(notes, "The user sent only the paragraphs they changed; each replaced the most similar paragraph and the rest was kept."),
	}, true
}

// replaceAll is the fallback: the section becomes the user's text. Blocks
// that are exactly an original block's text still get its source, once.
func replaceAll(body string, sec Section, user []string, notes []string) Result {
	orig := sec.Blocks
	keys := make([]string, len(orig))
	var spans []Span
	for i, b := range orig {
		keys[i] = normalize(b.Plain)
		spans = append(spans, b.Spans...)
	}
	m := &merger{body: body, sec: sec, user: user, keys: keys}
	used := make([]bool, len(orig))
	var out []piece
	for _, u := range user {
		if j := m.unusedMatch(u, used); j >= 0 {
			used[j] = true
			out = append(out, piece{orig[j].Source, j})
			continue
		}
		out = append(out, piece{toMarkdown(u, nil, spans), -1})
	}
	src := m.join(out)
	return Result{
		Source: src, Mode: Replaced, Unchanged: src == body[sec.Start:sec.End],
		Changes: []Change{{Before: plainOf(orig), After: strings.Join(user, "\n\n")}},
		Notes:   append(notes, "The text could not be matched to the original paragraphs, so it replaced the whole section. Check it as a full rewrite."),
	}
}

// join assembles the pieces. Consecutive original blocks keep the exact
// separator they had; anything else is separated by one blank line.
func (m *merger) join(out []piece) string {
	var b strings.Builder
	orig := m.sec.Blocks
	for i, p := range out {
		if i > 0 {
			prev := out[i-1].orig
			if prev >= 0 && p.orig == prev+1 {
				b.WriteString(m.body[orig[prev].End:orig[p.orig].Start])
			} else {
				b.WriteString("\n\n")
			}
		}
		b.WriteString(p.src)
	}
	return b.String()
}

// toMarkdown turns a block of the user's plain text into Markdown. like is
// the original block it replaces, when known, whose kind is carried over;
// spans are formatted pieces of the original that are put back where the
// user's text still contains them.
func toMarkdown(text string, like *Block, spans []Span) string {
	lines := strings.Split(text, "\n")
	if like != nil && like.Kind == Heading && len(lines) == 1 {
		return strings.Repeat("#", like.Level) + " " + strings.TrimLeft(restore(lines[0], spans), "# ")
	}
	if like != nil && like.Kind == Table {
		if t, ok := toTable(lines, like, spans); ok {
			return t
		}
	}
	// Rows and items the user left alone keep their source.
	var known []Span
	if like != nil && like.Kind == List {
		known = like.Lines
	}
	used := make([]bool, len(known))
	for i, l := range lines {
		if j := lineMatch(l, known, used); j >= 0 {
			lines[i] = known[j].Source
			continue
		}
		l = restore(l, spans)
		switch {
		case strings.HasPrefix(l, "• "):
			l = "- " + l[len("• "):]
		case strings.HasPrefix(l, "☐ "):
			l = "- [ ] " + l[len("☐ "):]
		case strings.HasPrefix(l, "☑ "):
			l = "- [x] " + l[len("☑ "):]
		}
		if like != nil && like.Kind == Quote {
			l = "> " + l
		}
		lines[i] = l
	}
	return strings.Join(lines, "\n")
}

// lineMatch finds an unused line of the original block whose plain text is
// exactly l, and marks it used.
func lineMatch(l string, known []Span, used []bool) int {
	for j, k := range known {
		if !used[j] && normalizeLine(k.Plain) == l {
			used[j] = true
			return j
		}
	}
	return -1
}

// toTable rebuilds a table from "a | b" lines when they have as many
// columns as the original table. Unchanged rows keep their source.
func toTable(lines []string, like *Block, spans []Span) (string, bool) {
	src := strings.Split(like.Source, "\n")
	cols := len(splitRow(strings.TrimSpace(src[0])))
	rule := ""
	if len(src) > 1 && tableRule.MatchString(strings.TrimSpace(src[1])) {
		rule = src[1]
	}
	used := make([]bool, len(like.Lines))
	var out []string
	for i, l := range lines {
		if j := lineMatch(l, like.Lines, used); j >= 0 {
			out = append(out, like.Lines[j].Source)
		} else {
			cells := strings.Split(restore(l, spans), "|")
			if cols < 2 || len(cells) != cols {
				return "", false
			}
			for k := range cells {
				cells[k] = strings.TrimSpace(cells[k])
			}
			out = append(out, "| "+strings.Join(cells, " | ")+" |")
		}
		if i == 0 && rule != "" {
			out = append(out, rule)
		}
	}
	return strings.Join(out, "\n"), true
}

// restore puts formatted spans back: each plain rendering found in text
// (as a whole word) becomes its source again, at most as often as it
// occurred in the original. Longer spans go first. Dollar signs elsewhere
// are escaped.
func restore(text string, spans []Span) string {
	type cand struct {
		plain, source string
		n             int
	}
	idx := map[[2]string]*cand{}
	var cands []*cand
	for _, s := range spans {
		if s.Plain == s.Source || runes(strings.TrimSpace(s.Plain)) < 2 || strings.Contains(s.Plain, "\n") {
			continue
		}
		k := [2]string{normalizeLine(s.Plain), s.Source}
		if c := idx[k]; c != nil {
			c.n++
			continue
		}
		c := &cand{k[0], s.Source, 1}
		idx[k] = c
		cands = append(cands, c)
	}
	sort.SliceStable(cands, func(i, j int) bool { return len(cands[i].plain) > len(cands[j].plain) })
	var sources []string
	for _, c := range cands {
		for ; c.n > 0; c.n-- {
			at := wordIndex(text, c.plain)
			if at < 0 {
				break
			}
			// A private-use character stands in for the restored span so
			// shorter spans cannot match inside it.
			mark := string(rune(0xE000 + len(sources)))
			sources = append(sources, c.source)
			text = text[:at] + mark + text[at+len(c.plain):]
		}
	}
	// The user's text is plain: a dollar sign is a dollar, not a formula.
	text = strings.ReplaceAll(text, "$", `\$`)
	for i, s := range sources {
		text = strings.Replace(text, string(rune(0xE000+i)), s, 1)
	}
	return text
}

// wordIndex finds needle in s where it is not part of a longer word.
func wordIndex(s, needle string) int {
	for off := 0; off <= len(s); {
		i := strings.Index(s[off:], needle)
		if i < 0 {
			return -1
		}
		at := off + i
		first, _ := utf8.DecodeRuneInString(needle)
		last, _ := utf8.DecodeLastRuneInString(needle)
		before, _ := utf8.DecodeLastRuneInString(s[:at])
		after, _ := utf8.DecodeRuneInString(s[at+len(needle):])
		okBefore := at == 0 || !isWord(first) || !isWord(before)
		okAfter := at+len(needle) == len(s) || !isWord(last) || !isWord(after)
		if okBefore && okAfter {
			return at
		}
		_, n := utf8.DecodeRuneInString(s[at:])
		off = at + n
	}
	return -1
}

func isWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// similarity compares two texts word by word: 1 for the same words in the
// same order, 0 for nothing in common.
func similarity(a, b string) float64 {
	wa, wb := words(a), words(b)
	if len(wa) == 0 || len(wb) == 0 {
		return 0
	}
	prev := make([]int, len(wb)+1)
	cur := make([]int, len(wb)+1)
	for i := range wa {
		for j := range wb {
			if wa[i] == wb[j] {
				cur[j+1] = prev[j] + 1
			} else {
				cur[j+1] = max(prev[j+1], cur[j])
			}
		}
		prev, cur = cur, prev
	}
	return 2 * float64(prev[len(wb)]) / float64(len(wa)+len(wb))
}

func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !isWord(r) })
}

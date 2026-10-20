package search

import (
	"maps"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Mimic890/terakoya/internal/content"
)

// Query is a parsed search query.
type Query struct {
	// Text is the query without its filters, as typed.
	Text string
	// Words are its folded words, without stop words.
	Words []string
	// Grade, if not 0, and Kinds, if any, narrow the results: «10 класс»,
	// «теорема».
	Grade int
	Kinds []string
	// parts are the words of the query as typed, each with its role.
	parts []part
}

// Roles of the words of a query.
const (
	roleWord = iota
	roleGrade
	roleKind
)

type part struct {
	typed string
	word  string // folded
	role  int
}

// rebuild is the query with the words of a role left out and, for
// suggestions, words replaced: index i of the parts by with[i].
func (q Query) rebuild(drop int, with map[int]string) string {
	var out []string
	for i, p := range q.parts {
		switch {
		case p.role == drop:
		case with[i] != "":
			out = append(out, with[i])
		default:
			out = append(out, p.typed)
		}
	}
	return strings.Join(out, " ")
}

// kindWords are the words that narrow a search to a kind of note, by
// their stems, and the kind folders they stand for.
var kindWords = func() map[string]string {
	m := map[string]string{}
	for w, kind := range map[string]string{"теорема": "теоремы", "задача": "задачи", "аксиома": "аксиомы", "следствие": "следствия"} {
		m[stem(w)] = kind
	}
	return m
}()

var (
	gradeWord   = regexp.MustCompile(`^(?:кл|класс\p{L}*)$`)
	gradeJoined = regexp.MustCompile(`^(\d{1,2})(?:кл|класс\p{L}*)$`)
	ordinalEnd  = regexp.MustCompile(`^(?:й|ый|ой|ий|го|м)$`)
)

// Parse splits a query into its words and filters.
func Parse(raw string) Query {
	var q Query
	const (
		word  = roleWord
		grade = roleGrade
		kind  = roleKind
	)
	toks := tokens(raw)
	role := make([]int, len(toks))
	for i, tk := range toks {
		if role[i] != word {
			continue
		}
		if m := gradeJoined.FindStringSubmatch(tk.word); m != nil {
			q.Grade, _ = strconv.Atoi(m[1])
			role[i] = grade
			continue
		}
		if n, err := strconv.Atoi(tk.word); err == nil && n > 0 && n <= 12 {
			// «10 класс», «10 кл», «10-й класс».
			j := i + 1
			if j < len(toks) && ordinalEnd.MatchString(toks[j].word) {
				j++
			}
			if j < len(toks) && gradeWord.MatchString(toks[j].word) {
				q.Grade = n
				for k := i; k <= j; k++ {
					role[k] = grade
				}
				continue
			}
		}
		if folder, ok := kindWords[stem(tk.word)]; ok {
			if !contains(q.Kinds, folder) {
				q.Kinds = append(q.Kinds, folder)
			}
			role[i] = kind
		}
	}
	for i, tk := range toks {
		q.parts = append(q.parts, part{typed: raw[tk.start:tk.end], word: tk.word, role: role[i]})
		if role[i] == word && indexed(tk.word) {
			q.Words = append(q.Words, tk.word)
		}
	}
	q.Text = q.rebuild(-1, nil)
	if q.Grade != 0 || len(q.Kinds) > 0 {
		var text []string
		for _, p := range q.parts {
			if p.role == word {
				text = append(text, p.typed)
			}
		}
		q.Text = strings.Join(text, " ")
	}
	return q
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Normalize is the form a query is counted under in the statistics:
// folded, its words separated by single spaces.
func Normalize(raw string) string {
	var words []string
	for _, tk := range tokens(raw) {
		words = append(words, tk.word)
	}
	return strings.Join(words, " ")
}

// Hit is a note found.
type Hit struct {
	Note  *content.Note
	Score float64
	doc   *doc
}

// Result is the outcome of a search: every note found, best first.
type Result struct {
	Query Query
	Hits  []Hit
	// stems are those the query matched, for snippets.
	stems map[string]bool
}

// fewHits: with fewer results than this, words are also matched with
// typos.
const fewHits = 3

// Search finds the notes that match a query. Words match by their stems;
// the last also as the start of a word («параллел»). Words that match
// nothing, or all of them when there are fewer than fewHits results, also
// match words with a typo or two.
func (ix *Index) Search(raw string) Result {
	q := Parse(raw)
	r := ix.run(q, false)
	if len(r.Hits) < fewHits && len(q.Words) > 0 {
		if f := ix.run(q, true); len(f.Hits) > len(r.Hits) {
			r = f
		}
	}
	return r
}

// alt is a stem a query word matches, with how much a match counts.
type alt struct {
	stem   string
	weight float64
}

// Match weights: a match by the stem counts fully, the start of a word
// and a typo less.
const (
	prefixWeight = 0.8
	typoWeight   = 0.6
	// Boosts: all words present, the words as a phrase in the title or
	// elsewhere, and the main page of a topic.
	allWordsBoost    = 1.5
	titlePhraseBoost = 2
	phraseBoost      = 1.3
	mainBoost        = 1.05
	// maxPrefixed bounds the words a prefix may stand for.
	maxPrefixed = 50
)

func (ix *Index) expand(q Query, fuzzy bool) [][]alt {
	out := make([][]alt, len(q.Words))
	for i, w := range q.Words {
		seen := map[string]bool{}
		add := func(s string, weight float64) {
			if !seen[s] && ix.terms[s] != nil {
				seen[s] = true
				out[i] = append(out[i], alt{s, weight})
			}
		}
		add(stem(w), 1)
		if i == len(q.Words)-1 && utf8.RuneCountInString(w) >= 3 {
			for _, v := range ix.prefixed(w, maxPrefixed) {
				add(ix.stems[v], prefixWeight)
			}
		}
		if fuzzy || len(out[i]) == 0 {
			for _, v := range ix.similar(w) {
				add(ix.stems[v], typoWeight)
			}
		}
	}
	return out
}

func (ix *Index) run(q Query, fuzzy bool) Result {
	r := Result{Query: q, stems: map[string]bool{}}
	if len(q.Words) == 0 {
		if q.Grade == 0 && len(q.Kinds) == 0 {
			return r
		}
		// Filters only: everything they let through, in catalog order.
		for _, d := range ix.docs {
			if q.allows(d) {
				r.Hits = append(r.Hits, Hit{Note: d.note, doc: d})
			}
		}
		return r
	}
	alts := ix.expand(q, fuzzy)
	type acc struct {
		score float64
		words int
	}
	found := map[int32]*acc{}
	for _, word := range alts {
		// A word counts by how rare it is with all its forms: the start
		// of a word stands for every word it starts, not the rarest one.
		best := map[int32]float64{}
		for _, a := range word {
			r.stems[a.stem] = true
			for _, p := range ix.terms[a.stem] {
				if s := a.weight * ix.score(p); s > best[p.doc] {
					best[p.doc] = s
				}
			}
		}
		idf := ix.idf(len(best))
		for id, s := range best {
			s *= idf
			a := found[id]
			if a == nil {
				a = &acc{}
				found[id] = a
			}
			a.score += s
			a.words++
		}
	}
	n := len(q.Words)
	for id, a := range found {
		d := ix.docs[id]
		if !q.allows(d) {
			continue
		}
		s := a.score
		if n > 1 && a.words == n {
			s *= allWordsBoost
		}
		if n > 1 {
			switch {
			case phrase(d.seq[fTitle], alts):
				s *= titlePhraseBoost
			case phrase(d.seq[fHeadings], alts) || phrase(d.seq[fBody], alts) || phrase(d.seq[fTopic], alts):
				s *= phraseBoost
			}
		}
		if d.note.IsMain() {
			s *= mainBoost
		}
		r.Hits = append(r.Hits, Hit{Note: d.note, Score: s, doc: d})
	}
	sort.Slice(r.Hits, func(i, j int) bool {
		a, b := r.Hits[i], r.Hits[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		return a.doc.order < b.doc.order
	})
	return r
}

// phrase reports whether the words of a query come one after another in
// a field.
func phrase(seq []string, alts [][]alt) bool {
	for _, word := range alts {
		if len(word) == 0 {
			return false
		}
	}
	matches := func(s string, word []alt) bool {
		for _, a := range word {
			if a.stem == s {
				return true
			}
		}
		return false
	}
next:
	for i := 0; i+len(alts) <= len(seq); i++ {
		for k, word := range alts {
			if !matches(seq[i+k], word) {
				continue next
			}
		}
		return true
	}
	return false
}

// allows reports whether a note passes the query's filters. A kind
// filter lets through notes of that kind and notes whose title has the
// word, such as a main page «Теорема о трёх перпендикулярах».
func (q Query) allows(d *doc) bool {
	if q.Grade != 0 && d.note.Grade().Number != q.Grade {
		return false
	}
	if len(q.Kinds) == 0 {
		return true
	}
	for _, k := range q.Kinds {
		if d.note.Kind == k {
			return true
		}
		for _, s := range d.seq[fTitle] {
			if kindWords[s] == k {
				return true
			}
		}
	}
	return false
}

// Piece is a part of a snippet; Bold ones are the words found.
type Piece struct {
	Text string
	Bold bool
}

// Snippet is about width letters of a note's text around the words
// found, or its start. It is nil for a note without text.
func (r Result) Snippet(h Hit, width int) []Piece {
	d := h.doc
	if d == nil || d.body == "" {
		return nil
	}
	// Letter offsets of the words.
	type word struct {
		stemToken
		at, to int // in letters
		found  bool
	}
	words := make([]word, len(d.toks))
	pos, runes := 0, 0
	for i, t := range d.toks {
		runes += utf8.RuneCountInString(d.body[pos:t.start])
		at := runes
		runes += utf8.RuneCountInString(d.body[t.start:t.end])
		pos = t.end
		words[i] = word{t, at, runes, r.stems[t.stem]}
	}
	// The window with the most different words found; it starts a little
	// before the first of them.
	const lead = 20
	first, most := -1, 0
	for i, w := range words {
		if !w.found {
			continue
		}
		seen := map[string]bool{}
		for _, v := range words[i:] {
			if v.to-w.at > width-lead {
				break
			}
			if v.found {
				seen[v.stem] = true
			}
		}
		if len(seen) > most {
			first, most = i, len(seen)
		}
	}
	start := 0 // byte offset
	if first >= 0 {
		for _, w := range words[:first+1] {
			if w.at >= words[first].at-lead {
				start = w.start
				break
			}
		}
	}
	end := len(d.body)
	if count := utf8.RuneCountInString(d.body[start:]); count > width {
		// Cut at the end of the last whole word that fits.
		limit := byteOffset(d.body[start:], width) + start
		end = start
		for _, w := range words {
			if w.start >= start && w.end <= limit {
				end = w.end
			}
		}
		if end == start {
			end = limit
		}
	}
	var out []Piece
	if start > 0 {
		out = append(out, Piece{Text: "…"})
	}
	at := start
	for _, w := range words {
		if !w.found || w.start < start || w.end > end {
			continue
		}
		gap := d.body[at:w.start]
		if last := len(out) - 1; last >= 0 && out[last].Bold && strings.TrimSpace(gap) == "" {
			// Words found one after another are one bold piece.
			out[last].Text += d.body[at:w.end]
			at = w.end
			continue
		}
		if gap != "" {
			out = append(out, Piece{Text: gap})
		}
		out = append(out, Piece{Text: d.body[w.start:w.end], Bold: true})
		at = w.end
	}
	if at < end {
		out = append(out, Piece{Text: d.body[at:end]})
	}
	if end < len(d.body) {
		out = append(out, Piece{Text: "…"})
	}
	return out
}

// byteOffset is the byte offset of the n-th letter of s.
func byteOffset(s string, n int) int {
	for i := range s {
		if n == 0 {
			return i
		}
		n--
	}
	return len(s)
}

// Suggest offers other queries for one that found nothing: its words
// replaced by the closest words of the index, or the query without one of
// its filters. Only queries that find something are offered, at most n.
func (ix *Index) Suggest(raw string, n int) []string {
	q := Parse(raw)
	var out []string
	add := func(s string) {
		if len(out) < n && s != "" && Normalize(s) != Normalize(raw) && !contains(out, s) && len(ix.Search(s).Hits) > 0 {
			out = append(out, s)
		}
	}
	// The closest words for each word the index does not know; the
	// others stay as typed.
	best := map[int]string{}
	options := map[int][]string{}
	for i, p := range q.parts {
		if p.role != roleWord || !indexed(p.word) || ix.terms[stem(p.word)] != nil {
			continue
		}
		for _, c := range ix.near(p.word, max(2, typos(utf8.RuneCountInString(p.word))+1)) {
			if len(options[i]) == n {
				break
			}
			options[i] = append(options[i], c.word)
		}
		if len(options[i]) > 0 {
			best[i] = options[i][0]
		}
	}
	for i := range q.parts {
		for _, o := range options[i] {
			with := maps.Clone(best)
			with[i] = o
			add(q.rebuild(-1, with))
		}
	}
	add(q.rebuild(roleGrade, best))
	add(q.rebuild(roleKind, best))
	return out
}

// Package search finds notes of the vault by their words. The index is
// built in memory each time the vault is loaded: titles, headings, the
// topic, the text without Markdown and math read out as words, all
// folded (lowercase, ё as е), stemmed with Snowball and without stop
// words. Notes are ranked BM25-style with a weight per field.
package search

import (
	"maps"
	"math"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/Mimic890/terakoya/internal/content"
)

// Fields of a note, each with its weight in the ranking: the title
// counts far more than headings, and they more than the text.
const (
	fTitle = iota
	fHeadings
	fTopic
	fBody
	fMeta // the subject and the kind
	nFields
)

var fieldWeight = [nFields]float64{fTitle: 10, fHeadings: 3, fTopic: 2, fBody: 1, fMeta: 0.5}

// BM25 parameters.
const (
	k1   = 1.2
	bLen = 0.75
)

// Index is the search index of a catalog. It is read-only once built.
type Index struct {
	docs  []*doc
	terms map[string][]posting // stem → the notes it is in
	// words are the folded words of the index, sorted, for prefixes and
	// typos; stems and freq map each to its stem and how often it occurs.
	words  []string
	stems  map[string]string
	freq   map[string]int
	grams  map[string][]int32 // trigram → indexes into words
	avgLen [nFields]float64
}

type doc struct {
	note  *content.Note
	order int // in the catalog
	// seq are the stems of each field in order, for phrases.
	seq [nFields][]string
	// body is the text for snippets, toks its words with their stems.
	body string
	toks []stemToken
}

type stemToken struct {
	start, end int // bytes in body
	stem       string
}

type posting struct {
	doc int32
	tf  [nFields]uint16
}

// Build indexes every note of the catalog. Notes are read in parallel,
// then put together in catalog order.
func Build(cat *content.Catalog) *Index {
	ix := &Index{terms: map[string][]posting{}, stems: map[string]string{}, freq: map[string]int{}, grams: map[string][]int32{}}
	notes := cat.Notes()
	read := make([]readDoc, len(notes))
	workers := min(runtime.GOMAXPROCS(0), max(1, len(notes)/50))
	readers := make([]*reader, workers)
	var wg sync.WaitGroup
	for w := range workers {
		r := &reader{stems: map[string]string{}, freq: map[string]int{}}
		readers[w] = r
		wg.Go(func() {
			for i := w; i < len(notes); i += workers {
				read[i] = r.read(notes[i], i)
			}
		})
	}
	wg.Wait()
	for _, r := range readers {
		maps.Copy(ix.stems, r.stems)
		for w, n := range r.freq {
			ix.freq[w] += n
		}
	}
	var total [nFields]int
	for i, rd := range read {
		for _, s := range rd.order {
			ix.terms[s] = append(ix.terms[s], posting{doc: int32(i), tf: *rd.tf[s]})
		}
		for f := range total {
			total[f] += len(rd.doc.seq[f])
		}
		ix.docs = append(ix.docs, rd.doc)
	}
	for f := range total {
		ix.avgLen[f] = max(1, float64(total[f])/float64(max(1, len(ix.docs))))
	}
	for w := range ix.freq {
		ix.words = append(ix.words, w)
	}
	sort.Strings(ix.words)
	for i, w := range ix.words {
		for _, g := range trigrams(w) {
			ix.grams[g] = append(ix.grams[g], int32(i))
		}
	}
	return ix
}

// reader reads notes for the index, with its own stems and counts.
type reader struct {
	stems map[string]string
	freq  map[string]int
}

// readDoc is a note read: the doc and how often each stem is in each
// field, the stems in the order met.
type readDoc struct {
	doc   *doc
	tf    map[string]*[nFields]uint16
	order []string
}

func (r *reader) stem(w string) string {
	s, ok := r.stems[w]
	if !ok {
		s = stem(w)
		r.stems[w] = s
	}
	return s
}

func (r *reader) read(n *content.Note, order int) readDoc {
	d := &doc{note: n, order: order}
	src, _ := content.SplitBacklink(n.Body)
	t := readNote(src, n.TitleLine)
	d.body = t.body
	fields := [nFields][]string{
		fTitle:    {n.Title},
		fHeadings: t.headings,
		fTopic:    {n.Topic.Title()},
		fBody:     {t.math}, // and the text, below
		fMeta:     {n.Subject().Name, n.KindLabel()},
	}
	rd := readDoc{doc: d, tf: map[string]*[nFields]uint16{}}
	text := tokens(d.body)
	d.toks = make([]stemToken, 0, len(text))
	d.seq[fBody] = make([]string, 0, len(text))
	count := func(f int, word, s string) {
		r.freq[word]++
		d.seq[f] = append(d.seq[f], s)
		c := rd.tf[s]
		if c == nil {
			c = new([nFields]uint16)
			rd.tf[s] = c
			rd.order = append(rd.order, s)
		}
		if c[f] < math.MaxUint16 {
			c[f]++
		}
	}
	// The words of the text are kept with their places, for snippets.
	for _, tk := range text {
		if indexed(tk.word) {
			s := r.stem(tk.word)
			d.toks = append(d.toks, stemToken{tk.start, tk.end, s})
			count(fBody, tk.word, s)
		}
	}
	for f, texts := range fields {
		for _, text := range texts {
			for _, tk := range tokens(text) {
				if indexed(tk.word) {
					count(f, tk.word, r.stem(tk.word))
				}
			}
		}
	}
	return rd
}

// Len is the number of notes indexed.
func (ix *Index) Len() int { return len(ix.docs) }

// idf is the inverse document frequency of a word found in df notes.
func (ix *Index) idf(df int) float64 {
	n := float64(len(ix.docs))
	return math.Log(1 + (n-float64(df)+0.5)/(float64(df)+0.5))
}

// score is the BM25 score of a posting, summed over the fields with
// their weights.
func (ix *Index) score(p posting) float64 {
	d := ix.docs[p.doc]
	var s float64
	for f := range nFields {
		if p.tf[f] == 0 {
			continue
		}
		tf := float64(p.tf[f])
		norm := 1 - bLen + bLen*float64(len(d.seq[f]))/ix.avgLen[f]
		s += fieldWeight[f] * tf * (k1 + 1) / (tf + k1*norm)
	}
	return s
}

// prefixed lists the words of the index that start with w, at most limit.
func (ix *Index) prefixed(w string, limit int) []string {
	i := sort.SearchStrings(ix.words, w)
	var out []string
	for ; i < len(ix.words) && strings.HasPrefix(ix.words[i], w) && len(out) < limit; i++ {
		out = append(out, ix.words[i])
	}
	return out
}

// typos is how many edits a word of n letters may have to still match:
// none below 4 letters, one up to 7 and two from 8.
func typos(n int) int {
	switch {
	case n < 4:
		return 0
	case n < 8:
		return 1
	}
	return 2
}

// candidate is a word of the index close to a word of a query.
type candidate struct {
	word string
	dist int
	sim  float64 // of the trigrams
}

// near lists the words of the index that share a trigram with w and are
// at most maxDist edits away, best first.
func (ix *Index) near(w string, maxDist int) []candidate {
	seen := map[int32]bool{}
	grams := trigrams(w)
	n := utf8.RuneCountInString(w)
	var out []candidate
	for _, g := range grams {
		for _, i := range ix.grams[g] {
			if seen[i] {
				continue
			}
			seen[i] = true
			v := ix.words[i]
			if v == w || abs(utf8.RuneCountInString(v)-n) > maxDist {
				continue
			}
			d := distance(w, v)
			if d > maxDist {
				continue
			}
			out = append(out, candidate{v, d, similarity(grams, trigrams(v))})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.dist != b.dist {
			return a.dist < b.dist
		}
		if a.sim != b.sim {
			return a.sim > b.sim
		}
		if ix.freq[a.word] != ix.freq[b.word] {
			return ix.freq[a.word] > ix.freq[b.word]
		}
		return a.word < b.word
	})
	return out
}

// similar lists the words of the index within the typos allowed for w.
func (ix *Index) similar(w string) []string {
	k := typos(utf8.RuneCountInString(w))
	if k == 0 {
		return nil
	}
	var out []string
	for _, c := range ix.near(w, k) {
		if len(out) == 20 {
			break
		}
		out = append(out, c.word)
	}
	return out
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// trigrams are the three-letter pieces of a word padded with spaces.
func trigrams(w string) []string {
	r := []rune(" " + w + " ")
	var out []string
	for i := 0; i+3 <= len(r); i++ {
		out = append(out, string(r[i:i+3]))
	}
	return out
}

// similarity is the share of trigrams two words have in common.
func similarity(a, b []string) float64 {
	set := map[string]bool{}
	for _, g := range a {
		set[g] = true
	}
	common := 0
	for _, g := range b {
		if set[g] {
			common++
			delete(set, g)
		}
	}
	return float64(common) / float64(len(a)+len(b)-common)
}

// distance is the Damerau-Levenshtein distance (optimal string alignment)
// between two words, in letters.
func distance(a, b string) int {
	s, t := []rune(a), []rune(b)
	prev2 := make([]int, len(t)+1)
	prev := make([]int, len(t)+1)
	cur := make([]int, len(t)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(s); i++ {
		cur[0] = i
		for j := 1; j <= len(t); j++ {
			cost := 1
			if s[i-1] == t[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			if i > 1 && j > 1 && s[i-1] == t[j-2] && s[i-2] == t[j-1] {
				cur[j] = min(cur[j], prev2[j-2]+1)
			}
		}
		prev2, prev, cur = prev, cur, prev2
	}
	return prev[len(t)]
}

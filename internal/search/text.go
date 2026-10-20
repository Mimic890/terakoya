package search

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kljensen/snowball/english"
	"github.com/kljensen/snowball/russian"
)

// fold lowercases text and turns ё into е, as both the index and queries
// are compared.
func fold(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'А' && r <= 'Я':
			r += 'a' - 'A' // the same offset in both alphabets
		case r == 'ё' || r == 'Ё':
			r = 'е'
		case r >= utf8.RuneSelf && !(r >= 'а' && r <= 'я'):
			r = unicode.ToLower(r)
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// token is a word of a text: folded, with its byte offsets in the text.
type token struct {
	word       string
	start, end int
}

// tokens splits text into words: runs of letters and digits.
func tokens(text string) []token {
	// Folding the whole text at once is faster; it keeps the offsets
	// unless a letter changes its length, which Cyrillic ones do not.
	folded := fold(text)
	same := len(folded) == len(text)
	var out []token
	start := -1
	add := func(end int) {
		w := folded[start:end]
		if !same {
			w = fold(text[start:end])
		}
		out = append(out, token{w, start, end})
	}
	for i, r := range text {
		word := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r >= 'А' && r <= 'я' || r == 'ё' || r == 'Ё' ||
			r >= utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r))
		switch {
		case word && start < 0:
			start = i
		case !word && start >= 0:
			add(i)
			start = -1
		}
	}
	if start >= 0 {
		add(len(text))
	}
	return out
}

// stem reduces a folded word to its Snowball stem: Russian for Cyrillic
// words, English for Latin ones; anything else, such as a number, stays.
func stem(w string) string {
	switch script(w) {
	case cyrillic:
		return strings.ReplaceAll(russian.Stem(w, true), "ё", "е")
	case latin:
		return english.Stem(w, true)
	}
	return w
}

const (
	other = iota
	cyrillic
	latin
)

func script(w string) int {
	for _, r := range w {
		switch {
		case unicode.Is(unicode.Cyrillic, r):
			return cyrillic
		case r < unicode.MaxASCII && unicode.IsLetter(r):
			return latin
		}
	}
	return other
}

// indexed reports whether a word counts for search: not a stop word, and
// not a lone letter such as the a of $a \parallel b$.
func indexed(w string) bool {
	if stopWords[w] {
		return false
	}
	if utf8.RuneCountInString(w) == 1 {
		r, _ := utf8.DecodeRuneInString(w)
		return unicode.IsDigit(r)
	}
	return true
}

// stopWords are left out of the index and of queries. Numerals and
// prepositions that mean something in geometry, such as «две», «через»
// and «между», are not stop words here.
var stopWords = setOf(`и в во не что он на я с со как а то все она так его но да ты к у же вы за бы по
	только ее мне было вот от меня еще нет о из ему теперь когда даже ну вдруг ли если уже или ни быть был
	него до вас нибудь опять уж вам ведь там потом себя ничего ей может они тут где есть надо ней для мы
	тебя их чем была сам чтоб будто чего раз тоже себе будет ж тогда кто этот того потому этого какой
	совсем ним здесь этом почти мой тем чтобы нее сейчас были куда зачем всех никогда можно наконец об хоть
	тот эти нас про всего них какая много разве эту моя впрочем хорошо свою этой иногда лучше чуть том
	нельзя такой им всегда конечно всю это эта какие который которая которые которого котором ко
	the a an and or of to in on at by for with is are be as it this that from`)

func setOf(words string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(words) {
		m[w] = true
	}
	return m
}

// latexCommands are what math reads as: a symbol for snippets and words
// for the index, so «параллельны» finds $a \parallel b$.
var latexCommands = map[string][2]string{
	"parallel": {"∥", "параллельна"}, "nparallel": {"∦", "не параллельна"},
	"perp": {"⊥", "перпендикулярна"}, "angle": {"∠", "угол"}, "measuredangle": {"∠", "угол"},
	"triangle": {"△", "треугольник"}, "square": {"□", "квадрат"}, "odot": {"⊙", "окружность"},
	"cap": {"∩", "пересечение"}, "cup": {"∪", "объединение"},
	"in": {"∈", "принадлежит"}, "notin": {"∉", "не принадлежит"}, "ni": {"∋", "содержит"},
	"subset": {"⊂", "лежит"}, "subseteq": {"⊆", "лежит"}, "supset": {"⊃", "содержит"},
	"Rightarrow": {"⇒", "следует"}, "Longrightarrow": {"⇒", "следует"}, "implies": {"⇒", "следует"},
	"Leftrightarrow": {"⇔", "равносильно"}, "iff": {"⇔", "равносильно"},
	"rightarrow": {"→", ""}, "to": {"→", ""}, "leftarrow": {"←", ""},
	"sim": {"∼", "подобен"}, "cong": {"≅", "равен"}, "equiv": {"≡", "равен"}, "approx": {"≈", "приблизительно"},
	"ne": {"≠", "не равно"}, "neq": {"≠", "не равно"}, "le": {"≤", ""}, "leq": {"≤", ""}, "ge": {"≥", ""}, "geq": {"≥", ""},
	"cdot": {"·", ""}, "times": {"×", ""}, "div": {"÷", ""}, "pm": {"±", ""}, "mp": {"∓", ""},
	"infty": {"∞", "бесконечность"}, "circ": {"°", "градус"}, "degree": {"°", "градус"},
	"sqrt": {"√", "корень"}, "vec": {"", "вектор"}, "overrightarrow": {"", "вектор"},
	"exists": {"∃", "существует"}, "forall": {"∀", "любой"}, "emptyset": {"∅", "пусто"}, "varnothing": {"∅", "пусто"},
	"alpha": {"α", "альфа"}, "beta": {"β", "бета"}, "gamma": {"γ", "гамма"}, "Gamma": {"Γ", "гамма"},
	"delta": {"δ", "дельта"}, "Delta": {"Δ", "дельта"}, "epsilon": {"ε", "эпсилон"}, "varepsilon": {"ε", "эпсилон"},
	"theta": {"θ", "тета"}, "lambda": {"λ", "лямбда"}, "mu": {"μ", "мю"}, "pi": {"π", "пи"}, "rho": {"ρ", "ро"},
	"sigma": {"σ", "сигма"}, "Sigma": {"Σ", "сумма"}, "tau": {"τ", "тау"}, "phi": {"φ", "фи"}, "varphi": {"φ", "фи"},
	"omega": {"ω", "омега"}, "Omega": {"Ω", "омега"},
	"sin": {"sin", "синус"}, "cos": {"cos", "косинус"}, "tg": {"tg", "тангенс"}, "tan": {"tan", "тангенс"},
	"ctg": {"ctg", "котангенс"}, "cot": {"cot", "котангенс"}, "log": {"log", "логарифм"}, "ln": {"ln", "логарифм"},
	"lg":   {"lg", "логарифм"},
	"dots": {"…", ""}, "ldots": {"…", ""}, "cdots": {"…", ""},
}

var (
	latexNot  = regexp.MustCompile(`\\not\s*\\(subset|in|parallel)\b`)
	latexFrac = regexp.MustCompile(`\\[dt]?frac\s*\{([^{}]*)\}\s*\{([^{}]*)\}`)
	latexEnv  = regexp.MustCompile(`\\begin\s*\{[^{}]*\}(?:\s*\{[^{}]*\})?|\\end\s*\{[^{}]*\}`)
	latexText = regexp.MustCompile(`\\(?:text|mathrm|textrm|mathit|textbf|mathbf|operatorname)\s*\{([^{}]*)\}`)
)

// readMath turns LaTeX into text: symbols, or with words set, the words
// of the index. Unknown commands go.
func readMath(src string, words bool) string {
	if !strings.Contains(src, `\`) {
		return strings.Join(strings.Fields(mathMarks.Replace(src)), " ")
	}
	pick := func(s [2]string) string {
		if words {
			return " " + s[1] + " "
		}
		return s[0]
	}
	if strings.Contains(src, `\not`) {
		src = latexNot.ReplaceAllStringFunc(src, func(m string) string {
			switch latexNot.FindStringSubmatch(m)[1] {
			case "subset":
				return pick([2]string{"⊄", "не лежит"})
			case "in":
				return pick([2]string{"∉", "не принадлежит"})
			}
			return pick([2]string{"∦", "не параллельна"})
		})
	}
	if strings.Contains(src, `\begin`) || strings.Contains(src, `\end`) {
		src = latexEnv.ReplaceAllString(src, " ")
	}
	if strings.Contains(src, "{") {
		src = latexText.ReplaceAllString(src, " $1 ")
		src = latexFrac.ReplaceAllString(src, "($1)/($2)")
	}
	// The commands left, one by one.
	var sb strings.Builder
	for i := 0; i < len(src); i++ {
		c := src[i]
		if c != '\\' || i+1 == len(src) {
			sb.WriteByte(c)
			continue
		}
		j := i + 1
		for j < len(src) && isLetter(src[j]) {
			j++
		}
		if j == i+1 {
			// \\ is a line break; \, \; \  \! are spaces and others
			// escaped characters.
			if src[j] == '\\' {
				sb.WriteString(pick([2]string{", ", " "}))
			} else {
				sb.WriteByte(' ')
			}
			i = j
			continue
		}
		if s, ok := latexCommands[src[i+1:j]]; ok {
			sb.WriteString(pick(s))
		}
		i = j - 1
	}
	return strings.Join(strings.Fields(mathMarks.Replace(sb.String())), " ")
}

var mathMarks = strings.NewReplacer("{", "", "}", "", "&", " ", "_", "", "~", " ")

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

var (
	// A link target may hold one level of parentheses: «(задача 2).svg».
	mdImage     = regexp.MustCompile(`!\[[^\]]*\]\((?:[^()]|\([^()]*\))*\)|!\[\[[^\]]*\]\]`)
	mdLink      = regexp.MustCompile(`\[([^\]]*)\]\((?:[^()]|\([^()]*\))*\)`)
	mdWikiLink  = regexp.MustCompile(`\[\[([^\]|]*)(?:\|([^\]]*))?\]\]`)
	mdHTML      = regexp.MustCompile(`<[^>]+>`)
	mdMath      = regexp.MustCompile(`\$\$([^$]*)\$\$|\$([^$]+)\$`)
	mdHeading   = regexp.MustCompile(`^\s{0,3}(#{1,6})\s+(.*?)\s*#*\s*$`)
	mdLineStart = regexp.MustCompile(`^\s*(?:>\s*)*(?:[-*+]\s+|\d+[.)]\s+)?(?:\[[ xX]\]\s+)?`)
	mdMarks     = strings.NewReplacer("**", "", "__", "", "~~", "", "==", "", "`", "", "*", "")
)

// noteText is what a note says, without Markdown.
type noteText struct {
	headings []string
	// body is the text, math as symbols; math is the words the math reads
	// as, which the index adds to those of the text.
	body, math string
}

// readNote strips a note's Markdown: pictures and link targets go, links
// keep their text, math reads as described in readMath. The H1 is the
// title, kept elsewhere, and the back-link block at the end is left out.
func readNote(src string, titleLine int) noteText {
	var t noteText
	var body, words []string
	inMath, inCode := false, false
	var formula []string
	for i, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case i+1 == titleLine:
			continue
		case strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~"):
			inCode = !inCode
			continue
		case inCode:
			body = append(body, trimmed)
			continue
		case inMath:
			// A display formula over several lines, up to its closing $$.
			formula = append(formula, strings.TrimSuffix(trimmed, "$$"))
			if strings.HasSuffix(trimmed, "$$") {
				m := strings.Join(formula, " ")
				body, words = append(body, readMath(m, false)), append(words, readMath(m, true))
				formula, inMath = nil, false
			}
			continue
		case strings.HasPrefix(trimmed, "$$") && !(len(trimmed) > 4 && strings.HasSuffix(trimmed, "$$")):
			formula, inMath = []string{trimmed[2:]}, true
			continue
		case isRule(trimmed):
			continue
		case strings.HasPrefix(trimmed, "|"):
			// A table row: its cells, without the line under the header.
			if strings.Trim(trimmed, "|-: ") == "" {
				continue
			}
			line = strings.Join(strings.FieldsFunc(trimmed, func(r rune) bool { return r == '|' }), " ")
		}
		if strings.HasPrefix(trimmed, "#") {
			if m := mdHeading.FindStringSubmatch(line); m != nil {
				if h, _ := readInline(m[2]); h != "" {
					t.headings = append(t.headings, h)
				}
				continue
			}
		}
		if trimmed != "" && strings.ContainsRune(">-*+0123456789", rune(trimmed[0])) {
			line = mdLineStart.ReplaceAllString(line, "")
		}
		if s, w := readInline(line); s != "" {
			body = append(body, s)
			if w != "" {
				words = append(words, w)
			}
		}
	}
	t.body, t.math = strings.Join(body, " "), strings.Join(words, " ")
	return t
}

// readInline strips the Markdown of one line. It returns the text, math
// as symbols, and the words its math reads as.
func readInline(s string) (text, math string) {
	if !strings.ContainsAny(s, "[]!$<*_`~=\\") {
		return strings.Join(strings.Fields(s), " "), ""
	}
	if strings.Contains(s, "![") {
		s = mdImage.ReplaceAllString(s, "")
	}
	// Math first: a formula may hold brackets that look like a link.
	var maths []string
	if strings.Contains(s, "$") {
		s = mdMath.ReplaceAllStringFunc(s, func(m string) string {
			sub := mdMath.FindStringSubmatch(m)
			maths = append(maths, sub[1]+sub[2])
			return "\x00"
		})
	}
	if strings.Contains(s, "[") {
		s = links(s)
	}
	if strings.Contains(s, "<") {
		s = mdHTML.ReplaceAllString(s, "")
	}
	s = strings.ReplaceAll(mdMarks.Replace(s), `\`, "")
	var words []string
	for _, m := range maths {
		s = strings.Replace(s, "\x00", readMath(m, false), 1)
		if w := readMath(m, true); w != "" {
			words = append(words, w)
		}
	}
	return strings.Join(strings.Fields(s), " "), strings.Join(words, " ")
}

// links keeps the text of links and wiki-links.
func links(s string) string {
	s = mdWikiLink.ReplaceAllStringFunc(s, func(m string) string {
		sub := mdWikiLink.FindStringSubmatch(m)
		if sub[2] != "" {
			return sub[2]
		}
		target := sub[1]
		if i := strings.LastIndex(target, "/"); i >= 0 {
			target = target[i+1:]
		}
		target, _, _ = strings.Cut(target, "#")
		return strings.TrimSuffix(target, ".md")
	})
	return mdLink.ReplaceAllString(s, "$1")
}

func isRule(l string) bool {
	if l == "" || !strings.ContainsRune("-*_", rune(l[0])) {
		return false
	}
	t := strings.ReplaceAll(l, " ", "")
	if len(t) < 3 {
		return false
	}
	return strings.Trim(t, "-") == "" || strings.Trim(t, "*") == "" || strings.Trim(t, "_") == ""
}

package plain

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// symbols maps LaTeX commands to the characters a reader expects.
var symbols = map[string]string{
	"angle": "∠", "measuredangle": "∠", "circ": "∘", "degree": "°", "perp": "⊥", "parallel": "∥",
	"cdot": "·", "times": "×", "div": "÷", "pm": "±", "mp": "∓",
	"le": "≤", "leq": "≤", "leqslant": "≤", "ge": "≥", "geq": "≥", "geqslant": "≥",
	"ne": "≠", "neq": "≠", "approx": "≈", "equiv": "≡", "sim": "∼", "cong": "≅", "propto": "∝",
	"infty": "∞", "in": "∈", "notin": "∉", "subset": "⊂", "subseteq": "⊆", "cup": "∪", "cap": "∩",
	"emptyset": "∅", "varnothing": "∅", "forall": "∀", "exists": "∃", "neg": "¬",
	"Rightarrow": "⇒", "implies": "⇒", "Leftarrow": "⇐", "Leftrightarrow": "⇔", "iff": "⇔",
	"rightarrow": "→", "to": "→", "leftarrow": "←", "leftrightarrow": "↔", "mapsto": "↦",
	"triangle": "△", "square": "□", "odot": "⊙", "sum": "∑", "prod": "∏", "int": "∫",
	"partial": "∂", "nabla": "∇", "ldots": "…", "dots": "…", "cdots": "⋯", "prime": "′",
	"alpha": "α", "beta": "β", "gamma": "γ", "delta": "δ", "epsilon": "ε", "varepsilon": "ε",
	"zeta": "ζ", "eta": "η", "theta": "θ", "vartheta": "θ", "iota": "ι", "kappa": "κ",
	"lambda": "λ", "mu": "μ", "nu": "ν", "xi": "ξ", "pi": "π", "rho": "ρ", "sigma": "σ",
	"tau": "τ", "upsilon": "υ", "phi": "φ", "varphi": "φ", "chi": "χ", "psi": "ψ", "omega": "ω",
	"Gamma": "Γ", "Delta": "Δ", "Theta": "Θ", "Lambda": "Λ", "Xi": "Ξ", "Pi": "Π",
	"Sigma": "Σ", "Phi": "Φ", "Psi": "Ψ", "Omega": "Ω",
	"sin": "sin", "cos": "cos", "tan": "tan", "tg": "tg", "cot": "cot", "ctg": "ctg",
	"arcsin": "arcsin", "arccos": "arccos", "arctan": "arctan", "arctg": "arctg",
	"log": "log", "ln": "ln", "lg": "lg", "exp": "exp", "max": "max", "min": "min", "lim": "lim",
	"quad": " ", "qquad": " ", ",": " ", ";": " ", ":": " ", "!": "", " ": " ",
	"{": "{", "}": "}", "%": "%", "$": "$", "_": "_", "#": "#", "&": "&", "|": "‖",
	"left": "", "right": "", "big": "", "Big": "", "bigl": "", "bigr": "", "displaystyle": "",
	"limits": "", "nolimits": "",
}

// unwrap lists commands whose argument is shown as is.
var unwrap = map[string]string{
	"text": "", "mathrm": "", "mathbf": "", "mathit": "", "mathsf": "", "textbf": "", "textit": "",
	"operatorname": "", "mbox": "", "boldsymbol": "", "overline": "", "underline": "",
	"vec": "⃗", "overrightarrow": "⃗", "hat": "̂", "bar": "̅",
}

var superscripts = map[rune]rune{
	'0': '⁰', '1': '¹', '2': '²', '3': '³', '4': '⁴', '5': '⁵', '6': '⁶', '7': '⁷', '8': '⁸', '9': '⁹',
	'+': '⁺', '-': '⁻', '=': '⁼', '(': '⁽', ')': '⁾', 'n': 'ⁿ', 'i': 'ⁱ',
}

var subscripts = map[rune]rune{
	'0': '₀', '1': '₁', '2': '₂', '3': '₃', '4': '₄', '5': '₅', '6': '₆', '7': '₇', '8': '₈', '9': '₉',
	'+': '₊', '-': '₋', '=': '₌', '(': '₍', ')': '₎',
}

// Latex renders a LaTeX formula as readable text: ∠A + ∠B = 180° instead of
// \angle A + \angle B = 180^\circ. Commands it does not know are kept as
// they are. Line breaks (\\) become newlines.
func Latex(src string) string {
	p := &latexParser{src: src}
	out := p.run()
	// Tidy spacing line by line.
	lines := strings.Split(out, "\n")
	var kept []string
	for _, l := range lines {
		l = strings.Join(strings.Fields(l), " ")
		if l != "" {
			kept = append(kept, l)
		}
	}
	return strings.Join(kept, "\n")
}

type latexParser struct {
	src string
	i   int
}

func (p *latexParser) run() string {
	var b strings.Builder
	for p.i < len(p.src) {
		b.WriteString(p.atom())
	}
	return b.String()
}

// atom converts the next unit: a command, a group, a script or a character.
func (p *latexParser) atom() string {
	c := p.src[p.i]
	switch c {
	case '\\':
		return p.command()
	case '{':
		return sub(p.group())
	case '}':
		p.i++
		return ""
	case '^':
		p.i++
		return script(p.arg(), superscripts, "^")
	case '_':
		p.i++
		return script(p.arg(), subscripts, "_")
	case '&':
		p.i++
		return ""
	case '~':
		p.i++
		return " "
	case '\n', '\t':
		p.i++
		return " "
	}
	r, n := utf8.DecodeRuneInString(p.src[p.i:])
	p.i += n
	return string(r)
}

// command converts \name and its arguments.
func (p *latexParser) command() string {
	p.i++ // backslash
	if p.i >= len(p.src) {
		return `\`
	}
	if p.src[p.i] == '\\' {
		p.i++
		return "\n"
	}
	start := p.i
	for p.i < len(p.src) && isLetter(p.src[p.i]) {
		p.i++
	}
	if p.i == start {
		// A one-character command such as \, or \{.
		r, n := utf8.DecodeRuneInString(p.src[p.i:])
		p.i += n
		if s, ok := symbols[string(r)]; ok {
			return s
		}
		return `\` + string(r)
	}
	name := p.src[start:p.i]
	// As in TeX, spaces after a command name are not output.
	p.skipSpaces()
	switch name {
	case "frac", "dfrac", "tfrac":
		num, den := p.arg(), p.arg()
		return paren(num) + "/" + paren(den)
	case "sqrt":
		p.skipSpaces()
		if p.i < len(p.src) && p.src[p.i] == '[' {
			// \sqrt[n]{x}: the index goes in front, as in ³√x.
			end := strings.IndexByte(p.src[p.i:], ']')
			if end > 0 {
				n := sub(p.src[p.i+1 : p.i+end])
				p.i += end + 1
				return script(n, superscripts, "") + "√" + paren(p.arg())
			}
		}
		return "√" + paren(p.arg())
	case "begin", "end":
		p.arg() // environment name
		if p.i < len(p.src) && p.src[p.i] == '{' && name == "begin" {
			p.arg() // column spec of array
		}
		return "\n"
	}
	if mark, ok := unwrap[name]; ok {
		arg := p.arg()
		if mark == "" {
			return arg
		}
		return arg + mark
	}
	if s, ok := symbols[name]; ok {
		switch {
		case isWordRune(s) && len(s) > 1:
			// sin, log: keep a space before the argument.
			return s + " "
		case binary[name]:
			return " " + s + " "
		}
		return s
	}
	if p.i < len(p.src) && p.src[p.i] == '{' {
		// Unknown command: keep it with its argument for the reviewer.
		start := p.i
		p.group()
		return `\` + name + p.src[start:p.i]
	}
	return `\` + name + " "
}

// binary lists operators and relations, which get spaces around them.
var binary = map[string]bool{
	"perp": true, "parallel": true, "cdot": true, "times": true, "div": true, "pm": true, "mp": true,
	"le": true, "leq": true, "leqslant": true, "ge": true, "geq": true, "geqslant": true,
	"ne": true, "neq": true, "approx": true, "equiv": true, "sim": true, "cong": true, "propto": true,
	"in": true, "notin": true, "subset": true, "subseteq": true, "cup": true, "cap": true,
	"Rightarrow": true, "implies": true, "Leftarrow": true, "Leftrightarrow": true, "iff": true,
	"rightarrow": true, "to": true, "leftarrow": true, "leftrightarrow": true, "mapsto": true,
}

// arg reads one argument: a braced group, a command or a character.
func (p *latexParser) arg() string {
	p.skipSpaces()
	if p.i >= len(p.src) {
		return ""
	}
	switch p.src[p.i] {
	case '{':
		return sub(p.group())
	case '\\':
		return p.command()
	}
	r, n := utf8.DecodeRuneInString(p.src[p.i:])
	p.i += n
	return string(r)
}

// group returns the raw text inside the braces at p.i.
func (p *latexParser) group() string {
	depth := 0
	start := p.i + 1
	for ; p.i < len(p.src); p.i++ {
		switch p.src[p.i] {
		case '\\':
			p.i++
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				p.i++
				return p.src[start : p.i-1]
			}
		}
	}
	return p.src[start:]
}

func (p *latexParser) skipSpaces() {
	for p.i < len(p.src) && (p.src[p.i] == ' ' || p.src[p.i] == '\t') {
		p.i++
	}
}

func sub(src string) string { return (&latexParser{src: src}).run() }

// script writes a super- or subscript with Unicode characters when every
// character has one, and as ^x or ^(xy) otherwise. ^\circ becomes °.
func script(s string, table map[rune]rune, mark string) string {
	s = strings.TrimSpace(s)
	if mark == "^" && (s == "∘" || s == "°") {
		return "°"
	}
	if mark == "^" && s == "′" {
		return "′"
	}
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		t, ok := table[r]
		if !ok {
			if utf8.RuneCountInString(s) > 1 {
				return mark + "(" + s + ")"
			}
			return mark + s
		}
		b.WriteRune(t)
	}
	return b.String()
}

// paren wraps a converted argument in parentheses unless it is a single
// symbol or number.
func paren(s string) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= 1 {
		return s
	}
	simple := true
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '.' && r != ',' {
			simple = false
			break
		}
	}
	if simple {
		return s
	}
	return "(" + s + ")"
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func isWordRune(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

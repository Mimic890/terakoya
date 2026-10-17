package plain

import (
	"os"
	"strings"
	"testing"

	"github.com/Mimic890/terakoya/internal/content"
)

func TestLatex(t *testing.T) {
	cases := map[string]string{
		`\angle A + \angle B + \angle C = 180^\circ`: "∠A + ∠B + ∠C = 180°",
		`AC \perp BD`:                       "AC ⊥ BD",
		`a^2 + b^2 = c^2`:                   "a² + b² = c²",
		`\frac{a}{b} + \frac{n(n+1)}{2}`:    "a/b + (n(n+1))/2",
		`\sqrt{2}, \sqrt{a+b}, \sqrt[3]{x}`: "√2, √(a+b), ³√x",
		`S = a \cdot h_a`:                   "S = a · h_a",
		`x_1 \le x_{10}`:                    "x₁ ≤ x₁₀",
		`2^{n+1}`:                           "2ⁿ⁺¹",
		`e^{i\pi}`:                          "e^(iπ)",
		`ab \sin\alpha`:                     "ab sin α",
		`\left( \frac{1}{2} \right)`:        "( 1/2 )",
		`\text{если } x > 0`:                "если x > 0",
		`\vec{a}`:                           "a\u20d7",
		`\unknown{x} y`:                     `\unknown{x} y`,
		"\\begin{aligned}\nS &= a \\cdot h_a \\\\\n  &= a b \\sin\\alpha\n\\end{aligned}": "S = a · h_a\n= a b sin α",
	}
	for in, want := range cases {
		if got := Latex(in); got != want {
			t.Errorf("Latex(%q) = %q, want %q", in, got, want)
		}
	}
}

func loadArticle(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content.CleanSource(raw))
}

var titles = Options{LinkTitle: func(target string) string {
	if target == "basic-notions" {
		return "Основные понятия"
	}
	return ""
}}

func TestPlainView(t *testing.T) {
	body := loadArticle(t, "triangles.md")
	secs := Sections(body, 3500, titles)
	if len(secs) != 1 {
		t.Fatalf("sections = %d", len(secs))
	}
	want := `Треугольники

Треугольник — фигура из трёх точек, не лежащих на одной прямой, и трёх
отрезков, попарно соединяющих эти точки (см. основные понятия).

[картинка: Треугольник ABC]

Виды треугольников

По углам | Признак
Остроугольный | все углы меньше 90°
Прямоугольный | один угол равен 90°
Тупоугольный | один угол больше 90°

Сумма углов

Сумма углов любого треугольника равна 180°:

∠A + ∠B + ∠C = 180°

☐ Докажите это, проведя через вершину прямую, параллельную основанию.

Задача «на смекалку»: тетрадь стоит $2, а ручка на $1 дороже. Сколько стоят обе вместе?`
	if got := secs[0].Plain(); got != want {
		t.Errorf("plain view:\n%s\n\nwant:\n%s", got, want)
	}
	if body[secs[0].Start:secs[0].End] != strings.TrimSpace(body) {
		t.Errorf("section bounds %d..%d", secs[0].Start, secs[0].End)
	}

	q := loadArticle(t, "quadrilaterals.md")
	got := Sections(q, 3500, Options{})[0].Plain()
	for _, want := range []string{"Сумма углов выпуклого четырёхугольника равна 360°, потому что диагональ\nделит его на два треугольника.",
		"1. Противоположные стороны равны: AB = CD, BC = AD.\n2. Противоположные углы равны.",
		"S = a · h_a\n= a b sin α",
		"Формулы в блоках кода не обрабатываются: $x$ и [[ссылки]] остаются как есть."} {
		if !strings.Contains(got, want) {
			t.Errorf("plain view misses %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\n\n\n") || strings.Contains(got, "**") || strings.Contains(got, "```") {
		t.Errorf("plain view has markup:\n%s", got)
	}
}

func TestInline(t *testing.T) {
	cases := map[string]string{
		"**Жирный** и *курсив* и _тоже_ и ~~зачёркнуто~~": "Жирный и курсив и тоже и зачёркнуто",
		"snake_case_name и 2 * 3 * 4": "snake_case_name и 2 * 3 * 4",
		"[[Треугольники]] и [[a|ссылка]] и [сайт](https://x.ru)": "Треугольники и ссылка и сайт",
		"`код` и \\*звёздочка\\*":                                "код и *звёздочка*",
		"цена $5 и $x^2$":                                        "цена $5 и x²",
		"**жирная формула $x$**":                                 "жирная формула x",
	}
	for in, want := range cases {
		if got, _ := inline(in, Options{}); got != want {
			t.Errorf("inline(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSectionsSplitLongArticles(t *testing.T) {
	var b strings.Builder
	b.WriteString("# Тема\n\nВступление.\n\n## Первый\n\n")
	for i := 0; i < 30; i++ {
		b.WriteString(strings.Repeat("слово ", 20) + "\n\n")
	}
	b.WriteString("## Второй\n\nКороткий раздел.\n")
	body := b.String()
	secs := Sections(body, 1000, Options{})
	if secs[0].Title != "" || secs[0].Part != 0 || !strings.HasPrefix(secs[0].Plain(), "Тема\n\nВступление.") {
		t.Errorf("first section = %+v", secs[0])
	}
	last := secs[len(secs)-1]
	if last.Title != "Второй" || last.Plain() != "Второй\n\nКороткий раздел." {
		t.Errorf("last section = %q %q", last.Title, last.Plain())
	}
	parts := 0
	for _, s := range secs {
		if s.Title == "Первый" {
			parts++
			if s.Part != parts || runes(s.Plain()) > 1000 {
				t.Errorf("part %d: %d runes", s.Part, runes(s.Plain()))
			}
		}
	}
	if parts < 2 {
		t.Errorf("long section not split: %d parts", parts)
	}
	// Sections cover the body without gaps or overlaps between blocks.
	for i := 1; i < len(secs); i++ {
		if strings.TrimSpace(body[secs[i-1].End:secs[i].Start]) != "" {
			t.Errorf("gap between sections %d and %d", i-1, i)
		}
	}
}

// mergeCase edits the plain view of the triangles article.
func mergeTriangles(t *testing.T, edit func(plain string) string) (Result, string) {
	t.Helper()
	body := loadArticle(t, "triangles.md")
	sec := Sections(body, 3500, titles)[0]
	r := Merge(body, sec, edit(sec.Plain()))
	return r, body[:sec.Start] + r.Source + body[sec.End:]
}

func paragraphs(s string) []string { return strings.Split(s, "\n\n") }

func TestMergeUnchanged(t *testing.T) {
	r, _ := mergeTriangles(t, func(p string) string { return p })
	if !r.Unchanged {
		t.Errorf("unchanged text gave %+v", r)
	}
	// Whitespace noise from copying is not a change.
	r, _ = mergeTriangles(t, func(p string) string {
		return "  " + strings.ReplaceAll(strings.ReplaceAll(p, " ", "\u00a0 "), "\n", " \r\n") + "\n\n"
	})
	if !r.Unchanged {
		t.Errorf("whitespace changes gave %+v", r.Changes)
	}
}

func TestMergeOneWordKeepsFormatting(t *testing.T) {
	r, got := mergeTriangles(t, func(p string) string {
		return strings.Replace(p, "Сумма углов любого треугольника равна 180°:", "Сумма углов любого треугольника всегда равна 180°:", 1)
	})
	if r.Mode != Matched || r.Unchanged || len(r.Changes) != 1 {
		t.Fatalf("result = %+v", r)
	}
	if !strings.Contains(got, "Сумма углов любого треугольника всегда равна $180^\\circ$:") {
		t.Errorf("formula not restored:\n%s", got)
	}
	// Everything else is byte for byte the original.
	body := loadArticle(t, "triangles.md")
	if strings.Replace(got, "всегда ", "", 1) != body {
		t.Errorf("other text changed:\n%s", got)
	}
	if c := r.Changes[0]; c.Before != "Сумма углов любого треугольника равна 180°:" || !strings.Contains(c.After, "всегда") {
		t.Errorf("change = %+v", c)
	}
}

func TestMergeRestoresLinksBoldAndKinds(t *testing.T) {
	r, got := mergeTriangles(t, func(p string) string {
		p = strings.Replace(p, "попарно соединяющих", "соединяющих попарно", 1)
		p = strings.Replace(p, "Виды треугольников", "Какие бывают треугольники", 1)
		p = strings.Replace(p, "Тупоугольный | один угол больше 90°", "Тупоугольный | один угол тупой (больше 90°)", 1)
		return p
	})
	// The heading and the table next to it form one change.
	if r.Mode != Matched || len(r.Changes) != 2 {
		t.Fatalf("result = %+v", r)
	}
	for _, want := range []string{
		"**Треугольник** — фигура из трёх точек, не лежащих на одной прямой, и трёх\nотрезков, соединяющих попарно эти точки (см. [[basic-notions|основные понятия]]).",
		"\n## Какие бывают треугольники\n",
		"| Тупоугольный | один угол тупой (больше $90^\\circ$) |",
		"| Прямоугольный   | один угол равен $90^\\circ$       |", // untouched rows keep their padding
	} {
		if !strings.Contains(got, want) {
			t.Errorf("merged article misses %q:\n%s", want, got)
		}
	}
}

func TestMergeJoinParagraphs(t *testing.T) {
	r, got := mergeTriangles(t, func(p string) string {
		return strings.Replace(p, "равна 180°:\n\n∠A", "равна 180°: ∠A", 1)
	})
	if r.Mode != Matched || len(r.Changes) != 1 {
		t.Fatalf("result = %+v", r)
	}
	if !strings.Contains(got, "Сумма углов любого треугольника равна $180^\\circ$: $$\\angle A + \\angle B + \\angle C = 180^\\circ$$") {
		t.Errorf("joined paragraphs:\n%s", got)
	}
	if !strings.Contains(got, "## Сумма углов\n\n") || !strings.Contains(got, "\n\n- [ ] Докажите") {
		t.Errorf("neighbours changed:\n%s", got)
	}
}

func TestMergeSplitParagraph(t *testing.T) {
	r, got := mergeTriangles(t, func(p string) string {
		return strings.Replace(p, "не лежащих на одной прямой, и трёх", "не лежащих на одной прямой.\n\nИ ещё из трёх", 1)
	})
	if r.Mode != Matched || len(r.Changes) != 1 || strings.Count(r.Changes[0].After, "\n\n") != 1 {
		t.Fatalf("result = %+v", r)
	}
	if !strings.Contains(got, "**Треугольник** — фигура из трёх точек, не лежащих на одной прямой.\n\nИ ещё из трёх\nотрезков") {
		t.Errorf("split paragraph:\n%s", got)
	}
}

func TestMergeReorder(t *testing.T) {
	r, got := mergeTriangles(t, func(p string) string {
		ps := paragraphs(p)
		// Swap the two last paragraphs: the task and the puzzle.
		n := len(ps)
		ps[n-1], ps[n-2] = ps[n-2], ps[n-1]
		return strings.Join(ps, "\n\n")
	})
	if r.Mode != Matched || r.Unchanged || len(r.Changes) != 0 {
		t.Fatalf("result = %+v", r)
	}
	if !strings.HasSuffix(strings.TrimSpace(got), "Сколько стоят обе вместе?\n\n- [ ] Докажите это, проведя через вершину прямую, параллельную основанию.") {
		t.Errorf("reordered:\n%s", got)
	}
	if !strings.Contains(got, `тетрадь стоит \$2`) || len(r.Notes) != 1 {
		t.Errorf("moved paragraph lost its source or note: %v\n%s", r.Notes, got)
	}
}

func TestMergeDelete(t *testing.T) {
	r, got := mergeTriangles(t, func(p string) string {
		return strings.Replace(p, "[картинка: Треугольник ABC]\n\n", "", 1)
	})
	if r.Mode != Matched || r.Shrunk || len(r.Changes) != 1 || r.Changes[0].After != "" || r.Changes[0].Before != "[картинка: Треугольник ABC]" {
		t.Fatalf("result = %+v", r)
	}
	if strings.Contains(got, "triangle.png") || !strings.Contains(got, "(см. [[basic-notions|основные понятия]]).\n\n## Виды") {
		t.Errorf("after delete:\n%s", got)
	}
}

func TestMergeTextBeforeFirstParagraph(t *testing.T) {
	r, got := mergeTriangles(t, func(p string) string { return "Это важная тема.\n\n" + p })
	if r.Mode != Matched || len(r.Changes) != 1 || r.Changes[0].Before != "" {
		t.Fatalf("result = %+v", r)
	}
	body := loadArticle(t, "triangles.md")
	if got != strings.Replace(body, "# Треугольники", "Это важная тема.\n\n# Треугольники", 1) {
		t.Errorf("prepended:\n%s", got)
	}
}

func TestMergePastedTwice(t *testing.T) {
	edit := func(p string) string {
		return strings.Replace(p, "Тупоугольный | один угол больше", "Тупоугольный | один угол строго больше", 1)
	}

	// Original first, edited copy second, and the other way round.
	for name, f := range map[string]func(string) string{
		"orig+edited": func(p string) string { return p + "\n\n" + edit(p) },
		"edited+orig": func(p string) string { return edit(p) + "\n\n" + p },
	} {
		r, got := mergeTriangles(t, f)
		if r.Mode != Matched || len(r.Changes) != 1 || len(r.Notes) == 0 || !strings.Contains(r.Notes[0], "pasted twice") {
			t.Errorf("%s: result = %+v", name, r)
		}
		if strings.Count(got, "Треугольники") != 1 || !strings.Contains(got, "строго больше $90^\\circ$") {
			t.Errorf("%s: merged:\n%s", name, got)
		}
	}

	// Pasted twice without changes.
	if r, _ := mergeTriangles(t, func(p string) string { return p + "\n\n" + p }); !r.Unchanged {
		t.Errorf("double paste of the original: %+v", r)
	}

	// Both copies edited: no way to tell which one is meant.
	r, got := mergeTriangles(t, func(p string) string {
		return edit(p) + "\n\n" + strings.Replace(p, "смекалку", "сообразительность", 1)
	})
	if r.Mode != Replaced || len(r.Notes) == 0 {
		t.Fatalf("both edited: result = %+v", r)
	}
	if strings.Count(got, "# Треугольники") != 1 || strings.Count(got, "Треугольники") != 2 {
		t.Errorf("fallback keeps the user's text, sources once:\n%s", got)
	}
}

func TestMergePartial(t *testing.T) {
	// The user sends back only the paragraph they fixed.
	r, got := mergeTriangles(t, func(string) string {
		return "Сумма углов любого треугольника равна ровно 180°:"
	})
	if r.Mode != Partial || len(r.Changes) != 1 || r.Shrunk {
		t.Fatalf("result = %+v", r)
	}
	body := loadArticle(t, "triangles.md")
	if got != strings.Replace(body, "равна $180", "равна ровно $180", 1) {
		t.Errorf("partial edit:\n%s", got)
	}

	// A short text that resembles nothing: most of the section would go.
	r, got = mergeTriangles(t, func(string) string { return "Тут всё неправильно, перепишите." })
	if r.Mode == Partial || !r.Shrunk {
		t.Fatalf("unrelated short text: %+v", r)
	}
	if strings.TrimSpace(got) != "Тут всё неправильно, перепишите." {
		t.Errorf("merged:\n%s", got)
	}
}

func TestMergeFallbackKeepsKnownBlocks(t *testing.T) {
	body := "Первый абзац.\n\n$$x^2$$\n\nТретий абзац.\n"
	sec := Sections(body, 3500, Options{})[0]
	// A repeated paragraph forces the fallback.
	r := Merge(body, sec, "Первый абзац.\n\nПервый абзац.\n\nx²\n\nНовый.")
	if r.Mode != Replaced {
		t.Fatalf("result = %+v", r)
	}
	if r.Source != "Первый абзац.\n\nПервый абзац.\n\n$$x^2$$\n\nНовый." {
		t.Errorf("source = %q", r.Source)
	}
}

func TestMergeListsAndBullets(t *testing.T) {
	body := "Свойства:\n\n- первое\n- второе\n\n1. раз\n2. два\n"
	sec := Sections(body, 3500, Options{})[0]
	if sec.Plain() != "Свойства:\n\n• первое\n• второе\n\n1. раз\n2. два" {
		t.Fatalf("plain = %q", sec.Plain())
	}
	r := Merge(body, sec, "Свойства:\n\n• первое\n• второе\n• третье\n\n1. раз\n2. два")
	if r.Source != "Свойства:\n\n- первое\n- второе\n- третье\n\n1. раз\n2. два" {
		t.Errorf("source = %q", r.Source)
	}
}

func TestMergeEmptySection(t *testing.T) {
	sec := Sections("", 3500, Options{})[0]
	r := Merge("", sec, "Новый текст.")
	if r.Source != "Новый текст." || r.Unchanged {
		t.Errorf("result = %+v", r)
	}
}

func TestRestoreWholeWordsOnly(t *testing.T) {
	spans := []Span{{Plain: "Треугольник", Source: "**Треугольник**"}}
	if got := restore("Треугольники и Треугольник.", spans); got != "Треугольники и **Треугольник**." {
		t.Errorf("restore = %q", got)
	}
	// Each span is restored as often as it occurred, no more.
	if got := restore("Треугольник, Треугольник", spans); got != "**Треугольник**, Треугольник" {
		t.Errorf("restore = %q", got)
	}
	// Longer spans win over the ones inside them.
	spans = []Span{{Plain: "x", Source: "$x$"}, {Plain: "AB = CD", Source: "$AB = CD$"}, {Plain: "AB", Source: "$AB$"}}
	if got := restore("AB = CD и AB", spans); got != "$AB = CD$ и $AB$" {
		t.Errorf("restore = %q", got)
	}
}

// Every block of every sample article: an untouched round trip changes
// nothing, and editing or deleting one block leaves all others byte for
// byte as they were.
func TestMergeEveryBlock(t *testing.T) {
	bodies := []string{loadArticle(t, "triangles.md"), loadArticle(t, "quadrilaterals.md")}
	for _, dir := range []string{"../../examples/vault", "../render/testdata/vault"} {
		cat, _, err := content.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range cat.Notes() {
			body, _ := content.SplitBacklink(n.Body)
			bodies = append(bodies, body)
		}
	}
	for _, body := range bodies {
		for _, max := range []int{3500, 120} {
			for _, sec := range Sections(body, max, Options{}) {
				if r := Merge(body, sec, sec.Plain()); !r.Unchanged {
					t.Errorf("round trip changed %q:\n%s", sec.Title, r.Source)
				}
				for i, b := range sec.Blocks {
					var ps []string
					for _, o := range sec.Blocks {
						ps = append(ps, o.Plain)
					}
					want := func(repl string) string {
						src := body[sec.Start:b.Start] + repl + body[b.End:sec.End]
						return strings.TrimPrefix(strings.ReplaceAll(src, "\n\n\n\n", "\n\n"), "\n\n")
					}

					ps[i] = "Совсем новый текст."
					r := Merge(body, sec, strings.Join(ps, "\n\n"))
					repl := "Совсем новый текст."
					switch b.Kind {
					case Heading:
						repl = strings.Repeat("#", b.Level) + " " + repl
					case Quote:
						repl = "> " + repl
					}
					if r.Mode != Matched || r.Source != want(repl) {
						t.Errorf("edit block %d (%q) mode %d:\n%s\n--- want:\n%s", i, b.Plain, r.Mode, r.Source, want(repl))
					}

					if len(sec.Blocks) > 2 {
						r = Merge(body, sec, strings.Join(append(ps[:i:i], ps[i+1:]...), "\n\n"))
						if r.Mode == Replaced || strings.Contains(r.Source, b.Source) && !strings.Contains(body[sec.Start:b.Start]+body[b.End:sec.End], b.Source) {
							t.Errorf("delete block %d (%q): mode %d\n%s", i, b.Plain, r.Mode, r.Source)
						}
					}
				}
			}
		}
	}
}

func TestUserDollarsAreEscaped(t *testing.T) {
	r, got := mergeTriangles(t, func(p string) string { return strings.Replace(p, "Задача", "Загадка", 1) })
	if r.Mode != Matched || !strings.Contains(got, "Загадка «на смекалку»: тетрадь стоит \\$2, а ручка на \\$1 дороже.") {
		t.Errorf("merged:\n%s", got)
	}
	if got := restore("стоит $5 и x²", []Span{{Plain: "x²", Source: "$x^2$"}}); got != `стоит \$5 и $x^2$` {
		t.Errorf("restore = %q", got)
	}
}

func TestLatexVault(t *testing.T) {
	cases := map[string]string{
		`a \not\subset \alpha,\ a \parallel b`: "a ⊄ α, a ∥ b",
		`M \notin b`:                           "M ∉ b",
		`a \mathbin{\dot{-}} b`:                "a -̇ b",
		`\begin{array}{l} a \not\subset \alpha \\ a \parallel b \end{array}`: "a ⊄ α\na ∥ b",
		`\exists\, b \subset \alpha:\ b \parallel a`:                         "∃ b ⊂ α: b ∥ a",
		`ABCA_1B_1C_1 \text{ — призма}`:                                      "ABCA₁B₁C₁ — призма",
	}
	for in, want := range cases {
		if got := Latex(in); got != want {
			t.Errorf("Latex(%q) = %q, want %q", in, got, want)
		}
	}
}

// Obsidian shows each line of a paragraph on its own line, and so does the
// plain view; rows of an inline array are separated by semicolons.
func TestPlainKeepsObsidianLines(t *testing.T) {
	body := "**Дано:**\n$\\begin{array}{l} ABCA_1B_1C_1 \\text{ — призма} \\\\ M \\in A_1B_1 \\end{array}$\n\n**Доказать:**\n$a \\parallel \\alpha$\n"
	sec := Sections(body, 3500, Options{})[0]
	if got, want := sec.Plain(), "Дано:\nABCA₁B₁C₁ — призма; M ∈ A₁B₁\n\nДоказать:\na ∥ α"; got != want {
		t.Errorf("plain = %q, want %q", got, want)
	}
	r := Merge(body, sec, strings.Replace(sec.Plain(), "Доказать:", "Доказать: (устно)", 1))
	if r.Source != strings.Replace(body[:len(body)-1], "**Доказать:**", "**Доказать:** (устно)", 1) {
		t.Errorf("merged = %q", r.Source)
	}
}

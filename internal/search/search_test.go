package search

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Mimic890/terakoya/internal/content"
)

// vault writes the notes, path → text, into a vault and loads it.
func vault(t testing.TB, notes map[string]string) *content.Catalog {
	t.Helper()
	root := t.TempDir()
	for p, text := range notes {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cat, _, err := content.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return cat
}

const (
	g10 = "Геометрия/10 класс/"
	g7  = "Геометрия/7 класс/"
	g8  = "Геометрия/8 класс/"
	par = g10 + "05 Параллельность прямой и плоскости/"
)

var fixture = map[string]string{
	par + "Параллельность прямой и плоскости.md": `# Параллельность прямой и плоскости

## Взаимное расположение прямой и плоскости

$a \parallel \alpha$ — нет общих точек.

## Признак параллельности прямой и плоскости

- [Признак параллельности прямой и плоскости](теоремы/Признак%20параллельности%20прямой%20и%20плоскости%20%285.1%29.md)
`,
	par + "теоремы/Признак параллельности прямой и плоскости (5.1).md": `# Признак параллельности прямой и плоскости

> Если прямая, не принадлежащая данной плоскости, параллельна какой-либо прямой, лежащей в этой плоскости, то данная прямая параллельна самой плоскости.

![](Чертёж%20(5.1).svg)

$\begin{array}{l} a \not\subset \alpha \\ a \parallel b \end{array}$
`,
	par + "теоремы/следствия/Следствие о прямой в плоскости.md": "# Следствие о прямой в плоскости\n\nВ плоскости есть прямая, $b \\parallel a$.\n",
	par + "задачи/Сечение тетраэдра (задача 4).md":              "# Сечение тетраэдра\n\nПостройте сечение тетраэдра плоскостью.\n",
	g10 + "06 Перпендикулярность/Перпендикулярность прямой и плоскости.md": `# Перпендикулярность прямой и плоскости

Если $a \perp b$ и $a \perp c$, то прямая перпендикулярна плоскости. Здесь $\angle ABC = 90^\circ$.
`,
	g7 + "01 Начальные понятия/Начальные понятия.md":                   "# Начальные понятия\n\nТочка, прямая, отрезок.\n",
	g7 + "01 Начальные понятия/теоремы/Теорема о смежных углах.md":     "# Теорема о смежных углах\n\nСумма смежных углов равна 180°.\n",
	g7 + "01 Начальные понятия/аксиомы/Аксиома параллельных прямых.md": "# Аксиома параллельных прямых\n\nЧерез точку проходит одна прямая, параллельная данной.\n",
	g8 + "02 Теорема Пифагора/Теорема Пифагора.md":                     "# Теорема Пифагора\n\nКвадрат гипотенузы равен сумме квадратов катетов.\n",
	g8 + "02 Теорема Пифагора/задачи/Лестница у стены.md":              "# Лестница у стены\n\nПо теореме Пифагора найдите длину лестницы.\n",
}

func titles(r Result) []string {
	var out []string
	for _, h := range r.Hits {
		out = append(out, h.Note.Title)
	}
	return out
}

func TestNormalisation(t *testing.T) {
	var words []string
	for _, tk := range tokens("Ёлка, ПАРАЛЛЕЛЬНОСТЬ и Lines!") {
		words = append(words, tk.word)
	}
	if got := strings.Join(words, " "); got != "елка параллельность и lines" {
		t.Errorf("tokens = %q", got)
	}
	for _, pair := range [][2]string{
		{"параллельности", "параллельность"}, {"прямой", "прямая"}, {"плоскостям", "плоскость"},
		{"теоремы", "теорема"}, {"lines", "line"}, {"ёлки", "елка"},
	} {
		if a, b := stem(fold(pair[0])), stem(fold(pair[1])); a != b {
			t.Errorf("stem(%s) = %s, stem(%s) = %s", pair[0], a, pair[1], b)
		}
	}
	q := Parse("Признак и the плоскость")
	if strings.Join(q.Words, " ") != "признак плоскость" {
		t.Errorf("words = %v", q.Words)
	}
	if got := Normalize("  Признак   ПАРАЛЛЕЛЬНОСТИ, Ёж "); got != "признак параллельности еж" {
		t.Errorf("Normalize = %q", got)
	}
}

func TestLatexAndMarkdown(t *testing.T) {
	for _, c := range []struct{ src, symbols, words string }{
		{`a \parallel b`, "a ∥ b", "a параллельна b"},
		{`a \perp \alpha`, "a ⊥ α", "a перпендикулярна альфа"},
		{`\angle ABC = 90^\circ`, "∠ ABC = 90^°", "угол ABC = 90^ градус"},
		{`a \not\subset \alpha`, "a ⊄ α", "a не лежит альфа"},
		{`\begin{array}{l} a \in b \\ c \end{array}`, "a ∈ b , c", "a принадлежит b c"},
		{`\frac{1}{2} \mathbin{\dot{-}} \unknown x`, "(1)/(2) - x", "(1)/(2) - x"},
		{`\text{если} \triangle ABC \sim \triangle DEF`, "если △ ABC ∼ △ DEF", "если треугольник ABC подобен треугольник DEF"},
	} {
		if got := readMath(c.src, false); got != c.symbols {
			t.Errorf("symbols of %s = %q, want %q", c.src, got, c.symbols)
		}
		if got := readMath(c.src, true); got != c.words {
			t.Errorf("words of %s = %q, want %q", c.src, got, c.words)
		}
	}
	src := "# Заголовок\n\n## Раздел\n\n> **Важно:** прямая [лежит](a.md) в [[Плоскость (5.1)|плоскости]] и [[папка/Точка.md]].\n" +
		"![](Чертёж%20(5.1).svg) ![[рис.png]]\n\n$$\na \\parallel b\n$$\n\n- пункт <b>жирный</b>\n\n" +
		"| Угол | Вид |\n|:-----|----:|\n| 90° | прямой |\n\n---\n"
	got := readNote(src, 1)
	if strings.Join(got.headings, "|") != "Раздел" {
		t.Errorf("headings = %q", got.headings)
	}
	if want := "Важно: прямая лежит в плоскости и Точка. a ∥ b пункт жирный Угол Вид 90° прямой"; got.body != want {
		t.Errorf("body =\n%q\nwant\n%q", got.body, want)
	}
	if got.math != "a параллельна b" {
		t.Errorf("math = %q", got.math)
	}
}

func TestSearchFindsByStemAndMath(t *testing.T) {
	ix := Build(vault(t, fixture))
	// «перпендикулярна» is only in the text; the math says ⊥ too.
	if got := titles(ix.Search("перпендикулярны")); len(got) != 1 || got[0] != "Перпендикулярность прямой и плоскости" {
		t.Errorf("перпендикулярны: %v", got)
	}
	// «угол» is only in the math: \angle.
	if got := titles(ix.Search("угол")); len(got) == 0 || got[0] != "Перпендикулярность прямой и плоскости" {
		t.Errorf("угол: %v", got)
	}
	// A stop word alone finds nothing.
	if got := ix.Search("и"); len(got.Hits) != 0 {
		t.Errorf("и: %v", titles(got))
	}
}

func TestRanking(t *testing.T) {
	ix := Build(vault(t, fixture))
	// The title and the phrase win over the text.
	if got := titles(ix.Search("признак параллельности")); got[0] != "Признак параллельности прямой и плоскости" {
		t.Errorf("признак параллельности: %v", got)
	}
	// The main page's title matches it all and is short.
	if got := titles(ix.Search("параллельность прямой и плоскости")); got[0] != "Параллельность прямой и плоскости" {
		t.Errorf("параллельность прямой и плоскости: %v", got)
	}
	// Title over headings over text: «сечение» is in a title, «смежных»
	// in a title and «катетов» in the text only.
	r := ix.Search("сечение")
	if got := titles(r); got[0] != "Сечение тетраэдра" {
		t.Errorf("сечение: %v", got)
	}
	// On equal scores, the main page comes first.
	ix2 := Build(vault(t, map[string]string{
		g8 + "01 Круг/Круг.md":            "# Круг\n\nОкружность.\n",
		g8 + "01 Круг/задачи/Круг (1).md": "# Круг\n\nОкружность.\n",
	}))
	r = ix2.Search("окружность")
	if len(r.Hits) != 2 || !r.Hits[0].Note.IsMain() {
		t.Errorf("main first: %v", titles(r))
	}
}

func TestTyposAndPrefix(t *testing.T) {
	ix := Build(vault(t, fixture))
	for q, want := range map[string]string{
		"парралельность прямой": "Параллельность прямой и плоскости", // 2 typos, 14 letters
		"пифогора":              "Теорема Пифагора",                  // 1 typo, 8 letters
		"тетраэдр":              "Сечение тетраэдра",
		"тетраедра":             "Сечение тетраэдра", // е for э, 9 letters
		"перпендикул":           "Перпендикулярность прямой и плоскости",
		"Параллел":              "Параллельность прямой и плоскости",
	} {
		if got := titles(ix.Search(q)); len(got) == 0 || got[0] != want {
			t.Errorf("%s: %v, want %s first", q, got, want)
		}
	}
	// Short words get no typos: «угл» is not «угол» with a letter missing
	// … but it is its start.
	if got := titles(ix.Search("уго")); len(got) == 0 {
		t.Error("уго: the start of угол found nothing")
	}
	if got := titles(ix.Search("угал")); len(got) == 0 {
		t.Error("угал: one typo in 4 letters found nothing")
	}
	if got := ix.Search("кот"); len(got.Hits) != 0 {
		t.Errorf("кот: %v", titles(got))
	}
	// Two typos in a word of 7 letters are too many.
	if got := ix.Search("сечанеи"); len(got.Hits) != 0 {
		t.Errorf("сечанеи: %v", titles(got))
	}
}

func TestFilters(t *testing.T) {
	ix := Build(vault(t, fixture))
	q := Parse("теорема 10 класс параллельность")
	if q.Grade != 10 || strings.Join(q.Kinds, ",") != "теоремы" || q.Text != "параллельность" {
		t.Errorf("parsed = %+v", q)
	}
	for _, s := range []string{"10 кл", "10кл", "10-й класс", "10 классе"} {
		if q := Parse(s + " прямая"); q.Grade != 10 || q.Text != "прямая" {
			t.Errorf("%s: %+v", s, q)
		}
	}
	if got := titles(ix.Search("теорема 10 класс")); strings.Join(got, "|") != "Признак параллельности прямой и плоскости" {
		t.Errorf("теорема 10 класс: %v", got)
	}
	if got := titles(ix.Search("прямая 7 класс")); strings.Join(got, "|") != "Аксиома параллельных прямых|Начальные понятия" {
		t.Errorf("прямая 7 класс: %v", got)
	}
	if got := titles(ix.Search("аксиома")); strings.Join(got, "|") != "Аксиома параллельных прямых" {
		t.Errorf("аксиома: %v", got)
	}
	if got := titles(ix.Search("следствие")); strings.Join(got, "|") != "Следствие о прямой в плоскости" {
		t.Errorf("следствие: %v", got)
	}
	// A main page titled «Теорема …» counts as a theorem.
	if got := titles(ix.Search("теорема пифагора")); len(got) != 1 || got[0] != "Теорема Пифагора" {
		t.Errorf("теорема пифагора: %v", got)
	}
	if got := titles(ix.Search("задача пифагора")); len(got) != 1 || got[0] != "Лестница у стены" {
		t.Errorf("задача пифагора: %v", got)
	}
}

func TestSnippet(t *testing.T) {
	ix := Build(vault(t, fixture))
	r := ix.Search("гипотенузы")
	var sb strings.Builder
	for _, p := range r.Snippet(r.Hits[0], 120) {
		if p.Bold {
			sb.WriteString("**" + p.Text + "**")
		} else {
			sb.WriteString(p.Text)
		}
	}
	if got := sb.String(); got != "Квадрат **гипотенузы** равен сумме квадратов катетов." {
		t.Errorf("snippet = %q", got)
	}
	r = ix.Search("теорема параллельна плоскости")
	pieces := r.Snippet(r.Hits[0], 60)
	var text string
	bold := 0
	for _, p := range pieces {
		text += p.Text
		if p.Bold {
			bold++
		}
	}
	if n := len([]rune(text)); n > 64 || bold < 1 || !strings.HasPrefix(text, "…") && !strings.HasSuffix(text, "…") {
		t.Errorf("snippet (%d letters, %d bold) = %q", n, bold, text)
	}
}

func TestSuggest(t *testing.T) {
	ix := Build(vault(t, fixture))
	// Three edits: too many to be found, close enough to be offered.
	if r := ix.Search("пеффагор"); len(r.Hits) != 0 {
		t.Fatalf("found %v", titles(r))
	}
	if got := ix.Suggest("пеффагор", 3); len(got) == 0 || got[0] != "пифагора" {
		t.Errorf("suggest = %v", got)
	}
	// Nothing in grade 11: without the grade, there is.
	if got := ix.Suggest("теорема пифагора 11 класс", 3); strings.Join(got, "|") != "теорема пифагора" {
		t.Errorf("suggest = %v", got)
	}
	if got := ix.Suggest("бутерброд", 3); len(got) != 0 {
		t.Errorf("suggest = %v", got)
	}
}

func TestPerformance(t *testing.T) {
	if raceEnabled || testing.Short() {
		t.Skip("timing")
	}
	rnd := rand.New(rand.NewSource(1))
	syll := strings.Fields("па ра ле ль но сть пря мо й пло ско сти тре у го ль ник ок ру жно сть ве кто р ди а го на ль")
	word := func() string {
		var sb strings.Builder
		for n := 2 + rnd.Intn(4); n > 0; n-- {
			sb.WriteString(syll[rnd.Intn(len(syll))])
		}
		return sb.String()
	}
	vocab := make([]string, 30000)
	for i := range vocab {
		vocab[i] = word()
	}
	notes := map[string]string{}
	for i := 0; len(notes) < 2000; i++ {
		var body strings.Builder
		fmt.Fprintf(&body, "# %s %s %d\n\n## %s\n\n", vocab[rnd.Intn(len(vocab))], vocab[rnd.Intn(len(vocab))], i, vocab[rnd.Intn(len(vocab))])
		// Twice as long as the notes of the vault, which average 150 words.
		for w := 0; w < 300; w++ {
			body.WriteString(vocab[rnd.Intn(len(vocab))])
			if w%12 == 11 {
				body.WriteString(" $a \\parallel b$.\n")
			} else {
				body.WriteString(" ")
			}
		}
		topic := fmt.Sprintf("Геометрия/%d класс/%02d Тема %d/", 7+i/20%5, i/20, i/20)
		if i%20 == 0 {
			notes[topic+fmt.Sprintf("Тема %d.md", i/20)] = body.String()
		} else {
			notes[topic+fmt.Sprintf("теоремы/Теорема %d.md", i)] = body.String()
		}
	}
	cat := vault(t, notes)
	start := time.Now()
	ix := Build(cat)
	t.Logf("indexed in %v", time.Since(start))
	if took := time.Since(start); took > time.Second {
		t.Errorf("indexing %d notes took %v", ix.Len(), took)
	}
	if ix.Len() != 2000 {
		t.Fatalf("indexed %d", ix.Len())
	}
	for _, q := range []string{vocab[0], vocab[1] + " " + vocab[2], "параллел", "плоскасть треугольнек", "теорема 9 класс " + vocab[3]} {
		start := time.Now()
		r := ix.Search(q)
		for _, h := range r.Hits[:min(8, len(r.Hits))] {
			r.Snippet(h, 120)
		}
		if took := time.Since(start); took > 20*time.Millisecond {
			t.Errorf("query %q took %v (%d found)", q, took, len(r.Hits))
		}
	}
}

func TestSnippetJoinsAdjacentWords(t *testing.T) {
	ix := Build(vault(t, fixture))
	r := ix.Search("признак параллельности")
	for _, h := range r.Hits {
		if !h.Note.IsMain() {
			continue
		}
		var bold []string
		for _, p := range r.Snippet(h, 120) {
			if p.Bold {
				bold = append(bold, p.Text)
			}
		}
		if len(bold) != 1 || bold[0] != "Признак параллельности" {
			t.Errorf("bold = %q", bold)
		}
	}
}

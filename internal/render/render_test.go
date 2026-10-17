package render

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Mimic890/terakoya/internal/content"
)

var update = flag.Bool("update", false, "rewrite golden files")

// TestGolden converts every fixture note and compares the Markdown, the
// media list and the issues with testdata/golden/<file>.golden.
func TestGolden(t *testing.T) {
	for _, dir := range []string{"testdata/vault", "../../examples/vault"} {
		cat, _, err := content.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range cat.Notes() {
			name := n.Topic.Dir + " - " + strings.TrimSuffix(n.FileName(), ".md")
			t.Run(name, func(t *testing.T) {
				got := formatResult(Convert(cat, n, Options{Continued: "%s (продолжение)"}))
				golden := filepath.Join("testdata", "golden", name+".golden")
				if *update {
					if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
				want, err := os.ReadFile(golden)
				if err != nil {
					t.Fatalf("%v (run go test -update)", err)
				}
				if got != string(want) {
					t.Errorf("output differs from %s:\n--- got ---\n%s\n--- want ---\n%s", golden, got, want)
				}
			})
		}
	}
}

func formatResult(r *Result) string {
	var b strings.Builder
	for i, p := range r.Parts {
		if len(r.Parts) > 1 {
			fmt.Fprintf(&b, "==== part %d ====\n", i+1)
		}
		b.WriteString(p.Markdown)
		b.WriteString("==== media ====\n")
		for _, m := range p.Media {
			fmt.Fprintf(&b, "%s %s svg=%v\n", m.ID, m.Path, m.SVG)
		}
		fmt.Fprintf(&b, "==== stats ====\n%+v\n", p.Stats)
	}
	b.WriteString("==== issues ====\n")
	for _, is := range r.Issues {
		fmt.Fprintf(&b, "%s\n", is)
	}
	return b.String()
}

// longNote builds a note with sections of known sizes.
func longNote(t *testing.T, body string) (*content.Catalog, *content.Note) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "Г", "7 класс", "01 Тема")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "Тема.md"), []byte(body), 0o644)
	cat, _, err := content.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return cat, cat.Notes()[0]
}

func para(word string, n int) string { return strings.TrimSpace(strings.Repeat(word+" ", n)) }

func TestSplitByHeadings(t *testing.T) {
	body := "# Тема\n\nВступление.\n\n" +
		"## Первый\n\n" + para("один", 20) + "\n\n" +
		"## Второй\n\n" + para("два", 20) + "\n\n" +
		"## Большой\n\n" + "### Часть А\n\n" + para("ааа", 30) + "\n\n" + "### Часть Б\n\n" + para("ббб", 30) + "\n\n" +
		"## Огромный\n\n" + para("x1", 40) + "\n\n- список " + para("x2", 30) + "\n\n- второй пункт\n\n" + para("x3", 40) + "\n\n" +
		"## Последний\n\nКонец.\n\n---\n↩ [Тема](Тема.md)\n"
	cat, n := longNote(t, body)
	res := Convert(cat, n, Options{Limits: Limits{Chars: 260, Blocks: 500, Media: 50, Nesting: 16, Columns: 20},
		Continued: "%s (продолжение)"})

	var heads []string
	for i, p := range res.Parts {
		if p.Stats.Chars > 260 {
			t.Errorf("part %d has %d chars", i+1, p.Stats.Chars)
		}
		lines := strings.Split(p.Markdown, "\n")
		heads = append(heads, lines[0]+" | "+firstHeading(lines[1:]))
		if i > 0 && lines[0] != "# Тема (продолжение)" {
			t.Errorf("part %d starts with %q", i+1, lines[0])
		}
		if strings.Contains(p.Markdown, "↩") {
			t.Errorf("back-link kept in part %d", i+1)
		}
	}
	// Whole sections are packed while they fit; «Большой» is split at
	// "###" and keeps its heading with the first subsection; «Огромный»
	// is split between blocks, the list staying whole.
	want := []string{
		"# Тема | ## Первый",
		"# Тема (продолжение) | ## Большой",
		"# Тема (продолжение) | ### Часть Б",
		"# Тема (продолжение) | ## Огромный",
		"# Тема (продолжение) | ## Последний",
	}
	if strings.Join(heads, "\n") != strings.Join(want, "\n") {
		for _, p := range res.Parts {
			t.Logf("---- %d chars\n%s", p.Stats.Chars, p.Markdown)
		}
		t.Errorf("parts:\n%s", strings.Join(heads, "\n"))
	}
	// Nothing is lost or cut inside a block: the list stays whole.
	all := ""
	for _, p := range res.Parts {
		all += p.Markdown
	}
	for _, w := range []string{para("один", 20), para("ааа", 30), "- список " + para("x2", 30) + "\n\n- второй пункт", "Конец."} {
		if !strings.Contains(all, w) {
			t.Errorf("lost %q", w[:20])
		}
	}
	if len(res.Issues) != 1 || res.Issues[0].Severity != content.Info || !strings.Contains(res.Issues[0].Msg, "5 parts") {
		t.Errorf("issues = %v", res.Issues)
	}
}

func firstHeading(lines []string) string {
	for _, l := range lines {
		if strings.HasPrefix(l, "##") {
			return l
		}
	}
	return ""
}

func TestSplitUnsplittableBlock(t *testing.T) {
	body := "# Тема\n\nКоротко.\n\n## Раздел\n\n" + para("слово", 80) + "\n\nХвост.\n"
	cat, n := longNote(t, body)
	res := Convert(cat, n, Options{Limits: Limits{Chars: 200, Blocks: 500, Media: 50, Nesting: 16, Columns: 20}})
	if len(res.Parts) != 3 {
		t.Fatalf("parts = %d", len(res.Parts))
	}
	errs := 0
	for _, is := range res.Issues {
		if is.Severity == content.Error && strings.Contains(is.Msg, "cannot be split further") && is.Line == 5 {
			errs++
		}
	}
	if errs != 1 {
		t.Errorf("issues = %v", res.Issues)
	}
}

func TestShortNoteIsOnePart(t *testing.T) {
	cat, n := longNote(t, "# Тема\n\nТекст.\n")
	res := Convert(cat, n, Options{})
	if len(res.Parts) != 1 || res.Parts[0].Markdown != "# Тема\n\nТекст.\n" || len(res.Issues) != 0 {
		t.Errorf("result = %+v", res)
	}
}

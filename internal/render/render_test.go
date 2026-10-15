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

// TestGolden converts every fixture article and compares the Markdown, the
// media list and the issues with testdata/golden/<id>.golden.
func TestGolden(t *testing.T) {
	for _, dir := range []string{"testdata/content", "../../examples/content"} {
		cat, issues, err := content.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(issues) > 0 {
			t.Fatalf("loader issues in %s: %v", dir, issues)
		}
		for _, a := range cat.Articles() {
			t.Run(a.ID, func(t *testing.T) {
				res := Convert(cat, a, Options{BotUsername: "terakoya_bot"})
				got := formatResult(res)
				golden := filepath.Join("testdata", "golden", a.ID+".golden")
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
	b.WriteString(r.Markdown)
	b.WriteString("\n==== media ====\n")
	for _, m := range r.Media {
		fmt.Fprintf(&b, "%s %s\n", m.ID, filepath.Base(m.AbsPath))
	}
	fmt.Fprintf(&b, "==== stats ====\n%+v\n", r.Stats)
	b.WriteString("==== issues ====\n")
	for _, i := range r.Issues {
		b.WriteString(i.String() + "\n")
	}
	return b.String()
}

func TestLimits(t *testing.T) {
	var md strings.Builder
	md.WriteString("| " + strings.Repeat("a | ", 21) + "\n|" + strings.Repeat("---|", 21) + "\n")
	for i := 0; i < 600; i++ {
		md.WriteString("\nparagraph\n")
	}
	s := Measure(md.String())
	msgs := DefaultLimits.Check(s)
	if len(msgs) != 2 {
		t.Fatalf("want blocks and columns errors, got %v (stats %+v)", msgs, s)
	}
	if s.Columns != 21 {
		t.Errorf("columns = %d, want 21", s.Columns)
	}
}

func TestMeasureCountsTextNotSyntax(t *testing.T) {
	s := Measure("**bold** [link](https://example.com/very/long/url)\n")
	if s.Chars != len("bold link") {
		t.Errorf("chars = %d, want %d", s.Chars, len("bold link"))
	}
	if s.Nesting != 2 {
		t.Errorf("nesting = %d, want 2", s.Nesting)
	}
}

func TestClosingMath(t *testing.T) {
	cases := map[string]int{
		"$x$":          3,
		"$x$ tail":     3,
		"$ x$":         0,
		"$x $":         0,
		"$$x$$":        5,
		`$a\$b$`:       6,
		"$5":           0,
		"$x$$y":        0,
		"$\\frac a b$": 11,
	}
	for in, want := range cases {
		if got := closingMath(in); got != want {
			t.Errorf("closingMath(%q) = %d, want %d", in, got, want)
		}
	}
}

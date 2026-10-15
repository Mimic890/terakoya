package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Mimic890/terakoya/internal/content"
)

func TestExamplesAreClean(t *testing.T) {
	issues, err := Run("../../examples/content")
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range issues {
		t.Error(i)
	}
}

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestFindsProblems(t *testing.T) {
	root := writeTree(t, map[string]string{
		"geometry/_subject.yml":        "title: Geometry\n",
		"geometry/7/01-a.md":           "---\ntitle: A\nid: same\n---\nSee [[Missing]].\n",
		"geometry/7/02-b.md":           "---\ntitle: B\nid: same\n---\nBody\n",
		"geometry/7/03-no-title.md":    "---\norder: 3\n---\nBody\n",
		"geometry/7/04-no-fm.md":       "Body\n",
		"geometry/7/05-треугольник.md": "---\ntitle: Cyrillic name\n---\nBody\n",
		"geometry/7/06-bad-id.md":      "---\ntitle: Bad\nid: has space\n---\nBody\n",
		"geometry/7/07-math.md":        "---\ntitle: Math\nfoo: bar\n---\n$$\nx\n",
		"geometry/7/08-dup.md":         "---\ntitle: Shared\n---\n",
		"geometry/8/01-dup.md":         "---\ntitle: Shared\n---\n",
		"geometry/7/assets/unused.png": "png",
		"physics/7/01-x.md":            "---\ntitle: X\n---\n",
		"geometry/7/09-broken.md":      "---\ntitle: [unclosed\n---\n",
	})
	issues, err := Run(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"geometry/7/01-a.md:5: error: link [[Missing]] does not match any article",
		"geometry/7/02-b.md:1: error: id \"same\" is already used by geometry/7/01-a.md",
		"geometry/7/03-no-title.md:1: error: frontmatter title is required",
		"geometry/7/04-no-fm.md:1: error: frontmatter with a title is required",
		"geometry/7/05-треугольник.md:1: error: derived id",
		"geometry/7/06-bad-id.md:1: error: id \"has space\" must be",
		"geometry/7/07-math.md:1: warning: unknown frontmatter key \"foo\"",
		"geometry/7/07-math.md:5: error: unterminated $$ block",
		"geometry/7/09-broken.md:1: error: invalid frontmatter",
		"geometry/7/assets/unused.png: warning: image is not used by any article",
		"geometry/8/01-dup.md:1: warning: title or alias is shared with geometry/7/08-dup.md",
		"physics: warning: directory has no _subject.yml",
	}
	got := make([]string, len(issues))
	for i, is := range issues {
		got[i] = is.String()
	}
	for _, w := range want {
		found := false
		for _, g := range got {
			if strings.HasPrefix(g, w) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing issue %q", w)
		}
	}
	if !content.HasErrors(issues) {
		t.Error("expected errors")
	}
	if t.Failed() {
		t.Logf("got:\n%s", strings.Join(got, "\n"))
	}
}

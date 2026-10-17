package lint

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Mimic890/terakoya/internal/content"
)

// fakeRaster fails on SVGs containing "broken".
type fakeRaster struct{}

func (fakeRaster) Tag() string { return "fake" }
func (fakeRaster) Rasterize(_ context.Context, p string) ([]byte, error) {
	data, _ := os.ReadFile(p)
	if strings.Contains(string(data), "broken") {
		return nil, errors.New("resvg: unexpected end of stream")
	}
	return []byte("\x89PNG"), nil
}

func TestExampleIsClean(t *testing.T) {
	issues, err := Run(context.Background(), "../../examples/vault", fakeRaster{})
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
	long := "# Длинная\n\n" + strings.Repeat("## Раздел\n\n"+strings.Repeat("слово ", 2000)+"\n\n", 4)
	root := writeTree(t, map[string]string{
		"Г/7 класс/01 А/А.md":               "# А\n\n[битая](Нет.md), [общая](Общая.md), [[Общая]], [по пути](теоремы/Общая.md)\n\n![](рис.svg)\n\n![](плохой.svg)\n",
		"Г/7 класс/01 А/теоремы/Общая.md":   "Без заголовка.\n",
		"Г/7 класс/01 А/_assets/рис.svg":    "<svg/>",
		"Г/7 класс/01 А/_assets/плохой.svg": "<svg broken",
		"Г/7 класс/01 А/_assets/лишний.png": "png",
		"Г/7 класс/01 Б/Б.md":               "# Б\n",
		"Г/7 класс/01 Б/теоремы/Общая.md":   "# Общая Б\n",
		"Г/7 класс/02 Пустая/_assets/x.png": "png",
		"Г/7 класс/03 Две/Одна.md":          "# Одна\n",
		"Г/7 класс/03 Две/Другая.md":        "# Другая\n",
		"Г/7 класс/04 Длинная/Длинная.md":   long,
		"Г/7 класс/notes.docx":              "x",
		".obsidian/app.json":                "{}",
	})
	issues, err := Run(context.Background(), root, fakeRaster{})
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, i := range issues {
		b.WriteString(i.String() + "\n")
	}
	got := b.String()
	for _, want := range []string{
		"Г/7 класс/01 А/А.md:3: error: link to Нет.md: not found",
		"Г/7 класс/01 А/А.md:3: error: link to Общая.md: ambiguous",
		"Г/7 класс/01 А/_assets/плохой.svg: error: cannot rasterize: resvg: unexpected end of stream",
		"Г/7 класс/01 А/_assets/лишний.png: warning: picture is not shown by any note",
		"Г/7 класс/01 А/теоремы/Общая.md: warning: note has no title line",
		"Г/7 класс/01 Б: error: topic number 1 is also used by «01 А»",
		"Г/7 класс/02 Пустая: error: topic folder has no main page",
		"Г/7 класс/03 Две: error: topic folder has 2 .md files in its root",
		"Г/7 класс/04 Длинная/Длинная.md: info: too long for one message; sent in",
		"Г/7 класс/notes.docx: warning: unexpected file type",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// A path link to a duplicated name is fine; dot folders are ignored.
	for _, bad := range []string{"теоремы/Общая.md: not found", ".obsidian", "рис.svg: error"} {
		if strings.Contains(got, bad) {
			t.Errorf("unexpected %q in:\n%s", bad, got)
		}
	}
	if !content.HasErrors(issues) {
		t.Error("no errors")
	}
}

func TestSkipsSVGWithoutRasterizer(t *testing.T) {
	root := writeTree(t, map[string]string{"Г/7 класс/01 А/А.md": "# А\n\n![](a.svg)\n", "Г/7 класс/01 А/_assets/a.svg": "<svg/>"})
	issues, _ := Run(context.Background(), root, nil)
	if len(issues) != 1 || issues[0].Severity != content.Info || !strings.Contains(issues[0].Msg, "not checked") {
		t.Errorf("issues = %v", issues)
	}
}

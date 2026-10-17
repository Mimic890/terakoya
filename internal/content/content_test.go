package content

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const topicDir = "Геометрия/10 класс/05 Параллельность прямой и плоскости"

func TestLoadExample(t *testing.T) {
	cat, issues, err := Load("../../examples/vault")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) > 0 {
		t.Fatalf("issues: %v", issues)
	}
	if len(cat.Subjects) != 1 || cat.Subjects[0].Name != "Геометрия" {
		t.Fatalf("subjects = %+v", cat.Subjects)
	}
	g := cat.Subjects[0].Grades[0]
	if g.Number != 10 || g.Title() != "10 класс" || cat.Grade(g.ID) != g || cat.Subject(cat.Subjects[0].ID) != cat.Subjects[0] {
		t.Fatalf("grade = %+v", g)
	}
	tp := g.Topics[0]
	if tp.Number != 5 || tp.Width != 2 || tp.Title() != "Параллельность прямой и плоскости" || !tp.Main.IsMain() {
		t.Errorf("topic = %+v", tp)
	}
	var got []string
	for _, n := range tp.Notes {
		s := n.KindLabel() + ": " + n.Title
		if n.Parent != nil {
			s = "↳ " + s + " ← " + n.Parent.Title
		}
		got = append(got, s)
	}
	want := []string{
		"Теорема: Признак параллельности прямой и плоскости",
		"Теорема: Прямая пересечения плоскостей",
		"↳ Следствие: Следствие о прямой в плоскости ← Прямая пересечения плоскостей",
		"Задача: Сечение призмы через ребро и точку",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("notes:\n%s", strings.Join(got, "\n"))
	}
	// Hidden files are not part of the vault.
	if cat.HasFile(".obsidian/app.json") || cat.HasFile(".gitignore") {
		t.Error("hidden files indexed")
	}
	n := cat.NoteByPath(topicDir + "/теоремы/Признак параллельности прямой и плоскости (5.1).md")
	if n == nil || cat.Note(n.ID) != n || len(n.ID) != 11 {
		t.Fatalf("note by path/ID: %+v", n)
	}
}

// vault writes files (path → content) into a temporary vault.
func vault(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for p, data := range files {
		abs := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestNumberPrefixes(t *testing.T) {
	root := vault(t, map[string]string{
		"Алгебра/7 класс/Без номера/Без номера.md":      "# Без номера\n",
		"Алгебра/7 класс/Аа без номера/Аа.md":           "# Аа\n",
		"Алгебра/7 класс/1234 Последняя/Последняя.md":   "# Последняя\n",
		"Алгебра/7 класс/005_Пятая/Пятая.md":            "# Пятая\n",
		"Алгебра/7 класс/5 Тоже пять/Тоже пять.md":      "# Тоже пять\n",
		"Алгебра/7 класс/12-Двенадцатая/Двенадцатая.md": "# Двенадцатая\n",
		"Алгебра/7 класс/02.Вторая/02 Вторая.md":        "# Вторая\n",
		"Алгебра/11 класс/01 Тема/Тема.md":              "# Тема\n",
		"Алгебра/9 класс/01 Тема/Тема.md":               "# Тема\n",
		"Алгебра/Прочее/x.md":                           "x",
		"Алгебра/Readme.md":                             "x",
		"Алгебра/7 класс/notes.txt":                     "x",
		"Геометрия/7 класс/01 Тема/Тема.md":             "# Тема\n",
		".obsidian/workspace.json":                      "{}",
		".trash/old.md":                                 "x",
	})
	cat, issues, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Subjects) != 2 || cat.Subjects[0].Name != "Алгебра" {
		t.Fatalf("subjects = %v", cat.Subjects)
	}
	var grades []int
	for _, g := range cat.Subjects[0].Grades {
		grades = append(grades, g.Number)
	}
	if len(grades) != 3 || grades[0] != 7 || grades[1] != 9 || grades[2] != 11 {
		t.Errorf("grades = %v", grades)
	}
	var topics []string
	for _, tp := range cat.Subjects[0].Grades[0].Topics {
		topics = append(topics, tp.Title())
	}
	want := "Вторая,Пятая,Тоже пять,Двенадцатая,Последняя,Аа,Без номера"
	if strings.Join(topics, ",") != want {
		t.Errorf("topics = %s", strings.Join(topics, ","))
	}
	msgs := issueText(issues)
	for _, w := range []string{
		"Алгебра/7 класс/5 Тоже пять: error: topic number 5 is also used by «005_Пятая»",
		"Алгебра/7 класс/Без номера: warning: topic folder has no number",
		"Алгебра/Прочее: warning: unexpected folder in a subject",
		"Алгебра/Readme.md: warning: unexpected file in a subject folder",
		"Алгебра/7 класс/notes.txt: warning: unexpected file type",
	} {
		if !strings.Contains(msgs, w) {
			t.Errorf("issues miss %q:\n%s", w, msgs)
		}
	}
	if strings.Contains(msgs, ".obsidian") || strings.Contains(msgs, ".trash") {
		t.Errorf("hidden files reported:\n%s", msgs)
	}
	// Renumbering a topic keeps its IDs.
	if ID("А/7 класс/05 Тема/Тема.md") != ID("А/7 класс/6 Тема/Тема.md") || ID("А/x.md") == ID("А/y.md") {
		t.Error("IDs depend on numbers or ignore names")
	}
}

func issueText(issues []Issue) string {
	var b strings.Builder
	for _, i := range issues {
		b.WriteString(i.String() + "\n")
	}
	return b.String()
}

func TestTopicProblemsAndTitles(t *testing.T) {
	root := vault(t, map[string]string{
		"Г/8 класс/01 Пустая/_assets/a.png":                  "png",
		"Г/8 класс/02 Две/Первая.md":                         "# Первая\n",
		"Г/8 класс/02 Две/Вторая.md":                         "# Вторая\n",
		"Г/8 класс/03 Тема/03 Тема.md":                       "Без заголовка.\n",
		"Г/8 класс/03 Тема/задачи/Про углы (задача 10).md":   "```\n# не заголовок\n```\n# Про углы\n",
		"Г/8 класс/03 Тема/задачи/Про стороны (задача 2).md": "# Про стороны\n",
		"Г/8 класс/03 Тема/задачи/Без номера.md":             "# Без номера\n",
		"Г/8 класс/03 Тема/леммы/Лемма (5.10).md":            "# Лемма X\n",
		"Г/8 класс/03 Тема/леммы/Лемма (5.2).md":             "# Лемма Y\n",
		"Г/8 класс/03 Тема/картинка.png":                     "png",
	})
	cat, issues, _ := Load(root)
	msgs := issueText(issues)
	for _, w := range []string{
		"Г/8 класс/01 Пустая: error: topic folder has no main page",
		"Г/8 класс/02 Две: error: topic folder has 2 .md files in its root",
		"Г/8 класс/03 Тема/03 Тема.md: warning: note has no title line",
		"Г/8 класс/03 Тема/картинка.png: warning: unexpected picture in the topic folder",
	} {
		if !strings.Contains(msgs, w) {
			t.Errorf("issues miss %q:\n%s", w, msgs)
		}
	}
	g := cat.Subjects[0].Grades[0]
	if len(g.Topics) != 2 {
		t.Fatalf("topics = %d", len(g.Topics))
	}
	tp := g.Topics[1]
	if tp.Main.Title != "Тема" {
		t.Errorf("fallback title = %q", tp.Main.Title)
	}
	var got []string
	for _, n := range tp.Notes {
		got = append(got, n.KindLabel()+": "+n.Title)
	}
	// Unlinked notes: by the numbers in "(…)", then by name.
	want := "Задача: Про стороны,Леммы: Лемма Y,Леммы: Лемма X,Задача: Про углы,Задача: Без номера"
	if strings.Join(got, ",") != want {
		t.Errorf("order = %s", strings.Join(got, ","))
	}
	if TitleFromFile("05 Признак (5.1).md") != "Признак" || TitleFromFile("(1).md") != "(1)" {
		t.Error("TitleFromFile")
	}
}

func TestResolve(t *testing.T) {
	root := vault(t, map[string]string{
		"Г/7 класс/01 А/А.md":                        "# А\n",
		"Г/7 класс/01 А/теоремы/Общая.md":            "# Общая А\n",
		"Г/7 класс/01 А/теоремы/Уникальная (1.1).md": "# Уникальная\n",
		"Г/7 класс/01 А/_assets/рис.svg":             "<svg/>",
		"Г/7 класс/01 А/_assets/общий.png":           "png",
		"Г/7 класс/02 Б/Б.md":                        "# Б\n",
		"Г/7 класс/02 Б/теоремы/Общая.md":            "# Общая Б\n",
		"Г/7 класс/02 Б/_assets/общий.png":           "png",
		"Г/7 класс/02 Б/_assets/только-б.png":        "png",
	})
	cat, _, _ := Load(root)
	a := cat.NoteByPath("Г/7 класс/01 А/А.md")
	cases := []struct {
		link string
		want string // path or error
	}{
		{"[x](Уникальная%20%281.1%29.md)", "Г/7 класс/01 А/теоремы/Уникальная (1.1).md"},
		{"[x](Уникальная%20(1.1).md)", "Г/7 класс/01 А/теоремы/Уникальная (1.1).md"},
		{"[x](<Уникальная (1.1).md>)", "Г/7 класс/01 А/теоремы/Уникальная (1.1).md"},
		{"[x](уникальная%20%281.1%29.md#Доказательство)", "Г/7 класс/01 А/теоремы/Уникальная (1.1).md"},
		{"[[Уникальная (1.1)]]", "Г/7 класс/01 А/теоремы/Уникальная (1.1).md"},
		{"[[Уникальная (1.1)|так]]", "Г/7 класс/01 А/теоремы/Уникальная (1.1).md"},
		{"[x](Общая.md)", "ambiguous"},
		{"[[Общая]]", "ambiguous"},
		{"[x](теоремы/Общая.md)", "Г/7 класс/01 А/теоремы/Общая.md"},
		{"[x](Г/7%20класс/02%20Б/теоремы/Общая.md)", "Г/7 класс/02 Б/теоремы/Общая.md"},
		{"[x](../02%20Б/Б.md)", "Г/7 класс/02 Б/Б.md"},
		{"[x](Нет.md)", "not found"},
		{"[x](теоремы/Нет.md)", "not found"},
	}
	for _, c := range cases {
		l, ok := LinkAt(c.link)
		if !ok || l.Len != len(c.link) {
			t.Errorf("LinkAt(%q) = %+v %v", c.link, l, ok)
			continue
		}
		n, err := cat.ResolveNote(a, l)
		got := ""
		switch {
		case err == ErrAmbiguous:
			got = "ambiguous"
		case err != nil:
			got = "not found"
		default:
			got = n.Path
		}
		if got != c.want {
			t.Errorf("%s → %s, want %s", c.link, got, c.want)
		}
	}
	// Images: the topic's _assets first, then a unique name.
	b := cat.NoteByPath("Г/7 класс/02 Б/Б.md")
	for link, want := range map[string]string{
		"![](общий.png)":    "Г/7 класс/02 Б/_assets/общий.png",
		"![](рис.svg)":      "Г/7 класс/01 А/_assets/рис.svg",
		"![](только-б.png)": "Г/7 класс/02 Б/_assets/только-б.png",
		"![[рис.svg]]":      "Г/7 класс/01 А/_assets/рис.svg",
	} {
		l, _ := LinkAt(link)
		if got, err := cat.ResolveImage(b, l); err != nil || got != want {
			t.Errorf("%s → %s %v, want %s", link, got, err, want)
		}
	}
	l, _ := LinkAt("![](общий.png)")
	if _, err := cat.ResolveImage(cat.NoteByPath("Г/7 класс/01 А/теоремы/Общая.md"), l); err != nil {
		t.Errorf("image in own _assets: %v", err)
	}
}

func TestLinkAt(t *testing.T) {
	for in, want := range map[string]Link{
		"![](Чертёж%20—%20сечение%20призмы%20(задача%203).svg) хвост": {Target: "Чертёж%20—%20сечение%20призмы%20(задача%203).svg", Image: true},
		"[текст [в скобках]](a.md \"T\")":                             {Text: "текст [в скобках]", Target: "a.md", Title: "T"},
		"[[a\\|b]]": {Text: "b", Target: "a", Wiki: true},
	} {
		got, ok := LinkAt(in)
		want.Len = len(strings.TrimSuffix(in, " хвост"))
		if !ok || got != want {
			t.Errorf("LinkAt(%q) = %+v, want %+v", in, got, want)
		}
	}
	for _, bad := range []string{"[a](b", "[a] (b)", "[[a", "[a](b \"t)"} {
		if _, ok := LinkAt(bad); ok {
			t.Errorf("LinkAt(%q) accepted", bad)
		}
	}
	links := Links("`[x](a.md)` [y](b.md)\n```\n[z](c.md)\n```\n![](d.svg)")
	if len(links) != 2 || links[0].Target != "b.md" || links[1].Target != "d.svg" {
		t.Errorf("Links = %+v", links)
	}
}

func TestSplitBacklink(t *testing.T) {
	cases := map[string][2]string{
		"Текст.\n\n---\n↩ [Тема](Тема.md)\n":    {"Текст.", "\n\n---\n↩ [Тема](Тема.md)\n"},
		"Текст.\n---\n↩ [А](А.md)\n↩ [Б](Б.md)": {"Текст.", "\n---\n↩ [А](А.md)\n↩ [Б](Б.md)"},
		"Текст.\n\n---\n\nЕщё текст.\n":         {"Текст.\n\n---\n\nЕщё текст.\n", ""},
		"Текст.\n\n↩ [Тема](Тема.md)\n":         {"Текст.\n\n↩ [Тема](Тема.md)\n", ""},
		"---\n↩ [Тема](Тема.md)":                {"", "---\n↩ [Тема](Тема.md)"},
	}
	for in, want := range cases {
		body, tail := SplitBacklink(in)
		if body != want[0] || tail != want[1] || body+tail != in {
			t.Errorf("SplitBacklink(%q) = %q, %q", in, body, tail)
		}
	}
}

func TestSafeFileName(t *testing.T) {
	for in, want := range map[string]string{
		"Теорема: о «трёх» точках?": "Теорема о «трёх» точках",
		"  a/b\\c  ":        "abc",
		"Конец...":          "Конец",
		"Сумма углов (5.3)": "Сумма углов (5.3)",
	} {
		if got := SafeFileName(in); got != want {
			t.Errorf("SafeFileName(%q) = %q, want %q", in, got, want)
		}
	}
}

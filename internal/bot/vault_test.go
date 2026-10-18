package bot

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Mimic890/terakoya/internal/i18n"
)

func ids(calls []call, method string) []string {
	var out []string
	for _, c := range calls {
		if c.method == method {
			out = append(out, c.params["message_id"])
		}
	}
	return out
}

// longNote has three sections that each fill most of a message.
func longNote() string {
	var b strings.Builder
	b.WriteString("# Длинная\n\nСм. [треугольники](Треугольники.md).\n\n")
	for _, h := range []string{"Первый", "Второй", "Третий"} {
		b.WriteString("## " + h + "\n\n")
		for i := 0; i < 40; i++ {
			b.WriteString(strings.Repeat("длинный текст раздела ", 20) + "\n\n")
		}
	}
	return b.String()
}

func TestMultiPartNote(t *testing.T) {
	long := "Геометрия/7 класс/03 Длинная/Длинная.md"
	h := newHarness(t, map[string]string{long: longNote()})

	h.tapOn(500, "a:"+h.id(long))
	calls := h.tg.take()
	var parts []call
	for _, c := range calls {
		if c.method == "sendRichMessage" {
			parts = append(parts, c)
		}
	}
	if len(parts) < 3 {
		t.Fatalf("parts = %d", len(parts))
	}
	for i, p := range parts {
		md := p.params["rich_message"]
		hasKB := p.params["reply_markup"] != ""
		if hasKB != (i == len(parts)-1) {
			t.Errorf("part %d keyboard = %v", i+1, hasKB)
		}
		if i > 0 && !strings.Contains(md, "# Длинная (продолжение)") {
			t.Errorf("part %d has no continuation title", i+1)
		}
	}
	if got := ids(calls, "deleteMessage"); len(got) != 1 || got[0] != "500" {
		t.Errorf("topic list not replaced: %v", got)
	}

	// «Назад» under the last part removes every part and shows the topic
	// list.
	group, _ := h.store.MessageGroup(h.ctx, 42, firstPart(t, h))
	if len(group) != len(parts) {
		t.Fatalf("group = %v", group)
	}
	h.tapOn(group[len(group)-1], "b:"+h.id(long))
	calls = h.tg.take()
	if got := ids(calls, "deleteMessage"); len(got) != len(parts) {
		t.Errorf("deleted %v, want all %d parts", got, len(parts))
	}
	if s := texts(calls, "sendMessage"); len(s) != 1 || !strings.HasPrefix(s[0], "Геометрия · 7 класс") {
		t.Errorf("topic list = %q", s)
	}

	// A link button in the first part also replaces the whole group.
	h.tapOn(1, "a:"+h.id(long))
	h.tg.take()
	group, _ = h.store.MessageGroup(h.ctx, 42, firstPart(t, h))
	h.tapOn(group[0], "a:"+h.id(triangles))
	if got := ids(h.tg.take(), "deleteMessage"); len(got) != len(group) {
		t.Errorf("link from part 1 deleted %v, want %d", got, len(group))
	}
}

// firstPart returns the first message of the last group sent.
func firstPart(t *testing.T, h *harness) int {
	t.Helper()
	for id := 2000; id > 1000; id-- {
		if g, _ := h.store.MessageGroup(h.ctx, 42, id); len(g) > 1 {
			return g[0]
		}
	}
	t.Fatal("no group")
	return 0
}

func TestBackFromNoteAndTopic(t *testing.T) {
	h := newHarness(t, nil)
	thm := vtopic + "/теоремы/Признак параллельности прямой и плоскости (5.1).md"
	h.tapOn(300, "b:"+h.id(thm))
	calls := h.tg.take()
	rich := texts(calls, "sendRichMessage")
	if len(rich) != 1 || len(ids(calls, "deleteMessage")) != 1 {
		t.Fatalf("back from a theorem: %+v", calls)
	}
	var md string
	for _, c := range calls {
		if c.method == "sendRichMessage" {
			md = c.params["rich_message"]
		}
	}
	if !strings.Contains(md, "# Параллельность прямой и плоскости") || strings.Contains(md, "↩") {
		t.Errorf("main page = %s", md)
	}
	h.tapOn(301, "b:"+h.id(vmain))
	if s := texts(h.tg.take(), "sendMessage"); len(s) != 1 || !strings.Contains(s[0], "10 класс") {
		t.Errorf("back from the main page = %q", s)
	}
}

// countingRaster counts conversions.
type countingRaster struct {
	mu    sync.Mutex
	calls int
}

func (r *countingRaster) Tag() string { return "count" }
func (r *countingRaster) Rasterize(context.Context, string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	return []byte("\x89PNG"), nil
}

func TestSVGConvertedOnce(t *testing.T) {
	h := newHarness(t, nil)
	ras := &countingRaster{}
	h.bot.media.Raster = ras
	h.bot.media.CacheChatID = -100
	thm := "a:" + h.id(vtopic+"/теоремы/Признак параллельности прямой и плоскости (5.1).md")

	h.tapOn(1, thm)
	calls := h.tg.take()
	if ras.calls != 1 || len(texts(calls, "sendPhoto")) != 1 {
		t.Fatalf("first send: conversions %d, uploads %d", ras.calls, len(texts(calls, "sendPhoto")))
	}
	h.tapOn(2, thm)
	calls = h.tg.take()
	if ras.calls != 1 || len(texts(calls, "sendPhoto")) != 0 {
		t.Errorf("second send: conversions %d, uploads %d", ras.calls, len(texts(calls, "sendPhoto")))
	}
	for _, c := range calls {
		if c.method == "sendRichMessage" && !strings.Contains(c.params["rich_message"], "photo") {
			t.Errorf("image missing: %v", c.params["rich_message"])
		}
	}
}

func TestNewNoteAndCorollary(t *testing.T) {
	h := newHarness(t, nil)
	h.bot.cfg.SuggestCooldown = 0
	h.tap("e:" + h.id(vmain))
	h.tap(cbAddNote)
	wantPrompt(t, h.tg.take(), i18n.RU.ChooseKind, "Теорема", "Аксиома", "Определение", "Задача", "Следствие", "Отмена")
	h.tap(cbKind + "3") // задачи
	wantPrompt(t, h.tg.take(), "Новая запись: Задача, тема «Параллельность прямой и плоскости».\n\nНапишите её название.", "Отмена")
	h.say("Ещё одна задача")
	h.say("Условие задачи.")
	h.photo("ris", "к условию")
	h.tap(cbSend)
	if s := texts(h.tg.take(), "editMessageText"); len(s) != 1 || s[0] != "Спасибо! Мы проверим «Ещё одна задача» и добавим в тему. Я напишу, когда это случится." {
		t.Errorf("outcome = %q", s)
	}
	pr := h.host.prs[0]
	files := git(t, h.remote, "diff", "--name-only", pr.Head+"^", pr.Head)
	for _, want := range []string{vtopic + "/задачи/Ещё одна задача.md", vmain, vtopic + "/_assets/eshche-odna-zadacha-"} {
		if !strings.Contains(files, want) {
			t.Errorf("files miss %q:\n%s", want, files)
		}
	}
	if !strings.Contains(h.file(pr.Head, vmain), "- [Ещё одна задача](Ещё%20одна%20задача.md)") ||
		pr.Title != "Suggest new задача: Ещё одна задача" || !strings.Contains(pr.Body, "Also changed: `"+vmain+"`") {
		t.Errorf("PR %s\n%s", pr.Title, pr.Body)
	}

	// A corollary: pick the theorem it belongs to.
	h.tap("e:" + h.id(vmain))
	h.tap(cbAddNote)
	h.tap(cbKind + "4") // следствия
	calls := h.tg.take()
	wantPrompt(t, calls, i18n.RU.ChooseParent, "Теорема: Признак параллельности прямой и плоскости", "Задача: Сечение призмы через ребро и точку")
	h.tap(cbParent + h.id(vtopic+"/теоремы/Признак параллельности прямой и плоскости (5.1).md"))
	h.say("Новое следствие")
	h.say("Текст.")
	h.tap(cbSend)
	h.tg.take()
	pr = h.host.prs[1]
	page := h.file(pr.Head, vmain)
	if !strings.Contains(page, "\\Rightarrow a \\parallel \\alpha$\n\t- [Новое следствие](Новое%20следствие.md)\n- [Прямая") {
		t.Errorf("main page:\n%s", page)
	}
	if got := h.file(pr.Head, vtopic+"/теоремы/следствия/Новое следствие.md"); !strings.HasSuffix(got, "↩ [Параллельность прямой и плоскости](Параллельность%20прямой%20и%20плоскости.md)") {
		t.Errorf("corollary:\n%s", got)
	}
}

func TestDownloadArchive(t *testing.T) {
	h := newHarness(t, nil)
	h.say("/download")
	calls := h.tg.take()
	// The command itself is deleted after the answer.
	if len(calls) != 2 || calls[0].method != "sendDocument" || calls[1].method != "deleteMessage" ||
		calls[1].params["message_id"] != fmt.Sprint(h.msgID) {
		t.Fatalf("calls = %+v", calls)
	}
	// The fake keeps no files; build the archive the same way to check it.
	data, err := zipVault(h.bot.catalog().Root)
	if err != nil {
		t.Fatal(err)
	}
	zr, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	all := strings.Join(names, "\n")
	if !strings.Contains(all, vmain) || strings.Contains(all, ".obsidian") || strings.Contains(all, ".git/") || strings.Contains(all, ".gitignore") {
		t.Errorf("archive:\n%s", all)
	}
	// The second request reuses the uploaded file.
	h.say("/download")
	calls = h.tg.take()
	if len(calls) != 2 || !strings.HasPrefix(calls[0].params["document"], "doc") {
		t.Errorf("second download = %+v", calls)
	}
}

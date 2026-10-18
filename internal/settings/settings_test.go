package settings

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func logger(buf *bytes.Buffer) *slog.Logger {
	var w io.Writer = io.Discard
	if buf != nil {
		w = buf
	}
	return slog.New(slog.NewTextHandler(w, nil))
}

func TestCreatedWithDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	f, err := Open(path, []string{"Геометрия"}, "", logger(nil))
	if err != nil {
		t.Fatal(err)
	}
	s := f.Get()
	if len(s.Ranks) != 8 || s.Ranks[0].Title != "Novice" || s.Ranks[7].From != 100 {
		t.Errorf("ranks = %+v", s.Ranks)
	}
	if id, ok := s.Emoji.Subjects["Геометрия"]; !ok || id != "" {
		t.Errorf("subjects = %v", s.Emoji.Subjects)
	}
	data, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(data), "# terakoya settings.") || !strings.Contains(string(data), "title: Sage") {
		t.Errorf("file:\n%s", data)
	}
	// The file reads back as it was written.
	again, err := Load(path)
	if err != nil || len(again.Ranks) != 8 || again.Emoji.Subjects == nil {
		t.Fatalf("reload = %+v, %v", again, err)
	}
}

func TestLegacyRanks(t *testing.T) {
	var log bytes.Buffer
	path := filepath.Join(t.TempDir(), FileName)
	f, err := Open(path, nil, "a:0,b:2", logger(&log))
	if err != nil {
		t.Fatal(err)
	}
	if l := f.Get().Ladder(); len(l) != 2 || l[1].Title != "b" || l[1].From != 2 {
		t.Errorf("ladder = %+v", l)
	}
	if !strings.Contains(log.String(), "TERAKOYA_RANKS is deprecated") {
		t.Errorf("log = %s", log.String())
	}
	// Only once: an existing file wins.
	log.Reset()
	f, _ = Open(path, nil, "x:0", logger(&log))
	if f.Get().Ranks[0].Title != "a" || !strings.Contains(log.String(), "TERAKOYA_RANKS is ignored") {
		t.Errorf("second start: %+v, log %s", f.Get().Ranks, log.String())
	}
	if _, err := Open(filepath.Join(t.TempDir(), FileName), nil, "a:1", logger(nil)); err == nil {
		t.Error("an invalid TERAKOYA_RANKS was migrated")
	}
}

func TestUpdateAndReload(t *testing.T) {
	var log bytes.Buffer
	path := filepath.Join(t.TempDir(), FileName)
	f, _ := Open(path, nil, "", logger(&log))
	var got []Settings
	f.OnChange = func(s Settings) { got = append(got, s) }

	if err := f.Update(func(s *Settings) { s.Emoji.Pin = "123" }); err != nil {
		t.Fatal(err)
	}
	if f.Get().Emoji.Pin != "123" || len(got) != 1 {
		t.Errorf("after update: %+v, %d changes", f.Get().Emoji, len(got))
	}
	if loaded, _ := Load(path); loaded.Emoji.Pin != "123" {
		t.Error("not saved")
	}
	// An invalid update changes nothing.
	if err := f.Update(func(s *Settings) { s.Ranks[1].From = 0 }); err == nil {
		t.Error("invalid update accepted")
	}
	if f.Get().Ranks[1].From != 1 {
		t.Error("invalid update applied")
	}
	// The bot's own write is not a change to reload.
	f.Check()
	if len(got) != 1 {
		t.Errorf("reloaded its own write: %d changes", len(got))
	}

	// A hand edit is picked up; an invalid one is not.
	edit := func(text string) {
		os.WriteFile(path, []byte(text), 0o644)
		future := time.Now().Add(time.Duration(len(got)+1) * time.Second)
		os.Chtimes(path, future, future)
		f.Check()
	}
	edit("ranks:\n  - {title: Новичок, from: 0}\n  - {title: Знаток, from: 5, emoji: \"77\"}\nemoji:\n  pin: \"9\"\n")
	if s := f.Get(); len(got) != 2 || s.Ranks[1].Title != "Знаток" || s.Ranks[1].Emoji != "77" || s.Emoji.Pin != "9" {
		t.Errorf("hand edit: %+v", s)
	}
	for _, bad := range []string{
		"ranks:\n  - {title: a, from: 1}\n",
		"ranks:\n  - {title: a, from: 0}\n  - {title: b, from: 5}\n  - {title: c, from: 5}\n",
		"ranks:\n  - {title: a, from: 0}\nemoji:\n  pin: \"📌\"\n",
		"ranks: [\n",
		"ranks:\n  - {title: a, from: 0}\nunknown: 1\n",
		"topic_list_style: grid\nranks:\n  - {title: a, from: 0}\n",
	} {
		log.Reset()
		edit(bad)
		if s := f.Get(); s.Ranks[1].Title != "Знаток" || !strings.Contains(log.String(), "keeping the previous settings") {
			t.Errorf("bad edit %q applied: %+v (log %s)", bad, s.Ranks, log.String())
		}
	}
}

func TestInvalidFileOnStart(t *testing.T) {
	var log bytes.Buffer
	path := filepath.Join(t.TempDir(), FileName)
	os.WriteFile(path, []byte("ranks: []\n"), 0o644)
	f, err := Open(path, nil, "", logger(&log))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Get().Ranks) != 8 || !strings.Contains(log.String(), "using defaults") {
		t.Errorf("start with a bad file: %+v, log %s", f.Get().Ranks, log.String())
	}
	if data, _ := os.ReadFile(path); string(data) != "ranks: []\n" {
		t.Error("the bad file was overwritten")
	}
}

func TestTopicListStyle(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	f, _ := Open(path, nil, "", logger(nil))
	if got := f.Get().TopicList(); got != TopicListList {
		t.Errorf("default = %q", got)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), "topic_list_style: list") {
		t.Errorf("file:\n%s", data)
	}
	// Files from before the setting keep working, as lists.
	os.WriteFile(path, []byte("ranks:\n  - {title: a, from: 0}\n"), 0o644)
	s, err := Load(path)
	if err != nil || s.TopicList() != TopicListList {
		t.Errorf("old file = %q, %v", s.TopicList(), err)
	}
	if err := f.Update(func(s *Settings) { s.TopicListStyle = TopicListButtons }); err != nil || f.Get().TopicList() != TopicListButtons {
		t.Errorf("buttons = %q, %v", f.Get().TopicList(), err)
	}
}

func TestTopSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	f, _ := Open(path, nil, "", logger(nil))
	if got := f.Get().Top; got != DefaultTop() {
		t.Errorf("default = %+v", got)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), "top:\n  size: 20\n  medals: 3\n  rank_style: emoji\n  show_count: true\n") {
		t.Errorf("file:\n%s", data)
	}
	// Files from before the section get the defaults; a partial section
	// keeps the defaults of the keys it leaves out.
	os.WriteFile(path, []byte("ranks:\n  - {title: a, from: 0}\n"), 0o644)
	if s, err := Load(path); err != nil || s.Top != DefaultTop() {
		t.Errorf("old file = %+v, %v", s.Top, err)
	}
	os.WriteFile(path, []byte("ranks:\n  - {title: a, from: 0}\ntop:\n  size: 5\n  show_count: false\n"), 0o644)
	if s, err := Load(path); err != nil || s.Top != (Top{Size: 5, Medals: 3, RankStyle: RankEmoji}) {
		t.Errorf("partial = %+v, %v", s.Top, err)
	}
	for _, bad := range []string{"size: 0", "size: 101", "medals: 6", "medals: -1", "rank_style: big"} {
		os.WriteFile(path, []byte("ranks:\n  - {title: a, from: 0}\ntop:\n  "+bad+"\n"), 0o644)
		if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "top.") {
			t.Errorf("%s accepted: %v", bad, err)
		}
	}
	os.WriteFile(path, []byte("ranks:\n  - {title: a, from: 0}\nemoji:\n  top: [\"1\", \"2\", \"3\", \"4\", \"5\", \"6\"]\n"), 0o644)
	if _, err := Load(path); err == nil {
		t.Error("six medals accepted")
	}

	// Medals are set by place and read back.
	if err := f.Update(func(s *Settings) { s.Emoji.SetMedal(2, "777") }); err != nil {
		t.Fatal(err)
	}
	if e := f.Get().Emoji; e.Medal(1) != "" || e.Medal(2) != "777" || e.Medal(3) != "" || e.Medal(0) != "" {
		t.Errorf("medals = %q", e.Top)
	}
}

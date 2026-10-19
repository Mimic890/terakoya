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
	f, err := Open(path, []string{"Геометрия"}, logger(nil))
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

func TestUpdateAndReload(t *testing.T) {
	var log bytes.Buffer
	path := filepath.Join(t.TempDir(), FileName)
	f, _ := Open(path, nil, logger(&log))
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
	f, err := Open(path, nil, logger(&log))
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

func TestListStyle(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	f, _ := Open(path, nil, logger(nil))
	if got := f.Get().List(); got != ListList {
		t.Errorf("default = %q", got)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), "\nlist_style: list\n") {
		t.Errorf("file:\n%s", data)
	}
	// Files from before the setting keep working, as lists.
	os.WriteFile(path, []byte("ranks:\n  - {title: a, from: 0}\n"), 0o644)
	s, err := Load(path)
	if err != nil || s.List() != ListList {
		t.Errorf("old file = %q, %v", s.List(), err)
	}
	if err := f.Update(func(s *Settings) { s.ListStyle = ListButtons }); err != nil || f.Get().List() != ListButtons {
		t.Errorf("buttons = %q, %v", f.Get().List(), err)
	}
	if err := (Settings{ListStyle: "grid", Top: DefaultTop()}).Validate(); err == nil || !strings.Contains(err.Error(), "list_style") {
		t.Errorf("bad style: %v", err)
	}
}

func TestLegacyListStyleKey(t *testing.T) {
	for _, c := range []struct{ old, want string }{{"buttons", ListButtons}, {"list", ListList}, {`""`, ListList}} {
		path := filepath.Join(t.TempDir(), FileName)
		os.WriteFile(path, []byte("topic_list_style: "+c.old+"\nranks:\n  - {title: a, from: 0}\n"), 0o644)
		var log bytes.Buffer
		f, err := Open(path, nil, logger(&log))
		if err != nil || f.Get().List() != c.want {
			t.Fatalf("%s: style %q, %v", c.old, f.Get().List(), err)
		}
		// The file now has the new key only, with the same value.
		data, _ := os.ReadFile(path)
		if strings.Contains(string(data), "topic_list_style") || !strings.Contains(string(data), "\nlist_style: "+strings.Trim(c.old, `"`)) ||
			!strings.Contains(log.String(), "topic_list_style is now list_style") {
			t.Errorf("%s: file:\n%s\nlog: %s", c.old, data, log.String())
		}
		// The new key wins when a hand edit has both.
		os.WriteFile(path, []byte("list_style: buttons\ntopic_list_style: list\nranks:\n  - {title: a, from: 0}\n"), 0o644)
		if s, err := Load(path); err != nil || s.List() != ListButtons {
			t.Errorf("both keys = %q, %v", s.List(), err)
		}
	}
}

func TestTopSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	f, _ := Open(path, nil, logger(nil))
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

func TestRanksSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	f, _ := Open(path, nil, logger(nil))
	if !f.Get().HideFutureRanks {
		t.Error("hide_future off by default")
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), "\nranks:\n  hide_future: true\n  ladder:\n    - title: Novice\n") {
		t.Errorf("file:\n%s", data)
	}
	// The list form of earlier files still loads, hiding future ranks.
	os.WriteFile(path, []byte("ranks:\n  - {title: a, from: 0}\n  - {title: b, from: 2}\n"), 0o644)
	if s, err := Load(path); err != nil || !s.HideFutureRanks || len(s.Ranks) != 2 || s.Ranks[1].From != 2 {
		t.Errorf("list form = %+v, %v", s, err)
	}
	os.WriteFile(path, []byte("ranks:\n  hide_future: false\n  ladder:\n    - {title: a, from: 0, emoji: \"5\"}\n"), 0o644)
	if s, err := Load(path); err != nil || s.HideFutureRanks || s.Ranks[0].Emoji != "5" {
		t.Errorf("section = %+v, %v", s, err)
	}
	os.WriteFile(path, []byte("ranks:\n  ladder:\n    - {title: a, from: 0}\n"), 0o644)
	if s, err := Load(path); err != nil || !s.HideFutureRanks {
		t.Errorf("without hide_future = %+v, %v", s, err)
	}
	for name, bad := range map[string]string{
		"unknown key":      "ranks:\n  hide: true\n  ladder:\n    - {title: a, from: 0}\n",
		"unknown rank key": "ranks:\n  ladder:\n    - {title: a, form: 0}\n",
		"unknown in list":  "ranks:\n  - {title: a, from: 0, icon: x}\n",
		"not a list":       "ranks:\n  ladder: a\n",
		"no ranks":         "ranks:\n  hide_future: true\n",
	} {
		os.WriteFile(path, []byte(bad), 0o644)
		if _, err := Load(path); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	// Saved settings keep the flag.
	if err := f.Update(func(s *Settings) { s.HideFutureRanks = false }); err != nil {
		t.Fatal(err)
	}
	if s, err := Load(path); err != nil || s.HideFutureRanks {
		t.Errorf("after update = %+v, %v", s.HideFutureRanks, err)
	}
}

func TestBackupSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	f, _ := Open(path, nil, logger(nil))
	if got := f.Get().Backup; got != DefaultBackup() || got != (Backup{Mode: "folder", Time: "03:00", Keep: 7}) {
		t.Errorf("default = %+v", got)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), "backup:\n  mode: folder\n  time: \"03:00\"\n  keep: 7\n") {
		t.Errorf("file:\n%s", data)
	}
	// A file without the section gets the defaults; a partial one keeps
	// the rest of them.
	os.WriteFile(path, []byte("ranks:\n  - {title: a, from: 0}\nbackup:\n  mode: archive\n"), 0o644)
	if s, err := Load(path); err != nil || s.Backup != (Backup{Mode: "archive", Time: "03:00", Keep: 7}) {
		t.Errorf("partial = %+v, %v", s.Backup, err)
	}
	for _, c := range []struct{ yaml, err string }{
		{"mode: weekly", "backup.mode"},
		{"time: \"3:00\"", "backup.time"},
		{"time: \"24:00\"", "backup.time"},
		{"time: \"03:60\"", "backup.time"},
		{"keep: 0", "backup.keep"},
		{"keep: 366", "backup.keep"},
		{"every: 2h", "field every not found"},
	} {
		os.WriteFile(path, []byte("ranks:\n  - {title: a, from: 0}\nbackup:\n  "+c.yaml+"\n"), 0o644)
		if _, err := Load(path); err == nil || !strings.Contains(err.Error(), c.err) {
			t.Errorf("%s: err = %v, want %s", c.yaml, err, c.err)
		}
	}
	for _, ok := range []string{"00:00", "23:59", "03:00"} {
		if _, _, err := ParseClock(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
}

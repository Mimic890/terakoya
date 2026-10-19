package backup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Mimic890/terakoya/internal/store"
)

func TestParseKeep(t *testing.T) {
	for in, want := range map[string]Keep{"": {7, 4}, "7,4": {7, 4}, "3": {3, 4}, " 10 , 0 ": {10, 0}} {
		if got, err := ParseKeep(in); err != nil || got != want {
			t.Errorf("ParseKeep(%q) = %v, %v", in, got, err)
		}
	}
	for _, bad := range []string{"0", "x", "7,-1", "7,x"} {
		if _, err := ParseKeep(bad); err == nil {
			t.Errorf("ParseKeep(%q) accepted", bad)
		}
	}
}

func TestTake(t *testing.T) {
	ctx := context.Background()
	data := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(data, "terakoya.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.TouchUser(ctx, 42, "anya", "Аня")
	settings := filepath.Join(data, "settings.yml")
	os.WriteFile(settings, []byte("top:\n  size: 10\n"), 0o600)
	dir := filepath.Join(data, store.BackupsDir)
	now := time.Date(2026, 10, 19, 3, 0, 0, 0, time.UTC)

	s, err := Take(ctx, st, dir, settings, now)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(s.Path) != "terakoya-20261019-030000.db" || s.Size == 0 || !s.At.Equal(now) {
		t.Errorf("snapshot = %+v", s)
	}
	copied := filepath.Join(dir, "terakoya-20261019-030000.settings.yml")
	if data, _ := os.ReadFile(copied); string(data) != "top:\n  size: 10\n" {
		t.Errorf("settings copy = %q", data)
	}
	if info, _ := os.Stat(copied); info.Mode().Perm() != 0o600 {
		t.Errorf("settings copy mode = %v", info.Mode())
	}
	if err := store.QuickCheck(ctx, s.Path); err != nil {
		t.Error(err)
	}
	// Without a settings file there is just the database.
	os.Remove(settings)
	if _, err := Take(ctx, st, dir, settings, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "terakoya-20261019-040000.settings.yml")); !os.IsNotExist(err) {
		t.Errorf("settings copy without settings: %v", err)
	}
	list, _ := List(dir)
	if len(list) != 2 || !list[0].At.Equal(now.Add(time.Hour)) {
		t.Errorf("list = %+v", list)
	}
}

// touch creates an empty snapshot file, with its settings copy.
func touch(t *testing.T, dir, name string) {
	t.Helper()
	os.WriteFile(filepath.Join(dir, name), []byte("db"), 0o644)
	if strings.HasSuffix(name, ".db") && !strings.Contains(name, "before") {
		os.WriteFile(filepath.Join(dir, strings.TrimSuffix(name, ".db")+".settings.yml"), []byte("s"), 0o600)
	}
}

func TestRotate(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 10, 19, 3, 0, 0, 0, time.UTC) // a Monday
	// 60 days of daily snapshots, and on the newest day two more by hand.
	for d := 0; d < 60; d++ {
		touch(t, dir, Name(start.AddDate(0, 0, -d)))
	}
	touch(t, dir, Name(start.Add(5*time.Hour)))
	touch(t, dir, Name(start.Add(9*time.Hour)))
	for v := 8; v <= 12; v++ {
		touch(t, dir, store.PreMigrationName(start.AddDate(0, 0, v-20), v))
	}
	touch(t, dir, Name(start)+".tmp") // being written
	touch(t, dir, "notes.txt")

	if _, err := Rotate(dir, Keep{Daily: 7, Weekly: 4}); err != nil {
		t.Fatal(err)
	}
	var left []string
	for _, s := range must(List(dir)) {
		left = append(left, s.At.Format("01-02 15h")+map[bool]string{true: " before", false: ""}[s.Before != 0])
	}
	want := []string{
		// The newest of each of the last 7 days: Monday 10-19 and the
		// week of 10-12 from Tuesday on.
		"10-19 12h", "10-18 03h", "10-17 03h", "10-16 03h", "10-15 03h", "10-14 03h", "10-13 03h",
		// Then the newest of each of the 4 weeks before (their Sundays),
		// and the 3 newest taken before a migration.
		"10-11 03h before", "10-11 03h", "10-10 03h before", "10-09 03h before",
		"10-04 03h", "09-27 03h", "09-20 03h",
	}
	if strings.Join(left, ", ") != strings.Join(want, ", ") {
		t.Errorf("kept:\n%s\nwant:\n%s", strings.Join(left, ", "), strings.Join(want, ", "))
	}
	// Settings copies go with their snapshots; other files stay.
	names, _ := filepath.Glob(filepath.Join(dir, "*.settings.yml"))
	if len(names) != 11 {
		t.Errorf("%d settings copies: %v", len(names), names)
	}
	for _, f := range []string{"notes.txt", Name(start) + ".tmp"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	// Rotating again changes nothing.
	if gone, _ := Rotate(dir, Keep{Daily: 7, Weekly: 4}); len(gone) != 0 {
		t.Errorf("second rotation removed %v", gone)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

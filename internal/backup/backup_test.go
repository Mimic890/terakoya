package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Mimic890/terakoya/internal/store"
)

// data opens a database with a user and a settings file next to it.
func data(t *testing.T) (*store.Store, string, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(context.Background(), filepath.Join(dir, store.DBFile))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	st.TouchUser(context.Background(), 42, "anya", "Аня")
	settings := filepath.Join(dir, store.SettingsFile)
	os.WriteFile(settings, []byte("backup:\n  mode: folder\n"), 0o600)
	return st, filepath.Join(dir, store.BackupsDir), settings
}

func TestFolder(t *testing.T) {
	ctx := context.Background()
	st, dir, settings := data(t)
	now := time.Date(2026, 10, 20, 3, 0, 0, 0, time.Local)
	b, err := Folder(ctx, st, dir, settings, now)
	if err != nil {
		t.Fatal(err)
	}
	if b.Path != filepath.Join(dir, Latest) || b.Size == 0 || !b.At.Equal(now) {
		t.Errorf("backup = %+v", b)
	}
	if err := store.QuickCheck(ctx, filepath.Join(dir, Latest, store.DBFile)); err != nil {
		t.Error(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, Latest, store.SettingsFile)); string(got) != "backup:\n  mode: folder\n" {
		t.Errorf("settings = %q", got)
	}
	// Again: still only latest/, nothing else kept.
	if _, err := Folder(ctx, st, dir, settings, now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 || entries[0].Name() != Latest {
		t.Errorf("backups = %v", entries)
	}
	if last, ok, err := Last(dir); err != nil || !ok || last.Path != filepath.Join(dir, Latest) {
		t.Errorf("last = %+v %v %v", last, ok, err)
	}
}

// unpack lists an archive's files and their contents.
func unpack(t *testing.T, p string) map[string]string {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	out := map[string]string{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(tr)
		out[h.Name] = string(data)
	}
}

func TestArchive(t *testing.T) {
	ctx := context.Background()
	st, dir, settings := data(t)
	start := time.Date(2026, 10, 1, 3, 0, 0, 0, time.Local)
	for d := 0; d < 5; d++ {
		if _, _, err := Archive(ctx, st, dir, settings, start.AddDate(0, 0, d), 3); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	// The newest 3, nothing temporary left.
	if strings.Join(names, " ") != "terakoya-2026-10-03-0300.tar.gz terakoya-2026-10-04-0300.tar.gz terakoya-2026-10-05-0300.tar.gz" {
		t.Errorf("archives = %v", names)
	}
	files := unpack(t, filepath.Join(dir, "terakoya-2026-10-05-0300.tar.gz"))
	if len(files) != 2 || files[store.SettingsFile] != "backup:\n  mode: folder\n" || !strings.HasPrefix(files[store.DBFile], "SQLite format 3") {
		t.Errorf("archive has %d files", len(files))
	}
	// Removes report how many went.
	if _, n, err := Archive(ctx, st, dir, settings, start.AddDate(0, 0, 9), 1); err != nil || n != 3 {
		t.Errorf("removed %d, %v", n, err)
	}
	last, ok, _ := Last(dir)
	if !ok || filepath.Base(last.Path) != "terakoya-2026-10-10-0300.tar.gz" {
		t.Errorf("last = %+v", last)
	}
	// Without a settings file the archive has the database only.
	os.Remove(settings)
	b, _, _ := Archive(ctx, st, dir, settings, start.AddDate(0, 0, 10), 5)
	if files := unpack(t, b.Path); len(files) != 1 {
		t.Errorf("without settings: %d files", len(files))
	}
}

func TestLastPicksTheNewest(t *testing.T) {
	ctx := context.Background()
	st, dir, settings := data(t)
	if _, ok, err := Last(dir); ok || err != nil {
		t.Fatalf("empty: %v %v", ok, err)
	}
	// A copy taken before a migration does not count.
	os.MkdirAll(filepath.Join(dir, "before-v12"), 0o755)
	os.WriteFile(filepath.Join(dir, "before-v12", store.DBFile), []byte("x"), 0o644)
	if _, ok, _ := Last(dir); ok {
		t.Error("before-v12 counted")
	}
	Archive(ctx, st, dir, settings, time.Now(), 3)
	old := time.Now().Add(-48 * time.Hour)
	Folder(ctx, st, dir, settings, old)
	os.Chtimes(filepath.Join(dir, Latest, store.DBFile), old, old)
	if last, _, _ := Last(dir); !strings.HasSuffix(last.Path, ".tar.gz") {
		t.Errorf("last = %s, want the newer archive", last.Path)
	}
}

func TestNext(t *testing.T) {
	loc := time.FixedZone("MSK", 3*3600)
	cases := []struct {
		now  time.Time
		at   string
		want time.Time
	}{
		{time.Date(2026, 10, 19, 2, 0, 0, 0, loc), "03:00", time.Date(2026, 10, 19, 3, 0, 0, 0, loc)},
		{time.Date(2026, 10, 19, 3, 0, 0, 0, loc), "03:00", time.Date(2026, 10, 20, 3, 0, 0, 0, loc)},
		{time.Date(2026, 10, 19, 23, 59, 0, 0, loc), "00:00", time.Date(2026, 10, 20, 0, 0, 0, 0, loc)},
		{time.Date(2026, 12, 31, 22, 0, 0, 0, loc), "21:30", time.Date(2027, 1, 1, 21, 30, 0, 0, loc)},
	}
	for _, c := range cases {
		if got := Next(c.now, c.at); !got.Equal(c.want) {
			t.Errorf("Next(%v, %s) = %v, want %v", c.now, c.at, got, c.want)
		}
	}
	if !Next(time.Now(), "25:00").IsZero() {
		t.Error("bad time scheduled")
	}
}

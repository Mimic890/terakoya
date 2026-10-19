package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSnapshot(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	s.TouchUser(ctx, 42, "anya", "Аня")
	os.Chmod(s.path, 0o640)
	p := filepath.Join(t.TempDir(), "copy.db")
	if err := s.Snapshot(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temporary file left: %v", err)
	}
	info, _ := os.Stat(p)
	if info.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v", info.Mode())
	}
	c, err := Open(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if u, err := c.User(ctx, 42); err != nil || u.FirstName != "Аня" {
		t.Errorf("user in the copy = %+v, %v", u, err)
	}
	// The bot goes on writing while copies are taken.
	s.TouchUser(ctx, 43, "", "Борис")
	if err := s.Snapshot(ctx, p); err != nil {
		t.Fatal(err)
	}
}

func TestQuickCheckRefusesBrokenFiles(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	for i := 0; i < 200; i++ {
		s.TouchUser(ctx, int64(i), strings.Repeat("x", 100), "")
	}
	p := filepath.Join(t.TempDir(), "copy.db")
	s.Snapshot(ctx, p)
	if err := QuickCheck(ctx, p); err != nil {
		t.Fatalf("sound copy: %v", err)
	}
	// Overwrite a page in the middle.
	f, _ := os.OpenFile(p, os.O_RDWR, 0)
	f.WriteAt([]byte(strings.Repeat("\xff", 4096)), 8192)
	f.Close()
	if err := QuickCheck(ctx, p); err == nil {
		t.Error("broken copy passed")
	}
	if err := QuickCheck(ctx, filepath.Join(t.TempDir(), "none.db")); err == nil {
		t.Error("missing file passed")
	}
}

func TestSnapshotBeforeMigrating(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "terakoya.db")
	os.WriteFile(filepath.Join(dir, SettingsFile), []byte("list_style: list\n"), 0o600)
	// A database of the previous version.
	db, _ := sql.Open("sqlite", "file:"+path)
	db.SetMaxOpenConns(1)
	old := &Store{db: db, path: path, now: time.Now}
	if err := old.migrate(ctx, len(migrations)-1); err != nil {
		t.Fatal(err)
	}
	old.TouchUser(ctx, 42, "anya", "Аня")
	db.Close()

	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	folder := filepath.Join(dir, BackupsDir, "before-v"+strconv.Itoa(len(migrations)))
	c, _ := sql.Open("sqlite", "file:"+filepath.Join(folder, DBFile)+"?mode=ro")
	defer c.Close()
	var v, users int
	c.QueryRow(`PRAGMA user_version`).Scan(&v)
	c.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&users)
	if v != len(migrations)-1 || users != 1 {
		t.Errorf("snapshot is version %d with %d users", v, users)
	}
	if data, _ := os.ReadFile(filepath.Join(folder, SettingsFile)); string(data) != "list_style: list\n" {
		t.Errorf("settings copy = %q", data)
	}
	// Opening an up-to-date or a new database takes none.
	s.Close()
	s, _ = Open(ctx, path)
	Open(ctx, filepath.Join(dir, "new.db"))
	if m, _ := filepath.Glob(filepath.Join(dir, BackupsDir, "*")); len(m) != 1 {
		t.Errorf("backups = %v", m)
	}
}

func TestBeforeMigrationKeepsThree(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	for v := 8; v <= 12; v++ {
		if err := s.beforeMigration(ctx, v); err != nil {
			t.Fatal(err)
		}
	}
	m, _ := filepath.Glob(filepath.Join(filepath.Dir(s.path), BackupsDir, "*"))
	var names []string
	for _, p := range m {
		names = append(names, filepath.Base(p))
	}
	if strings.Join(names, " ") != "before-v10 before-v11 before-v12" {
		t.Errorf("kept %v", names)
	}
}

func TestSnapshotFolderSwaps(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	parent := filepath.Join(t.TempDir(), "backups")
	extra := filepath.Join(t.TempDir(), "settings.yml")
	os.WriteFile(extra, []byte("v1"), 0o600)
	s.TouchUser(ctx, 1, "", "first")
	if err := s.SnapshotFolder(ctx, parent, "latest", extra, filepath.Join(t.TempDir(), "missing.yml")); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(extra, []byte("v2"), 0o600)
	s.TouchUser(ctx, 2, "", "second")
	if err := s.SnapshotFolder(ctx, parent, "latest", extra); err != nil {
		t.Fatal(err)
	}
	// Only latest/ is left, with the new contents.
	entries, _ := os.ReadDir(parent)
	if len(entries) != 1 || entries[0].Name() != "latest" {
		t.Fatalf("backups = %v", entries)
	}
	if data, _ := os.ReadFile(filepath.Join(parent, "latest", "settings.yml")); string(data) != "v2" {
		t.Errorf("settings = %q", data)
	}
	if info, _ := os.Stat(filepath.Join(parent, "latest", "settings.yml")); info.Mode().Perm() != 0o600 {
		t.Errorf("settings mode = %v", info.Mode())
	}
	c, err := Open(ctx, filepath.Join(parent, "latest", DBFile))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.User(ctx, 2); err != nil {
		t.Errorf("new user missing: %v", err)
	}
	// A failure leaves the previous backup as it was.
	if err := s.SnapshotFolder(ctx, parent, "latest", t.TempDir()); err == nil {
		t.Error("copying a folder as a file succeeded")
	}
	entries, _ = os.ReadDir(parent)
	if len(entries) != 1 {
		t.Errorf("after a failure: %v", entries)
	}
	if data, _ := os.ReadFile(filepath.Join(parent, "latest", "settings.yml")); string(data) != "v2" {
		t.Errorf("after a failure, settings = %q", data)
	}
}

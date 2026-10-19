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
	matches, _ := filepath.Glob(filepath.Join(dir, BackupsDir, "terakoya-*-before-v*.db"))
	if len(matches) != 1 || !strings.HasSuffix(matches[0], "-before-v"+strconv.Itoa(len(migrations))+".db") {
		t.Fatalf("snapshots = %v", matches)
	}
	c, _ := sql.Open("sqlite", "file:"+matches[0]+"?mode=ro")
	defer c.Close()
	var v, users int
	c.QueryRow(`PRAGMA user_version`).Scan(&v)
	c.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&users)
	if v != len(migrations)-1 || users != 1 {
		t.Errorf("snapshot is version %d with %d users", v, users)
	}
	// Opening an up-to-date or a new database takes none.
	s.Close()
	s, _ = Open(ctx, path)
	Open(ctx, filepath.Join(dir, "new.db"))
	if m, _ := filepath.Glob(filepath.Join(dir, BackupsDir, "*")); len(m) != 1 {
		t.Errorf("snapshots = %v", m)
	}
}

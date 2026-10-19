package store

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// BackupsDir is the folder, next to the database, that holds backups.
const BackupsDir = "backups"

// File names inside a backup folder. SettingsFile is the settings file
// next to the database, which the backup carries along.
const (
	DBFile       = "terakoya.db"
	SettingsFile = "settings.yml"
)

// keepBeforeMigration is how many copies taken before a migration stay.
const keepBeforeMigration = 3

// Snapshot writes a consistent copy of the database to path with VACUUM
// INTO, while the bot goes on. The copy is written under a temporary
// name, checked with PRAGMA quick_check, flushed to disk and only then
// renamed into place, so a file at path is always whole and sound. It
// gets the database file's permissions.
func (s *Store) Snapshot(ctx context.Context, path string) error {
	tmp := path + ".tmp"
	os.Remove(tmp)
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, tmp); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("vacuum into: %w", err)
	}
	if err := finish(ctx, tmp, s.path); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return syncDir(filepath.Dir(path))
}

// finish checks a fresh copy, gives it the permissions of like and flushes
// it to disk.
func finish(ctx context.Context, path, like string) error {
	if err := QuickCheck(ctx, path); err != nil {
		return err
	}
	if info, err := os.Stat(like); err == nil {
		if err := os.Chmod(path, info.Mode().Perm()); err != nil {
			return err
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// QuickCheck opens the database file at path read-only and runs PRAGMA
// quick_check on it.
func QuickCheck(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()
	var res string
	if err := db.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&res); err != nil {
		return fmt.Errorf("quick_check: %w", err)
	}
	if res != "ok" {
		return fmt.Errorf("quick_check: %s", res)
	}
	return nil
}

// SnapshotFolder writes parent/name/ with a snapshot of the database
// (DBFile) and copies of files, which are skipped if missing. It is built
// as parent/.tmp-<time>/, flushed, and then swapped in: an existing
// parent/name/ becomes parent/.old-<time>/, the new one takes its name,
// and the old one is removed. parent/name/ is thus always a whole backup.
func (s *Store) SnapshotFolder(ctx context.Context, parent, name string, files ...string) error {
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	stamp := fmt.Sprint(time.Now().UnixNano())
	tmp := filepath.Join(parent, ".tmp-"+stamp)
	if err := os.Mkdir(tmp, 0o755); err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			os.RemoveAll(tmp)
		}
	}()
	if err := s.Snapshot(ctx, filepath.Join(tmp, DBFile)); err != nil {
		return err
	}
	for _, f := range files {
		err := CopyFile(f, filepath.Join(tmp, filepath.Base(f)))
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("copy %s: %w", filepath.Base(f), err)
		}
	}
	if err := syncDir(tmp); err != nil {
		return err
	}
	target := filepath.Join(parent, name)
	old := filepath.Join(parent, ".old-"+stamp)
	hadOld := false
	if _, err := os.Lstat(target); err == nil {
		if err := os.Rename(target, old); err != nil {
			return err
		}
		hadOld = true
	}
	if err := os.Rename(tmp, target); err != nil {
		if hadOld {
			os.Rename(old, target)
		}
		return err
	}
	ok = true
	if err := syncDir(parent); err != nil {
		return err
	}
	if hadOld {
		return os.RemoveAll(old)
	}
	return nil
}

// CopyFile copies src to dst through a temporary file, flushed to disk,
// with src's permissions.
func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, dst)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// beforeMigration copies the database and the settings file to
// backups/before-v<to>/ and keeps the newest keepBeforeMigration such
// copies.
func (s *Store) beforeMigration(ctx context.Context, to int) error {
	data := filepath.Dir(s.path)
	parent := filepath.Join(data, BackupsDir)
	if err := s.SnapshotFolder(ctx, parent, fmt.Sprintf("before-v%d", to), filepath.Join(data, SettingsFile)); err != nil {
		return err
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return err
	}
	var versions []int
	for _, e := range entries {
		var v int
		if _, err := fmt.Sscanf(e.Name(), "before-v%d", &v); err == nil && e.IsDir() && e.Name() == fmt.Sprintf("before-v%d", v) {
			versions = append(versions, v)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(versions)))
	for _, v := range versions[min(len(versions), keepBeforeMigration):] {
		if err := os.RemoveAll(filepath.Join(parent, fmt.Sprintf("before-v%d", v))); err != nil {
			return err
		}
	}
	return nil
}

// syncDir flushes a folder, so a rename in it survives a power loss.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

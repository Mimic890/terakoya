package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// BackupsDir is the folder, next to the database, that holds snapshots.
const BackupsDir = "backups"

// SnapshotPrefix starts the name of every snapshot.
const SnapshotPrefix = "terakoya-"

// SnapshotTime formats the time in a snapshot's name, in UTC.
const SnapshotTime = "20060102-150405"

// PreMigrationName names the snapshot taken before migrating to version.
func PreMigrationName(t time.Time, version int) string {
	return fmt.Sprintf("%s%s-before-v%d.db", SnapshotPrefix, t.UTC().Format(SnapshotTime), version)
}

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

// syncDir flushes a folder, so a rename in it survives a power loss.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

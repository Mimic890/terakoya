// Package backup writes backups of the bot's data: the SQLite database,
// copied with VACUUM INTO and checked, and settings.yml with it. In the
// folder mode the newest backup is data/backups/latest/, for tools on the
// host to archive; in the archive mode each one is a .tar.gz of its own,
// of which the newest few are kept.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/Mimic890/terakoya/internal/settings"
	"github.com/Mimic890/terakoya/internal/store"
)

// Modes of the backup: section of settings.yml.
const (
	ModeFolder  = "folder"
	ModeArchive = "archive"
	ModeOff     = "off"
)

// Latest is the folder of the folder mode.
const Latest = "latest"

// Backup is a backup on disk.
type Backup struct {
	Path string    `json:"path"`
	At   time.Time `json:"at"`
	Size int64     `json:"size"`
}

// archiveName matches the archives, terakoya-YYYY-MM-DD-HHMM.tar.gz, in
// the container's local time.
var archiveName = regexp.MustCompile(`^terakoya-(\d{4}-\d{2}-\d{2}-\d{4})\.tar\.gz$`)

const archiveTime = "2006-01-02-1504"

// Folder writes dir/latest/ with the database and the settings file,
// replacing the previous one at once (see store.SnapshotFolder).
func Folder(ctx context.Context, st *store.Store, dir, settings string, now time.Time) (Backup, error) {
	if err := st.SnapshotFolder(ctx, dir, Latest, settings); err != nil {
		return Backup{}, err
	}
	p := filepath.Join(dir, Latest)
	return Backup{Path: p, At: now, Size: folderSize(p)}, nil
}

// Archive writes dir/terakoya-<time>.tar.gz with the database and the
// settings file, through a temporary file, and then removes all but the
// newest keep archives. It returns how many were removed.
func Archive(ctx context.Context, st *store.Store, dir, settings string, now time.Time, keep int) (Backup, int, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Backup{}, 0, err
	}
	work, err := os.MkdirTemp(dir, ".tmp-")
	if err != nil {
		return Backup{}, 0, err
	}
	defer os.RemoveAll(work)
	db := filepath.Join(work, store.DBFile)
	if err := st.Snapshot(ctx, db); err != nil {
		return Backup{}, 0, err
	}
	p := filepath.Join(dir, "terakoya-"+now.Format(archiveTime)+".tar.gz")
	if err := writeArchive(p, db, settings); err != nil {
		return Backup{}, 0, err
	}
	info, err := os.Stat(p)
	if err != nil {
		return Backup{}, 0, err
	}
	removed, err := prune(dir, keep)
	return Backup{Path: p, At: now, Size: info.Size()}, removed, err
}

// writeArchive packs the files, those that exist, into a .tar.gz at p,
// written under a temporary name, flushed and renamed.
func writeArchive(p string, files ...string) (err error) {
	tmp := p + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(tmp)
		}
	}()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, name := range files {
		if err := addFile(tw, name); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func addFile(tw *tar.Writer, name string) error {
	in, err := os.Open(name)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	hdr, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	hdr.Name = filepath.Base(name)
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err = io.Copy(tw, in)
	return err
}

// archives lists the archives in dir, newest first.
func archives(dir string) ([]Backup, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Backup
	for _, e := range entries {
		m := archiveName.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		at, err := time.ParseInLocation(archiveTime, m[1], time.Local)
		if err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Backup{Path: filepath.Join(dir, e.Name()), At: at, Size: info.Size()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path > out[j].Path })
	return out, nil
}

// prune removes all but the newest keep archives.
func prune(dir string, keep int) (int, error) {
	all, err := archives(dir)
	if err != nil {
		return 0, err
	}
	n := 0
	var errs []error
	for _, b := range all[min(len(all), keep):] {
		if err := os.Remove(b.Path); err != nil {
			errs = append(errs, err)
			continue
		}
		n++
	}
	return n, errors.Join(errs...)
}

// Last finds the newest backup in dir, of either mode; ok is false if
// there is none. Copies taken before a migration do not count.
func Last(dir string) (b Backup, ok bool, err error) {
	if info, err := os.Stat(filepath.Join(dir, Latest, store.DBFile)); err == nil {
		p := filepath.Join(dir, Latest)
		b, ok = Backup{Path: p, At: info.ModTime(), Size: folderSize(p)}, true
	}
	all, err := archives(dir)
	if err != nil {
		return b, ok, err
	}
	if len(all) > 0 {
		// An archive's time is in its name, to the minute.
		info, err := os.Stat(all[0].Path)
		if err == nil {
			all[0].At = info.ModTime()
		}
		if !ok || all[0].At.After(b.At) {
			b, ok = all[0], true
		}
	}
	return b, ok, nil
}

func folderSize(dir string) int64 {
	var n int64
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if info, err := e.Info(); err == nil && info.Mode().IsRegular() {
			n += info.Size()
		}
	}
	return n
}

// Next is the first moment after now at hhmm, in now's location.
func Next(now time.Time, hhmm string) time.Time {
	h, m, err := settings.ParseClock(hhmm)
	if err != nil {
		return time.Time{}
	}
	next := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, now.Location())
	if !next.After(now) {
		next = time.Date(now.Year(), now.Month(), now.Day()+1, h, m, 0, 0, now.Location())
	}
	return next
}

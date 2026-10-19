// Package backup takes and rotates snapshots of the bot's data: the
// SQLite database, copied with VACUUM INTO and checked, and settings.yml
// next to it. They are kept in data/backups for the admin to copy
// elsewhere.
package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Mimic890/terakoya/internal/store"
)

// Keep is how many snapshots rotation keeps: the newest of each of the
// last Daily days, and the newest of each of the Weekly ISO weeks before
// the weeks those days fall in.
type Keep struct {
	Daily, Weekly int
}

// DefaultKeep keeps a week of daily snapshots and a month of weekly ones.
var DefaultKeep = Keep{Daily: 7, Weekly: 4}

// keepBefore is how many snapshots taken before a migration are kept.
const keepBefore = 3

// ParseKeep reads "7,4" (daily, weekly) or "7" (daily; 4 weekly).
func ParseKeep(s string) (Keep, error) {
	k := DefaultKeep
	s = strings.TrimSpace(s)
	if s == "" {
		return k, nil
	}
	daily, weekly, two := strings.Cut(s, ",")
	var err error
	if k.Daily, err = strconv.Atoi(strings.TrimSpace(daily)); err != nil || k.Daily < 1 {
		return k, fmt.Errorf("%q: the number of daily snapshots must be at least 1", s)
	}
	if two {
		if k.Weekly, err = strconv.Atoi(strings.TrimSpace(weekly)); err != nil || k.Weekly < 0 {
			return k, fmt.Errorf("%q: the number of weekly snapshots must be 0 or more", s)
		}
	}
	return k, nil
}

func (k Keep) String() string { return fmt.Sprintf("%d daily, %d weekly", k.Daily, k.Weekly) }

// Snapshot is a backup on disk.
type Snapshot struct {
	Path string    `json:"path"`
	At   time.Time `json:"at"`
	Size int64     `json:"size"`
	// Before is the schema version a snapshot taken before a migration
	// was taken for, or 0.
	Before int `json:"before,omitempty"`
}

var (
	regular       = regexp.MustCompile(`^terakoya-(\d{8}-\d{6})\.db$`)
	preMigration  = regexp.MustCompile(`^terakoya-(\d{8}-\d{6})-before-v(\d+)\.db$`)
	settingsSufix = ".settings.yml"
)

// Name is the file name of a regular snapshot taken at t.
func Name(t time.Time) string {
	return store.SnapshotPrefix + t.UTC().Format(store.SnapshotTime) + ".db"
}

// settingsOf is the settings file kept with a snapshot.
func settingsOf(dbPath string) string { return strings.TrimSuffix(dbPath, ".db") + settingsSufix }

// Take writes a snapshot of the database into dir, named after now, and a
// copy of the settings file next to it if there is one.
func Take(ctx context.Context, st *store.Store, dir, settings string, now time.Time) (Snapshot, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Snapshot{}, err
	}
	p := filepath.Join(dir, Name(now))
	if err := st.Snapshot(ctx, p); err != nil {
		return Snapshot{}, err
	}
	if settings != "" {
		if err := copyFile(settings, settingsOf(p)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return Snapshot{}, fmt.Errorf("copy settings: %w", err)
		}
	}
	info, err := os.Stat(p)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Path: p, At: now.UTC().Truncate(time.Second), Size: info.Size()}, nil
}

// copyFile copies src to dst through a temporary file, with src's
// permissions.
func copyFile(src, dst string) error {
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

// List returns the snapshots in dir, newest first. Unfinished ones, still
// under a temporary name, are not listed.
func List(dir string) ([]Snapshot, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Snapshot
	for _, e := range entries {
		name := e.Name()
		var stamp string
		before := 0
		if m := regular.FindStringSubmatch(name); m != nil {
			stamp = m[1]
		} else if m := preMigration.FindStringSubmatch(name); m != nil {
			stamp = m[1]
			before, _ = strconv.Atoi(m[2])
		} else {
			continue
		}
		at, err := time.ParseInLocation(store.SnapshotTime, stamp, time.UTC)
		if err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Snapshot{Path: filepath.Join(dir, name), At: at, Size: info.Size(), Before: before})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.After(out[j].At)
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

// Rotate deletes the snapshots keep does not cover, with their settings
// copies, and returns them. Snapshots taken before a migration are
// rotated on their own: the newest few stay.
func Rotate(dir string, keep Keep) ([]Snapshot, error) {
	all, err := List(dir)
	if err != nil {
		return nil, err
	}
	var gone []Snapshot
	// Weeks with a daily snapshot kept are covered already; the weekly
	// ones reach further back.
	days, covered, weeks := map[string]bool{}, map[string]bool{}, map[string]bool{}
	before := 0
	for _, s := range all {
		if s.Before != 0 {
			if before++; before > keepBefore {
				gone = append(gone, s)
			}
			continue
		}
		day := s.At.Format("2006-01-02")
		y, w := s.At.ISOWeek()
		week := fmt.Sprintf("%d-%02d", y, w)
		switch {
		case days[day]:
			// An older one of a day already kept.
		case len(days) < keep.Daily:
			days[day], covered[week] = true, true
			continue
		case !covered[week] && !weeks[week] && len(weeks) < keep.Weekly:
			weeks[week] = true
			continue
		}
		gone = append(gone, s)
	}
	var errs []error
	for _, s := range gone {
		if err := os.Remove(s.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
		if err := os.Remove(settingsOf(s.Path)); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return gone, errors.Join(errs...)
}

package bot

import (
	"context"
	"time"

	"github.com/Mimic890/terakoya/internal/backup"
)

// BackupResult is the outcome of a snapshot, or the newest snapshot on
// disk, for /admin and `terakoya admin backup`.
type BackupResult struct {
	At   time.Time `json:"at"`
	Path string    `json:"path,omitempty"`
	Size int64     `json:"size,omitempty"`
	// Removed is how many old snapshots rotation deleted.
	Removed int `json:"removed"`
	// Count is how many snapshots there are now.
	Count int    `json:"count"`
	Err   string `json:"error,omitempty"`
}

// BackupNow takes a snapshot of the database and settings.yml into
// data/backups and rotates the old ones.
func (b *Bot) BackupNow(ctx context.Context) BackupResult {
	b.backupMu.Lock()
	defer b.backupMu.Unlock()
	keep, _ := backup.ParseKeep(b.cfg.BackupKeep) // checked by config.Validate
	dir := b.cfg.BackupsDir()
	r := BackupResult{At: b.clock()}
	s, err := backup.Take(ctx, b.store, dir, b.cfg.SettingsPath(), r.At)
	if err != nil {
		r.Err = err.Error()
		b.log.Error("backup failed", "err", err)
	} else {
		r.Path, r.Size = s.Path, s.Size
		gone, err := backup.Rotate(dir, keep)
		if err != nil {
			b.log.Warn("remove old backups", "err", err)
		}
		r.Removed = len(gone)
		b.log.Info("backup written", "path", s.Path, "bytes", s.Size, "removed", len(gone))
	}
	if list, err := backup.List(dir); err == nil {
		r.Count = len(list)
	}
	b.lastBackup = &r
	return r
}

// LastBackup is the outcome of the last snapshot since the start, or
// else the newest one on disk, or nil if there is none.
func (b *Bot) LastBackup() *BackupResult {
	b.backupMu.Lock()
	defer b.backupMu.Unlock()
	if b.lastBackup != nil {
		r := *b.lastBackup
		if list, err := backup.List(b.cfg.BackupsDir()); err == nil {
			r.Count = len(list)
		}
		return &r
	}
	list, err := backup.List(b.cfg.BackupsDir())
	if err != nil || len(list) == 0 {
		return nil
	}
	return &BackupResult{At: list[0].At.Local(), Path: list[0].Path, Size: list[0].Size, Count: len(list)}
}

// backupLoop takes a snapshot every BackupInterval, counted from the
// newest regular one on disk, so restarts do not put it off or add more.
func (b *Bot) backupLoop(ctx context.Context) {
	every := b.cfg.BackupInterval
	if every <= 0 {
		return
	}
	next := b.clock()
	if list, err := backup.List(b.cfg.BackupsDir()); err == nil {
		for _, s := range list {
			if s.Before == 0 {
				next = s.At.Add(every)
				break
			}
		}
	}
	for {
		t := time.NewTimer(max(next.Sub(b.clock()), 0))
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		func() {
			defer b.recovered("backup")
			b.BackupNow(ctx)
		}()
		next = b.clock().Add(every)
	}
}

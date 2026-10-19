package bot

import (
	"context"
	"time"

	"github.com/Mimic890/terakoya/internal/backup"
)

// BackupResult is the state of the backups, for /admin and
// `terakoya admin backup`: the mode, the last backup (or its error) and
// when the next one is due.
type BackupResult struct {
	Mode string    `json:"mode"`
	At   time.Time `json:"at,omitempty"` // zero if there is none yet
	Path string    `json:"path,omitempty"`
	Size int64     `json:"size,omitempty"`
	// Removed is how many old archives the archive mode deleted.
	Removed int    `json:"removed,omitempty"`
	Err     string `json:"error,omitempty"`
	// Next is when the next scheduled one is due, zero if mode is off.
	Next time.Time `json:"next,omitempty"`
}

// missedAfter: a backup older than this when the bot starts means one
// was missed while it was down, and one is taken at once.
const missedAfter = 24 * time.Hour

// BackupNow takes a backup in the mode settings.yml sets, the folder mode
// when backups are off.
func (b *Bot) BackupNow(ctx context.Context) BackupResult {
	b.backupMu.Lock()
	defer b.backupMu.Unlock()
	set := b.settings.Get().Backup
	mode := set.Mode
	if mode == backup.ModeOff {
		mode = backup.ModeFolder
	}
	r := BackupResult{Mode: set.Mode, At: b.clock(), Next: b.nextBackup()}
	var got backup.Backup
	var err error
	if mode == backup.ModeArchive {
		got, r.Removed, err = backup.Archive(ctx, b.store, b.cfg.BackupsDir(), b.cfg.SettingsPath(), r.At, set.Keep)
	} else {
		got, err = backup.Folder(ctx, b.store, b.cfg.BackupsDir(), b.cfg.SettingsPath(), r.At)
	}
	if err != nil && got.Path == "" {
		r.Err = err.Error()
		b.log.Error("backup failed", "mode", mode, "err", err)
	} else {
		if err != nil {
			b.log.Warn("remove old backups", "err", err)
		}
		r.Path, r.Size = got.Path, got.Size
		b.log.Info("backup written", "mode", mode, "path", got.Path, "bytes", got.Size, "removed", r.Removed)
	}
	b.lastBackup = &r
	return r
}

// LastBackup is the outcome of the last backup since the start, or else
// the newest one on disk, with the mode and the next run.
func (b *Bot) LastBackup() BackupResult {
	b.backupMu.Lock()
	defer b.backupMu.Unlock()
	r := BackupResult{Mode: b.settings.Get().Backup.Mode, Next: b.nextBackup()}
	if b.lastBackup != nil {
		last := *b.lastBackup
		last.Mode, last.Next = r.Mode, r.Next
		return last
	}
	if got, ok, err := backup.Last(b.cfg.BackupsDir()); err == nil && ok {
		r.At, r.Path, r.Size = got.At, got.Path, got.Size
	}
	return r
}

// nextBackup is when the next scheduled backup is due, or zero.
func (b *Bot) nextBackup() time.Time {
	set := b.settings.Get().Backup
	if set.Mode == backup.ModeOff {
		return time.Time{}
	}
	return backup.Next(b.clock(), set.Time)
}

// rescheduleBackups makes backupLoop look at settings.yml again.
func (b *Bot) rescheduleBackups() {
	select {
	case b.backupWake <- struct{}{}:
	default:
	}
}

// backupLoop takes the daily backup at the time settings.yml sets, and
// right away on start if the last one is more than a day old: the bot was
// down when it was due. A change of the settings reschedules it.
func (b *Bot) backupLoop(ctx context.Context) {
	if b.settings.Get().Backup.Mode != backup.ModeOff {
		last, ok, err := backup.Last(b.cfg.BackupsDir())
		if err != nil {
			b.log.Warn("find the last backup", "err", err)
		}
		if !ok || b.clock().Sub(last.At) > missedAfter {
			b.safeBackup(ctx)
		}
	}
	for {
		var due <-chan time.Time
		var timer *time.Timer
		if next := b.nextBackup(); !next.IsZero() {
			timer = time.NewTimer(max(next.Sub(b.clock()), 0))
			due = timer.C
		}
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case <-b.backupWake:
			if timer != nil {
				timer.Stop()
			}
		case <-due:
			b.safeBackup(ctx)
		}
	}
}

func (b *Bot) safeBackup(ctx context.Context) {
	defer b.recovered("backup")
	b.BackupNow(ctx)
}

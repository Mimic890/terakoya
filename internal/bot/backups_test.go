package bot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Mimic890/terakoya/internal/backup"
	"github.com/Mimic890/terakoya/internal/settings"
	"github.com/Mimic890/terakoya/internal/store"
)

func setBackup(h *harness, mode, at string, keep int) {
	h.t.Helper()
	if err := h.bot.settings.Update(func(s *settings.Settings) {
		s.Backup = settings.Backup{Mode: mode, Time: at, Keep: keep}
	}); err != nil {
		h.t.Fatal(err)
	}
}

func TestBackupNowFollowsTheMode(t *testing.T) {
	h := newHarness(t, nil)
	dir := h.bot.cfg.BackupsDir()
	if r := h.bot.LastBackup(); !r.At.IsZero() || r.Mode != "folder" || r.Next.IsZero() {
		t.Fatalf("before any = %+v", r)
	}

	// folder, the default: latest/ with the database and settings.yml.
	r := h.bot.BackupNow(h.ctx)
	if r.Err != "" || r.Path != filepath.Join(dir, backup.Latest) || r.Size == 0 || r.Mode != "folder" {
		t.Fatalf("folder = %+v", r)
	}
	if err := store.QuickCheck(h.ctx, filepath.Join(r.Path, store.DBFile)); err != nil {
		t.Error(err)
	}
	if _, err := os.Stat(filepath.Join(r.Path, store.SettingsFile)); err != nil {
		t.Errorf("settings copy: %v", err)
	}

	// archive: a .tar.gz, the newest keep kept.
	setBackup(h, "archive", "03:00", 1)
	r = h.bot.BackupNow(h.ctx)
	if r.Err != "" || !strings.HasSuffix(r.Path, ".tar.gz") || r.Mode != "archive" {
		t.Fatalf("archive = %+v", r)
	}

	// off: no schedule, but a backup by hand is still a folder one.
	setBackup(h, "off", "03:00", 7)
	r = h.bot.BackupNow(h.ctx)
	if r.Err != "" || r.Path != filepath.Join(dir, backup.Latest) || r.Mode != "off" || !r.Next.IsZero() {
		t.Errorf("off = %+v", r)
	}

	// After a restart, the newest on disk is shown.
	h.bot.lastBackup = nil
	if got := h.bot.LastBackup(); got.At.IsZero() || got.Path == "" {
		t.Errorf("from disk = %+v", got)
	}
}

func TestBackupRows(t *testing.T) {
	h := newHarness(t, nil)
	at := time.Date(2026, 10, 19, 3, 0, 0, 0, time.Local)
	next := at.Add(24 * time.Hour)
	for _, c := range []struct {
		r    BackupResult
		want string
	}{
		{BackupResult{Mode: "folder", Next: next}, "| Резервные копии | папка latest |\n| Последняя копия | ещё не было |\n| Следующая копия | 20 октября, 03:00 |"},
		{BackupResult{Mode: "archive", At: at, Size: 112 << 10, Next: next}, "| Резервные копии | архивы |\n| Последняя копия | 19 октября, 03:00, 112 КБ |\n| Следующая копия | 20 октября, 03:00 |"},
		{BackupResult{Mode: "off", At: at, Err: "disk_full"}, "| Резервные копии | выключены |\n| Последняя копия | не удалась (19 октября, 03:00): disk\\_full |\n| Следующая копия | по расписанию не делаются |"},
	} {
		if got := h.bot.storageTable(AdminServer{Backup: c.r}); !strings.HasSuffix(got, c.want) {
			t.Errorf("table =\n%s\nwant suffix\n%s", got, c.want)
		}
	}
}

// soonClock makes the backup time due in about d.
func soonClock(h *harness, at string, d time.Duration) {
	offset := backup.Next(time.Now(), at).Sub(time.Now()) - d
	h.bot.clock = func() time.Time { return time.Now().Add(offset) }
}

// runLoop runs backupLoop until stop is called.
func runLoop(h *harness) (stop func()) {
	ctx, cancel := context.WithCancel(h.ctx)
	done := make(chan struct{})
	go func() { h.bot.backupLoop(ctx); close(done) }()
	return func() { cancel(); <-done }
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for i := 0; i < 300; i++ {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestBackupLoop(t *testing.T) {
	h := newHarness(t, nil)
	latest := filepath.Join(h.bot.cfg.BackupsDir(), backup.Latest, store.DBFile)
	exists := func() bool { _, err := os.Stat(latest); return err == nil }

	// No backup yet: one is taken at start, as a missed run.
	stop := runLoop(h)
	waitFor(t, "the missed backup", exists)
	stop()

	// A recent one is enough at start; the next is at the set time, far
	// off here.
	info, _ := os.Stat(latest)
	stop = runLoop(h)
	time.Sleep(150 * time.Millisecond)
	stop()
	if again, _ := os.Stat(latest); !again.ModTime().Equal(info.ModTime()) {
		t.Error("a backup at start although the last one is recent")
	}

	// With mode off, nothing is taken, not even a missed run.
	os.RemoveAll(filepath.Dir(latest))
	setBackup(h, "off", "03:00", 7)
	stop = runLoop(h)
	time.Sleep(100 * time.Millisecond)
	if exists() {
		t.Error("a backup with mode off")
	}

	// Turning it on reschedules without a restart.
	soonClock(h, "03:00", 150*time.Millisecond)
	setBackup(h, "folder", "03:00", 7)
	waitFor(t, "the scheduled backup", exists)
	stop()
}

func TestOldLastBackupIsAMissedRun(t *testing.T) {
	h := newHarness(t, nil)
	h.bot.BackupNow(h.ctx)
	latest := filepath.Join(h.bot.cfg.BackupsDir(), backup.Latest, store.DBFile)
	old := time.Now().Add(-25 * time.Hour)
	os.Chtimes(latest, old, old)
	stop := runLoop(h)
	waitFor(t, "a new backup", func() bool {
		info, err := os.Stat(latest)
		return err == nil && info.ModTime().After(old.Add(time.Hour))
	})
	stop()
}

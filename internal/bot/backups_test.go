package bot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Mimic890/terakoya/internal/backup"
	"github.com/Mimic890/terakoya/internal/store"
)

func TestBackupNow(t *testing.T) {
	h := newHarness(t, nil)
	if h.bot.LastBackup() != nil {
		t.Fatal("a backup before any was taken")
	}
	r := h.bot.BackupNow(h.ctx)
	if r.Err != "" || r.Size == 0 || r.Count != 1 || filepath.Dir(r.Path) != filepath.Join(h.bot.cfg.DataDir, "backups") {
		t.Fatalf("backup = %+v", r)
	}
	if err := store.QuickCheck(h.ctx, r.Path); err != nil {
		t.Error(err)
	}
	if _, err := os.Stat(strings.TrimSuffix(r.Path, ".db") + ".settings.yml"); err != nil {
		t.Errorf("settings copy: %v", err)
	}
	// On «Сервер».
	line := h.bot.backupLine(h.bot.LastBackup())
	if !strings.HasPrefix(line, "Резервная копия: ") || !strings.HasSuffix(line, " · всего копий: 1") {
		t.Errorf("line = %q", line)
	}
	// After a restart, the newest on disk is shown.
	h.bot.lastBackup = nil
	if b := h.bot.LastBackup(); b == nil || b.Path != r.Path || b.Size != r.Size {
		t.Errorf("from disk = %+v", b)
	}
	// A failure is shown too.
	failed := &BackupResult{At: time.Now(), Err: "disk full"}
	if line := h.bot.backupLine(failed); !strings.Contains(line, "не удалась") || !strings.HasSuffix(line, ": disk full") {
		t.Errorf("failed line = %q", line)
	}
}

func TestBackupLoopSchedule(t *testing.T) {
	h := newHarness(t, nil)
	dir := h.bot.cfg.BackupsDir()
	os.MkdirAll(dir, 0o755)
	h.bot.cfg.BackupInterval = time.Hour
	run := func(wait time.Duration) []backup.Snapshot {
		ctx, cancel := context.WithTimeout(h.ctx, wait)
		defer cancel()
		h.bot.backupLoop(ctx)
		list, _ := backup.List(dir)
		return list
	}
	// One taken 10 minutes ago: the next is 50 minutes away.
	recent := time.Now().Add(-10 * time.Minute)
	os.WriteFile(filepath.Join(dir, backup.Name(recent)), []byte("x"), 0o644)
	if list := run(100 * time.Millisecond); len(list) != 1 {
		t.Fatalf("took one too early: %+v", list)
	}
	// One taken two hours ago is overdue: one is taken right away. A
	// snapshot from before a migration does not count.
	os.Remove(filepath.Join(dir, backup.Name(recent)))
	os.WriteFile(filepath.Join(dir, backup.Name(time.Now().Add(-2*time.Hour))), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, store.PreMigrationName(time.Now(), 99)), []byte("x"), 0o644)
	list := run(2 * time.Second)
	var newest *backup.Snapshot
	for i := range list {
		if list[i].Before == 0 {
			newest = &list[i]
			break
		}
	}
	if newest == nil || !newest.At.After(time.Now().Add(-time.Minute)) {
		t.Errorf("overdue: %+v", list)
	}
	// Turned off.
	h.bot.cfg.BackupInterval = 0
	done := make(chan struct{})
	go func() { h.bot.backupLoop(h.ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("loop runs with the interval 0")
	}
}

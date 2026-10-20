package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Mimic890/terakoya/internal/adminapi"
	"github.com/Mimic890/terakoya/internal/bot"
	"github.com/Mimic890/terakoya/internal/stats"
	"github.com/Mimic890/terakoya/internal/store"
)

// fakeBot serves fixed numbers and records what it was asked to do.
type fakeBot struct {
	mu         sync.Mutex
	resets     []string
	broadcasts []adminapi.BroadcastRequest
	syncErr    string
	backupErr  string
}

func (f *fakeBot) AdminStatus(context.Context) (bot.AdminStatus, error) {
	return bot.AdminStatus{Users: 120, Active24h: 15, OpenReports: 3, Commit: "abcdef1234567",
		CommitTime: time.Now().Add(-2 * time.Hour), Uptime: bot.Duration(26 * time.Hour)}, nil
}

func (f *fakeBot) AdminServer(context.Context) (bot.AdminServer, error) {
	return bot.AdminServer{RSS: 20 << 20, Heap: 5 << 20, Goroutines: 12, Version: "abcdef1234567",
		NetRx: 3 << 30, NetTx: 1 << 20, APIToday: store.Traffic{Calls: 10, Sent: 1024, Received: 2048},
		APITotal: store.Traffic{Calls: 100}, Vault: stats.Vault{Total: 50 << 20, Markdown: 1 << 20, Images: 20 << 20, Git: 29 << 20},
		Notes: 10, Commit: "abcdef1234567", Backup: bot.BackupResult{Mode: "folder",
			Next: time.Date(2026, 10, 20, 3, 0, 0, 0, time.UTC)}}, nil
}

func (f *fakeBot) AdminStats(context.Context) (bot.AdminStats, error) {
	return bot.AdminStats{Reports: store.SuggestionCounts{Open: 1, Accepted: 2, Rejected: 3},
		TopAll: []bot.NoteStat{{ID: "x", Title: "Треугольники", Opens: 7}, {ID: "gone", Title: "Старая", Gone: true, Opens: 1}}, Broadcasts: 0,
		Searches: []store.SearchCount{{Query: "параллельность", Count: 5, Results: 4}},
		Missing:  []store.SearchCount{{Query: "трапеция", Count: 3}}}, nil
}

func (f *fakeBot) AdminUsers(_ context.Context, q string, page int) (bot.AdminUsers, error) {
	return bot.AdminUsers{Query: q, Found: 1, Page: page, Pages: 3,
		Users: []bot.AdminUser{{ID: 42, Name: "@anya", Username: "anya", FirstName: "Аня"}}}, nil
}

func (f *fakeBot) AdminUser(_ context.Context, ref string) (bot.AdminUser, error) {
	if ref != "42" && ref != "@anya" {
		return bot.AdminUser{}, bot.ErrNoUser
	}
	return bot.AdminUser{ID: 42, Name: "Ёжик", Username: "anya", FirstName: "Аня", Nickname: "Ёжик", Rank: "Pupil", Accepted: 2}, nil
}

func (f *fakeBot) ResetNickname(_ context.Context, ref string, _ int64) (string, bot.AdminUser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ref == "@bob" {
		return "", bot.AdminUser{}, bot.ErrNoNickname
	}
	f.resets = append(f.resets, ref)
	return "Ёжик", bot.AdminUser{ID: 42, Name: "@anya"}, nil
}

func (f *fakeBot) SyncNow(context.Context) bot.SyncResult {
	if f.syncErr != "" {
		return bot.SyncResult{Err: f.syncErr}
	}
	return bot.SyncResult{OldCommit: "aaaaaaaaa", NewCommit: "bbbbbbbbb", NotesBefore: 10, NotesAfter: 11, Errors: 1,
		Issues: []string{"a.md:3: error: broken link"}}
}

func (f *fakeBot) BackupNow(context.Context) bot.BackupResult {
	if f.backupErr != "" {
		return bot.BackupResult{Err: f.backupErr}
	}
	return bot.BackupResult{Mode: "archive", Path: "/srv/data/backups/terakoya-2026-10-19-0300.tar.gz", Size: 96 << 10, Removed: 1,
		At: time.Date(2026, 10, 19, 3, 0, 0, 0, time.UTC), Next: time.Date(2026, 10, 20, 3, 0, 0, 0, time.UTC)}
}

func (f *fakeBot) RecipientCount(context.Context) (int, error) { return 5, nil }

func (f *fakeBot) Broadcast(_ context.Context, text string, photo []byte) (bot.BroadcastStarted, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.broadcasts = append(f.broadcasts, adminapi.BroadcastRequest{Text: text, Photo: photo})
	return bot.BroadcastStarted{ID: 9, Recipients: 5}, nil
}

func serveFake(t *testing.T) (*fakeBot, string) {
	t.Helper()
	f := &fakeBot{}
	path := filepath.Join(t.TempDir(), "admin.sock")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go adminapi.Serve(ctx, path, f, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(path); err == nil {
			return f, path
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no socket")
	return nil, ""
}

func admin(t *testing.T, socket, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := runAdmin(args, strings.NewReader(stdin), &out, &errOut, socket)
	return code, out.String(), errOut.String()
}

func TestAdminCommands(t *testing.T) {
	f, sock := serveFake(t)
	cases := []struct {
		args  []string
		code  int
		wants []string
	}{
		{[]string{"status"}, 0, []string{"Users:        120 (active in 24h: 15)", "Open reports: 3", "Vault:        abcdef1, committed 2h 0m ago", "Uptime:       1d 2h"}},
		{[]string{"server"}, 0, []string{"RSS 20 МБ, Go heap 5 МБ, 12 goroutines", "received 3 ГБ, sent 1 МБ", "today 10 calls, sent 1 КБ, received 2 КБ",
			"Vault disk:  50 МБ (notes 1 МБ, images 20 МБ, .git 29 МБ)", "Last sync:   none since start"}},
		{[]string{"stats"}, 0, []string{"open 1, accepted 2, rejected 3", "1. Треугольники — 7", "2. Старая (removed) — 1", "Broadcasts: 0",
			"Searched for most, 7 days:\n   1. параллельность — 5", "nothing found, 180 days:\n   1. трапеция — 3"}},
		{[]string{"users", "--search", "аня", "--page", "2"}, 0, []string{`Search "аня": 1 found`, "Page 2 of 3", "42", "@anya"}},
		{[]string{"user", "@anya"}, 0, []string{"Ёжик\n", "Nickname:   Ёжик", "Rank:       Pupil", "accepted 2"}},
		{[]string{"user", "@nobody"}, 1, nil},
		{[]string{"nick-reset", "42"}, 0, []string{`Nickname "Ёжик" reset; the user is now @anya and was told.`}},
		{[]string{"nick-reset", "@bob"}, 1, nil},
		{[]string{"sync"}, 0, []string{"Updated aaaaaaa → bbbbbbb, notes 10 → 11, 1 errors, 0 warnings", "a.md:3: error: broken link"}},
		{[]string{"backup"}, 0, []string{"Backup written and checked: /srv/data/backups/terakoya-2026-10-19-0300.tar.gz (96 КБ)",
			"Old archives removed: 1", "Backup:      mode archive; last 2026-10-19 03:00, 96 КБ; next 2026-10-20 03:00"}},
		{[]string{"server"}, 0, []string{"Backup:      mode folder; last none yet; next 2026-10-20 03:00"}},
		{[]string{"nope"}, 2, nil},
		{nil, 2, nil},
	}
	for _, c := range cases {
		code, out, _ := admin(t, sock, "", c.args...)
		if code != c.code {
			t.Errorf("%v: exit %d, want %d (%s)", c.args, code, c.code, out)
		}
		for _, w := range c.wants {
			if !strings.Contains(out, w) {
				t.Errorf("%v: no %q in\n%s", c.args, w, out)
			}
		}
	}
	if len(f.resets) != 1 || f.resets[0] != "42" {
		t.Errorf("resets = %v", f.resets)
	}
	// An error from the bot is shown and exits non-zero.
	_, _, errOut := admin(t, sock, "", "user", "@nobody")
	if !strings.Contains(errOut, "error: no such user") {
		t.Errorf("stderr = %q", errOut)
	}
	f.backupErr = "vacuum into: disk full"
	if code, out, _ := admin(t, sock, "", "backup"); code != 1 || !strings.Contains(out, "Backup failed: vacuum into: disk full") {
		t.Errorf("failed backup = %d %q", code, out)
	}
	f.syncErr = "fetch: timeout"
	if code, out, _ := admin(t, sock, "", "sync"); code != 1 || !strings.Contains(out, "Sync failed: fetch: timeout") {
		t.Errorf("failed sync = %d %q", code, out)
	}
	// Without a bot.
	if code, _, errOut := admin(t, filepath.Join(t.TempDir(), "none.sock"), "", "status"); code != 1 || !strings.Contains(errOut, "cannot reach the bot") {
		t.Errorf("no bot = %d %q", code, errOut)
	}
}

func TestAdminJSON(t *testing.T) {
	_, sock := serveFake(t)
	code, out, _ := admin(t, sock, "", "--json", "status")
	var s bot.AdminStatus
	if code != 0 || json.Unmarshal([]byte(out), &s) != nil || s.Users != 120 || time.Duration(s.Uptime) != 26*time.Hour {
		t.Errorf("json = %d %s", code, out)
	}
	code, out, _ = admin(t, sock, "", "user", "42", "--json")
	var u bot.AdminUser
	if code != 0 || json.Unmarshal([]byte(out), &u) != nil || u.Nickname != "Ёжик" {
		t.Errorf("json user = %d %s", code, out)
	}
}

func TestAdminBroadcast(t *testing.T) {
	f, sock := serveFake(t)
	// Asked first; anything but yes sends nothing.
	for _, answer := range []string{"\n", "n\n", ""} {
		code, out, _ := admin(t, sock, answer, "broadcast", "--text", "Привет")
		if code != 1 || !strings.Contains(out, "Send to 5 recipients? [y/N] ") || !strings.Contains(out, "Not sent.") {
			t.Errorf("answer %q = %d %q", answer, code, out)
		}
	}
	if len(f.broadcasts) != 0 {
		t.Fatalf("sent without a yes: %+v", f.broadcasts)
	}
	code, out, _ := admin(t, sock, "y\n", "broadcast", "--text", "Привет")
	if code != 0 || !strings.Contains(out, "Broadcast #9 started for 5 recipients") {
		t.Errorf("yes = %d %q", code, out)
	}
	photo := filepath.Join(t.TempDir(), "p.jpg")
	os.WriteFile(photo, []byte("JPEG"), 0o644)
	code, out, _ = admin(t, sock, "", "broadcast", "--text", "С картинкой", "--photo", photo, "--yes")
	if code != 0 || strings.Contains(out, "[y/N]") {
		t.Errorf("--yes = %d %q", code, out)
	}
	if len(f.broadcasts) != 2 || f.broadcasts[0].Text != "Привет" || string(f.broadcasts[1].Photo) != "JPEG" {
		t.Errorf("broadcasts = %+v", f.broadcasts)
	}
	if code, _, _ := admin(t, sock, "", "broadcast"); code != 2 {
		t.Errorf("no text = %d", code)
	}
	if code, _, _ := admin(t, sock, "", "broadcast", "--text", "x", "--photo", "/missing", "--yes"); code != 1 {
		t.Errorf("missing photo = %d", code)
	}
}

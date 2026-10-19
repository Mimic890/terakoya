package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestUsers(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	if _, err := s.User(ctx, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if _, err := s.TouchUser(ctx, 1, "old", "Name"); err != nil {
		t.Fatal(err)
	}
	u, err := s.TouchUser(ctx, 1, "new", "Name")
	if err != nil {
		t.Fatal(err)
	}
	if u.Username != "new" || u.Accepted != 0 {
		t.Errorf("user = %+v", u)
	}
}

func TestMediaCache(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	if err := s.PutMediaFileID(ctx, "abc", "f1"); err != nil {
		t.Fatal(err)
	}
	if err := s.PutMediaFileID(ctx, "abc", "f2"); err != nil {
		t.Fatal(err)
	}
	if id, err := s.MediaFileID(ctx, "abc"); err != nil || id != "f2" {
		t.Fatalf("id = %q, %v", id, err)
	}
	if err := s.DeleteMediaFileID(ctx, "abc"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MediaFileID(ctx, "abc"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestSessions(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	now := time.Unix(1000, 0)
	s.now = func() time.Time { return now }
	ses := Session{TgID: 7, State: "await_edit", ArticlePath: "geometry/7/a.md", BaseSHA: "deadbeef", ExpiresAt: now.Add(time.Hour)}
	if err := s.PutSession(ctx, ses); err != nil {
		t.Fatal(err)
	}
	got, err := s.Session(ctx, 7)
	if err != nil || got.BaseSHA != "deadbeef" || got.ArticlePath != ses.ArticlePath {
		t.Fatalf("session = %+v, %v", got, err)
	}
	now = now.Add(2 * time.Hour)
	// An expired session is reported once, then gone.
	if got, err := s.Session(ctx, 7); !errors.Is(err, ErrExpired) || got.State != ses.State {
		t.Fatalf("expired session: %+v, %v", got, err)
	}
	if _, err := s.Session(ctx, 7); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired session returned again: %v", err)
	}
}

func TestReopenKeepsSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
}

func TestArchives(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	if _, err := s.ArchiveFileID(ctx, "abc"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	s.PutArchiveFileID(ctx, "abc", "f1")
	s.PutArchiveFileID(ctx, "abc", "f2")
	if id, err := s.ArchiveFileID(ctx, "abc"); err != nil || id != "f2" {
		t.Errorf("id = %q, %v", id, err)
	}
}

func TestPrunePins(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	s.Pin(ctx, 1, "keep", 10)
	s.Pin(ctx, 1, "old", 10)
	s.Pin(ctx, 2, "old", 10)
	n, err := s.PrunePins(ctx, func(id string) bool { return id == "keep" })
	if err != nil || n != 2 {
		t.Fatalf("pruned %d, %v", n, err)
	}
	if ids, _ := s.Pins(ctx, 1); len(ids) != 1 || ids[0] != "keep" {
		t.Errorf("pins = %v", ids)
	}
}

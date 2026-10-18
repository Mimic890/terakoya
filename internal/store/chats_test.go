package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

func TestChatMenuTracking(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	last := func(chat int64) int {
		t.Helper()
		id, err := s.LastChatMenu(ctx, chat)
		must(err)
		return id
	}

	must(s.ChatMessageSeen(ctx, 1, 10)) // user's /start
	must(s.SetChatMenu(ctx, 1, 11, false)) // menu
	if got := last(1); got != 11 {
		t.Fatalf("lastMenu = %d, want 11", got)
	}
	must(s.ChatMessageSeen(ctx, 1, 12)) // article
	if got := last(1); got != 0 {
		t.Fatalf("menu is not last, got %d", got)
	}
	must(s.ChatMessageGone(ctx, 1, 12)) // article deleted
	if got := last(1); got != 11 {
		t.Fatalf("after delete lastMenu = %d, want 11", got)
	}
	must(s.DropChatMenu(ctx, 1, 11)) // keyboard removed
	if got := last(1); got != 0 {
		t.Fatalf("dropped menu still returned: %d", got)
	}
	if got := last(2); got != 0 {
		t.Fatalf("unknown chat lastMenu = %d", got)
	}
}

func TestChatMessagesCapped(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	for i := 1; i <= MaxTrackedMessages+10; i++ {
		if err := s.ChatMessageSeen(ctx, 1, i); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM chat_messages WHERE chat_id = 1`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != MaxTrackedMessages {
		t.Fatalf("tracked %d messages, want %d", n, MaxTrackedMessages)
	}
}

// TestChatMenuSurvivesRestart reopens the database, as a restarted bot would.
func TestChatMenuSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetChatMenu(ctx, 1, 20, true); err != nil {
		t.Fatal(err)
	}
	if err := s.ChatMessageSeen(ctx, 1, 21); err != nil { // article
		t.Fatal(err)
	}
	s.Close()

	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ChatMessageGone(ctx, 1, 21); err != nil {
		t.Fatal(err)
	}
	if rich, err := s.RichMenu(ctx, 1, 20); err != nil || !rich {
		t.Errorf("rich = %v, %v", rich, err)
	}
	if rich, _ := s.RichMenu(ctx, 1, 21); rich {
		t.Error("an article counts as a rich menu")
	}
	if id, err := s.LastChatMenu(ctx, 1); err != nil || id != 20 {
		t.Fatalf("lastMenu after restart = %d, %v; want 20", id, err)
	}
}

func TestMessageGroups(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	if err := s.SetMessageGroup(ctx, 42, []int{10, 11, 12}); err != nil {
		t.Fatal(err)
	}
	s.SetMessageGroup(ctx, 7, []int{11, 13}) // another chat
	for _, id := range []int{10, 11, 12} {
		if got, err := s.MessageGroup(ctx, 42, id); err != nil || fmt.Sprint(got) != "[10 11 12]" {
			t.Errorf("group of %d = %v, %v", id, got, err)
		}
	}
	if got, _ := s.MessageGroup(ctx, 42, 99); fmt.Sprint(got) != "[99]" {
		t.Errorf("single message = %v", got)
	}
	if err := s.DropMessageGroup(ctx, 42, 12); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.MessageGroup(ctx, 42, 10); fmt.Sprint(got) != "[10]" {
		t.Errorf("after drop = %v", got)
	}
	if got, _ := s.MessageGroup(ctx, 7, 13); fmt.Sprint(got) != "[11 13]" {
		t.Errorf("other chat = %v", got)
	}
}

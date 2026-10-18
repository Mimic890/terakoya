package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAccessLifecycle(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	now := time.Unix(5000, 0)
	s.now = func() time.Time { return now }
	for id, name := range map[int64]string{42: "anya", 1: "admin", 2: "other"} {
		if _, err := s.TouchUser(ctx, id, name, name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Access(ctx, 42); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no request yet: %v", err)
	}
	if err := s.RequestAccess(ctx, 42, "note", "request"); err != nil {
		t.Fatal(err)
	}
	for admin, msg := range map[int64]int{1: 10, 2: 20} {
		if err := s.PutAccessMessage(ctx, 42, admin, msg); err != nil {
			t.Fatal(err)
		}
	}
	a, err := s.Access(ctx, 42)
	if err != nil || a.Status != AccessPending || a.NoteID != "note" || a.RequestText != "request" || !a.DecidedAt.IsZero() {
		t.Fatalf("pending = %+v, %v", a, err)
	}

	// The first admin wins.
	if ok, err := s.DecideAccess(ctx, 42, true, 1); !ok || err != nil {
		t.Fatalf("decide = %v, %v", ok, err)
	}
	if ok, _ := s.DecideAccess(ctx, 42, false, 2); ok {
		t.Fatal("second decision accepted")
	}
	a, _ = s.Access(ctx, 42)
	if a.Status != AccessGranted || a.DecidedBy != 1 || !a.DecidedAt.Equal(now) {
		t.Fatalf("granted = %+v", a)
	}
	msgs, _ := s.AccessMessages(ctx, 42)
	if len(msgs) != 2 || msgs[2] != 20 {
		t.Fatalf("messages = %v", msgs)
	}
	grants, err := s.Grants(ctx)
	if err != nil || len(grants) != 1 || grants[0].Username != "anya" || grants[0].DecidedBy.Username != "admin" {
		t.Fatalf("grants = %+v, %v", grants, err)
	}

	if ok, _ := s.RevokeAccess(ctx, 42, 2); !ok {
		t.Fatal("revoke refused")
	}
	if ok, _ := s.RevokeAccess(ctx, 42, 2); ok {
		t.Fatal("second revoke accepted")
	}
	if a, _ = s.Access(ctx, 42); a.Status != AccessDenied || a.DecidedBy != 2 {
		t.Fatalf("revoked = %+v", a)
	}

	// A new request starts over.
	if err := s.RequestAccess(ctx, 42, "", "again"); err != nil {
		t.Fatal(err)
	}
	if a, _ = s.Access(ctx, 42); a.Status != AccessPending || !a.DecidedAt.IsZero() || a.DecidedBy != 0 {
		t.Fatalf("re-request = %+v", a)
	}
	if msgs, _ = s.AccessMessages(ctx, 42); len(msgs) != 0 {
		t.Fatalf("old admin messages kept: %v", msgs)
	}

	if u, err := s.UserByUsername(ctx, "@ANYA"); err != nil || u.TgID != 42 {
		t.Fatalf("by username = %+v, %v", u, err)
	}
	if _, err := s.UserByUsername(ctx, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty username: %v", err)
	}
}

func TestAcceptedCounts(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	s.TouchUser(ctx, 42, "", "")
	for i, k := range []string{KindEdit, KindNew, KindReport, KindReport, KindEdit} {
		sg := &Suggestion{ShortID: string(rune('a' + i)), TgID: 42, Kind: k, PRNumber: i + 1}
		if err := s.CreateSuggestion(ctx, sg); err != nil {
			t.Fatal(err)
		}
		if i < 4 {
			s.ResolveSuggestion(ctx, sg.ID, StatusMerged)
		}
	}
	edits, reports, err := s.AcceptedCounts(ctx, 42)
	if err != nil || edits != 2 || reports != 2 {
		t.Fatalf("counts = %d, %d, %v", edits, reports, err)
	}
	all, err := s.UserSuggestions(ctx, 42)
	if err != nil || len(all) != 5 || all[0].Status != StatusOpen || !all[0].ResolvedAt.IsZero() || all[4].ResolvedAt.IsZero() {
		t.Fatalf("history = %+v, %v", all, err)
	}
	if err := s.SetSuggestionComment(ctx, all[0].ID, "Слишком коротко."); err != nil {
		t.Fatal(err)
	}
	if sg, _ := s.SuggestionByNumber(ctx, 5); sg.Comment != "Слишком коротко." {
		t.Errorf("comment = %q", sg.Comment)
	}
}

func TestPins(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	tick := int64(0)
	s.now = func() time.Time { tick++; return time.Unix(0, tick) }
	for _, id := range []string{"a", "b", "c"} {
		if err := s.Pin(ctx, 42, id, 3); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Pin(ctx, 42, "d", 3); !errors.Is(err, ErrPinLimit) {
		t.Fatalf("over the limit: %v", err)
	}
	if err := s.Pin(ctx, 7, "a", 3); err != nil {
		t.Fatalf("other user: %v", err)
	}
	if err := s.Unpin(ctx, 42, "b"); err != nil {
		t.Fatal(err)
	}
	pins, _ := s.Pins(ctx, 42)
	if len(pins) != 2 || pins[0] != "c" || pins[1] != "a" {
		t.Fatalf("pins = %v, want newest first", pins)
	}
	if ok, _ := s.Pinned(ctx, 42, "a"); !ok {
		t.Error("a not pinned")
	}
	if ok, _ := s.Pinned(ctx, 42, "b"); ok {
		t.Error("b still pinned")
	}
}

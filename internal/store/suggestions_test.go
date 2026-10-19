package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSuggestionLifecycle(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	now := time.Unix(5000, 0)
	s.now = func() time.Time { return now }
	if _, err := s.TouchUser(ctx, 42, "anya", "Аня"); err != nil {
		t.Fatal(err)
	}

	open, last, err := s.SuggestQuota(ctx, 42)
	if err != nil || open != 0 || !last.IsZero() {
		t.Fatalf("quota = %d, %v, %v", open, last, err)
	}

	sg := &Suggestion{ShortID: "abc123", TgID: 42, Kind: KindReport, ArticlePath: "content/geometry/7/a.md", Title: "A",
		BaseSHA: "deadbeef", Branch: "report/42-a-abc123", Issue: 7, IssueURL: "https://host/issues/7"}
	if err := s.CreateSuggestion(ctx, sg); err != nil {
		t.Fatal(err)
	}
	open, last, _ = s.SuggestQuota(ctx, 42)
	if open != 1 || !last.Equal(now) {
		t.Fatalf("quota after create = %d, %v", open, last)
	}

	got, err := s.SuggestionByNumber(ctx, 7)
	if err != nil || got.BaseSHA != "deadbeef" || got.Title != "A" || got.Branch != sg.Branch || got.Status != StatusOpen {
		t.Fatalf("by PR = %+v, %v", got, err)
	}
	if _, err := s.SuggestionByNumber(ctx, 99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown PR: %v", err)
	}

	changed, err := s.ResolveSuggestion(ctx, sg.ID, StatusMerged)
	if err != nil || !changed {
		t.Fatalf("resolve = %v, %v", changed, err)
	}
	// A repeated webhook delivery must not count twice.
	changed, err = s.ResolveSuggestion(ctx, sg.ID, StatusMerged)
	if err != nil || changed {
		t.Fatalf("second resolve = %v, %v", changed, err)
	}
	u, _ := s.User(ctx, 42)
	if u.Accepted != 1 {
		t.Errorf("accepted = %d, want 1", u.Accepted)
	}

	closed := &Suggestion{ShortID: "def456", TgID: 42, Kind: KindReport, ArticlePath: "content/geometry/7/b.md",
		BaseSHA: "deadbeef", Issue: 8}
	if err := s.CreateSuggestion(ctx, closed); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.ResolveSuggestion(ctx, closed.ID, StatusClosed); err != nil || !changed {
		t.Fatalf("close = %v, %v", changed, err)
	}
	u, _ = s.User(ctx, 42)
	open, _, _ = s.SuggestQuota(ctx, 42)
	if u.Accepted != 1 || open != 0 {
		t.Errorf("after close: accepted %d open %d", u.Accepted, open)
	}

	// Rows of another kind, which early versions wrote, do not count.
	edit := &Suggestion{ShortID: "e1", TgID: 42, Kind: "edit", ArticlePath: "content/geometry/7/c.md",
		BaseSHA: "deadbeef", Branch: "suggest/42-c-e1", Issue: 9}
	s.CreateSuggestion(ctx, edit)
	s.ResolveSuggestion(ctx, edit.ID, StatusMerged)
	u, _ = s.User(ctx, 42)
	all, _ := s.UserSuggestions(ctx, 42)
	if u.Accepted != 1 || len(all) != 3 || all[0].Kind != "edit" || all[0].ResolvedAt.IsZero() {
		t.Errorf("with an edit: accepted %d, history %+v", u.Accepted, all)
	}
	if err := s.SetSuggestionComment(ctx, closed.ID, "Слишком коротко."); err != nil {
		t.Fatal(err)
	}
	if sg, _ := s.SuggestionByNumber(ctx, 8); sg.Comment != "Слишком коротко." {
		t.Errorf("comment = %q", sg.Comment)
	}
}

func TestReports(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	s.TouchUser(ctx, 42, "anya", "Аня")
	rep := &Suggestion{ShortID: "r1", TgID: 42, Kind: KindReport, ArticlePath: "content/geometry/7/a.md", BaseSHA: "deadbeef", Issue: 12}
	edit := &Suggestion{ShortID: "e1", TgID: 42, Kind: "edit", ArticlePath: "content/geometry/7/a.md", BaseSHA: "deadbeef", Branch: "b", Issue: 13}
	for _, sg := range []*Suggestion{rep, edit} {
		if err := s.CreateSuggestion(ctx, sg); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.OpenSuggestions(ctx)
	if err != nil || len(list) != 2 || list[0].Kind != KindReport || list[1].Issue != 13 {
		t.Fatalf("open = %+v, %v", list, err)
	}
	// Only a closed report can be accepted late, once.
	if ok, err := s.AcceptLate(ctx, rep.ID); err != nil || ok {
		t.Errorf("open report accepted late: %v %v", ok, err)
	}
	s.ResolveSuggestion(ctx, rep.ID, StatusClosed)
	s.ResolveSuggestion(ctx, edit.ID, StatusClosed)
	if ok, err := s.AcceptLate(ctx, edit.ID); err != nil || ok {
		t.Errorf("closed edit accepted late: %v %v", ok, err)
	}
	if ok, err := s.AcceptLate(ctx, rep.ID); err != nil || !ok {
		t.Errorf("late accept = %v %v", ok, err)
	}
	if ok, _ := s.AcceptLate(ctx, rep.ID); ok {
		t.Error("accepted twice")
	}
	if u, _ := s.User(ctx, 42); u.Accepted != 1 {
		t.Errorf("accepted = %d", u.Accepted)
	}
	if list, _ := s.OpenSuggestions(ctx); len(list) != 0 {
		t.Errorf("open after resolve = %+v", list)
	}
}

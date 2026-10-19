package store

import (
	"context"
	"testing"
	"time"
)

func TestSeenAndUserCounts(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.Local)
	s.now = func() time.Time { return now }
	// Ольга joined 10 days ago and was last seen 3 days ago.
	s.now = func() time.Time { return now.Add(-10 * 24 * time.Hour) }
	s.TouchUser(ctx, 1, "", "Ольга")
	s.now = func() time.Time { return now.Add(-3 * 24 * time.Hour) }
	s.Seen(ctx, 1, "", "Ольга")
	// Аня joined today and is active; Seen creates Борис.
	s.now = func() time.Time { return now }
	s.TouchUser(ctx, 2, "anya", "Аня")
	s.Seen(ctx, 2, "anya", "Аня")
	s.Seen(ctx, 3, "boris", "Борис")
	s.MarkBlocked(ctx, 3)

	c, err := s.CountUsers(ctx, time.Date(2026, 10, 19, 0, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatal(err)
	}
	if c != (UserCounts{All: 3, NewToday: 2, New7d: 2, Active24h: 2, Active7d: 3, Blocked: 1}) {
		t.Errorf("counts = %+v", c)
	}
	u, err := s.UserInfo(ctx, 1)
	if err != nil || !u.LastSeen.Equal(now.Add(-3*24*time.Hour).Truncate(time.Second)) || !u.FirstSeen.Before(u.LastSeen) {
		t.Errorf("info = %+v, %v", u, err)
	}
	if u, _ := s.UserInfo(ctx, 3); u.FirstName != "Борис" || !u.Blocked {
		t.Errorf("created by Seen = %+v", u)
	}
	if _, err := s.UserInfo(ctx, 99); err != ErrNotFound {
		t.Errorf("unknown = %v", err)
	}
}

func TestFindUsers(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	at := int64(1000)
	s.now = func() time.Time { at += 10; return time.Unix(at, 0) }
	s.Seen(ctx, 10, "olga", "Ольга")
	s.Seen(ctx, 20, "anya", "Аня")
	s.Seen(ctx, 30, "", "Борис")
	s.SetNickname(ctx, 30, "Звезда", "звезда")
	s.Seen(ctx, 10, "olga", "Ольга") // the most recent
	sg := &Suggestion{ShortID: "a", TgID: 20, Kind: KindReport, PRNumber: 1}
	s.CreateSuggestion(ctx, sg)
	s.ResolveSuggestion(ctx, sg.ID, StatusMerged)
	s.CreateSuggestion(ctx, &Suggestion{ShortID: "b", TgID: 20, Kind: KindReport, PRNumber: 2})

	ids := func(q string) []int64 {
		all, err := s.FindUsers(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		var out []int64
		for _, u := range all {
			out = append(out, u.TgID)
		}
		return out
	}
	if got := ids(""); len(got) != 3 || got[0] != 10 || got[1] != 30 || got[2] != 20 {
		t.Errorf("all = %v", got)
	}
	for q, want := range map[string]int64{"ЗВЁЗ": 0, "звез": 30, "@ANYA": 20, "оль": 10, "20": 20} {
		got := ids(q)
		if want == 0 && len(got) != 0 || want != 0 && (len(got) != 1 || got[0] != want) {
			t.Errorf("%q = %v, want %d", q, got, want)
		}
	}
	all, _ := s.FindUsers(ctx, "anya")
	if all[0].Accepted != 1 || all[0].Open != 1 || all[0].Rejected != 0 {
		t.Errorf("counts = %+v", all[0])
	}
}

func TestNoteOpensAndTraffic(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	day := time.Date(2026, 10, 10, 12, 0, 0, 0, time.Local)
	s.now = func() time.Time { return day }
	s.NoteOpened(ctx, "old")
	s.NoteOpened(ctx, "old")
	s.NoteOpened(ctx, "old")
	day = day.Add(8 * 24 * time.Hour)
	for i := 0; i < 2; i++ {
		s.NoteOpened(ctx, "new")
	}
	s.NoteOpened(ctx, "old")
	all, _ := s.TopNotes(ctx, "", 10)
	if len(all) != 2 || all[0] != (NoteCount{"old", 4}) || all[1] != (NoteCount{"new", 2}) {
		t.Errorf("all time = %+v", all)
	}
	week, _ := s.TopNotes(ctx, Day(day.Add(-7*24*time.Hour)), 1)
	if len(week) != 1 || week[0] != (NoteCount{"new", 2}) {
		t.Errorf("week = %+v", week)
	}

	s.AddTraffic(ctx, "2026-10-17", Traffic{1, 10, 100})
	s.AddTraffic(ctx, "2026-10-18", Traffic{2, 20, 200})
	s.AddTraffic(ctx, "2026-10-18", Traffic{3, 30, 300})
	today, total, err := s.TrafficTotals(ctx, "2026-10-18")
	if err != nil || today != (Traffic{5, 50, 500}) || total != (Traffic{6, 60, 600}) {
		t.Errorf("traffic = %+v %+v %v", today, total, err)
	}
}

func TestCountSuggestions(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	now := time.Unix(100*24*3600, 0)
	s.now = func() time.Time { return now }
	s.TouchUser(ctx, 1, "", "")
	add := func(n int, status string, age time.Duration) {
		s.now = func() time.Time { return now.Add(-age) }
		sg := &Suggestion{ShortID: string(rune('a' + n)), TgID: 1, Kind: KindReport, PRNumber: n}
		s.CreateSuggestion(ctx, sg)
		if status != StatusOpen {
			s.ResolveSuggestion(ctx, sg.ID, status)
		}
	}
	add(1, StatusOpen, time.Hour)
	add(2, StatusOpen, 30*24*time.Hour)
	add(3, StatusMerged, time.Hour)
	add(4, StatusMerged, 30*24*time.Hour)
	add(5, StatusClosed, 30*24*time.Hour)
	s.now = func() time.Time { return now }
	if c, _ := s.CountSuggestions(ctx, time.Time{}); c != (SuggestionCounts{2, 2, 1}) {
		t.Errorf("total = %+v", c)
	}
	if c, _ := s.CountSuggestions(ctx, now.Add(-7*24*time.Hour)); c != (SuggestionCounts{1, 1, 0}) {
		t.Errorf("7 days = %+v", c)
	}
	if n, _ := s.OpenCount(ctx); n != 2 {
		t.Errorf("open = %d", n)
	}
}

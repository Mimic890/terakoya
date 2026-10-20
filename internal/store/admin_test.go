package store

import (
	"context"
	"fmt"
	"strings"
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
	sg := &Suggestion{ShortID: "a", TgID: 20, Kind: KindReport, Issue: 1}
	s.CreateSuggestion(ctx, sg)
	s.ResolveSuggestion(ctx, sg.ID, StatusMerged)
	s.CreateSuggestion(ctx, &Suggestion{ShortID: "b", TgID: 20, Kind: KindReport, Issue: 2})

	ids := func(q string) []int64 {
		all, total, err := s.FindUsers(ctx, q, 10, 0)
		if err != nil || total != len(all) {
			t.Fatalf("%q: %d of %d, %v", q, len(all), total, err)
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
	all, _, _ := s.FindUsers(ctx, "anya", 10, 0)
	if all[0].Accepted != 1 || all[0].Open != 1 || all[0].Rejected != 0 {
		t.Errorf("counts = %+v", all[0])
	}
	// LIKE's wildcards are plain characters here.
	for _, q := range []string{"%", "_", "о_ьга"} {
		if got := ids(q); len(got) != 0 {
			t.Errorf("%q = %v", q, got)
		}
	}
	// A user never seen comes last.
	s.TouchUser(ctx, 40, "", "Новенький")
	s.db.Exec(`UPDATE users SET last_seen_at = NULL WHERE tg_id = 40`)
	if got := ids(""); len(got) != 4 || got[3] != 40 {
		t.Errorf("with one never seen = %v", got)
	}
	// Pages.
	page, total, _ := s.FindUsers(ctx, "", 2, 2)
	if total != 4 || len(page) != 2 || page[0].TgID != 20 || page[1].TgID != 40 {
		t.Errorf("page 2 = %+v of %d", page, total)
	}
	if page, total, _ := s.FindUsers(ctx, "", 2, 4); total != 4 || len(page) != 0 {
		t.Errorf("past the end = %+v of %d", page, total)
	}
}

func TestFindUsersAtScale(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	tx, _ := s.db.Begin()
	for i := 1; i <= 20000; i++ {
		tx.Exec(`INSERT INTO users (tg_id, username, first_name, created_at, updated_at, last_seen_at) VALUES (?, ?, ?, ?, ?, ?)`,
			i, fmt.Sprintf("user%d", i), "Пользователь", i, i, i)
	}
	tx.Commit()
	start := time.Now()
	page, total, err := s.FindUsers(ctx, "", 10, 100)
	took := time.Since(start)
	if err != nil || total != 20000 || len(page) != 10 || page[0].TgID != 19900 {
		t.Fatalf("page = %d of %d, first %+v, %v", len(page), total, page, err)
	}
	if found, n, _ := s.FindUsers(ctx, "USER1999", 10, 0); n != 11 || found[0].TgID != 19999 {
		t.Errorf("search = %d", n)
	}
	t.Logf("a page of 20000 users took %v", took)
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
		sg := &Suggestion{ShortID: string(rune('a' + n)), TgID: 1, Kind: KindReport, Issue: n}
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

func TestCleanup(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	now := time.Now()
	s.SetClock(func() time.Time { return now.Add(-8 * 24 * time.Hour) })
	s.SetMessageGroup(ctx, 42, []int{1, 2, 3})
	s.SetClock(func() time.Time { return now })
	s.SetMessageGroup(ctx, 42, []int{10, 11})
	for _, ses := range []Session{
		{TgID: 1, State: "a", ExpiresAt: now.Add(-48 * time.Hour)},
		{TgID: 2, State: "a", ExpiresAt: now.Add(-time.Hour)}, // still told once
		{TgID: 3, State: "a", ExpiresAt: now.Add(time.Hour)},
	} {
		s.PutSession(ctx, ses)
	}
	for _, days := range []int{200, 181, 179, 0} {
		s.SetClock(func() time.Time { return now.AddDate(0, 0, -days) })
		s.NoteOpened(ctx, "n")
		s.LogSearch(ctx, "трапеция", 0)
	}
	s.SetClock(func() time.Time { return now })

	c, err := s.Cleanup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if c != (Cleaned{MessageParts: 3, Sessions: 1, NoteOpens: 2, Searches: 2}) {
		t.Errorf("cleaned = %+v", c)
	}
	if g, _ := s.MessageGroup(ctx, 42, 2); len(g) != 1 {
		t.Errorf("old group = %v", g)
	}
	if g, _ := s.MessageGroup(ctx, 42, 11); len(g) != 2 {
		t.Errorf("new group = %v", g)
	}
	if _, err := s.Session(ctx, 2); err != ErrExpired {
		t.Errorf("recently expired session: %v", err)
	}
	if top, _ := s.TopNotes(ctx, "", 1); len(top) != 1 || top[0].Opens != 2 {
		t.Errorf("opens = %+v", top)
	}
	if top, _ := s.TopSearches(ctx, time.Time{}, 1); len(top) != 1 || top[0].Count != 2 {
		t.Errorf("searches = %+v", top)
	}
}

func TestSearches(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	now := time.Now()
	at := func(days int) { s.SetClock(func() time.Time { return now.AddDate(0, 0, -days) }) }
	for _, q := range []struct {
		days    int
		query   string
		results int
	}{
		{10, "окружность", 0}, {10, "окружность", 0}, {10, "окружность", 0},
		{3, "трапеция", 0}, {2, "трапеция", 0},
		{3, "параллельность", 4}, {2, "параллельность", 4}, {1, "параллельность", 5},
		// Material was added: the last search finds it.
		{5, "призма", 0}, {5, "призма", 0}, {1, "призма", 2},
		{1, "угол", 1},
	} {
		at(q.days)
		if err := s.LogSearch(ctx, q.query, q.results); err != nil {
			t.Fatal(err)
		}
	}
	week := now.AddDate(0, 0, -7)
	top, err := s.TopSearches(ctx, week, 10)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range top {
		got = append(got, fmt.Sprintf("%s:%d:%d", c.Query, c.Count, c.Results))
	}
	if strings.Join(got, " ") != "параллельность:3:5 призма:3:2 трапеция:2:0 угол:1:1" {
		t.Errorf("top = %v", got)
	}
	missing, _ := s.MissingSearches(ctx, now.Add(-KeepSearches), 10)
	got = nil
	for _, c := range missing {
		got = append(got, fmt.Sprintf("%s:%d", c.Query, c.Count))
	}
	if strings.Join(got, " ") != "окружность:3 трапеция:2" {
		t.Errorf("missing = %v", got)
	}
	if top, _ := s.TopSearches(ctx, week, 2); len(top) != 2 {
		t.Errorf("limit: %d", len(top))
	}
}

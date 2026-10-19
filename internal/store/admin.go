package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Day is the key of per-day counters, in local time.
func Day(t time.Time) string { return t.Format("2006-01-02") }

// Seen records that the user did something now, creating them if needed.
// Callers throttle it.
func (s *Store) Seen(ctx context.Context, tgID int64, username, firstName string) error {
	now := s.now().Unix()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (tg_id, username, first_name, created_at, updated_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (tg_id) DO UPDATE SET last_seen_at = excluded.last_seen_at`,
		tgID, username, firstName, now, now, now)
	return err
}

// NoteOpened counts an opening of a note today.
func (s *Store) NoteOpened(ctx context.Context, noteID string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO note_opens (note_id, day, count) VALUES (?, ?, 1)
		ON CONFLICT (note_id, day) DO UPDATE SET count = count + 1`, noteID, Day(s.now()))
	return err
}

// NoteCount is how often a note was opened.
type NoteCount struct {
	NoteID string
	Opens  int
}

// TopNotes lists the n most opened notes since the given day ("" for all
// time), most opened first.
func (s *Store) TopNotes(ctx context.Context, since string, n int) ([]NoteCount, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT note_id, SUM(count) AS n FROM note_opens WHERE day >= ?
		GROUP BY note_id ORDER BY n DESC, note_id LIMIT ?`, since, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NoteCount
	for rows.Next() {
		var c NoteCount
		if err := rows.Scan(&c.NoteID, &c.Opens); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Traffic is calls to the Telegram API and the bytes they moved.
type Traffic struct {
	Calls, Sent, Received int64
}

// Add sums two traffic counts.
func (t Traffic) Add(u Traffic) Traffic {
	return Traffic{t.Calls + u.Calls, t.Sent + u.Sent, t.Received + u.Received}
}

// AddTraffic adds to a day's traffic.
func (s *Store) AddTraffic(ctx context.Context, day string, t Traffic) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO api_traffic (day, calls, sent, received) VALUES (?, ?, ?, ?)
		ON CONFLICT (day) DO UPDATE SET calls = calls + excluded.calls, sent = sent + excluded.sent,
			received = received + excluded.received`, day, t.Calls, t.Sent, t.Received)
	return err
}

// TrafficTotals returns the stored traffic of a day and of all days.
func (s *Store) TrafficTotals(ctx context.Context, day string) (today, total Traffic, err error) {
	err = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(CASE WHEN day = ? THEN calls END), 0), COALESCE(SUM(CASE WHEN day = ? THEN sent END), 0),
			COALESCE(SUM(CASE WHEN day = ? THEN received END), 0),
			COALESCE(SUM(calls), 0), COALESCE(SUM(sent), 0), COALESCE(SUM(received), 0)
		FROM api_traffic`, day, day, day).Scan(&today.Calls, &today.Sent, &today.Received, &total.Calls, &total.Sent, &total.Received)
	return today, total, err
}

// UserCounts are the totals of /admin's «Пользователи».
type UserCounts struct {
	All, NewToday, New7d, Active24h, Active7d, Blocked int
}

// CountUsers counts users; today starts at the given time.
func (s *Store) CountUsers(ctx context.Context, today time.Time) (UserCounts, error) {
	now := s.now()
	var c UserCounts
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*),
			COALESCE(SUM(created_at >= ?), 0), COALESCE(SUM(created_at >= ?), 0),
			COALESCE(SUM(last_seen_at >= ?), 0), COALESCE(SUM(last_seen_at >= ?), 0),
			COALESCE(SUM(blocked), 0)
		FROM users`,
		today.Unix(), now.Add(-7*24*time.Hour).Unix(), now.Add(-24*time.Hour).Unix(), now.Add(-7*24*time.Hour).Unix(),
	).Scan(&c.All, &c.NewToday, &c.New7d, &c.Active24h, &c.Active7d, &c.Blocked)
	return c, err
}

// UserInfo is a user with what /admin shows about them.
type UserInfo struct {
	User
	Open, Rejected int
	FirstSeen      time.Time
	LastSeen       time.Time // zero if never recorded
	Blocked        bool
}

const userInfoColumns = `u.tg_id, u.username, u.first_name, u.nickname, u.hide_top, u.blocked, u.created_at, u.last_seen_at,
	COALESCE(SUM(s.kind = 'report' AND s.status = 'merged'), 0),
	COALESCE(SUM(s.status = 'open'), 0),
	COALESCE(SUM(s.status = 'closed'), 0)`

func scanUserInfo(row interface{ Scan(...any) error }) (UserInfo, error) {
	var u UserInfo
	var created int64
	var seen sql.NullInt64
	err := row.Scan(&u.TgID, &u.Username, &u.FirstName, &u.Nickname, &u.HideTop, &u.Blocked, &created, &seen,
		&u.Accepted, &u.Open, &u.Rejected)
	u.FirstSeen = time.Unix(created, 0)
	if seen.Valid {
		u.LastSeen = time.Unix(seen.Int64, 0)
	}
	return u, err
}

// UserInfo returns one user.
func (s *Store) UserInfo(ctx context.Context, tgID int64) (UserInfo, error) {
	u, err := scanUserInfo(s.db.QueryRowContext(ctx, `
		SELECT `+userInfoColumns+` FROM users u LEFT JOIN suggestions s ON s.tg_id = u.tg_id
		WHERE u.tg_id = ? GROUP BY u.tg_id`, tgID))
	if errors.Is(err, sql.ErrNoRows) {
		return UserInfo{}, ErrNotFound
	}
	return u, err
}

// FindUsers lists users, the most recently active first. A query matches
// a Telegram ID exactly, or the nickname, @username or first name as a
// part, ignoring case.
func (s *Store) FindUsers(ctx context.Context, query string) ([]UserInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+userInfoColumns+` FROM users u LEFT JOIN suggestions s ON s.tg_id = u.tg_id
		GROUP BY u.tg_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	q := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(query), "@"))
	id, idErr := strconv.ParseInt(q, 10, 64)
	var out []UserInfo
	for rows.Next() {
		u, err := scanUserInfo(rows)
		if err != nil {
			return nil, err
		}
		match := q == "" || idErr == nil && u.TgID == id
		for _, f := range []string{u.Nickname, u.Username, u.FirstName} {
			match = match || q != "" && strings.Contains(strings.ToLower(f), q)
		}
		if match {
			out = append(out, u)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].LastSeen, out[j].LastSeen
		if !a.Equal(b) {
			return a.After(b)
		}
		return out[i].FirstSeen.After(out[j].FirstSeen)
	})
	return out, rows.Err()
}

// SuggestionCounts are open, accepted and rejected reports.
type SuggestionCounts struct {
	Open, Accepted, Rejected int
}

// CountSuggestions counts reports; with since non-zero, only those opened
// (open ones) or resolved (the others) since then.
func (s *Store) CountSuggestions(ctx context.Context, since time.Time) (SuggestionCounts, error) {
	var c SuggestionCounts
	at := since.Unix()
	if since.IsZero() {
		at = 0
	}
	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(status = 'open' AND created_at >= ?), 0),
			COALESCE(SUM(status = 'merged' AND COALESCE(resolved_at, 0) >= ?), 0),
			COALESCE(SUM(status = 'closed' AND COALESCE(resolved_at, 0) >= ?), 0)
		FROM suggestions WHERE kind = 'report'`, at, at, at).Scan(&c.Open, &c.Accepted, &c.Rejected)
	return c, err
}

// OpenCount counts every unresolved suggestion.
func (s *Store) OpenCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM suggestions WHERE status = 'open'`).Scan(&n)
	return n, err
}

// MediaCacheRows counts cached file_ids.
func (s *Store) MediaCacheRows(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_cache`).Scan(&n)
	return n, err
}

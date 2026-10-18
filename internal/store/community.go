package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ErrNicknameTaken is returned when another user has the nickname.
var ErrNicknameTaken = errors.New("nickname taken")

// SetNickname sets the user's nickname, or clears it with "". key is the
// folded form that must be unique among users.
func (s *Store) SetNickname(ctx context.Context, tgID int64, nick, key string) error {
	var k any = key
	if nick == "" {
		k = nil
	}
	_, err := s.db.ExecContext(ctx, `UPDATE users SET nickname = ?, nickname_key = ? WHERE tg_id = ?`, nick, k, tgID)
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return ErrNicknameTaken
	}
	return err
}

// NicknameTaken reports whether a user other than tgID has the key.
func (s *Store) NicknameTaken(ctx context.Context, tgID int64, key string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE nickname_key = ? AND tg_id != ?`, key, tgID).Scan(&n)
	return n > 0, err
}

// SetHideTop leaves the user out of the top, or puts them back.
func (s *Store) SetHideTop(ctx context.Context, tgID int64, hide bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET hide_top = ? WHERE tg_id = ?`, hide, tgID)
	return err
}

// TopEntry is a user with accepted reports.
type TopEntry struct {
	User
	// Reached is when the user's latest accepted report was accepted:
	// of two users with the same count, the earlier one is higher.
	Reached time.Time
}

// Leaderboard lists every user with accepted reports, hidden ones
// included: most accepted first, then whoever reached the count first.
func (s *Store) Leaderboard(ctx context.Context) ([]TopEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.tg_id, u.username, u.first_name, u.nickname, u.hide_top, COUNT(*) AS n,
			MAX(COALESCE(s.resolved_at, s.created_at)) AS reached, MAX(s.id) AS last
		FROM suggestions s JOIN users u ON u.tg_id = s.tg_id
		WHERE s.kind = 'report' AND s.status = 'merged'
		GROUP BY u.tg_id
		ORDER BY n DESC, reached ASC, last ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TopEntry
	for rows.Next() {
		var e TopEntry
		var reached, last int64
		if err := rows.Scan(&e.TgID, &e.Username, &e.FirstName, &e.Nickname, &e.HideTop, &e.Accepted, &reached, &last); err != nil {
			return nil, err
		}
		e.Reached = time.Unix(reached, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Recipients lists the users a broadcast goes to: everyone who has
// started the bot, except those in skip and those who blocked it.
func (s *Store) Recipients(ctx context.Context, skip []int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT tg_id FROM users WHERE blocked = 0 ORDER BY tg_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	skipped := map[int64]bool{}
	for _, id := range skip {
		skipped[id] = true
	}
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if !skipped[id] {
			out = append(out, id)
		}
	}
	return out, rows.Err()
}

// MarkBlocked records that the user blocked the bot.
func (s *Store) MarkBlocked(ctx context.Context, tgID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET blocked = 1 WHERE tg_id = ?`, tgID)
	return err
}

// Broadcast is a message an admin sent to every user.
type Broadcast struct {
	ID       int64
	AdminID  int64
	Markdown string // as sent, rich Markdown
	Photo    string // file_id, or ""
	Created  time.Time
	Finished time.Time // zero while sending
	// Recipients, and how many got it, had blocked the bot or failed.
	Recipients, Sent, Blocked, Failed int
}

// CreateBroadcast stores a broadcast about to be sent.
func (s *Store) CreateBroadcast(ctx context.Context, b *Broadcast) error {
	b.Created = time.Unix(s.now().Unix(), 0)
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO broadcasts (admin_id, markdown, photo, created_at, recipients) VALUES (?, ?, ?, ?, ?)`,
		b.AdminID, b.Markdown, b.Photo, b.Created.Unix(), b.Recipients)
	if err != nil {
		return err
	}
	b.ID, _ = res.LastInsertId()
	return nil
}

// UpdateBroadcast stores the counts so far; finished marks it done.
func (s *Store) UpdateBroadcast(ctx context.Context, b *Broadcast, finished bool) error {
	var at any
	if finished {
		b.Finished = time.Unix(s.now().Unix(), 0)
		at = b.Finished.Unix()
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE broadcasts SET sent = ?, blocked = ?, failed = ?, finished_at = COALESCE(?, finished_at) WHERE id = ?`,
		b.Sent, b.Blocked, b.Failed, at, b.ID)
	return err
}

// Broadcasts lists the broadcasts, newest first.
func (s *Store) Broadcasts(ctx context.Context) ([]Broadcast, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, admin_id, markdown, photo, created_at, finished_at, recipients, sent, blocked, failed
		FROM broadcasts ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Broadcast
	for rows.Next() {
		var b Broadcast
		var created int64
		var finished sql.NullInt64
		if err := rows.Scan(&b.ID, &b.AdminID, &b.Markdown, &b.Photo, &created, &finished,
			&b.Recipients, &b.Sent, &b.Blocked, &b.Failed); err != nil {
			return nil, err
		}
		b.Created = time.Unix(created, 0)
		if finished.Valid {
			b.Finished = time.Unix(finished.Int64, 0)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

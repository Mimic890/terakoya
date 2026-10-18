package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Suggestion kinds and statuses.
const (
	KindEdit   = "edit"
	KindNew    = "new"
	KindReport = "report" // an issue rather than a pull request

	StatusOpen   = "open"
	StatusMerged = "merged"
	StatusClosed = "closed"
)

// Suggestion is an edit proposed by a user, submitted as a pull request,
// or an error report, submitted as an issue. PRNumber holds the issue
// number for reports; both hosts number issues and pull requests together.
type Suggestion struct {
	ID          int64
	ShortID     string
	TgID        int64
	Kind        string
	ArticlePath string
	Title       string
	BaseSHA     string
	Branch      string
	PRNumber    int
	PRURL       string
	Status      string
	CreatedAt   time.Time
	ResolvedAt  time.Time // zero while open
	// Comment is the reviewer's last comment on a rejected suggestion.
	Comment string
}

// CreateSuggestion stores a new suggestion and records the time of the
// user's latest suggestion for rate limiting.
func (s *Store) CreateSuggestion(ctx context.Context, sg *Suggestion) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := s.now()
	res, err := tx.ExecContext(ctx, `
		INSERT INTO suggestions (short_id, tg_id, kind, article_path, title, base_sha, branch, pr_number, pr_url, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'open', ?)`,
		sg.ShortID, sg.TgID, sg.Kind, sg.ArticlePath, sg.Title, sg.BaseSHA, sg.Branch, sg.PRNumber, sg.PRURL, now.Unix())
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET last_suggest_at = ? WHERE tg_id = ?`, now.Unix(), sg.TgID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	sg.ID, _ = res.LastInsertId()
	sg.Status, sg.CreatedAt = StatusOpen, time.Unix(now.Unix(), 0)
	return nil
}

const suggestionColumns = `id, short_id, tg_id, kind, article_path, title, base_sha, branch, pr_number, pr_url, status,
	created_at, resolved_at, comment`

func scanSuggestion(row interface{ Scan(...any) error }) (Suggestion, error) {
	var sg Suggestion
	var created int64
	var resolved sql.NullInt64
	err := row.Scan(&sg.ID, &sg.ShortID, &sg.TgID, &sg.Kind, &sg.ArticlePath, &sg.Title, &sg.BaseSHA, &sg.Branch,
		&sg.PRNumber, &sg.PRURL, &sg.Status, &created, &resolved, &sg.Comment)
	sg.CreatedAt = time.Unix(created, 0)
	if resolved.Valid {
		sg.ResolvedAt = time.Unix(resolved.Int64, 0)
	}
	return sg, err
}

// UserSuggestions lists a user's suggestions, newest first.
func (s *Store) UserSuggestions(ctx context.Context, tgID int64) ([]Suggestion, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+suggestionColumns+` FROM suggestions WHERE tg_id = ? ORDER BY id DESC`, tgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Suggestion
	for rows.Next() {
		sg, err := scanSuggestion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sg)
	}
	return out, rows.Err()
}

// SetSuggestionComment stores the reviewer's comment.
func (s *Store) SetSuggestionComment(ctx context.Context, id int64, comment string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE suggestions SET comment = ? WHERE id = ?`, comment, id)
	return err
}

// SuggestionByNumber finds the suggestion behind a pull request or issue.
func (s *Store) SuggestionByNumber(ctx context.Context, number int) (Suggestion, error) {
	sg, err := scanSuggestion(s.db.QueryRowContext(ctx,
		`SELECT `+suggestionColumns+` FROM suggestions WHERE pr_number = ?`, number))
	if errors.Is(err, sql.ErrNoRows) {
		return Suggestion{}, ErrNotFound
	}
	return sg, err
}

// OpenSuggestions lists all unresolved suggestions, oldest first.
func (s *Store) OpenSuggestions(ctx context.Context) ([]Suggestion, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+suggestionColumns+` FROM suggestions WHERE status = 'open' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Suggestion
	for rows.Next() {
		sg, err := scanSuggestion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sg)
	}
	return out, rows.Err()
}

// AcceptLate turns a report that was closed without being accepted into an
// accepted one, when the label is added afterwards. It increments the
// author's merged count and reports false if there was nothing to change.
func (s *Store) AcceptLate(ctx context.Context, id int64) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `
		UPDATE suggestions SET status = 'merged', resolved_at = ?
		WHERE id = ? AND status = 'closed' AND kind = 'report'`, s.now().Unix(), id)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE users SET merged_count = merged_count + 1
		WHERE tg_id = (SELECT tg_id FROM suggestions WHERE id = ?)`, id); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// ResolveSuggestion marks an open suggestion as merged or closed. Merging
// increments the author's merged count. It reports false if the suggestion
// was already resolved, so repeated webhook deliveries are harmless.
func (s *Store) ResolveSuggestion(ctx context.Context, id int64, status string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `
		UPDATE suggestions SET status = ?, resolved_at = ? WHERE id = ? AND status = 'open'`,
		status, s.now().Unix(), id)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil
	}
	if status == StatusMerged {
		if _, err := tx.ExecContext(ctx, `
			UPDATE users SET merged_count = merged_count + 1
			WHERE tg_id = (SELECT tg_id FROM suggestions WHERE id = ?)`, id); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}

// SuggestQuota returns how many of the user's suggestions are open and when
// they last suggested something (zero if never).
func (s *Store) SuggestQuota(ctx context.Context, tgID int64) (open int, last time.Time, err error) {
	var lastAt sql.NullInt64
	err = s.db.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM suggestions WHERE tg_id = ? AND status = 'open'),
		       (SELECT last_suggest_at FROM users WHERE tg_id = ?)`, tgID, tgID,
	).Scan(&open, &lastAt)
	if lastAt.Valid {
		last = time.Unix(lastAt.Int64, 0)
	}
	return open, last, err
}

package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Suggestion kinds and statuses.
const (
	KindEdit = "edit"
	KindNew  = "new"

	StatusOpen   = "open"
	StatusMerged = "merged"
	StatusClosed = "closed"
)

// Suggestion is an edit proposed by a user, submitted as a pull request.
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

// SuggestionByPR finds the suggestion behind a pull request.
func (s *Store) SuggestionByPR(ctx context.Context, number int) (Suggestion, error) {
	var sg Suggestion
	var created int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, short_id, tg_id, kind, article_path, title, base_sha, branch, pr_number, pr_url, status, created_at
		FROM suggestions WHERE pr_number = ?`, number,
	).Scan(&sg.ID, &sg.ShortID, &sg.TgID, &sg.Kind, &sg.ArticlePath, &sg.Title, &sg.BaseSHA, &sg.Branch,
		&sg.PRNumber, &sg.PRURL, &sg.Status, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Suggestion{}, ErrNotFound
	}
	sg.CreatedAt = time.Unix(created, 0)
	return sg, err
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

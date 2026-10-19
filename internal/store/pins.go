package store

import (
	"context"
	"errors"
)

// ErrPinLimit is returned when a user already has the most pins allowed.
var ErrPinLimit = errors.New("too many pins")

// Pin adds a note to the user's pins. It fails with ErrPinLimit when the
// user already has max pins.
func (s *Store) Pin(ctx context.Context, tgID int64, noteID string, max int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pins WHERE tg_id = ?`, tgID).Scan(&n); err != nil {
		return err
	}
	if n >= max {
		return ErrPinLimit
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO pins (tg_id, note_id, pinned_at) VALUES (?, ?, ?)
		ON CONFLICT (tg_id, note_id) DO NOTHING`, tgID, noteID, s.now().UnixNano()); err != nil {
		return err
	}
	return tx.Commit()
}

// Unpin removes a note from the user's pins.
func (s *Store) Unpin(ctx context.Context, tgID int64, noteID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM pins WHERE tg_id = ? AND note_id = ?`, tgID, noteID)
	return err
}

// Pinned reports whether the user pinned a note.
func (s *Store) Pinned(ctx context.Context, tgID int64, noteID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pins WHERE tg_id = ? AND note_id = ?`, tgID, noteID).Scan(&n)
	return n > 0, err
}

// Pins lists the user's pinned note IDs, newest first.
func (s *Store) Pins(ctx context.Context, tgID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT note_id FROM pins WHERE tg_id = ? ORDER BY pinned_at DESC`, tgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// PrunePins removes the pins of notes known reports false for, such as
// notes that left the vault or whose ID changed, and returns how many.
func (s *Store) PrunePins(ctx context.Context, known func(noteID string) bool) (int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT note_id FROM pins`)
	if err != nil {
		return 0, err
	}
	var gone []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		if !known(id) {
			gone = append(gone, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	var n int64
	for _, id := range gone {
		res, err := s.db.ExecContext(ctx, `DELETE FROM pins WHERE note_id = ?`, id)
		if err != nil {
			return n, err
		}
		k, _ := res.RowsAffected()
		n += k
	}
	return n, nil
}

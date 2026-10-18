package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Access statuses. A user without a row has never asked.
const (
	AccessPending = "pending"
	AccessGranted = "granted"
	AccessDenied  = "denied" // also after a revoke
)

// ErrPinLimit is returned when a user already has the most pins allowed.
var ErrPinLimit = errors.New("too many pins")

// Access is a user's edit access: the latest request and its outcome.
type Access struct {
	TgID   int64
	Status string
	// NoteID is the note the user was on when asking, so the reply can
	// lead back to it.
	NoteID string
	// RequestText is the request as the admins got it; their copies are
	// edited to it plus the decision.
	RequestText string
	RequestedAt time.Time
	DecidedAt   time.Time // zero while pending
	DecidedBy   int64
}

const accessColumns = `tg_id, status, note_id, request_text, requested_at, decided_at, decided_by`

func scanAccess(row interface{ Scan(...any) error }) (Access, error) {
	var a Access
	var req int64
	var dec, by sql.NullInt64
	err := row.Scan(&a.TgID, &a.Status, &a.NoteID, &a.RequestText, &req, &dec, &by)
	a.RequestedAt = time.Unix(req, 0)
	if dec.Valid {
		a.DecidedAt = time.Unix(dec.Int64, 0)
	}
	a.DecidedBy = by.Int64
	return a, err
}

// Access returns the user's access row.
func (s *Store) Access(ctx context.Context, tgID int64) (Access, error) {
	a, err := scanAccess(s.db.QueryRowContext(ctx, `SELECT `+accessColumns+` FROM access WHERE tg_id = ?`, tgID))
	if errors.Is(err, sql.ErrNoRows) {
		return Access{}, ErrNotFound
	}
	return a, err
}

// RequestAccess records a pending request, replacing an earlier outcome,
// and forgets the admin messages of the earlier request.
func (s *Store) RequestAccess(ctx context.Context, tgID int64, noteID, text string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO access (tg_id, status, note_id, request_text, requested_at) VALUES (?, 'pending', ?, ?, ?)
		ON CONFLICT (tg_id) DO UPDATE SET status = 'pending', note_id = excluded.note_id,
			request_text = excluded.request_text, requested_at = excluded.requested_at,
			decided_at = NULL, decided_by = NULL`,
		tgID, noteID, text, s.now().Unix()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM access_messages WHERE tg_id = ?`, tgID); err != nil {
		return err
	}
	return tx.Commit()
}

// DecideAccess grants or denies a pending request. It reports false if
// the request was not pending any more, so only the first admin decides.
func (s *Store) DecideAccess(ctx context.Context, tgID int64, granted bool, by int64) (bool, error) {
	status := AccessDenied
	if granted {
		status = AccessGranted
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE access SET status = ?, decided_at = ?, decided_by = ? WHERE tg_id = ? AND status = 'pending'`,
		status, s.now().Unix(), by, tgID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// RevokeAccess takes granted access away; it then counts as a denial. It
// reports false if the user had no access.
func (s *Store) RevokeAccess(ctx context.Context, tgID, by int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE access SET status = 'denied', decided_at = ?, decided_by = ? WHERE tg_id = ? AND status = 'granted'`,
		s.now().Unix(), by, tgID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Grant is a user with access, for the admins' list.
type Grant struct {
	User
	Since     time.Time
	DecidedBy User // only TgID is set if the admin is unknown
}

// Grants lists the users with access, oldest grant first.
func (s *Store) Grants(ctx context.Context) ([]Grant, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.tg_id, u.username, u.first_name, u.merged_count, a.decided_at,
		       a.decided_by, COALESCE(d.username, ''), COALESCE(d.first_name, '')
		FROM access a JOIN users u ON u.tg_id = a.tg_id LEFT JOIN users d ON d.tg_id = a.decided_by
		WHERE a.status = 'granted' ORDER BY a.decided_at, u.tg_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Grant
	for rows.Next() {
		var g Grant
		var since int64
		if err := rows.Scan(&g.TgID, &g.Username, &g.FirstName, &g.MergedCount, &since,
			&g.DecidedBy.TgID, &g.DecidedBy.Username, &g.DecidedBy.FirstName); err != nil {
			return nil, err
		}
		g.Since = time.Unix(since, 0)
		out = append(out, g)
	}
	return out, rows.Err()
}

// PutAccessMessage remembers the copy of a request sent to an admin.
func (s *Store) PutAccessMessage(ctx context.Context, tgID, adminID int64, msgID int) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO access_messages (tg_id, admin_id, msg_id) VALUES (?, ?, ?)
		ON CONFLICT (tg_id, admin_id) DO UPDATE SET msg_id = excluded.msg_id`, tgID, adminID, msgID)
	return err
}

// AccessMessages returns the admin copies of the user's request: admin ID
// to message ID.
func (s *Store) AccessMessages(ctx context.Context, tgID int64) (map[int64]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT admin_id, msg_id FROM access_messages WHERE tg_id = ?`, tgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var admin int64
		var msg int
		if err := rows.Scan(&admin, &msg); err != nil {
			return nil, err
		}
		out[admin] = msg
	}
	return out, rows.Err()
}

// AcceptedCounts returns how many of the user's edits (including new
// topics and notes) and error reports were accepted.
func (s *Store) AcceptedCounts(ctx context.Context, tgID int64) (edits, reports int, err error) {
	err = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(kind != 'report'), 0), COALESCE(SUM(kind = 'report'), 0)
		FROM suggestions WHERE tg_id = ? AND status = 'merged'`, tgID).Scan(&edits, &reports)
	return edits, reports, err
}

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

// Package store keeps bot state in SQLite: users, suggestions, dialog
// sessions and the Telegram file_id cache.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// Store wraps the database.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// Open opens (and migrates) the database at path.
func Open(ctx context.Context, path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite allows one writer; a single connection avoids SQLITE_BUSY.
	db.SetMaxOpenConns(1)
	s := &Store{db: db, now: time.Now}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

var migrations = []string{
	`CREATE TABLE users (
		tg_id           INTEGER PRIMARY KEY,
		username        TEXT NOT NULL DEFAULT '',
		first_name      TEXT NOT NULL DEFAULT '',
		merged_count    INTEGER NOT NULL DEFAULT 0,
		last_suggest_at INTEGER,
		created_at      INTEGER NOT NULL,
		updated_at      INTEGER NOT NULL
	);
	CREATE TABLE suggestions (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		short_id     TEXT NOT NULL UNIQUE,
		tg_id        INTEGER NOT NULL REFERENCES users(tg_id),
		kind         TEXT NOT NULL CHECK (kind IN ('edit', 'new')),
		article_path TEXT NOT NULL,
		base_sha     TEXT NOT NULL,
		branch       TEXT NOT NULL,
		pr_number    INTEGER,
		pr_url       TEXT NOT NULL DEFAULT '',
		status       TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'merged', 'closed')),
		created_at   INTEGER NOT NULL,
		resolved_at  INTEGER
	);
	CREATE INDEX suggestions_tg_id ON suggestions(tg_id);
	CREATE INDEX suggestions_pr ON suggestions(pr_number);
	CREATE TABLE sessions (
		tg_id        INTEGER PRIMARY KEY,
		state        TEXT NOT NULL,
		article_path TEXT NOT NULL DEFAULT '',
		base_sha     TEXT NOT NULL DEFAULT '',
		payload      TEXT NOT NULL DEFAULT '',
		expires_at   INTEGER NOT NULL
	);
	CREATE TABLE media_cache (
		sha256     TEXT PRIMARY KEY,
		file_id    TEXT NOT NULL,
		updated_at INTEGER NOT NULL
	);
	CREATE TABLE archives (
		commit_sha TEXT PRIMARY KEY,
		file_id    TEXT NOT NULL,
		created_at INTEGER NOT NULL
	);`,
	`CREATE TABLE chat_messages (
		chat_id INTEGER NOT NULL,
		msg_id  INTEGER NOT NULL,
		PRIMARY KEY (chat_id, msg_id)
	) WITHOUT ROWID;
	CREATE TABLE chat_menus (
		chat_id INTEGER PRIMARY KEY,
		msg_id  INTEGER NOT NULL
	);`,
	`ALTER TABLE suggestions ADD COLUMN title TEXT NOT NULL DEFAULT '';`,
}

func (s *Store) migrate(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	for i := version; i < len(migrations); i++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// User is a Telegram user known to the bot.
type User struct {
	TgID        int64
	Username    string
	FirstName   string
	MergedCount int
}

// TouchUser creates the user or refreshes their name, and returns the row.
func (s *Store) TouchUser(ctx context.Context, tgID int64, username, firstName string) (User, error) {
	now := s.now().Unix()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (tg_id, username, first_name, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (tg_id) DO UPDATE SET username = excluded.username,
			first_name = excluded.first_name, updated_at = excluded.updated_at`,
		tgID, username, firstName, now, now)
	if err != nil {
		return User{}, err
	}
	return s.User(ctx, tgID)
}

// User returns a user by Telegram ID.
func (s *Store) User(ctx context.Context, tgID int64) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx,
		`SELECT tg_id, username, first_name, merged_count FROM users WHERE tg_id = ?`, tgID,
	).Scan(&u.TgID, &u.Username, &u.FirstName, &u.MergedCount)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

// MediaFileID returns the cached file_id for a file hash.
func (s *Store) MediaFileID(ctx context.Context, sha string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT file_id FROM media_cache WHERE sha256 = ?`, sha).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}

// PutMediaFileID caches the file_id for a file hash.
func (s *Store) PutMediaFileID(ctx context.Context, sha, fileID string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO media_cache (sha256, file_id, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (sha256) DO UPDATE SET file_id = excluded.file_id, updated_at = excluded.updated_at`,
		sha, fileID, s.now().Unix())
	return err
}

// DeleteMediaFileID drops a cached file_id that Telegram rejected.
func (s *Store) DeleteMediaFileID(ctx context.Context, sha string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM media_cache WHERE sha256 = ?`, sha)
	return err
}

// Session is a pending multi-step dialog, such as a suggestion waiting for
// the user's file.
type Session struct {
	TgID        int64
	State       string
	ArticlePath string
	// BaseSHA is the content commit the user saw when the dialog started.
	// Suggestion branches are created from it, so concurrent edits surface
	// as merge conflicts instead of silent overwrites.
	BaseSHA   string
	Payload   string
	ExpiresAt time.Time
}

// PutSession stores the user's dialog state, replacing any previous one.
func (s *Store) PutSession(ctx context.Context, ses Session) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (tg_id, state, article_path, base_sha, payload, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (tg_id) DO UPDATE SET state = excluded.state, article_path = excluded.article_path,
			base_sha = excluded.base_sha, payload = excluded.payload, expires_at = excluded.expires_at`,
		ses.TgID, ses.State, ses.ArticlePath, ses.BaseSHA, ses.Payload, ses.ExpiresAt.Unix())
	return err
}

// Session returns the user's active dialog state.
func (s *Store) Session(ctx context.Context, tgID int64) (Session, error) {
	var ses Session
	var exp int64
	err := s.db.QueryRowContext(ctx, `
		SELECT tg_id, state, article_path, base_sha, payload, expires_at
		FROM sessions WHERE tg_id = ? AND expires_at > ?`, tgID, s.now().Unix(),
	).Scan(&ses.TgID, &ses.State, &ses.ArticlePath, &ses.BaseSHA, &ses.Payload, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	ses.ExpiresAt = time.Unix(exp, 0)
	return ses, err
}

// DeleteSession ends the user's dialog.
func (s *Store) DeleteSession(ctx context.Context, tgID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE tg_id = ?`, tgID)
	return err
}

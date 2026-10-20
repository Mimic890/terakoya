// Package store keeps bot state in SQLite: users, suggestions, dialog
// sessions and the Telegram file_id cache.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// ErrExpired is returned for a dialog session that timed out.
var ErrExpired = errors.New("session expired")

// Store wraps the database.
type Store struct {
	db   *sql.DB
	path string
	now  func() time.Time
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
	s := &Store{db: db, path: path, now: time.Now}
	if err := s.migrate(ctx, len(migrations)); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// SetClock replaces the clock used for timestamps; for tests.
func (s *Store) SetClock(now func() time.Time) { s.now = now }

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
	// Error reports are issues; SQLite cannot alter a CHECK constraint, so
	// the table is rebuilt.
	`CREATE TABLE suggestions_new (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		short_id     TEXT NOT NULL UNIQUE,
		tg_id        INTEGER NOT NULL REFERENCES users(tg_id),
		kind         TEXT NOT NULL CHECK (kind IN ('edit', 'new', 'report')),
		article_path TEXT NOT NULL,
		base_sha     TEXT NOT NULL,
		branch       TEXT NOT NULL,
		pr_number    INTEGER,
		pr_url       TEXT NOT NULL DEFAULT '',
		status       TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'merged', 'closed')),
		created_at   INTEGER NOT NULL,
		resolved_at  INTEGER,
		title        TEXT NOT NULL DEFAULT ''
	);
	INSERT INTO suggestions_new (id, short_id, tg_id, kind, article_path, base_sha, branch, pr_number, pr_url, status, created_at, resolved_at, title)
		SELECT id, short_id, tg_id, kind, article_path, base_sha, branch, pr_number, pr_url, status, created_at, resolved_at, title FROM suggestions;
	DROP TABLE suggestions;
	ALTER TABLE suggestions_new RENAME TO suggestions;
	CREATE INDEX suggestions_tg_id ON suggestions(tg_id);
	CREATE INDEX suggestions_pr ON suggestions(pr_number);`,
	// The Obsidian vault format: notes may be sent as several messages, and
	// cached file_ids and archives of the old format are of no use.
	`CREATE TABLE message_parts (
		chat_id  INTEGER NOT NULL,
		msg_id   INTEGER NOT NULL,
		group_id INTEGER NOT NULL,
		PRIMARY KEY (chat_id, msg_id)
	) WITHOUT ROWID;
	CREATE INDEX message_parts_group ON message_parts(chat_id, group_id);
	DELETE FROM media_cache;
	DELETE FROM archives;
	DELETE FROM sessions;`,
	// Edit access granted by admins, the admin copies of each request, and
	// pinned notes.
	`CREATE TABLE access (
		tg_id        INTEGER PRIMARY KEY REFERENCES users(tg_id),
		status       TEXT NOT NULL CHECK (status IN ('pending', 'granted', 'denied')),
		note_id      TEXT NOT NULL DEFAULT '',
		request_text TEXT NOT NULL DEFAULT '',
		requested_at INTEGER NOT NULL,
		decided_at   INTEGER,
		decided_by   INTEGER
	);
	CREATE TABLE access_messages (
		tg_id    INTEGER NOT NULL,
		admin_id INTEGER NOT NULL,
		msg_id   INTEGER NOT NULL,
		PRIMARY KEY (tg_id, admin_id)
	) WITHOUT ROWID;
	CREATE TABLE pins (
		tg_id     INTEGER NOT NULL,
		note_id   TEXT NOT NULL,
		pinned_at INTEGER NOT NULL,
		PRIMARY KEY (tg_id, note_id)
	) WITHOUT ROWID;`,
	// The reviewer's comment on a rejected suggestion, for «Мои сообщения»;
	// and menus sent as rich messages, which cannot be edited into text.
	`ALTER TABLE suggestions ADD COLUMN comment TEXT NOT NULL DEFAULT '';
	ALTER TABLE chat_menus ADD COLUMN rich INTEGER NOT NULL DEFAULT 0;`,
	// Only error reports are left: no edit access, and ranks count the
	// accepted reports, from the suggestions themselves.
	`DROP TABLE access;
	DROP TABLE access_messages;
	ALTER TABLE users DROP COLUMN merged_count;`,
	// Nicknames (nickname_key is the folded form that must be unique),
	// leaving the top, users who blocked the bot, and broadcasts.
	`ALTER TABLE users ADD COLUMN nickname TEXT NOT NULL DEFAULT '';
	ALTER TABLE users ADD COLUMN nickname_key TEXT;
	CREATE UNIQUE INDEX users_nickname_key ON users(nickname_key);
	ALTER TABLE users ADD COLUMN hide_top INTEGER NOT NULL DEFAULT 0;
	ALTER TABLE users ADD COLUMN blocked INTEGER NOT NULL DEFAULT 0;
	CREATE TABLE broadcasts (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		admin_id    INTEGER NOT NULL,
		markdown    TEXT NOT NULL,
		photo       TEXT NOT NULL DEFAULT '',
		created_at  INTEGER NOT NULL,
		finished_at INTEGER,
		recipients  INTEGER NOT NULL DEFAULT 0,
		sent        INTEGER NOT NULL DEFAULT 0,
		blocked     INTEGER NOT NULL DEFAULT 0,
		failed      INTEGER NOT NULL DEFAULT 0
	);`,
	// For /admin: when each user was last seen, note opens per day and the
	// traffic to the Telegram API per day.
	`ALTER TABLE users ADD COLUMN last_seen_at INTEGER;
	UPDATE users SET last_seen_at = updated_at;
	CREATE INDEX users_last_seen ON users(last_seen_at);
	CREATE TABLE note_opens (
		note_id TEXT NOT NULL,
		day     TEXT NOT NULL,
		count   INTEGER NOT NULL,
		PRIMARY KEY (note_id, day)
	) WITHOUT ROWID;
	CREATE TABLE api_traffic (
		day      TEXT PRIMARY KEY,
		calls    INTEGER NOT NULL DEFAULT 0,
		sent     INTEGER NOT NULL DEFAULT 0,
		received INTEGER NOT NULL DEFAULT 0
	) WITHOUT ROWID;`,
	// Broadcasts keep who they go to and how far they got, so one cut
	// short by a restart resumes without sending anyone a second copy.
	// Earlier unfinished ones have no such list and count as done.
	`ALTER TABLE broadcasts ADD COLUMN progress_chat INTEGER NOT NULL DEFAULT 0;
	ALTER TABLE broadcasts ADD COLUMN progress_msg INTEGER NOT NULL DEFAULT 0;
	CREATE TABLE broadcast_recipients (
		broadcast_id INTEGER NOT NULL REFERENCES broadcasts(id),
		tg_id        INTEGER NOT NULL,
		state        TEXT NOT NULL DEFAULT 'pending'
			CHECK (state IN ('pending', 'sending', 'sent', 'blocked', 'failed')),
		PRIMARY KEY (broadcast_id, tg_id)
	) WITHOUT ROWID;
	UPDATE broadcasts SET finished_at = created_at WHERE finished_at IS NULL;`,
	// When a user last changed their nickname, for the once-a-day limit;
	// when a note's messages were sent, so old groups can be forgotten;
	// and indexes for the top, the open reports and the note statistics.
	`ALTER TABLE users ADD COLUMN nickname_changed_at INTEGER;
	ALTER TABLE message_parts ADD COLUMN created_at INTEGER NOT NULL DEFAULT 0;
	UPDATE message_parts SET created_at = CAST(strftime('%s', 'now') AS INTEGER);
	CREATE INDEX suggestions_kind_status ON suggestions(kind, status, tg_id);
	CREATE INDEX suggestions_status ON suggestions(status);
	CREATE INDEX note_opens_day ON note_opens(day);`,
	// Search queries, normalized, and how many notes each found, for
	// /admin: what is searched for and what is missing. Who searched is
	// not kept.
	`CREATE TABLE search_queries (
		query   TEXT NOT NULL,
		results INTEGER NOT NULL,
		at      INTEGER NOT NULL
	);
	CREATE INDEX search_queries_at ON search_queries(at);`,
	// A note's title and path as of each day it was opened, so /admin
	// names notes that were renamed or removed since.
	`ALTER TABLE note_opens ADD COLUMN title TEXT NOT NULL DEFAULT '';
	ALTER TABLE note_opens ADD COLUMN path TEXT NOT NULL DEFAULT '';`,
}

// migrate brings the schema to version to. An existing database is
// copied to backups/before-v<to>/ first, with the settings file, so a
// migration gone wrong loses nothing.
func (s *Store) migrate(ctx context.Context, to int) error {
	var version int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version > 0 && version < to {
		if err := s.beforeMigration(ctx, to); err != nil {
			return fmt.Errorf("backup before migrating to version %d: %w", to, err)
		}
	}
	for i := version; i < to; i++ {
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
	TgID      int64
	Username  string
	FirstName string
	// Nickname is the name the user chose, or "".
	Nickname string
	// HideTop leaves the user out of the top for everyone else.
	HideTop bool
	// Accepted is how many of the user's error reports were accepted,
	// which sets their rank.
	Accepted int
	// NickChanged is when the user last chose their name, or zero.
	NickChanged time.Time
}

// AddressName is how the bot addresses the user: the nickname, else the
// first name (the @username only if Telegram gave no first name).
func (u User) AddressName() string {
	switch {
	case u.Nickname != "":
		return u.Nickname
	case u.FirstName != "":
		return u.FirstName
	case u.Username != "":
		return "@" + u.Username
	}
	return ""
}

// DisplayName names the user where people must be told apart, such as
// the top and admin views: the nickname, else @username, else the first
// name.
func (u User) DisplayName() string {
	switch {
	case u.Nickname != "":
		return u.Nickname
	case u.Username != "":
		return "@" + u.Username
	}
	return u.FirstName
}

// TouchUser creates the user or refreshes their name, and returns the row.
// A user who blocked the bot and uses it again counts as unblocked.
func (s *Store) TouchUser(ctx context.Context, tgID int64, username, firstName string) (User, error) {
	now := s.now().Unix()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (tg_id, username, first_name, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (tg_id) DO UPDATE SET username = excluded.username,
			first_name = excluded.first_name, updated_at = excluded.updated_at, blocked = 0`,
		tgID, username, firstName, now, now)
	if err != nil {
		return User{}, err
	}
	return s.User(ctx, tgID)
}

// User returns a user by Telegram ID.
func (s *Store) User(ctx context.Context, tgID int64) (User, error) {
	return s.userWhere(ctx, `tg_id = ?`, tgID)
}

// UserByUsername finds a user by @username, ignoring case and the "@".
func (s *Store) UserByUsername(ctx context.Context, username string) (User, error) {
	return s.userWhere(ctx, `username = ? COLLATE NOCASE AND username != ''`, strings.TrimPrefix(username, "@"))
}

func (s *Store) userWhere(ctx context.Context, where string, args ...any) (User, error) {
	var u User
	var changed sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT tg_id, username, first_name, nickname, hide_top,
			(SELECT COUNT(*) FROM suggestions s WHERE s.tg_id = users.tg_id AND s.kind = 'report' AND s.status = 'merged'),
			nickname_changed_at
		FROM users WHERE `+where, args...,
	).Scan(&u.TgID, &u.Username, &u.FirstName, &u.Nickname, &u.HideTop, &u.Accepted, &changed)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if changed.Valid {
		u.NickChanged = time.Unix(changed.Int64, 0)
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

// Session returns the user's active dialog state. A session that timed
// out is returned once more with ErrExpired, so the user can be told, and
// is then deleted.
func (s *Store) Session(ctx context.Context, tgID int64) (Session, error) {
	var ses Session
	var exp int64
	err := s.db.QueryRowContext(ctx, `
		SELECT tg_id, state, article_path, base_sha, payload, expires_at
		FROM sessions WHERE tg_id = ?`, tgID,
	).Scan(&ses.TgID, &ses.State, &ses.ArticlePath, &ses.BaseSHA, &ses.Payload, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}
	ses.ExpiresAt = time.Unix(exp, 0)
	if exp <= s.now().Unix() {
		if err := s.DeleteSession(ctx, tgID); err != nil {
			return Session{}, err
		}
		return ses, ErrExpired
	}
	return ses, nil
}

// DeleteSession ends the user's dialog.
func (s *Store) DeleteSession(ctx context.Context, tgID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE tg_id = ?`, tgID)
	return err
}

// ArchiveFileID returns the file_id of the vault archive sent for a
// commit, so /download does not upload the same zip again.
func (s *Store) ArchiveFileID(ctx context.Context, commit string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT file_id FROM archives WHERE commit_sha = ?`, commit).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}

// PutArchiveFileID remembers the archive of a commit.
func (s *Store) PutArchiveFileID(ctx context.Context, commit, fileID string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO archives (commit_sha, file_id, created_at) VALUES (?, ?, ?)
		ON CONFLICT (commit_sha) DO UPDATE SET file_id = excluded.file_id`, commit, fileID, s.now().Unix())
	return err
}

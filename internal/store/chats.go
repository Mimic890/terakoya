package store

import (
	"context"
	"database/sql"
	"errors"
)

// MaxTrackedMessages bounds the message IDs remembered per chat.
const MaxTrackedMessages = 32

// The chat_messages and chat_menus tables remember the latest messages of
// each private chat and which of them is the navigation menu, so the bot
// can tell whether the menu is still the last message and edit it in place,
// also after a restart.

// ChatMessageSeen records a message sent by the bot or the user.
func (s *Store) ChatMessageSeen(ctx context.Context, chatID int64, msgID int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO chat_messages (chat_id, msg_id) VALUES (?, ?)`, chatID, msgID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM chat_messages WHERE chat_id = ? AND msg_id NOT IN (
			SELECT msg_id FROM chat_messages WHERE chat_id = ? ORDER BY msg_id DESC LIMIT ?)`,
		chatID, chatID, MaxTrackedMessages); err != nil {
		return err
	}
	return tx.Commit()
}

// SetChatMenu marks a message as the chat's navigation menu.
func (s *Store) SetChatMenu(ctx context.Context, chatID int64, msgID int) error {
	if err := s.ChatMessageSeen(ctx, chatID, msgID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO chat_menus (chat_id, msg_id) VALUES (?, ?)
		ON CONFLICT (chat_id) DO UPDATE SET msg_id = excluded.msg_id`, chatID, msgID)
	return err
}

// ChatMessageGone forgets a deleted message.
func (s *Store) ChatMessageGone(ctx context.Context, chatID int64, msgID int) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM chat_messages WHERE chat_id = ? AND msg_id = ?`, chatID, msgID); err != nil {
		return err
	}
	return s.DropChatMenu(ctx, chatID, msgID)
}

// DropChatMenu stops treating a message as the menu while keeping it as a
// live message, e.g. after its keyboard was removed.
func (s *Store) DropChatMenu(ctx context.Context, chatID int64, msgID int) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM chat_menus WHERE chat_id = ? AND msg_id = ?`, chatID, msgID)
	return err
}

// LastChatMenu returns the menu message ID if it is the last known message
// in the chat, or 0.
func (s *Store) LastChatMenu(ctx context.Context, chatID int64) (int, error) {
	var id int
	err := s.db.QueryRowContext(ctx, `
		SELECT m.msg_id FROM chat_menus m
		WHERE m.chat_id = ? AND m.msg_id = (SELECT MAX(msg_id) FROM chat_messages WHERE chat_id = ?)`,
		chatID, chatID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

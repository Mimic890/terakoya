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

// A note too long for one message is sent as a group of messages; the
// message_parts table remembers the group, so the whole note can be
// removed when the user moves on from any of its parts.

// SetMessageGroup records the messages of one note, in order.
func (s *Store) SetMessageGroup(ctx context.Context, chatID int64, msgIDs []int) error {
	if len(msgIDs) < 2 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range msgIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO message_parts (chat_id, msg_id, group_id) VALUES (?, ?, ?)
			ON CONFLICT (chat_id, msg_id) DO UPDATE SET group_id = excluded.group_id`,
			chatID, id, msgIDs[0]); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// MessageGroup returns all messages of the group msgID belongs to, in
// order, or just msgID if it is not part of a group.
func (s *Store) MessageGroup(ctx context.Context, chatID int64, msgID int) ([]int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT msg_id FROM message_parts
		WHERE chat_id = ? AND group_id = (SELECT group_id FROM message_parts WHERE chat_id = ? AND msg_id = ?)
		ORDER BY msg_id`, chatID, chatID, msgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		out = []int{msgID}
	}
	return out, nil
}

// DropMessageGroup forgets the group msgID belongs to.
func (s *Store) DropMessageGroup(ctx context.Context, chatID int64, msgID int) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM message_parts WHERE chat_id = ? AND group_id = (
			SELECT group_id FROM message_parts WHERE chat_id = ? AND msg_id = ?)`, chatID, chatID, msgID)
	return err
}

// ChatMenu returns the chat's menu message ID, wherever it is, or 0.
func (s *Store) ChatMenu(ctx context.Context, chatID int64) (int, error) {
	var id int
	err := s.db.QueryRowContext(ctx, `SELECT msg_id FROM chat_menus WHERE chat_id = ?`, chatID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

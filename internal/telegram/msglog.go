package telegram

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type BotMessage struct {
	ChatID    int64
	MessageID int
}

type MessageLog struct {
	db *sql.DB
}

func OpenMessageLog(dbFile string) (*MessageLog, error) {
	if err := os.MkdirAll(filepath.Dir(dbFile), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", filepath.ToSlash(dbFile))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	s := &MessageLog{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *MessageLog) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *MessageLog) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS bot_messages (
	chat_id INTEGER NOT NULL,
	message_id INTEGER NOT NULL,
	sent_at INTEGER NOT NULL,
	PRIMARY KEY (chat_id, message_id)
);
CREATE INDEX IF NOT EXISTS bot_messages_sent_at ON bot_messages(sent_at);
`)
	return err
}

func (s *MessageLog) Record(chatID int64, messageID int, sentAt time.Time) error {
	if s == nil || chatID == 0 || messageID == 0 {
		return nil
	}
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO bot_messages (chat_id, message_id, sent_at) VALUES (?, ?, ?)`,
		chatID, messageID, sentAt.Unix(),
	)
	return err
}

func (s *MessageLog) Expired(before time.Time) ([]BotMessage, error) {
	if s == nil {
		return nil, nil
	}
	rows, err := s.db.Query(
		`SELECT chat_id, message_id FROM bot_messages WHERE sent_at <= ? ORDER BY sent_at`,
		before.Unix(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BotMessage
	for rows.Next() {
		var m BotMessage
		if err := rows.Scan(&m.ChatID, &m.MessageID); err != nil {
			return out, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *MessageLog) Remove(chatID int64, messageID int) error {
	if s == nil {
		return nil
	}
	_, err := s.db.Exec(
		`DELETE FROM bot_messages WHERE chat_id = ? AND message_id = ?`,
		chatID, messageID,
	)
	return err
}

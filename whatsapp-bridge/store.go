package main

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Message represents a chat message for our client
type Message struct {
	Time      time.Time
	Sender    string
	Content   string
	IsFromMe  bool
	MediaType string
	Filename  string
}

// Database handler for storing message history
type MessageStore struct {
	db *sql.DB
}

// Initialize message store
func NewMessageStore() (*MessageStore, error) {
	// Create directory for database if it doesn't exist
	if err := os.MkdirAll("store", 0755); err != nil {
		return nil, fmt.Errorf("failed to create store directory: %v", err)
	}

	// Open SQLite database for messages
	db, err := sql.Open("sqlite3", "file:store/messages.db?_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("failed to open message database: %v", err)
	}

	// Create tables if they don't exist
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS chats (
			jid TEXT PRIMARY KEY,
			name TEXT,
			last_message_time TIMESTAMP
		);
		
		CREATE TABLE IF NOT EXISTS messages (
			id TEXT,
			chat_jid TEXT,
			sender TEXT,
			content TEXT,
			timestamp TIMESTAMP,
			is_from_me BOOLEAN,
			media_type TEXT,
			filename TEXT,
			url TEXT,
			media_key BLOB,
			file_sha256 BLOB,
			file_enc_sha256 BLOB,
			file_length INTEGER,
			PRIMARY KEY (id, chat_jid),
			FOREIGN KEY (chat_jid) REFERENCES chats(jid)
		);

		-- Permanent record of everything this bridge sent (Send record), also
		-- used by the Repeat lock. content_hash is a SHA-256 of the text plus
		-- file contents; sent_at is unix seconds (UTC).
		CREATE TABLE IF NOT EXISTS sent_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			message_id TEXT,
			chat_jid TEXT NOT NULL,
			kind TEXT NOT NULL,
			text TEXT,
			file_name TEXT,
			content_hash TEXT NOT NULL,
			sent_at INTEGER NOT NULL,
			deliberate BOOLEAN NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_sent_log_repeat ON sent_log (chat_jid, content_hash, sent_at);
	`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create tables: %v", err)
	}

	// Make sure the store folder and databases are private to this user.
	os.Chmod("store", 0o700)
	return &MessageStore{db: db}, nil
}

// Close the database connection
func (store *MessageStore) Close() error {
	return store.db.Close()
}

// Store a chat in the database
func (store *MessageStore) StoreChat(jid, name string, lastMessageTime time.Time) error {
	_, err := store.db.Exec(
		"INSERT OR REPLACE INTO chats (jid, name, last_message_time) VALUES (?, ?, ?)",
		jid, name, lastMessageTime,
	)
	return err
}

// Store a message in the database
func (store *MessageStore) StoreMessage(id, chatJID, sender, content string, timestamp time.Time, isFromMe bool,
	mediaType, filename, url string, mediaKey, fileSHA256, fileEncSHA256 []byte, fileLength uint64) error {
	// Only store if there's actual content or media
	if content == "" && mediaType == "" {
		return nil
	}

	_, err := store.db.Exec(
		`INSERT OR REPLACE INTO messages 
		(id, chat_jid, sender, content, timestamp, is_from_me, media_type, filename, url, media_key, file_sha256, file_enc_sha256, file_length) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, chatJID, sender, content, timestamp, isFromMe, mediaType, filename, url, mediaKey, fileSHA256, fileEncSHA256, fileLength,
	)
	return err
}

// Get messages from a chat
func (store *MessageStore) GetMessages(chatJID string, limit int) ([]Message, error) {
	rows, err := store.db.Query(
		"SELECT sender, content, timestamp, is_from_me, media_type, filename FROM messages WHERE chat_jid = ? ORDER BY timestamp DESC LIMIT ?",
		chatJID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		var timestamp time.Time
		err := rows.Scan(&msg.Sender, &msg.Content, &timestamp, &msg.IsFromMe, &msg.MediaType, &msg.Filename)
		if err != nil {
			return nil, err
		}
		msg.Time = timestamp
		messages = append(messages, msg)
	}

	return messages, nil
}

// Get all chats
func (store *MessageStore) GetChats() (map[string]time.Time, error) {
	rows, err := store.db.Query("SELECT jid, last_message_time FROM chats ORDER BY last_message_time DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	chats := make(map[string]time.Time)
	for rows.Next() {
		var jid string
		var lastMessageTime time.Time
		err := rows.Scan(&jid, &lastMessageTime)
		if err != nil {
			return nil, err
		}
		chats[jid] = lastMessageTime
	}

	return chats, nil
}

// SentRecord is one row of the sent_log table.
type SentRecord struct {
	MessageID   string
	ChatJID     string
	Kind        string // "text" or media type: image, video, audio, document
	Text        string
	FileName    string
	ContentHash string
	SentAt      time.Time
	Deliberate  bool
}

// RecentlySent reports whether the same content was already sent to the same
// chat since the given time.
func (store *MessageStore) RecentlySent(chatJID, contentHash string, since time.Time) (bool, error) {
	var n int
	err := store.db.QueryRow(
		"SELECT COUNT(*) FROM sent_log WHERE chat_jid = ? AND content_hash = ? AND sent_at >= ?",
		chatJID, contentHash, since.Unix(),
	).Scan(&n)
	return n > 0, err
}

// RecordSent stores the send record and also adds the message to the normal
// messages table, so it shows up when the chat is read back.
func (store *MessageStore) RecordSent(rec SentRecord, chatName, sender string, url string, mediaKey, fileSHA256, fileEncSHA256 []byte, fileLength uint64) error {
	tx, err := store.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		"INSERT INTO sent_log (message_id, chat_jid, kind, text, file_name, content_hash, sent_at, deliberate) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		rec.MessageID, rec.ChatJID, rec.Kind, rec.Text, rec.FileName, rec.ContentHash, rec.SentAt.Unix(), rec.Deliberate,
	); err != nil {
		return err
	}
	// Keep an existing chat name; only move the last-message time forward.
	if _, err := tx.Exec(
		`INSERT INTO chats (jid, name, last_message_time) VALUES (?, ?, ?)
		 ON CONFLICT(jid) DO UPDATE SET last_message_time = excluded.last_message_time`,
		rec.ChatJID, chatName, rec.SentAt,
	); err != nil {
		return err
	}
	mediaType := ""
	if rec.Kind != "text" {
		mediaType = rec.Kind
	}
	if _, err := tx.Exec(
		`INSERT OR REPLACE INTO messages
		(id, chat_jid, sender, content, timestamp, is_from_me, media_type, filename, url, media_key, file_sha256, file_enc_sha256, file_length)
		VALUES (?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?)`,
		rec.MessageID, rec.ChatJID, sender, rec.Text, rec.SentAt, mediaType, rec.FileName, url, mediaKey, fileSHA256, fileEncSHA256, fileLength,
	); err != nil {
		return err
	}
	return tx.Commit()
}

package database

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// DB wraps a SQLite database connection
type DB struct {
	conn *sql.DB
}

// NewDB creates a new database connection and initializes tables
func NewDB(dbPath string) (*DB, error) {
	conn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	db := &DB{conn: conn}

	// Initialize schema
	if err := db.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return db, nil
}

// initSchema creates the necessary tables
func (db *DB) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS pomo_sessions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		message_id TEXT NOT NULL UNIQUE,
		channel_id TEXT NOT NULL,
		guild_id TEXT NOT NULL,
		user_id TEXT,
		state TEXT NOT NULL DEFAULT 'stopped',
		voice_channel_id TEXT,
		start_time INTEGER,
		elapsed_seconds INTEGER DEFAULT 0,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_pomo_message_id ON pomo_sessions(message_id);
	CREATE INDEX IF NOT EXISTS idx_pomo_guild_id ON pomo_sessions(guild_id);
	`

	_, err := db.conn.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to execute schema: %w", err)
	}

	return nil
}

// Close closes the database connection
func (db *DB) Close() error {
	return db.conn.Close()
}

// PomoSession represents a pomodoro session
type PomoSession struct {
	ID             int64
	MessageID      string
	ChannelID      string
	GuildID        string
	UserID         string
	State          string // stopped, running, paused
	VoiceChannelID string
	StartTime      *time.Time
	ElapsedSeconds int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// CreatePomoSession creates a new pomo session
func (db *DB) CreatePomoSession(messageID, channelID, guildID string) (*PomoSession, error) {
	now := time.Now().Unix()

	result, err := db.conn.Exec(`
		INSERT INTO pomo_sessions (message_id, channel_id, guild_id, state, created_at, updated_at)
		VALUES (?, ?, ?, 'stopped', ?, ?)
	`, messageID, channelID, guildID, now, now)

	if err != nil {
		return nil, fmt.Errorf("failed to create pomo session: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get last insert ID: %w", err)
	}

	return db.GetPomoSessionByID(id)
}

// GetPomoSessionByID gets a session by ID
func (db *DB) GetPomoSessionByID(id int64) (*PomoSession, error) {
	var session PomoSession
	var startTime, createdAt, updatedAt int64
	var userID, voiceChannelID sql.NullString
	var startTimePtr sql.NullInt64

	err := db.conn.QueryRow(`
		SELECT id, message_id, channel_id, guild_id, user_id, state, 
		       voice_channel_id, start_time, elapsed_seconds, created_at, updated_at
		FROM pomo_sessions WHERE id = ?
	`, id).Scan(
		&session.ID, &session.MessageID, &session.ChannelID, &session.GuildID,
		&userID, &session.State, &voiceChannelID, &startTimePtr,
		&session.ElapsedSeconds, &createdAt, &updatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get pomo session: %w", err)
	}

	if userID.Valid {
		session.UserID = userID.String
	}
	if voiceChannelID.Valid {
		session.VoiceChannelID = voiceChannelID.String
	}
	if startTimePtr.Valid {
		startTime = startTimePtr.Int64
		t := time.Unix(startTime, 0)
		session.StartTime = &t
	}

	session.CreatedAt = time.Unix(createdAt, 0)
	session.UpdatedAt = time.Unix(updatedAt, 0)

	return &session, nil
}

// GetPomoSessionByMessageID gets a session by message ID
func (db *DB) GetPomoSessionByMessageID(messageID string) (*PomoSession, error) {
	var session PomoSession
	var startTime, createdAt, updatedAt int64
	var userID, voiceChannelID sql.NullString
	var startTimePtr sql.NullInt64

	err := db.conn.QueryRow(`
		SELECT id, message_id, channel_id, guild_id, user_id, state, 
		       voice_channel_id, start_time, elapsed_seconds, created_at, updated_at
		FROM pomo_sessions WHERE message_id = ?
	`, messageID).Scan(
		&session.ID, &session.MessageID, &session.ChannelID, &session.GuildID,
		&userID, &session.State, &voiceChannelID, &startTimePtr,
		&session.ElapsedSeconds, &createdAt, &updatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get pomo session: %w", err)
	}

	if userID.Valid {
		session.UserID = userID.String
	}
	if voiceChannelID.Valid {
		session.VoiceChannelID = voiceChannelID.String
	}
	if startTimePtr.Valid {
		startTime = startTimePtr.Int64
		t := time.Unix(startTime, 0)
		session.StartTime = &t
	}

	session.CreatedAt = time.Unix(createdAt, 0)
	session.UpdatedAt = time.Unix(updatedAt, 0)

	return &session, nil
}

// UpdatePomoSession updates a pomo session
func (db *DB) UpdatePomoSession(session *PomoSession) error {
	now := time.Now().Unix()

	var startTime interface{}
	if session.StartTime != nil {
		startTime = session.StartTime.Unix()
	}

	var userID interface{}
	if session.UserID != "" {
		userID = session.UserID
	}

	var voiceChannelID interface{}
	if session.VoiceChannelID != "" {
		voiceChannelID = session.VoiceChannelID
	}

	_, err := db.conn.Exec(`
		UPDATE pomo_sessions
		SET user_id = ?, state = ?, voice_channel_id = ?, 
		    start_time = ?, elapsed_seconds = ?, updated_at = ?
		WHERE id = ?
	`, userID, session.State, voiceChannelID, startTime, session.ElapsedSeconds, now, session.ID)

	if err != nil {
		return fmt.Errorf("failed to update pomo session: %w", err)
	}

	return nil
}

// DeletePomoSession deletes a pomo session
func (db *DB) DeletePomoSession(id int64) error {
	_, err := db.conn.Exec("DELETE FROM pomo_sessions WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete pomo session: %w", err)
	}
	return nil
}

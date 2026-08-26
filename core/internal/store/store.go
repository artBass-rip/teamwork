package store

import (
	"database/sql"
	"encoding/json"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

type Event struct {
	ID         string `json:"id"`
	Type       string `json:"event_type"`
	Producer   string `json:"producer"`
	OccurredAt string `json:"occurred_at"`
	Payload    any    `json:"payload"`
}
type LogEntry struct {
	ID         int64  `json:"id"`
	Level      string `json:"level"`
	Logger     string `json:"logger"`
	Message    string `json:"message"`
	OccurredAt string `json:"occurred_at"`
	Fields     any    `json:"fields,omitempty"`
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	statements := `PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;
CREATE TABLE IF NOT EXISTS events(id TEXT PRIMARY KEY,event_type TEXT NOT NULL,producer TEXT NOT NULL,occurred_at TEXT NOT NULL,payload TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS deliveries(event_id TEXT NOT NULL,module_id TEXT NOT NULL,status TEXT NOT NULL,attempts INTEGER NOT NULL DEFAULT 0,last_error TEXT,PRIMARY KEY(event_id,module_id));
CREATE TABLE IF NOT EXISTS logs(id INTEGER PRIMARY KEY AUTOINCREMENT,level TEXT NOT NULL,logger TEXT NOT NULL,message TEXT NOT NULL,occurred_at TEXT NOT NULL,fields TEXT NOT NULL DEFAULT '{}');
CREATE INDEX IF NOT EXISTS logs_occurred_at_idx ON logs(occurred_at);`
	if _, err := db.Exec(statements); err != nil {
		db.Close()
		return nil, err
	}
	store := &Store{db: db}
	_ = store.PurgeLogs(48 * time.Hour)
	return store, nil
}

func (s *Store) RecordLog(level, logger, message string, fields any) error {
	encoded, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO logs(level,logger,message,occurred_at,fields) VALUES(?,?,?,?,?)", level, logger, message, time.Now().UTC().Format(time.RFC3339Nano), string(encoded))
	if err == nil {
		_, _ = s.db.Exec("DELETE FROM logs WHERE occurred_at < ?", time.Now().UTC().Add(-48*time.Hour).Format(time.RFC3339Nano))
	}
	return err
}
func (s *Store) PurgeLogs(retention time.Duration) error {
	_, err := s.db.Exec("DELETE FROM logs WHERE occurred_at < ?", time.Now().UTC().Add(-retention).Format(time.RFC3339Nano))
	return err
}
func (s *Store) RecentLogs(minLevel string, limit int) ([]LogEntry, error) {
	weights := map[string]int{"debug": 10, "info": 20, "warn": 30, "error": 40}
	minimum := weights[minLevel]
	if minimum == 0 {
		minimum = 20
	}
	if limit < 1 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := s.db.Query(`SELECT id,level,logger,message,occurred_at,fields FROM logs WHERE CASE level WHEN 'debug' THEN 10 WHEN 'info' THEN 20 WHEN 'warn' THEN 30 WHEN 'error' THEN 40 ELSE 20 END >= ? ORDER BY occurred_at DESC LIMIT ?`, minimum, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []LogEntry{}
	for rows.Next() {
		var item LogEntry
		var fields []byte
		if err = rows.Scan(&item.ID, &item.Level, &item.Logger, &item.Message, &item.OccurredAt, &fields); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(fields, &item.Fields)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) RecordEvent(id, producer, eventType string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO events VALUES(?,?,?,?,?)", id, eventType, producer, time.Now().UTC().Format(time.RFC3339Nano), string(encoded))
	return err
}

func (s *Store) Delivery(eventID, moduleID, status, message string) error {
	_, err := s.db.Exec(`INSERT INTO deliveries VALUES(?,?,?,?,?) ON CONFLICT(event_id,module_id) DO UPDATE SET status=excluded.status,attempts=deliveries.attempts+1,last_error=excluded.last_error`, eventID, moduleID, status, 1, message)
	return err
}

func (s *Store) Recent(limit int) ([]Event, error) {
	if limit < 1 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := s.db.Query("SELECT id,event_type,producer,occurred_at,payload FROM events ORDER BY occurred_at DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Event{}
	for rows.Next() {
		var event Event
		var payload []byte
		if err := rows.Scan(&event.ID, &event.Type, &event.Producer, &event.OccurredAt, &payload); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(payload, &event.Payload); err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	return result, rows.Err()
}

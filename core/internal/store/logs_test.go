package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLogsFilterAndRetention(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "teamwork.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, level := range []string{"debug", "info", "warn", "error"} {
		if err := s.RecordLog(level, "test.module", level+" message", map[string]any{"level": level}); err != nil {
			t.Fatal(err)
		}
	}
	items, err := s.RecentLogs("warn", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Level != "error" || items[1].Level != "warn" {
		t.Fatalf("unexpected filtered logs: %#v", items)
	}
	if _, err := s.db.Exec("INSERT INTO logs(level,logger,message,occurred_at,fields) VALUES('error','old','expired',?, '{}')", time.Now().UTC().Add(-49*time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := s.PurgeLogs(48 * time.Hour); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM logs WHERE logger='old'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expired log was not removed")
	}
}

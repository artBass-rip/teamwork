package store

import (
	"path/filepath"
	"testing"
)

func TestPendingDeliveriesExcludesCompletedEvents(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.RecordEvent("pending", "producer", "test.created", map[string]any{"value": "one"}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordEvent("complete", "producer", "test.created", map[string]any{"value": "two"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Delivery("pending", "subscriber", "pending", "module unavailable"); err != nil {
		t.Fatal(err)
	}
	if err := store.Delivery("complete", "subscriber", "completed", ""); err != nil {
		t.Fatal(err)
	}
	events, err := store.PendingDeliveries("subscriber", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != "pending" {
		t.Fatalf("unexpected pending events: %#v", events)
	}
}

package main

import "testing"

func TestViewValuesReadsSelectsAndText(t *testing.T) {
	payload := map[string]any{"view": map[string]any{"state": map[string]any{"values": map[string]any{
		"content":  map[string]any{"mode": map[string]any{"selected_option": map[string]any{"value": "thread"}}},
		"new_page": map[string]any{"title": map[string]any{"value": "Incident notes"}},
	}}}}
	got := viewValues(payload)
	if got["content.mode"] != "thread" || got["new_page.title"] != "Incident notes" {
		t.Fatalf("unexpected values: %#v", got)
	}
}

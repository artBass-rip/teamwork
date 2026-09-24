package main

import (
	"strings"
	"testing"
)

func TestRenderContentEscapesSlackHTML(t *testing.T) {
	source := map[string]any{
		"workspace": "Engineering & Ops",
		"permalink": "https://slack.example/message",
		"users":     map[string]any{"U1": "Alice"},
		"messages":  []any{map[string]any{"user": "U1", "text": "<deploy> & verify"}},
	}
	got := renderContent(source, "thread")
	if !strings.Contains(got, "Engineering &amp; Ops") || !strings.Contains(got, "&lt;deploy&gt; &amp; verify") {
		t.Fatalf("content is not escaped: %s", got)
	}
}

func TestFirstLineCreatesSafeDefaultTitle(t *testing.T) {
	if got := firstLine("First line\nSecond line"); got != "First line" {
		t.Fatalf("unexpected title: %q", got)
	}
}

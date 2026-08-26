package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIssuesUsesAuthenticatedJQLAndMapsADF(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/search/jql" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Basic "+base64.StdEncoding.EncodeToString([]byte("me@example.com:token")) {
			t.Fatalf("unexpected authorization header")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["jql"] != `project = "TEAM" ORDER BY updated DESC` {
			t.Fatalf("unexpected JQL: %v", body["jql"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[{"key":"TEAM-1","fields":{"summary":"OAuth error","description":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Fix refresh flow"}]}]},"status":{"name":"In Progress"},"issuetype":{"name":"Bug"},"assignee":{"displayName":"Alex"},"reporter":{"displayName":"Maria"},"labels":["oauth"],"customfield_10020":[{"name":"Sprint 12","state":"active"}]}}]}`))
	}))
	defer server.Close()
	c := jiraClient{account: Account{BaseURL: server.URL, Email: "me@example.com"}, token: "token", http: server.Client()}
	project, err := c.issues(context.Background(), "TEAM")
	if err != nil {
		t.Fatal(err)
	}
	issues := project["issues"].([]map[string]any)
	if len(issues) != 1 || issues[0]["title"] != "OAuth error" || issues[0]["description"] != "Fix refresh flow" || issues[0]["author"] != "Maria" {
		t.Fatalf("unexpected mapping: %#v", issues)
	}
	if issues[0]["sprint"] != "Sprint 12" {
		t.Fatalf("unexpected sprint: %#v", issues[0]["sprint"])
	}
	if issues[0]["sprintState"] != "active" {
		t.Fatalf("unexpected sprint state: %#v", issues[0]["sprintState"])
	}
	if _, exists := issues[0]["labels"]; exists {
		t.Fatal("Jira labels must not be imported")
	}
}

func TestKeysValueNormalizesAndDeduplicates(t *testing.T) {
	got := keysValue(" team,OPS, team ")
	if len(got) != 2 || got[0] != "TEAM" || got[1] != "OPS" {
		t.Fatalf("unexpected keys: %#v", got)
	}
}

func TestSprintInfoPrefersUnfinishedSprint(t *testing.T) {
	name, state := sprintInfo([]any{
		map[string]any{"name": "Sprint 41", "state": "closed"},
		map[string]any{"name": "Sprint 42", "state": "active"},
	})
	if name != "Sprint 42" || state != "active" {
		t.Fatalf("unexpected sprint: %q %q", name, state)
	}
}

func TestDecodeResourcesAcceptsEmptyAndWrappedArrays(t *testing.T) {
	empty, err := decodeResources([]any{})
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty array: %#v, %v", empty, err)
	}
	wrapped, err := decodeResources(map[string]any{"resources": []any{map[string]any{"id": "cloud-1", "name": "Team", "url": "https://team.atlassian.net"}}})
	if err != nil || len(wrapped) != 1 || wrapped[0].ID != "cloud-1" {
		t.Fatalf("wrapped resources: %#v, %v", wrapped, err)
	}
}

func TestIssuesFollowsNextPageToken(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			if body["nextPageToken"] != nil {
				t.Fatalf("first request contains page token")
			}
			_, _ = w.Write([]byte(`{"issues":[{"key":"P-1","fields":{"summary":"First"}}],"nextPageToken":"page-2"}`))
			return
		}
		if body["nextPageToken"] != "page-2" {
			t.Fatalf("unexpected token: %v", body["nextPageToken"])
		}
		_, _ = w.Write([]byte(`{"issues":[{"key":"P-2","fields":{"summary":"Second"}}]}`))
	}))
	defer server.Close()
	c := jiraClient{account: Account{BaseURL: server.URL}, http: server.Client()}
	project, err := c.issues(context.Background(), "P")
	if err != nil {
		t.Fatal(err)
	}
	issues := project["issues"].([]map[string]any)
	if calls != 2 || len(issues) != 2 {
		t.Fatalf("calls=%d issues=%#v", calls, issues)
	}
}

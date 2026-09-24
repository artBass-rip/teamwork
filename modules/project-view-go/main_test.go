package main

import "testing"

func TestImportPreservesLocalComments(t *testing.T) {
	repo, err := openRepository(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.importProject(map[string]any{
		"key": "TEAM", "name": "Team", "issues": []any{map[string]any{"key": "TEAM-1", "title": "Old title"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.updateIssue(map[string]any{"projectKey": "TEAM", "issueKey": "TEAM-1", "comment": "Local note", "labels": []any{"local", "urgent"}})
	if err != nil {
		t.Fatal(err)
	}
	state, err := repo.importProject(map[string]any{
		"key": "TEAM", "name": "Team", "issues": []any{map[string]any{"key": "TEAM-1", "title": "Updated from Jira"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	issue := state.Projects[0].Issues[0]
	if issue.Title != "Updated from Jira" || len(issue.Comments) != 1 || issue.Comments[0].Text != "Local note" || len(issue.Labels) != 2 || issue.Labels[0] != "local" {
		t.Fatalf("local comments were not preserved: %#v", issue)
	}
}

func TestUpdateAndDeleteLocalComment(t *testing.T) {
	repo, err := openRepository(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.importProject(map[string]any{
		"key": "TEAM", "name": "Team", "issues": []any{map[string]any{"key": "TEAM-1", "title": "Task"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err := repo.updateIssue(map[string]any{"projectKey": "TEAM", "issueKey": "TEAM-1", "comment": "Original"})
	if err != nil {
		t.Fatal(err)
	}
	commentID := state.Projects[0].Issues[0].Comments[0].ID
	state, err = repo.updateComment("TEAM", "TEAM-1", commentID, "Edited")
	if err != nil {
		t.Fatal(err)
	}
	comment := state.Projects[0].Issues[0].Comments[0]
	if comment.Text != "Edited" || comment.UpdatedAt == "" {
		t.Fatalf("comment was not updated: %#v", comment)
	}
	state, err = repo.deleteComment("TEAM", "TEAM-1", commentID)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Projects[0].Issues[0].Comments) != 0 {
		t.Fatalf("comment was not deleted: %#v", state.Projects[0].Issues[0].Comments)
	}
}

func TestImportSortsIssueKeysDescending(t *testing.T) {
	repo, err := openRepository(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	state, err := repo.importProject(map[string]any{
		"key": "TEAM", "name": "Team", "issues": []any{
			map[string]any{"key": "TEAM-9", "title": "Nine"},
			map[string]any{"key": "TEAM-100", "title": "Hundred"},
			map[string]any{"key": "TEAM-20", "title": "Twenty"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	issues := state.Projects[0].Issues
	if issues[0].Key != "TEAM-100" || issues[1].Key != "TEAM-20" || issues[2].Key != "TEAM-9" {
		t.Fatalf("unexpected order: %#v", issues)
	}
}

func TestDeleteLabelRemovesItFromEveryIssue(t *testing.T) {
	repo, err := openRepository(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.importProject(map[string]any{
		"key": "TEAM", "name": "Team", "issues": []any{
			map[string]any{"key": "TEAM-1", "title": "One", "labels": []any{"urgent", "local"}},
			map[string]any{"key": "TEAM-2", "title": "Two", "labels": []any{"URGENT"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err := repo.deleteLabel("urgent")
	if err != nil {
		t.Fatal(err)
	}
	labelsByIssue := map[string][]string{}
	for _, issue := range state.Projects[0].Issues {
		labelsByIssue[issue.Key] = issue.Labels
	}
	if len(labelsByIssue["TEAM-1"]) != 1 || labelsByIssue["TEAM-1"][0] != "local" || len(labelsByIssue["TEAM-2"]) != 0 {
		t.Fatalf("label was not deleted globally: %#v", state.Projects[0].Issues)
	}
}

func TestApplyLabelGroupsPreservesExistingLabelsAndReportsMissing(t *testing.T) {
	repo, err := openRepository(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.importProject(map[string]any{
		"key": "DO", "name": "DO", "issues": []any{
			map[string]any{"key": "DO-1", "title": "One", "labels": []any{"existing"}},
			map[string]any{"key": "DO-2", "title": "Two"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	state, report, err := repo.applyLabelGroups([]LabelGroup{{Label: "Database", IssueKeys: []string{"DO-1", "DO-2", "DO-404"}}})
	if err != nil {
		t.Fatal(err)
	}
	if report["requested"] != 3 || report["matched"] != 2 || report["labelsAdded"] != 2 || len(report["missing"].([]string)) != 1 {
		t.Fatalf("unexpected report: %#v", report)
	}
	for _, issue := range state.Projects[0].Issues {
		if issue.Key == "DO-1" && (len(issue.Labels) != 2 || issue.Labels[0] != "existing" || issue.Labels[1] != "Database") {
			t.Fatalf("existing label was not preserved: %#v", issue.Labels)
		}
	}
}

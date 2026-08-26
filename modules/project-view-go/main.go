package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	sdk "teamwork/sdk/go/runtime"
)

//go:embed web/index.html
var applicationHTML string

type Issue struct {
	Key         string    `json:"key"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Status      string    `json:"status,omitempty"`
	Type        string    `json:"type,omitempty"`
	Assignee    string    `json:"assignee,omitempty"`
	Author      string    `json:"author,omitempty"`
	Goal        string    `json:"goal,omitempty"`
	Sprint      string    `json:"sprint,omitempty"`
	SprintState string    `json:"sprintState,omitempty"`
	URL         string    `json:"url,omitempty"`
	Labels      []string  `json:"labels,omitempty"`
	Comments    []Comment `json:"comments,omitempty"`
}
type Comment struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	CreatedAt string `json:"createdAt"`
}
type Project struct {
	Key        string  `json:"key"`
	Name       string  `json:"name"`
	Issues     []Issue `json:"issues"`
	ImportedAt string  `json:"importedAt"`
}
type State struct {
	Projects []Project `json:"projects"`
	LastSync string    `json:"lastSync,omitempty"`
}
type LabelGroup struct {
	Label     string   `json:"label"`
	IssueKeys []string `json:"issueKeys"`
}
type repository struct {
	mu    sync.RWMutex
	path  string
	state State
}

func openRepository(path string) (*repository, error) {
	r := &repository{path: path, state: State{Projects: []Project{}}}
	data, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(data, &r.state)
	} else if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	if r.state.Projects == nil {
		r.state.Projects = []Project{}
	}
	return r, err
}
func (r *repository) snapshot() State {
	r.mu.RLock()
	defer r.mu.RUnlock()
	data, _ := json.Marshal(r.state)
	var value State
	_ = json.Unmarshal(data, &value)
	if value.Projects == nil {
		value.Projects = []Project{}
	}
	return value
}
func (r *repository) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r.state, "", "  ")
	if err != nil {
		return err
	}
	temporary := r.path + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0600); err != nil {
		return err
	}
	return os.Rename(temporary, r.path)
}
func (r *repository) importProject(payload map[string]any) (State, error) {
	data, _ := json.Marshal(payload)
	var project Project
	if err := json.Unmarshal(data, &project); err != nil {
		return State{}, err
	}
	project.Key = strings.ToUpper(strings.TrimSpace(project.Key))
	project.Name = strings.TrimSpace(project.Name)
	if project.Key == "" || project.Name == "" {
		return State{}, fmt.Errorf("project key and name are required")
	}
	seen := map[string]bool{}
	for i := range project.Issues {
		issue := &project.Issues[i]
		issue.Key = strings.ToUpper(strings.TrimSpace(issue.Key))
		issue.Title = strings.TrimSpace(issue.Title)
		if issue.Key == "" || issue.Title == "" {
			return State{}, fmt.Errorf("every issue needs key and title")
		}
		if seen[issue.Key] {
			return State{}, fmt.Errorf("duplicate issue %s", issue.Key)
		}
		seen[issue.Key] = true
	}
	sort.Slice(project.Issues, func(i, j int) bool {
		leftPrefix, leftNumber := issueKeyParts(project.Issues[i].Key)
		rightPrefix, rightNumber := issueKeyParts(project.Issues[j].Key)
		if leftPrefix == rightPrefix && leftNumber != rightNumber {
			return leftNumber > rightNumber
		}
		return project.Issues[i].Key > project.Issues[j].Key
	})
	project.ImportedAt = time.Now().UTC().Format(time.RFC3339)
	r.mu.Lock()
	defer r.mu.Unlock()
	replaced := false
	for i := range r.state.Projects {
		if r.state.Projects[i].Key == project.Key {
			localComments := map[string][]Comment{}
			localLabels := map[string][]string{}
			for _, issue := range r.state.Projects[i].Issues {
				if len(issue.Comments) > 0 {
					localComments[issue.Key] = append([]Comment(nil), issue.Comments...)
				}
				if len(issue.Labels) > 0 {
					localLabels[issue.Key] = append([]string(nil), issue.Labels...)
				}
			}
			for ii := range project.Issues {
				if comments, ok := localComments[project.Issues[ii].Key]; ok {
					project.Issues[ii].Comments = comments
				}
				if labels, ok := localLabels[project.Issues[ii].Key]; ok {
					project.Issues[ii].Labels = labels
				}
			}
			r.state.Projects[i] = project
			replaced = true
		}
	}
	if !replaced {
		r.state.Projects = append(r.state.Projects, project)
	}
	sort.Slice(r.state.Projects, func(i, j int) bool { return r.state.Projects[i].Key < r.state.Projects[j].Key })
	return r.state, r.saveLocked()
}

func issueKeyParts(key string) (string, int) {
	separator := strings.LastIndexByte(key, '-')
	if separator < 0 || separator == len(key)-1 {
		return key, -1
	}
	number := -1
	if _, err := fmt.Sscan(key[separator+1:], &number); err != nil {
		return key, -1
	}
	return key[:separator], number
}

func (r *repository) updateIssue(payload map[string]any) (State, error) {
	projectKey, _ := payload["projectKey"].(string)
	issueKey, _ := payload["issueKey"].(string)
	labelsValue, _ := payload["labels"].([]any)
	comment, _ := payload["comment"].(string)
	r.mu.Lock()
	defer r.mu.Unlock()
	for pi := range r.state.Projects {
		if r.state.Projects[pi].Key != projectKey {
			continue
		}
		for ii := range r.state.Projects[pi].Issues {
			issue := &r.state.Projects[pi].Issues[ii]
			if issue.Key != issueKey {
				continue
			}
			if labelsValue != nil {
				labels := []string{}
				seen := map[string]bool{}
				for _, raw := range labelsValue {
					label := strings.TrimSpace(fmt.Sprint(raw))
					if label != "" && !seen[strings.ToLower(label)] {
						labels = append(labels, label)
						seen[strings.ToLower(label)] = true
					}
				}
				issue.Labels = labels
			}
			if strings.TrimSpace(comment) != "" {
				issue.Comments = append(issue.Comments, Comment{ID: fmt.Sprintf("%d", time.Now().UnixNano()), Text: strings.TrimSpace(comment), CreatedAt: time.Now().UTC().Format(time.RFC3339)})
			}
			return r.state, r.saveLocked()
		}
	}
	return State{}, fmt.Errorf("issue %s/%s not found", projectKey, issueKey)
}

func (r *repository) deleteLabel(raw string) (State, error) {
	label := strings.TrimSpace(raw)
	if label == "" {
		return State{}, fmt.Errorf("label is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for pi := range r.state.Projects {
		for ii := range r.state.Projects[pi].Issues {
			labels := r.state.Projects[pi].Issues[ii].Labels[:0]
			for _, existing := range r.state.Projects[pi].Issues[ii].Labels {
				if !strings.EqualFold(existing, label) {
					labels = append(labels, existing)
				}
			}
			r.state.Projects[pi].Issues[ii].Labels = labels
		}
	}
	return r.state, r.saveLocked()
}

func (r *repository) applyLabelGroups(groups []LabelGroup) (State, map[string]any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	labelsByKey := map[string][]string{}
	requested := map[string]bool{}
	for _, group := range groups {
		label := strings.TrimSpace(group.Label)
		if label == "" {
			return State{}, nil, fmt.Errorf("group label is required")
		}
		for _, rawKey := range group.IssueKeys {
			key := strings.ToUpper(strings.TrimSpace(rawKey))
			if key == "" {
				continue
			}
			requested[key] = true
			labelsByKey[key] = append(labelsByKey[key], label)
		}
	}
	matched := map[string]bool{}
	added := 0
	for pi := range r.state.Projects {
		for ii := range r.state.Projects[pi].Issues {
			issue := &r.state.Projects[pi].Issues[ii]
			labels, ok := labelsByKey[issue.Key]
			if !ok {
				continue
			}
			matched[issue.Key] = true
			for _, label := range labels {
				exists := false
				for _, current := range issue.Labels {
					if strings.EqualFold(current, label) {
						exists = true
						break
					}
				}
				if !exists {
					issue.Labels = append(issue.Labels, label)
					added++
				}
			}
		}
	}
	missing := []string{}
	for key := range requested {
		if !matched[key] {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	if err := r.saveLocked(); err != nil {
		return State{}, nil, err
	}
	return r.state, map[string]any{"requested": len(requested), "matched": len(matched), "labelsAdded": added, "missing": missing}, nil
}

func main() {
	dataDir := os.Getenv("TEAMWORK_MODULE_DATA")
	if dataDir == "" {
		log.Fatal("TEAMWORK_MODULE_DATA is required")
	}
	repo, err := openRepository(filepath.Join(dataDir, "state.json"))
	if err != nil {
		log.Fatal(err)
	}
	runtime := sdk.New()
	runtime.Capability("ui.app.render", func(context.Context, map[string]any) (any, error) { return applicationHTML, nil })
	runtime.Capability("projectview.state", func(context.Context, map[string]any) (any, error) { return repo.snapshot(), nil })
	runtime.Capability("projectview.import", func(ctx context.Context, payload map[string]any) (any, error) {
		state, err := repo.importProject(payload)
		if err == nil {
			_ = runtime.Publish(ctx, "projectview.imported", map[string]any{"project": payload["key"]})
		}
		return state, err
	})
	runtime.Capability("projectview.issue.update", func(ctx context.Context, payload map[string]any) (any, error) {
		state, err := repo.updateIssue(payload)
		if err == nil {
			_ = runtime.Publish(ctx, "projectview.issue.updated", map[string]any{"issue": payload["issueKey"]})
		}
		return state, err
	})
	runtime.Capability("projectview.label.delete", func(ctx context.Context, payload map[string]any) (any, error) {
		state, err := repo.deleteLabel(fmt.Sprint(payload["label"]))
		if err == nil {
			_ = runtime.Publish(ctx, "projectview.label.deleted", map[string]any{"label": payload["label"]})
		}
		return state, err
	})
	runtime.Capability("projectview.labels.apply", func(ctx context.Context, payload map[string]any) (any, error) {
		data, _ := json.Marshal(payload["groups"])
		var groups []LabelGroup
		if err := json.Unmarshal(data, &groups); err != nil {
			return nil, err
		}
		state, report, err := repo.applyLabelGroups(groups)
		if err == nil {
			_ = runtime.Publish(ctx, "projectview.labels.applied", report)
		}
		return map[string]any{"state": state, "report": report}, err
	})
	runtime.Capability("projectview.sync", func(ctx context.Context, _ map[string]any) (any, error) {
		repo.mu.Lock()
		repo.state.LastSync = time.Now().UTC().Format(time.RFC3339)
		err := repo.saveLocked()
		state := repo.state
		repo.mu.Unlock()
		if err == nil {
			_ = runtime.Publish(ctx, "projectview.sync.requested", map[string]any{})
		}
		return state, err
	})
	if err := runtime.Serve(context.Background()); err != nil {
		log.Fatal(err)
	}
}

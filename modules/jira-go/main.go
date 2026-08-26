package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	sdk "teamwork/sdk/go/runtime"
)

type Account struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	BaseURL     string   `json:"baseUrl"`
	WebURL      string   `json:"webUrl,omitempty"`
	AuthType    string   `json:"authType,omitempty"`
	Email       string   `json:"email"`
	ProjectKeys []string `json:"projectKeys,omitempty"`
	ExternalID  string   `json:"externalId,omitempty"`
	DisplayName string   `json:"displayName,omitempty"`
	Connected   bool     `json:"connected"`
	ExpiresAt   string   `json:"expiresAt,omitempty"`
	LastSync    string   `json:"lastSync,omitempty"`
	LastError   string   `json:"lastError,omitempty"`
}
type State struct {
	Accounts []Account `json:"accounts"`
}
type repository struct {
	mu    sync.RWMutex
	path  string
	state State
}

func openRepository(path string) (*repository, error) {
	r := &repository{path: path}
	b, e := os.ReadFile(path)
	if e == nil {
		e = json.Unmarshal(b, &r.state)
	} else if errors.Is(e, os.ErrNotExist) {
		e = nil
	}
	return r, e
}
func (r *repository) snapshot() State {
	r.mu.RLock()
	defer r.mu.RUnlock()
	b, _ := json.Marshal(r.state)
	var s State
	_ = json.Unmarshal(b, &s)
	return s
}
func (r *repository) saveLocked() error {
	if e := os.MkdirAll(filepath.Dir(r.path), 0700); e != nil {
		return e
	}
	b, e := json.MarshalIndent(r.state, "", "  ")
	if e != nil {
		return e
	}
	p := r.path + ".tmp"
	if e = os.WriteFile(p, append(b, '\n'), 0600); e != nil {
		return e
	}
	return os.Rename(p, r.path)
}
func (r *repository) put(a Account) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.state.Accounts {
		if r.state.Accounts[i].ID == a.ID {
			r.state.Accounts[i] = a
			return r.saveLocked()
		}
	}
	r.state.Accounts = append(r.state.Accounts, a)
	return r.saveLocked()
}
func (r *repository) remove(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.state.Accounts[:0]
	for _, a := range r.state.Accounts {
		if a.ID != id {
			out = append(out, a)
		}
	}
	r.state.Accounts = out
	return r.saveLocked()
}
func (r *repository) get(id string) (Account, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, a := range r.state.Accounts {
		if a.ID == id {
			return a, true
		}
	}
	return Account{}, false
}

type jiraClient struct {
	account Account
	token   string
	http    *http.Client
}

func (c jiraClient) request(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			return e
		}
		reader = bytes.NewReader(b)
	}
	req, e := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.account.BaseURL, "/")+path, reader)
	if e != nil {
		return e
	}
	if c.account.AuthType == "oauth" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	} else {
		req.SetBasicAuth(c.account.Email, c.token)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, e := c.http.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("Jira API %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
func (c jiraClient) myself(ctx context.Context) (string, string, error) {
	var v struct {
		AccountID   string `json:"accountId"`
		DisplayName string `json:"displayName"`
	}
	e := c.request(ctx, "GET", "/rest/api/3/myself", nil, &v)
	return v.AccountID, v.DisplayName, e
}

type jiraProject struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

func (c jiraClient) projects(ctx context.Context) ([]jiraProject, error) {
	var v struct {
		Values []jiraProject `json:"values"`
	}
	e := c.request(ctx, "GET", "/rest/api/3/project/search?maxResults=100", nil, &v)
	return v.Values, e
}
func adfText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		parts := []string{}
		for _, v := range x {
			if s := strings.TrimSpace(adfText(v)); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		if t, _ := x["text"].(string); t != "" {
			return t
		}
		return adfText(x["content"])
	}
	return ""
}
func sprintInfo(v any) (string, string) {
	if list, ok := v.([]any); ok {
		bestName, bestState, bestRank, bestNumber := "", "", 9, -1
		for _, item := range list {
			name, state := sprintInfo(item)
			rank := 1
			if state == "active" || state == "future" || state == "open" {
				rank = 0
			} else if state == "closed" || state == "complete" || state == "completed" {
				rank = 2
			}
			number := -1
			if match := regexp.MustCompile(`\d+`).FindAllString(name, -1); len(match) > 0 {
				_, _ = fmt.Sscan(match[len(match)-1], &number)
			}
			if name != "" && (rank < bestRank || rank == bestRank && number > bestNumber) {
				bestName, bestState, bestRank, bestNumber = name, state, rank, number
			}
		}
		return bestName, bestState
	}
	if m, ok := v.(map[string]any); ok {
		return strings.TrimSpace(fmt.Sprint(m["name"])), strings.ToLower(strings.TrimSpace(fmt.Sprint(m["state"])))
	}
	text := fmt.Sprint(v)
	name, state := "", ""
	if match := regexp.MustCompile(`name=([^,\]]+)`).FindStringSubmatch(text); len(match) > 1 {
		name = strings.TrimSpace(match[1])
	}
	if match := regexp.MustCompile(`state=([^,\]]+)`).FindStringSubmatch(text); len(match) > 1 {
		state = strings.ToLower(strings.TrimSpace(match[1]))
	}
	return name, state
}
func (c jiraClient) issues(ctx context.Context, key string) (map[string]any, error) {
	body := map[string]any{"jql": "project = \"" + strings.ReplaceAll(key, "\"", "\\\"") + "\" ORDER BY updated DESC", "maxResults": 100, "fields": []string{"summary", "description", "status", "issuetype", "assignee", "reporter", "customfield_10020"}}
	var v struct {
		Issues []struct {
			Key    string `json:"key"`
			Fields struct {
				Summary     string `json:"summary"`
				Description any    `json:"description"`
				Status      struct {
					Name string `json:"name"`
				} `json:"status"`
				IssueType struct {
					Name string `json:"name"`
				} `json:"issuetype"`
				Assignee *struct {
					DisplayName string `json:"displayName"`
				} `json:"assignee"`
				Reporter *struct {
					DisplayName string `json:"displayName"`
				} `json:"reporter"`
				Sprint any `json:"customfield_10020"`
			} `json:"fields"`
		} `json:"issues"`
		NextPageToken string `json:"nextPageToken"`
	}
	issues := []map[string]any{}
	seen := map[string]bool{}
	for {
		v.Issues = nil
		v.NextPageToken = ""
		if e := c.request(ctx, "POST", "/rest/api/3/search/jql", body, &v); e != nil {
			return nil, e
		}
		for _, i := range v.Issues {
			assignee := ""
			if i.Fields.Assignee != nil {
				assignee = i.Fields.Assignee.DisplayName
			}
			browseURL := c.account.WebURL
			if browseURL == "" {
				browseURL = c.account.BaseURL
			}
			author := ""
			if i.Fields.Reporter != nil {
				author = i.Fields.Reporter.DisplayName
			}
			sprint, sprintState := sprintInfo(i.Fields.Sprint)
			issues = append(issues, map[string]any{"key": i.Key, "title": i.Fields.Summary, "description": adfText(i.Fields.Description), "status": i.Fields.Status.Name, "type": i.Fields.IssueType.Name, "assignee": assignee, "author": author, "sprint": sprint, "sprintState": sprintState, "url": strings.TrimRight(browseURL, "/") + "/browse/" + i.Key})
		}
		if v.NextPageToken == "" {
			break
		}
		if seen[v.NextPageToken] {
			return nil, fmt.Errorf("Jira REST repeated page token")
		}
		seen[v.NextPageToken] = true
		body["nextPageToken"] = v.NextPageToken
	}
	return map[string]any{"key": key, "name": key, "issues": issues}, nil
}

func stringValue(p map[string]any, k string) string { return strings.TrimSpace(fmt.Sprint(p[k])) }
func keysValue(v any) []string {
	var raw []string
	switch x := v.(type) {
	case string:
		raw = strings.Split(x, ",")
	case []any:
		for _, v := range x {
			raw = append(raw, fmt.Sprint(v))
		}
	}
	out := []string{}
	seen := map[string]bool{}
	for _, s := range raw {
		k := strings.ToUpper(strings.TrimSpace(s))
		if k != "" && !seen[k] {
			out = append(out, k)
			seen[k] = true
		}
	}
	return out
}
func randomID() string { b := make([]byte, 8); _, _ = rand.Read(b); return hex.EncodeToString(b) }

const jiraOAuthCallback = "http://127.0.0.1:8976/oauth/jira/callback"
const jiraMCPURL = "https://mcp.atlassian.com/v1/mcp/authv2"
const jiraMCPMetadataURL = "https://mcp.atlassian.com/.well-known/oauth-protected-resource/v1/mcp/authv2"

type oauthFlow struct {
	mu                                                                                                     sync.RWMutex
	ID, State, ClientID, Verifier, AuthEndpoint, TokenEndpoint, AuthorizationURL, Status, Error, AccountID string
}

func (f *oauthFlow) result() map[string]any {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return map[string]any{"id": f.ID, "status": f.Status, "error": f.Error, "accountId": f.AccountID}
}

type oauthToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}
type oauthResource struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	URL    string   `json:"url"`
	Scopes []string `json:"scopes"`
}

func oauthTokenPost(ctx context.Context, endpoint string, body url.Values, out any) error {
	req, e := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(body.Encode()))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("Atlassian OAuth %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type oauthMetadata struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	RegistrationEndpoint  string `json:"registration_endpoint"`
}

func getJSON(ctx context.Context, endpoint, token string, out any) error {
	req, e := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if e != nil {
		return e
	}
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("GET %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
func discoverOAuth(ctx context.Context) (oauthMetadata, error) {
	var resource struct {
		AuthorizationServers []string `json:"authorization_servers"`
	}
	if e := getJSON(ctx, jiraMCPMetadataURL, "", &resource); e != nil {
		return oauthMetadata{}, e
	}
	if len(resource.AuthorizationServers) == 0 {
		return oauthMetadata{}, fmt.Errorf("Atlassian MCP did not advertise an authorization server")
	}
	issuer, e := url.Parse(resource.AuthorizationServers[0])
	if e != nil {
		return oauthMetadata{}, e
	}
	var meta oauthMetadata
	e = getJSON(ctx, issuer.Scheme+"://"+issuer.Host+"/.well-known/oauth-authorization-server"+issuer.Path, "", &meta)
	return meta, e
}
func registerOAuthClient(ctx context.Context, endpoint string) (string, error) {
	body := map[string]any{"client_name": "TeamWork Jira", "redirect_uris": []string{jiraOAuthCallback}, "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}, "token_endpoint_auth_method": "none"}
	b, _ := json.Marshal(body)
	req, e := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(b))
	if e != nil {
		return "", e
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		return "", e
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("OAuth client registration %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	var v struct {
		ClientID string `json:"client_id"`
	}
	e = json.NewDecoder(resp.Body).Decode(&v)
	if e == nil && v.ClientID == "" {
		e = fmt.Errorf("OAuth registration returned no client_id")
	}
	return v.ClientID, e
}
func mcpTool(ctx context.Context, token, name string, args map[string]any) (any, error) {
	session := ""
	call := func(method string, params any, id int) (map[string]any, error) {
		payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
		req, e := http.NewRequestWithContext(ctx, "POST", jiraMCPURL, bytes.NewReader(payload))
		if e != nil {
			return nil, e
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if session != "" {
			req.Header.Set("Mcp-Session-Id", session)
		}
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			return nil, e
		}
		defer resp.Body.Close()
		if s := resp.Header.Get("Mcp-Session-Id"); s != "" {
			session = s
		}
		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("Atlassian MCP %s: %s", resp.Status, strings.TrimSpace(string(data)))
		}
		if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "data:") {
					data = []byte(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
					break
				}
			}
		}
		var envelope map[string]any
		if e = json.Unmarshal(data, &envelope); e != nil {
			return nil, e
		}
		if failure := envelope["error"]; failure != nil {
			return nil, fmt.Errorf("Atlassian MCP: %v", failure)
		}
		value, _ := envelope["result"].(map[string]any)
		return value, nil
	}
	if _, e := call("initialize", map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "teamwork-jira", "version": "1.0.0"}}, 1); e != nil {
		return nil, e
	}
	notification, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	req, e := http.NewRequestWithContext(ctx, "POST", jiraMCPURL, bytes.NewReader(notification))
	if e != nil {
		return nil, e
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
	response, e := http.DefaultClient.Do(req)
	if e != nil {
		return nil, e
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Atlassian MCP initialization notification: %s", response.Status)
	}
	result, e := call("tools/call", map[string]any{"name": name, "arguments": args}, 2)
	if e != nil {
		return nil, e
	}
	if value := result["structuredContent"]; value != nil {
		return value, nil
	}
	if content, ok := result["content"].([]any); ok {
		for _, raw := range content {
			item, _ := raw.(map[string]any)
			if item["type"] == "text" {
				text := fmt.Sprint(item["text"])
				var parsed any
				if json.Unmarshal([]byte(text), &parsed) == nil {
					return parsed, nil
				}
				return text, nil
			}
		}
	}
	return result, nil
}
func mcpResources(ctx context.Context, token string) ([]oauthResource, error) {
	value, e := mcpTool(ctx, token, "getAccessibleAtlassianResources", map[string]any{})
	if e != nil {
		return nil, e
	}
	return decodeResources(value)
}
func decodeResources(value any) ([]oauthResource, error) {
	b, e := json.Marshal(value)
	if e != nil {
		return nil, e
	}
	var direct []oauthResource
	if e = json.Unmarshal(b, &direct); e == nil {
		return direct, nil
	}
	var wrapped struct {
		Resources []oauthResource `json:"resources"`
		Values    []oauthResource `json:"values"`
	}
	e = json.Unmarshal(b, &wrapped)
	if e != nil {
		return nil, fmt.Errorf("unsupported Atlassian resources response: %w", e)
	}
	if len(wrapped.Resources) > 0 {
		return wrapped.Resources, e
	}
	return wrapped.Values, e
}
func mcpProjects(ctx context.Context, token, cloudID string) ([]jiraProject, error) {
	all := []jiraProject{}
	for start := 0; ; {
		value, e := mcpTool(ctx, token, "getVisibleJiraProjects", map[string]any{"cloudId": cloudID, "action": "browse", "maxResults": 50, "startAt": start})
		if e != nil {
			return nil, e
		}
		b, _ := json.Marshal(value)
		var direct []jiraProject
		if e = json.Unmarshal(b, &direct); e == nil {
			all = append(all, direct...)
			if len(direct) == 0 || len(direct) < 50 {
				return all, nil
			}
			start += len(direct)
			continue
		}
		var page struct {
			Values   []jiraProject `json:"values"`
			Projects []jiraProject `json:"projects"`
			IsLast   bool          `json:"isLast"`
			Total    int           `json:"total"`
		}
		if e = json.Unmarshal(b, &page); e != nil {
			return nil, e
		}
		items := page.Values
		if len(items) == 0 {
			items = page.Projects
		}
		all = append(all, items...)
		start += len(items)
		if page.IsLast || len(items) == 0 || (page.Total > 0 && start >= page.Total) || len(items) < 50 {
			return all, nil
		}
	}
}
func mcpIssues(ctx context.Context, token string, a Account, key string) (map[string]any, error) {
	issues := []map[string]any{}
	next := ""
	seen := map[string]bool{}
	for {
		args := map[string]any{"cloudId": a.ExternalID, "jql": "project = \"" + strings.ReplaceAll(key, "\"", "\\\"") + "\" ORDER BY updated DESC", "fields": []string{"summary", "description", "status", "issuetype", "assignee", "reporter", "customfield_10020"}, "maxResults": 100, "responseContentFormat": "markdown", "searchResultMode": "issues"}
		if next != "" {
			args["nextPageToken"] = next
		}
		value, e := mcpTool(ctx, token, "searchJiraIssuesUsingJql", args)
		if e != nil {
			return nil, e
		}
		b, _ := json.Marshal(value)
		var page struct {
			Issues []struct {
				Key    string         `json:"key"`
				Fields map[string]any `json:"fields"`
			} `json:"issues"`
			NextPageToken string `json:"nextPageToken"`
		}
		if e = json.Unmarshal(b, &page); e != nil {
			return nil, e
		}
		for _, i := range page.Issues {
			f := i.Fields
			name := func(v any) string { m, _ := v.(map[string]any); return fmt.Sprint(m["name"]) }
			assignee := ""
			if m, _ := f["assignee"].(map[string]any); m != nil {
				assignee = fmt.Sprint(m["displayName"])
			}
			author := ""
			if m, _ := f["reporter"].(map[string]any); m != nil {
				author = fmt.Sprint(m["displayName"])
			}
			sprint, sprintState := sprintInfo(f["customfield_10020"])
			issues = append(issues, map[string]any{"key": i.Key, "title": fmt.Sprint(f["summary"]), "description": adfText(f["description"]), "status": name(f["status"]), "type": name(f["issuetype"]), "assignee": assignee, "author": author, "sprint": sprint, "sprintState": sprintState, "url": strings.TrimRight(a.WebURL, "/") + "/browse/" + i.Key})
		}
		if page.NextPageToken == "" {
			break
		}
		if seen[page.NextPageToken] {
			return nil, fmt.Errorf("Atlassian MCP repeated Jira page token")
		}
		seen[page.NextPageToken] = true
		next = page.NextPageToken
	}
	return map[string]any{"key": key, "name": key, "issues": issues}, nil
}

func main() {
	data := os.Getenv("TEAMWORK_MODULE_DATA")
	if data == "" {
		log.Fatal("TEAMWORK_MODULE_DATA is required")
	}
	repo, e := openRepository(filepath.Join(data, "accounts.json"))
	if e != nil {
		log.Fatal(e)
	}
	rt := sdk.New()
	writeLog := func(ctx context.Context, level, message string, fields map[string]any) {
		_ = rt.Log(ctx, level, message, fields)
	}
	flows := map[string]*oauthFlow{}
	var flowsMu sync.RWMutex
	clientFor := func(ctx context.Context, a Account) (jiraClient, error) {
		ref := "account/" + a.ID + "/api-token"
		if a.AuthType == "oauth" || a.AuthType == "rovo-mcp" {
			ref = "account/" + a.ID + "/access-token"
			if expires, err := time.Parse(time.RFC3339, a.ExpiresAt); err == nil && time.Now().Add(time.Minute).After(expires) {
				refresh, e := rt.SecretGet(ctx, "account/"+a.ID+"/refresh-token")
				if e != nil {
					return jiraClient{}, e
				}
				clientID, e := rt.SecretGet(ctx, "account/"+a.ID+"/client-id")
				if e != nil {
					return jiraClient{}, e
				}
				tokenEndpoint, e := rt.SecretGet(ctx, "account/"+a.ID+"/token-endpoint")
				if e != nil {
					return jiraClient{}, e
				}
				var next oauthToken
				e = oauthTokenPost(ctx, tokenEndpoint, url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {refresh}, "resource": {jiraMCPURL}}, &next)
				if e != nil {
					return jiraClient{}, e
				}
				if e = rt.SecretPut(ctx, ref, next.AccessToken); e != nil {
					return jiraClient{}, e
				}
				if next.RefreshToken != "" {
					if e = rt.SecretPut(ctx, "account/"+a.ID+"/refresh-token", next.RefreshToken); e != nil {
						return jiraClient{}, e
					}
				}
				a.ExpiresAt = time.Now().Add(time.Duration(next.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
				if e = repo.put(a); e != nil {
					return jiraClient{}, e
				}
			}
		}
		token, e := rt.SecretGet(ctx, ref)
		return jiraClient{account: a, token: token, http: &http.Client{Timeout: 30 * time.Second}}, e
	}
	rt.Capability("jira.account.list", func(context.Context, map[string]any) (any, error) { return repo.snapshot(), nil })
	rt.Capability("jira.oauth.config", func(context.Context, map[string]any) (any, error) {
		return map[string]any{"configured": true, "provider": "Atlassian Rovo MCP", "callbackUrl": jiraOAuthCallback}, nil
	})
	rt.Capability("jira.oauth.start", func(ctx context.Context, p map[string]any) (any, error) {
		writeLog(ctx, "info", "Jira browser authorization requested", map[string]any{"provider": "Atlassian Rovo MCP"})
		flowsMu.RLock()
		for _, existing := range flows {
			existing.mu.RLock()
			active := existing.Status == "waiting" || existing.Status == "exchanging"
			flowID, authorizationURL := existing.ID, existing.AuthorizationURL
			existing.mu.RUnlock()
			if active && authorizationURL != "" {
				flowsMu.RUnlock()
				return map[string]any{"flowId": flowID, "authorizationUrl": authorizationURL, "callbackUrl": jiraOAuthCallback, "reused": true}, nil
			}
		}
		flowsMu.RUnlock()
		metadata, err := discoverOAuth(ctx)
		if err != nil {
			writeLog(ctx, "error", "OAuth discovery failed", map[string]any{"error": err.Error()})
			return nil, err
		}
		clientID, err := registerOAuthClient(ctx, metadata.RegistrationEndpoint)
		if err != nil {
			writeLog(ctx, "error", "OAuth dynamic client registration failed", map[string]any{"error": err.Error()})
			return nil, err
		}
		verifierBytes := make([]byte, 32)
		if _, err = rand.Read(verifierBytes); err != nil {
			return nil, err
		}
		verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)
		challenge := sha256.Sum256([]byte(verifier))
		q := url.Values{"client_id": {clientID}, "scope": {"read:me read:account email offline_access read:jira-work read:all:twg"}, "redirect_uri": {jiraOAuthCallback}, "state": {randomID() + randomID()}, "response_type": {"code"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "code_challenge_method": {"S256"}, "resource": {jiraMCPURL}}
		flow := &oauthFlow{ID: randomID(), State: q.Get("state"), ClientID: clientID, Verifier: verifier, AuthEndpoint: metadata.AuthorizationEndpoint, TokenEndpoint: metadata.TokenEndpoint, AuthorizationURL: metadata.AuthorizationEndpoint + "?" + q.Encode(), Status: "waiting"}
		flowsMu.Lock()
		flows[flow.ID] = flow
		flowsMu.Unlock()
		listener, err := net.Listen("tcp", "127.0.0.1:8976")
		if err != nil {
			writeLog(ctx, "error", "OAuth callback listener failed", map[string]any{"address": "127.0.0.1:8976", "error": err.Error()})
			return nil, fmt.Errorf("OAuth callback port 8976 is unavailable: %w", err)
		}
		server := &http.Server{ReadHeaderTimeout: 5 * time.Second}
		mux := http.NewServeMux()
		server.Handler = mux
		mux.HandleFunc("/oauth/jira/callback", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("state") != flow.State {
				writeLog(r.Context(), "warn", "OAuth callback rejected invalid state", map[string]any{"flowId": flow.ID})
				http.Error(w, "Invalid OAuth state", http.StatusBadRequest)
				return
			}
			code := r.URL.Query().Get("code")
			if code == "" {
				writeLog(r.Context(), "warn", "OAuth callback contains no authorization code", map[string]any{"flowId": flow.ID, "providerError": r.URL.Query().Get("error")})
				http.Error(w, "Authorization was not completed", http.StatusBadRequest)
				return
			}
			flow.mu.Lock()
			flow.Status = "exchanging"
			flow.mu.Unlock()
			writeLog(r.Context(), "info", "OAuth callback received", map[string]any{"flowId": flow.ID})
			go func() {
				defer server.Shutdown(context.Background())
				exchangeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				var token oauthToken
				err := oauthTokenPost(exchangeCtx, flow.TokenEndpoint, url.Values{"grant_type": {"authorization_code"}, "client_id": {flow.ClientID}, "code": {code}, "redirect_uri": {jiraOAuthCallback}, "code_verifier": {flow.Verifier}, "resource": {jiraMCPURL}}, &token)
				var resources []oauthResource
				if err == nil {
					resources, err = mcpResources(exchangeCtx, token.AccessToken)
				}
				if err == nil && len(resources) == 0 {
					err = fmt.Errorf("Atlassian account has no accessible Jira sites")
				}
				if err == nil {
					resource := resources[0]
					id := randomID()
					a := Account{ID: id, Name: resource.Name, BaseURL: jiraMCPURL, WebURL: resource.URL, AuthType: "rovo-mcp", Connected: true, ExternalID: resource.ID, DisplayName: resource.Name, ProjectKeys: []string{}, ExpiresAt: time.Now().Add(time.Duration(token.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)}
					err = rt.SecretPut(exchangeCtx, "account/"+id+"/access-token", token.AccessToken)
					if err == nil && token.RefreshToken != "" {
						err = rt.SecretPut(exchangeCtx, "account/"+id+"/refresh-token", token.RefreshToken)
					}
					if err == nil {
						err = rt.SecretPut(exchangeCtx, "account/"+id+"/client-id", flow.ClientID)
					}
					if err == nil {
						err = rt.SecretPut(exchangeCtx, "account/"+id+"/token-endpoint", flow.TokenEndpoint)
					}
					if err == nil {
						err = repo.put(a)
					}
					if err == nil {
						flow.mu.Lock()
						flow.AccountID = id
						flow.Status = "completed"
						flow.mu.Unlock()
						_ = rt.Publish(exchangeCtx, "jira.account.configured", map[string]any{"accountId": id, "authType": "rovo-mcp"})
						writeLog(exchangeCtx, "info", "Jira account connected", map[string]any{"accountId": id, "site": resource.Name})
					}
				}
				if err != nil {
					writeLog(exchangeCtx, "error", "Jira OAuth completion failed", map[string]any{"flowId": flow.ID, "error": err.Error()})
					flow.mu.Lock()
					flow.Status = "failed"
					flow.Error = err.Error()
					flow.mu.Unlock()
				}
			}()
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, "<!doctype html><meta charset=utf-8><title>TeamWork Jira</title><style>body{font:16px system-ui;display:grid;place-items:center;height:100vh;margin:0}main{text-align:center}</style><main><h1>Jira подключена</h1><p>Вернитесь в TeamWork — окно можно закрыть.</p></main>")
		})
		go func() {
			if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
				writeLog(context.Background(), "error", "OAuth callback server stopped unexpectedly", map[string]any{"error": err.Error()})
				flow.mu.Lock()
				flow.Status = "failed"
				flow.Error = err.Error()
				flow.mu.Unlock()
			}
		}()
		time.AfterFunc(5*time.Minute, func() {
			flow.mu.Lock()
			if flow.Status == "waiting" {
				flow.Status = "failed"
				flow.Error = "OAuth authorization timed out"
				writeLog(context.Background(), "warn", "Jira OAuth authorization timed out", map[string]any{"flowId": flow.ID})
				_ = server.Shutdown(context.Background())
			}
			flow.mu.Unlock()
		})
		return map[string]any{"flowId": flow.ID, "authorizationUrl": flow.AuthorizationURL, "callbackUrl": jiraOAuthCallback}, nil
	})
	rt.Capability("jira.oauth.status", func(_ context.Context, p map[string]any) (any, error) {
		flowsMu.RLock()
		f := flows[stringValue(p, "flowId")]
		flowsMu.RUnlock()
		if f == nil {
			return nil, fmt.Errorf("OAuth flow not found")
		}
		return f.result(), nil
	})
	rt.Capability("jira.account.configure", func(ctx context.Context, p map[string]any) (any, error) {
		base := strings.TrimRight(stringValue(p, "baseUrl"), "/")
		email := stringValue(p, "email")
		token := stringValue(p, "token")
		if _, e := url.ParseRequestURI(base); e != nil || !strings.HasPrefix(base, "http") {
			return nil, fmt.Errorf("valid Jira base URL is required")
		}
		if email == "" || token == "" {
			return nil, fmt.Errorf("email and API token are required")
		}
		id := stringValue(p, "id")
		if id == "" {
			id = randomID()
		}
		a := Account{ID: id, Name: stringValue(p, "name"), BaseURL: base, Email: email, ProjectKeys: keysValue(p["projectKeys"])}
		if a.Name == "" {
			a.Name = base
		}
		if e := rt.SecretPut(ctx, "account/"+id+"/api-token", token); e != nil {
			return nil, e
		}
		c := jiraClient{account: a, token: token, http: &http.Client{Timeout: 30 * time.Second}}
		a.ExternalID, a.DisplayName, e = c.myself(ctx)
		a.Connected = e == nil
		if e != nil {
			a.LastError = e.Error()
			_ = repo.put(a)
			return nil, e
		}
		if e = repo.put(a); e != nil {
			return nil, e
		}
		_ = rt.Publish(ctx, "jira.account.configured", map[string]any{"accountId": id, "baseUrl": base})
		return repo.snapshot(), nil
	})
	rt.Capability("jira.account.disconnect", func(ctx context.Context, p map[string]any) (any, error) {
		id := stringValue(p, "id")
		if id == "" {
			return nil, fmt.Errorf("account id is required")
		}
		a, _ := repo.get(id)
		if a.AuthType == "oauth" || a.AuthType == "rovo-mcp" {
			_ = rt.SecretDelete(ctx, "account/"+id+"/access-token")
			_ = rt.SecretDelete(ctx, "account/"+id+"/refresh-token")
			_ = rt.SecretDelete(ctx, "account/"+id+"/client-id")
			_ = rt.SecretDelete(ctx, "account/"+id+"/client-secret")
			_ = rt.SecretDelete(ctx, "account/"+id+"/token-endpoint")
		} else if e := rt.SecretDelete(ctx, "account/"+id+"/api-token"); e != nil {
			return nil, e
		}
		if e := repo.remove(id); e != nil {
			return nil, e
		}
		return repo.snapshot(), nil
	})
	rt.Capability("jira.projects", func(ctx context.Context, p map[string]any) (any, error) {
		a, ok := repo.get(stringValue(p, "accountId"))
		if !ok {
			return nil, fmt.Errorf("Jira account not found")
		}
		c, e := clientFor(ctx, a)
		if e != nil {
			return nil, e
		}
		if a.AuthType == "rovo-mcp" {
			return mcpProjects(ctx, c.token, a.ExternalID)
		}
		return c.projects(ctx)
	})
	rt.Capability("jira.projects.select", func(_ context.Context, p map[string]any) (any, error) {
		a, ok := repo.get(stringValue(p, "accountId"))
		if !ok {
			return nil, fmt.Errorf("Jira account not found")
		}
		a.ProjectKeys = keysValue(p["projectKeys"])
		if e := repo.put(a); e != nil {
			return nil, e
		}
		return repo.snapshot(), nil
	})
	rt.Capability("jira.sync", func(ctx context.Context, p map[string]any) (any, error) {
		id := stringValue(p, "accountId")
		accounts := repo.snapshot().Accounts
		synced := 0
		for _, a := range accounts {
			if id != "" && a.ID != id {
				continue
			}
			c, err := clientFor(ctx, a)
			if err == nil {
				for _, key := range a.ProjectKeys {
					var project map[string]any
					if a.AuthType == "rovo-mcp" {
						project, err = mcpIssues(ctx, c.token, a, key)
					} else {
						project, err = c.issues(ctx, key)
					}
					if err != nil {
						break
					}
					_, err = rt.Invoke(ctx, "projectview.import", project)
					if err != nil {
						break
					}
					synced++
				}
			}
			a.Connected = err == nil
			a.LastError = ""
			if err != nil {
				a.LastError = err.Error()
			} else {
				a.LastSync = time.Now().UTC().Format(time.RFC3339)
			}
			_ = repo.put(a)
			if err != nil {
				return nil, err
			}
		}
		_ = rt.Publish(ctx, "jira.sync.completed", map[string]any{"projects": synced})
		return map[string]any{"projects": synced, "state": repo.snapshot()}, nil
	})
	if e := rt.Serve(context.Background()); e != nil {
		log.Fatal(e)
	}
}

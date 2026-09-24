package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	sdk "teamwork/sdk/go/runtime"
)

const graphBase = "https://graph.microsoft.com/v1.0"

type Config struct {
	ClientID string `json:"clientId"`
	Tenant   string `json:"tenant"`
}
type Account struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	ExternalID  string `json:"externalId"`
	RefreshRef  string `json:"refreshRef"`
	SectionID   string `json:"sectionId,omitempty"`
	SectionName string `json:"sectionName,omitempty"`
}
type State struct {
	Config    Config                      `json:"config"`
	Accounts  []Account                   `json:"accounts"`
	PageCache map[string][]map[string]any `json:"pageCache,omitempty"`
}
type Flow struct {
	ID, Status, Error, AuthorizationURL, AccountID string
}
type repository struct {
	mu    sync.RWMutex
	path  string
	state State
	flows map[string]*Flow
}

func (r *repository) updateFlow(id, status, message, accountID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if flow := r.flows[id]; flow != nil {
		flow.Status = status
		flow.Error = message
		flow.AccountID = accountID
	}
}

func (r *repository) flow(id string) (Flow, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	flow := r.flows[id]
	if flow == nil {
		return Flow{}, false
	}
	return *flow, true
}

func openRepository(path string) (*repository, error) {
	r := &repository{path: path, state: State{Config: Config{Tenant: "common"}, Accounts: []Account{}, PageCache: map[string][]map[string]any{}}, flows: map[string]*Flow{}}
	data, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(data, &r.state)
	} else if os.IsNotExist(err) {
		err = nil
	}
	if r.state.Config.Tenant == "" {
		r.state.Config.Tenant = "common"
	}
	if r.state.PageCache == nil {
		r.state.PageCache = map[string][]map[string]any{}
	}
	return r, err
}
func (r *repository) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r.state, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0600); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}
func randomString(size int) string {
	b := make([]byte, size)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
func challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
func decodeMap(value any, target any) error {
	b, _ := json.Marshal(value)
	return json.Unmarshal(b, target)
}
func tokenEndpoint(tenant string) string {
	return "https://login.microsoftonline.com/" + url.PathEscape(tenant) + "/oauth2/v2.0/token"
}
func loopbackRedirect(port int) string {
	return fmt.Sprintf("http://localhost:%d", port)
}
func exchange(ctx context.Context, values url.Values) (map[string]any, error) {
	endpoint := values.Get("token_endpoint")
	values.Del("token_endpoint")
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("Microsoft token exchange failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var result map[string]any
	return result, json.Unmarshal(body, &result)
}
func graph(ctx context.Context, token, method, path, contentType string, body io.Reader, target any) error {
	req, err := http.NewRequestWithContext(ctx, method, graphBase+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("Microsoft Graph failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if target != nil && len(data) > 0 {
		return json.Unmarshal(data, target)
	}
	return nil
}
func (r *repository) account(id string) (Account, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, a := range r.state.Accounts {
		if a.ID == id {
			return a, true
		}
	}
	return Account{}, false
}
func (r *repository) accessToken(ctx context.Context, runtime *sdk.Runtime, account Account) (string, error) {
	refresh, err := runtime.SecretGet(ctx, account.RefreshRef)
	if err != nil {
		return "", err
	}
	r.mu.RLock()
	config := r.state.Config
	r.mu.RUnlock()
	values := url.Values{"token_endpoint": {tokenEndpoint(config.Tenant)}, "client_id": {config.ClientID}, "grant_type": {"refresh_token"}, "refresh_token": {refresh}, "scope": {"offline_access User.Read Notes.ReadWrite"}}
	result, err := exchange(ctx, values)
	if err != nil {
		return "", err
	}
	if rotated := strings.TrimSpace(fmt.Sprint(result["refresh_token"])); rotated != "" {
		_ = runtime.SecretPut(ctx, account.RefreshRef, rotated)
	}
	token := strings.TrimSpace(fmt.Sprint(result["access_token"]))
	if token == "" {
		return "", fmt.Errorf("Microsoft did not return an access token")
	}
	return token, nil
}
func (r *repository) ensureSection(ctx context.Context, runtime *sdk.Runtime, account *Account) (string, error) {
	if account.SectionID != "" {
		return account.SectionID, nil
	}
	token, err := r.accessToken(ctx, runtime, *account)
	if err != nil {
		return "", err
	}
	var sections struct {
		Value []struct{ ID, DisplayName string } `json:"value"`
	}
	if err := graph(ctx, token, http.MethodGet, "/me/onenote/sections?$select=id,displayName&$top=100", "", nil, &sections); err != nil {
		return "", err
	}
	for _, section := range sections.Value {
		if strings.EqualFold(strings.TrimSpace(section.DisplayName), "Operation Notes") {
			account.SectionID, account.SectionName = section.ID, section.DisplayName
			break
		}
	}
	if account.SectionID == "" {
		var notebooks struct {
			Value []struct{ ID string } `json:"value"`
		}
		if err := graph(ctx, token, http.MethodGet, "/me/onenote/notebooks?$select=id&$top=1", "", nil, &notebooks); err != nil || len(notebooks.Value) == 0 {
			if err == nil {
				err = fmt.Errorf("OneNote notebook not found")
			}
			return "", err
		}
		var section struct{ ID, DisplayName string }
		if err := graph(ctx, token, http.MethodPost, "/me/onenote/notebooks/"+url.PathEscape(notebooks.Value[0].ID)+"/sections", "application/json", strings.NewReader(`{"displayName":"Operation Notes"}`), &section); err != nil {
			return "", err
		}
		account.SectionID, account.SectionName = section.ID, section.DisplayName
	}
	r.mu.Lock()
	for i := range r.state.Accounts {
		if r.state.Accounts[i].ID == account.ID {
			r.state.Accounts[i] = *account
		}
	}
	err = r.saveLocked()
	r.mu.Unlock()
	return account.SectionID, err
}

func (r *repository) cachedPages(accountID string) []map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]map[string]any(nil), r.state.PageCache[accountID]...)
}

func (r *repository) fetchPages(ctx context.Context, runtime *sdk.Runtime, account Account) ([]map[string]any, string, error) {
	sectionID, err := r.ensureSection(ctx, runtime, &account)
	if err != nil {
		return nil, "", err
	}
	token, err := r.accessToken(ctx, runtime, account)
	if err != nil {
		return nil, "", err
	}
	var result struct {
		Value []map[string]any `json:"value"`
	}
	err = graph(ctx, token, http.MethodGet, "/me/onenote/sections/"+url.PathEscape(sectionID)+"/pages?$select=id,title,links,lastModifiedDateTime&$top=100", "", nil, &result)
	if err != nil {
		return nil, sectionID, err
	}
	r.mu.Lock()
	r.state.PageCache[account.ID] = result.Value
	err = r.saveLocked()
	r.mu.Unlock()
	return result.Value, sectionID, err
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
	runtime.Capability("onenote.config.get", func(context.Context, map[string]any) (any, error) {
		repo.mu.RLock()
		defer repo.mu.RUnlock()
		return map[string]any{"clientId": repo.state.Config.ClientID, "tenant": repo.state.Config.Tenant, "configured": repo.state.Config.ClientID != ""}, nil
	})
	runtime.Capability("onenote.config.set", func(_ context.Context, payload map[string]any) (any, error) {
		repo.mu.Lock()
		defer repo.mu.Unlock()
		repo.state.Config.ClientID = strings.TrimSpace(fmt.Sprint(payload["clientId"]))
		repo.state.Config.Tenant = strings.TrimSpace(fmt.Sprint(payload["tenant"]))
		if repo.state.Config.Tenant == "" {
			repo.state.Config.Tenant = "common"
		}
		return repo.state.Config, repo.saveLocked()
	})
	runtime.Capability("onenote.account.list", func(context.Context, map[string]any) (any, error) {
		repo.mu.RLock()
		defer repo.mu.RUnlock()
		return map[string]any{"accounts": repo.state.Accounts}, nil
	})
	runtime.Capability("onenote.oauth.start", func(ctx context.Context, _ map[string]any) (any, error) {
		repo.mu.RLock()
		config := repo.state.Config
		repo.mu.RUnlock()
		if config.ClientID == "" {
			return nil, fmt.Errorf("Microsoft OAuth client ID is not configured")
		}
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		flowID, state, verifier := randomString(12), randomString(24), randomString(48)
		port := listener.Addr().(*net.TCPAddr).Port
		redirect := loopbackRedirect(port)
		query := url.Values{"client_id": {config.ClientID}, "response_type": {"code"}, "redirect_uri": {redirect}, "response_mode": {"query"}, "scope": {"offline_access User.Read Notes.ReadWrite"}, "state": {state}, "code_challenge": {challenge(verifier)}, "code_challenge_method": {"S256"}}
		authURL := "https://login.microsoftonline.com/" + url.PathEscape(config.Tenant) + "/oauth2/v2.0/authorize?" + query.Encode()
		flow := &Flow{ID: flowID, Status: "pending", AuthorizationURL: authURL}
		repo.mu.Lock()
		repo.flows[flowID] = flow
		repo.mu.Unlock()
		server := &http.Server{ReadHeaderTimeout: 5 * time.Second}
		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
			if req.URL.Query().Get("state") != state || req.URL.Query().Get("code") == "" {
				message := "OAuth state or code is invalid"
				repo.updateFlow(flowID, "failed", message, "")
				http.Error(w, message, http.StatusBadRequest)
				return
			}
			values := url.Values{"token_endpoint": {tokenEndpoint(config.Tenant)}, "client_id": {config.ClientID}, "grant_type": {"authorization_code"}, "code": {req.URL.Query().Get("code")}, "redirect_uri": {redirect}, "code_verifier": {verifier}, "scope": {"offline_access User.Read Notes.ReadWrite"}}
			result, exchangeErr := exchange(req.Context(), values)
			if exchangeErr != nil {
				repo.updateFlow(flowID, "failed", exchangeErr.Error(), "")
				http.Error(w, exchangeErr.Error(), http.StatusBadGateway)
				return
			}
			access, refresh := fmt.Sprint(result["access_token"]), fmt.Sprint(result["refresh_token"])
			var me struct{ ID, DisplayName, Mail, UserPrincipalName string }
			if exchangeErr = graph(req.Context(), access, http.MethodGet, "/me?$select=id,displayName,mail,userPrincipalName", "", nil, &me); exchangeErr != nil {
				repo.updateFlow(flowID, "failed", exchangeErr.Error(), "")
				http.Error(w, exchangeErr.Error(), http.StatusBadGateway)
				return
			}
			accountID, ref := "microsoft-"+me.ID, "account-"+me.ID+"-refresh"
			if exchangeErr = runtime.SecretPut(req.Context(), ref, refresh); exchangeErr != nil {
				repo.updateFlow(flowID, "failed", exchangeErr.Error(), "")
				http.Error(w, exchangeErr.Error(), http.StatusInternalServerError)
				return
			}
			email := me.Mail
			if email == "" {
				email = me.UserPrincipalName
			}
			account := Account{ID: accountID, Name: me.DisplayName, Email: email, ExternalID: me.ID, RefreshRef: ref}
			repo.mu.Lock()
			replaced := false
			for i := range repo.state.Accounts {
				if repo.state.Accounts[i].ID == accountID {
					repo.state.Accounts[i] = account
					replaced = true
				}
			}
			if !replaced {
				repo.state.Accounts = append(repo.state.Accounts, account)
			}
			exchangeErr = repo.saveLocked()
			repo.mu.Unlock()
			if exchangeErr != nil {
				repo.updateFlow(flowID, "failed", exchangeErr.Error(), "")
				http.Error(w, exchangeErr.Error(), http.StatusInternalServerError)
				return
			}
			repo.updateFlow(flowID, "completed", "", accountID)
			fmt.Fprint(w, "<!doctype html><meta charset=utf-8><title>TeamWork</title><h1>OneNote connected</h1><p>You may close this window.</p>")
			go server.Shutdown(context.Background())
		})
		server.Handler = mux
		go func() {
			if serveErr := server.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
				repo.updateFlow(flowID, "failed", serveErr.Error(), "")
			}
		}()
		go func() {
			<-time.After(10 * time.Minute)
			_ = server.Shutdown(context.Background())
			if current, ok := repo.flow(flowID); ok && current.Status == "pending" {
				repo.updateFlow(flowID, "failed", "OAuth flow expired", "")
			}
		}()
		_ = runtime.Log(ctx, "info", "OneNote browser authorization requested", map[string]any{"flowId": flowID})
		return map[string]any{"flowId": flowID, "authorizationUrl": authURL}, nil
	})
	runtime.Capability("onenote.oauth.status", func(_ context.Context, payload map[string]any) (any, error) {
		flow, ok := repo.flow(fmt.Sprint(payload["flowId"]))
		if !ok {
			return nil, fmt.Errorf("OAuth flow not found")
		}
		return map[string]any{"status": flow.Status, "error": flow.Error, "accountId": flow.AccountID}, nil
	})
	runtime.Capability("onenote.account.disconnect", func(ctx context.Context, payload map[string]any) (any, error) {
		id := fmt.Sprint(payload["id"])
		repo.mu.Lock()
		defer repo.mu.Unlock()
		out := repo.state.Accounts[:0]
		for _, account := range repo.state.Accounts {
			if account.ID == id {
				_ = runtime.SecretDelete(ctx, account.RefreshRef)
				continue
			}
			out = append(out, account)
		}
		repo.state.Accounts = out
		delete(repo.state.PageCache, id)
		return map[string]bool{"disconnected": true}, repo.saveLocked()
	})
	runtime.Capability("onenote.pages.cached", func(_ context.Context, payload map[string]any) (any, error) {
		accountID := fmt.Sprint(payload["accountId"])
		if _, ok := repo.account(accountID); !ok {
			return nil, fmt.Errorf("OneNote account not found")
		}
		return map[string]any{"sectionName": "Operation Notes", "pages": repo.cachedPages(accountID)}, nil
	})
	runtime.Capability("onenote.pages.list", func(ctx context.Context, payload map[string]any) (any, error) {
		account, ok := repo.account(fmt.Sprint(payload["accountId"]))
		if !ok {
			return nil, fmt.Errorf("OneNote account not found")
		}
		pages, sectionID, err := repo.fetchPages(ctx, runtime, account)
		return map[string]any{"sectionId": sectionID, "sectionName": "Operation Notes", "pages": pages}, err
	})
	runtime.Capability("onenote.page.save", func(ctx context.Context, payload map[string]any) (any, error) {
		account, ok := repo.account(fmt.Sprint(payload["accountId"]))
		if !ok {
			return nil, fmt.Errorf("OneNote account not found")
		}
		sectionID, err := repo.ensureSection(ctx, runtime, &account)
		if err != nil {
			return nil, err
		}
		token, err := repo.accessToken(ctx, runtime, account)
		if err != nil {
			return nil, err
		}
		mode, pageID, title, content := fmt.Sprint(payload["mode"]), fmt.Sprint(payload["pageId"]), strings.TrimSpace(fmt.Sprint(payload["title"])), fmt.Sprint(payload["html"])
		var page map[string]any
		if mode == "new" {
			if title == "" {
				return nil, fmt.Errorf("page title is required")
			}
			pageHTML := "<!doctype html><html><head><title>" + html.EscapeString(title) + "</title><meta name=created content=\"" + time.Now().UTC().Format(time.RFC3339) + "\"></head><body>" + content + "</body></html>"
			err = graph(ctx, token, http.MethodPost, "/me/onenote/sections/"+url.PathEscape(sectionID)+"/pages", "text/html; charset=utf-8", strings.NewReader(pageHTML), &page)
		} else {
			if pageID == "" {
				return nil, fmt.Errorf("existing page is required")
			}
			commands, _ := json.Marshal([]map[string]any{{"target": "body", "action": "append", "content": content}})
			err = graph(ctx, token, http.MethodPatch, "/me/onenote/pages/"+url.PathEscape(pageID)+"/content", "application/json", strings.NewReader(string(commands)), nil)
			page = map[string]any{"id": pageID, "title": title}
		}
		if err == nil {
			_ = runtime.Log(ctx, "info", "Slack content saved to OneNote", map[string]any{"pageId": page["id"], "mode": mode})
		}
		return page, err
	})
	go func() {
		<-runtime.Ready()
		repo.mu.RLock()
		accounts := append([]Account(nil), repo.state.Accounts...)
		repo.mu.RUnlock()
		for _, account := range accounts {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_, _, refreshErr := repo.fetchPages(ctx, runtime, account)
			cancel()
			if refreshErr != nil {
				logCtx, logCancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = runtime.Log(logCtx, "warn", "OneNote page cache refresh failed", map[string]any{"accountId": account.ID, "error": refreshErr.Error()})
				logCancel()
			}
		}
	}()
	if err := runtime.Serve(context.Background()); err != nil {
		log.Fatal(err)
	}
}

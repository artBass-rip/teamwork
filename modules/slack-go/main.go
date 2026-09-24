package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	sdk "teamwork/sdk/go/runtime"
)

const slackAPI = "https://slack.com/api/"

type Account struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	TeamID   string `json:"teamId"`
	TeamName string `json:"teamName"`
	BotRef   string `json:"botRef"`
	AppRef   string `json:"appRef"`
}
type State struct {
	Accounts []Account `json:"accounts"`
}
type repository struct {
	mu      sync.RWMutex
	path    string
	state   State
	cancels map[string]context.CancelFunc
}

func openRepository(path string) (*repository, error) {
	r := &repository{path: path, state: State{Accounts: []Account{}}, cancels: map[string]context.CancelFunc{}}
	data, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(data, &r.state)
	} else if os.IsNotExist(err) {
		err = nil
	}
	return r, err
}
func (r *repository) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r.state, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err = os.WriteFile(tmp, append(b, '\n'), 0600); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
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
func slackCall(ctx context.Context, token, method string, values url.Values, target any) error {
	if values == nil {
		values = url.Values{}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, slackAPI+method, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("Slack %s failed (%d): %s", method, resp.StatusCode, string(data))
	}
	var envelope map[string]any
	if err = json.Unmarshal(data, &envelope); err != nil {
		return err
	}
	if ok, _ := envelope["ok"].(bool); !ok {
		return fmt.Errorf("Slack %s: %v", method, envelope["error"])
	}
	if target != nil {
		b, _ := json.Marshal(envelope)
		return json.Unmarshal(b, target)
	}
	return nil
}
func secretTokens(ctx context.Context, runtime *sdk.Runtime, a Account) (string, string, error) {
	bot, e := runtime.SecretGet(ctx, a.BotRef)
	if e != nil {
		return "", "", e
	}
	app, e := runtime.SecretGet(ctx, a.AppRef)
	return bot, app, e
}
func socketURL(ctx context.Context, appToken string) (string, error) {
	var out struct {
		URL string `json:"url"`
	}
	if err := slackCall(ctx, appToken, "apps.connections.open", nil, &out); err != nil {
		return "", err
	}
	return out.URL, nil
}
func mapValue(m map[string]any, path ...string) any {
	var v any = m
	for _, key := range path {
		next, _ := v.(map[string]any)
		v = next[key]
	}
	return v
}
func stringValue(v any) string { return strings.TrimSpace(fmt.Sprint(v)) }
func viewValues(payload map[string]any) map[string]string {
	out := map[string]string{}
	values, _ := mapValue(payload, "view", "state", "values").(map[string]any)
	for block, raw := range values {
		actions, _ := raw.(map[string]any)
		for action, value := range actions {
			m, _ := value.(map[string]any)
			selected, _ := m["selected_option"].(map[string]any)
			if selected != nil {
				out[block+"."+action] = stringValue(selected["value"])
			} else {
				out[block+"."+action] = stringValue(m["value"])
			}
		}
	}
	return out
}
func (r *repository) startSocket(parent context.Context, runtime *sdk.Runtime, a Account) {
	r.mu.Lock()
	if cancel := r.cancels[a.ID]; cancel != nil {
		cancel()
	}
	ctx, cancel := context.WithCancel(parent)
	r.cancels[a.ID] = cancel
	r.mu.Unlock()
	go func() {
		for ctx.Err() == nil {
			bot, app, err := secretTokens(ctx, runtime, a)
			if err != nil {
				_ = runtime.Log(ctx, "error", "Slack credentials unavailable", map[string]any{"accountId": a.ID, "error": err.Error()})
				return
			}
			endpoint, err := socketURL(ctx, app)
			if err != nil {
				_ = runtime.Log(ctx, "error", "Slack Socket Mode connection failed", map[string]any{"accountId": a.ID, "error": err.Error()})
				select {
				case <-ctx.Done():
					return
				case <-time.After(5 * time.Second):
					continue
				}
			}
			conn, _, err := websocket.DefaultDialer.DialContext(ctx, endpoint, nil)
			if err != nil {
				select {
				case <-ctx.Done():
					return
				case <-time.After(5 * time.Second):
					continue
				}
			}
			_ = runtime.Log(ctx, "info", "Slack Socket Mode connected", map[string]any{"accountId": a.ID, "workspace": a.TeamName})
			for ctx.Err() == nil {
				var envelope map[string]any
				if err = conn.ReadJSON(&envelope); err != nil {
					break
				}
				envelopeID := stringValue(envelope["envelope_id"])
				if envelopeID != "" {
					_ = conn.WriteJSON(map[string]any{"envelope_id": envelopeID})
				}
				if stringValue(envelope["type"]) != "interactive" {
					continue
				}
				payload, _ := envelope["payload"].(map[string]any)
				interactionType := stringValue(payload["type"])
				_ = runtime.Log(ctx, "info", "Slack interaction received", map[string]any{"accountId": a.ID, "type": interactionType, "callbackId": stringValue(payload["callback_id"])})
				switch interactionType {
				case "message_action":
					if stringValue(payload["callback_id"]) != "teamwork_save_onenote" {
						continue
					}
					prepared, callErr := runtime.Invoke(ctx, "workflow.slack-onenote.modal.prepare", map[string]any{"slackAccountId": a.ID, "interaction": payload})
					if callErr != nil {
						_ = runtime.Log(ctx, "error", "OneNote modal preparation failed", map[string]any{"error": callErr.Error()})
						continue
					}
					viewMap, _ := prepared.(map[string]any)
					viewJSON, _ := json.Marshal(viewMap["view"])
					if openErr := slackCall(ctx, bot, "views.open", url.Values{"trigger_id": {stringValue(payload["trigger_id"])}, "view": {string(viewJSON)}}, nil); openErr != nil {
						_ = runtime.Log(ctx, "error", "Slack OneNote modal opening failed", map[string]any{"accountId": a.ID, "error": openErr.Error()})
					} else {
						_ = runtime.Log(ctx, "info", "Slack OneNote modal opened", map[string]any{"accountId": a.ID})
					}
				case "view_submission":
					if stringValue(mapValue(payload, "view", "callback_id")) != "teamwork_slack_onenote" {
						continue
					}
					_ = runtime.Publish(ctx, "slack.onenote.save-submitted", map[string]any{"slackAccountId": a.ID, "privateMetadata": stringValue(mapValue(payload, "view", "private_metadata")), "values": viewValues(payload), "userId": stringValue(mapValue(payload, "user", "id"))})
				}
			}
			conn.Close()
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
	}()
}
func main() {
	data := os.Getenv("TEAMWORK_MODULE_DATA")
	if data == "" {
		log.Fatal("TEAMWORK_MODULE_DATA is required")
	}
	repo, err := openRepository(filepath.Join(data, "state.json"))
	if err != nil {
		log.Fatal(err)
	}
	runtime := sdk.New()
	rootCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime.Capability("slack.account.list", func(context.Context, map[string]any) (any, error) {
		repo.mu.RLock()
		defer repo.mu.RUnlock()
		return map[string]any{"accounts": repo.state.Accounts}, nil
	})
	runtime.Capability("slack.account.configure", func(ctx context.Context, p map[string]any) (any, error) {
		bot, app := stringValue(p["botToken"]), stringValue(p["appToken"])
		if bot == "" || app == "" {
			return nil, fmt.Errorf("bot token and app token are required")
		}
		var auth struct {
			TeamID string `json:"team_id"`
			Team   string `json:"team"`
			URL    string `json:"url"`
		}
		if err := slackCall(ctx, bot, "auth.test", nil, &auth); err != nil {
			return nil, err
		}
		id := "slack-" + auth.TeamID
		account := Account{ID: id, Name: stringValue(p["accountName"]), TeamID: auth.TeamID, TeamName: auth.Team, BotRef: "account-" + auth.TeamID + "-bot", AppRef: "account-" + auth.TeamID + "-app"}
		if account.Name == "" {
			account.Name = auth.Team
		}
		if err := runtime.SecretPut(ctx, account.BotRef, bot); err != nil {
			return nil, err
		}
		if err := runtime.SecretPut(ctx, account.AppRef, app); err != nil {
			return nil, err
		}
		repo.mu.Lock()
		found := false
		for i := range repo.state.Accounts {
			if repo.state.Accounts[i].ID == id {
				repo.state.Accounts[i] = account
				found = true
			}
		}
		if !found {
			repo.state.Accounts = append(repo.state.Accounts, account)
		}
		sort.Slice(repo.state.Accounts, func(i, j int) bool { return repo.state.Accounts[i].Name < repo.state.Accounts[j].Name })
		err := repo.saveLocked()
		repo.mu.Unlock()
		if err == nil {
			repo.startSocket(rootCtx, runtime, account)
		}
		return account, err
	})
	runtime.Capability("slack.account.disconnect", func(ctx context.Context, p map[string]any) (any, error) {
		id := stringValue(p["id"])
		repo.mu.Lock()
		out := repo.state.Accounts[:0]
		for _, a := range repo.state.Accounts {
			if a.ID == id {
				_ = runtime.SecretDelete(ctx, a.BotRef)
				_ = runtime.SecretDelete(ctx, a.AppRef)
				if c := repo.cancels[id]; c != nil {
					c()
				}
				continue
			}
			out = append(out, a)
		}
		repo.state.Accounts = out
		err := repo.saveLocked()
		repo.mu.Unlock()
		return map[string]bool{"disconnected": true}, err
	})
	runtime.Capability("slack.content.get", func(ctx context.Context, p map[string]any) (any, error) {
		a, ok := repo.account(stringValue(p["accountId"]))
		if !ok {
			return nil, fmt.Errorf("Slack account not found")
		}
		bot, _, err := secretTokens(ctx, runtime, a)
		if err != nil {
			return nil, err
		}
		channel, ts, mode := stringValue(p["channelId"]), stringValue(p["messageTs"]), stringValue(p["mode"])
		messages := []map[string]any{}
		if mode == "thread" {
			root := stringValue(p["threadTs"])
			if root == "" {
				root = ts
			}
			cursor := ""
			for {
				var replies struct {
					Messages         []map[string]any `json:"messages"`
					ResponseMetadata struct {
						NextCursor string `json:"next_cursor"`
					} `json:"response_metadata"`
				}
				values := url.Values{"channel": {channel}, "ts": {root}, "limit": {"100"}}
				if cursor != "" {
					values.Set("cursor", cursor)
				}
				err = slackCall(ctx, bot, "conversations.replies", values, &replies)
				if err != nil {
					break
				}
				messages = append(messages, replies.Messages...)
				cursor = strings.TrimSpace(replies.ResponseMetadata.NextCursor)
				if cursor == "" {
					break
				}
			}
		} else {
			if snapshot, ok := p["message"].(map[string]any); ok {
				messages = []map[string]any{snapshot}
			}
		}
		if err != nil {
			return nil, err
		}
		var permalink struct {
			Permalink string `json:"permalink"`
		}
		_ = slackCall(ctx, bot, "chat.getPermalink", url.Values{"channel": {channel}, "message_ts": {ts}}, &permalink)
		users := map[string]string{}
		for _, m := range messages {
			uid := stringValue(m["user"])
			if uid == "" || users[uid] != "" {
				continue
			}
			var info struct {
				User struct {
					RealName string `json:"real_name"`
					Name     string `json:"name"`
				} `json:"user"`
			}
			if slackCall(ctx, bot, "users.info", url.Values{"user": {uid}}, &info) == nil {
				users[uid] = info.User.RealName
				if users[uid] == "" {
					users[uid] = info.User.Name
				}
			}
		}
		return map[string]any{"workspace": a.TeamName, "channelId": channel, "messages": messages, "users": users, "permalink": permalink.Permalink}, nil
	})
	runtime.Capability("slack.notify.ephemeral", func(ctx context.Context, p map[string]any) (any, error) {
		a, ok := repo.account(stringValue(p["accountId"]))
		if !ok {
			return nil, fmt.Errorf("Slack account not found")
		}
		bot, _, err := secretTokens(ctx, runtime, a)
		if err != nil {
			return nil, err
		}
		err = slackCall(ctx, bot, "chat.postEphemeral", url.Values{"channel": {stringValue(p["channelId"])}, "user": {stringValue(p["userId"])}, "text": {stringValue(p["text"])}}, nil)
		return map[string]bool{"sent": err == nil}, err
	})
	serveErr := make(chan error, 1)
	go func() { serveErr <- runtime.Serve(rootCtx) }()
	go func() {
		time.Sleep(750 * time.Millisecond)
		repo.mu.RLock()
		accounts := append([]Account(nil), repo.state.Accounts...)
		repo.mu.RUnlock()
		for _, a := range accounts {
			repo.startSocket(rootCtx, runtime, a)
		}
	}()
	if err := <-serveErr; err != nil {
		log.Fatal(err)
	}
}

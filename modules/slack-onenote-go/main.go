package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	sdk "teamwork/sdk/go/runtime"
)

type Link struct {
	ID, SlackAccountID, ChannelID, MessageTS, ContentMode, OneNoteAccountID, PageID, PageTitle, CreatedAt string
}
type State struct {
	Links []Link `json:"links"`
}
type repository struct {
	mu    sync.RWMutex
	path  string
	state State
}

func openRepository(path string) (*repository, error) {
	r := &repository{path: path, state: State{Links: []Link{}}}
	b, e := os.ReadFile(path)
	if e == nil {
		e = json.Unmarshal(b, &r.state)
	} else if os.IsNotExist(e) {
		e = nil
	}
	return r, e
}
func (r *repository) saveLocked() error {
	if e := os.MkdirAll(filepath.Dir(r.path), 0700); e != nil {
		return e
	}
	b, e := json.MarshalIndent(r.state, "", "  ")
	if e != nil {
		return e
	}
	tmp := r.path + ".tmp"
	if e = os.WriteFile(tmp, append(b, '\n'), 0600); e != nil {
		return e
	}
	return os.Rename(tmp, r.path)
}
func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func asList(v any) []any         { a, _ := v.([]any); return a }
func text(v any) string          { return strings.TrimSpace(fmt.Sprint(v)) }
func nested(m map[string]any, path ...string) any {
	var v any = m
	for _, k := range path {
		next, _ := v.(map[string]any)
		v = next[k]
	}
	return v
}
func option(label, value string) map[string]any {
	return map[string]any{"text": map[string]any{"type": "plain_text", "text": label}, "value": value}
}
func firstLine(value string) string {
	value = strings.TrimSpace(value)
	if i := strings.IndexByte(value, '\n'); i >= 0 {
		value = value[:i]
	}
	r := []rune(value)
	if len(r) > 80 {
		value = string(r[:80])
	}
	if value == "" {
		return "Slack note"
	}
	return value
}
func webURL(page map[string]any) string { return text(nested(page, "links", "oneNoteWebUrl", "href")) }
func renderContent(source map[string]any, mode string) string {
	var b strings.Builder
	b.WriteString(`<section data-teamwork-source="slack"><h2>Slack ` + html.EscapeString(mode) + `</h2><p><strong>Workspace:</strong> ` + html.EscapeString(text(source["workspace"])) + `<br><strong>Saved:</strong> ` + time.Now().Format("2006-01-02 15:04") + `<br>`)
	if link := text(source["permalink"]); link != "" {
		b.WriteString(`<a href="` + html.EscapeString(link) + `">Open in Slack</a>`)
	}
	b.WriteString(`</p>`)
	users := asMap(source["users"])
	for _, raw := range asList(source["messages"]) {
		message := asMap(raw)
		author := text(users[text(message["user"])])
		if author == "" {
			author = text(message["user"])
		}
		b.WriteString(`<p><strong>` + html.EscapeString(author) + `</strong><br>` + strings.ReplaceAll(html.EscapeString(text(message["text"])), "\n", "<br>") + `</p>`)
	}
	b.WriteString(`<hr></section>`)
	return b.String()
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
	runtime.Capability("workflow.slack-onenote.modal.prepare", func(ctx context.Context, p map[string]any) (any, error) {
		interaction := asMap(p["interaction"])
		accountsValue, err := runtime.Invoke(ctx, "onenote.account.list", map[string]any{})
		if err != nil {
			return nil, err
		}
		accounts := asList(asMap(accountsValue)["accounts"])
		if len(accounts) == 0 {
			return nil, fmt.Errorf("connect OneNote before using the Slack shortcut")
		}
		accountOptions := []map[string]any{}
		pageOptions := []map[string]any{}
		for _, raw := range accounts {
			a := asMap(raw)
			id := text(a["id"])
			accountOptions = append(accountOptions, option(text(a["name"]), id))
			pagesValue, pageErr := runtime.Invoke(ctx, "onenote.pages.cached", map[string]any{"accountId": id})
			if pageErr == nil {
				for _, pageRaw := range asList(asMap(pagesValue)["pages"]) {
					if len(pageOptions) >= 100 {
						break
					}
					page := asMap(pageRaw)
					pageOptions = append(pageOptions, option(text(page["title"]), id+"|"+text(page["id"])))
				}
			}
		}
		hasExistingPages := len(pageOptions) > 0
		if len(pageOptions) == 0 {
			pageOptions = append(pageOptions, option("No existing pages — create a new page", "__none__"))
		}
		message := asMap(interaction["message"])
		metadata, _ := json.Marshal(map[string]any{"slackAccountId": p["slackAccountId"], "channelId": nested(interaction, "channel", "id"), "messageTs": message["ts"], "threadTs": message["thread_ts"], "message": message, "userId": nested(interaction, "user", "id")})
		title := firstLine(text(message["text"]))
		destinationInitial := option("Create a new page", "new")
		if hasExistingPages {
			destinationInitial = option("Existing page", "existing")
		}
		pageSelect := map[string]any{"type": "static_select", "action_id": "id", "options": pageOptions}
		if hasExistingPages {
			pageSelect["initial_option"] = pageOptions[0]
		}
		view := map[string]any{"type": "modal", "callback_id": "teamwork_slack_onenote", "private_metadata": string(metadata), "title": map[string]any{"type": "plain_text", "text": "Save to OneNote"}, "submit": map[string]any{"type": "plain_text", "text": "Save"}, "close": map[string]any{"type": "plain_text", "text": "Cancel"}, "blocks": []any{
			map[string]any{"type": "input", "block_id": "content", "label": map[string]any{"type": "plain_text", "text": "What to save"}, "element": map[string]any{"type": "static_select", "action_id": "mode", "initial_option": option("Only this message", "message"), "options": []any{option("Only this message", "message"), option("Entire thread", "thread")}}},
			map[string]any{"type": "input", "block_id": "destination", "label": map[string]any{"type": "plain_text", "text": "Destination"}, "element": map[string]any{"type": "static_select", "action_id": "mode", "initial_option": destinationInitial, "options": []any{option("Existing page", "existing"), option("Create a new page", "new")}}},
			map[string]any{"type": "input", "block_id": "account", "label": map[string]any{"type": "plain_text", "text": "OneNote account"}, "element": map[string]any{"type": "static_select", "action_id": "id", "initial_option": accountOptions[0], "options": accountOptions}},
			map[string]any{"type": "input", "block_id": "page", "optional": true, "label": map[string]any{"type": "plain_text", "text": "Existing Operation Notes page"}, "element": pageSelect},
			map[string]any{"type": "input", "block_id": "new_page", "optional": true, "label": map[string]any{"type": "plain_text", "text": "New page title"}, "element": map[string]any{"type": "plain_text_input", "action_id": "title", "initial_value": title}},
		}}
		return map[string]any{"view": view}, nil
	})
	runtime.Capability("workflow.slack-onenote.links", func(context.Context, map[string]any) (any, error) {
		repo.mu.RLock()
		defer repo.mu.RUnlock()
		return map[string]any{"links": repo.state.Links}, nil
	})
	runtime.On("slack.onenote.save-submitted", func(ctx context.Context, event map[string]any) error {
		eventID := text(event["id"])
		repo.mu.RLock()
		for _, existing := range repo.state.Links {
			if existing.ID == eventID {
				repo.mu.RUnlock()
				return nil
			}
		}
		repo.mu.RUnlock()
		payload := asMap(event["payload"])
		values := asMap(payload["values"])
		var meta map[string]any
		if err := json.Unmarshal([]byte(text(payload["privateMetadata"])), &meta); err != nil {
			return err
		}
		notifyFailure := func(message string) {
			_, _ = runtime.Invoke(ctx, "slack.notify.ephemeral", map[string]any{"accountId": meta["slackAccountId"], "channelId": meta["channelId"], "userId": payload["userId"], "text": "TeamWork could not save this item to OneNote: " + message})
			_ = runtime.Log(ctx, "error", "Slack content could not be saved to OneNote", map[string]any{"messageTs": meta["messageTs"], "error": message})
		}
		contentMode, destination := text(values["content.mode"]), text(values["destination.mode"])
		accountID := text(values["account.id"])
		selected := text(values["page.id"])
		pageID := ""
		if parts := strings.SplitN(selected, "|", 2); len(parts) == 2 {
			if destination == "existing" {
				accountID = parts[0]
			}
			pageID = parts[1]
		}
		title := text(values["new_page.title"])
		if destination == "existing" && pageID == "" {
			message := "existing OneNote page was not selected"
			notifyFailure(message)
			return fmt.Errorf("%s", message)
		}
		if destination == "new" && title == "" {
			message := "new OneNote page title is required"
			notifyFailure(message)
			return fmt.Errorf("%s", message)
		}
		sourceValue, err := runtime.Invoke(ctx, "slack.content.get", map[string]any{"accountId": meta["slackAccountId"], "channelId": meta["channelId"], "messageTs": meta["messageTs"], "threadTs": meta["threadTs"], "message": meta["message"], "mode": contentMode})
		if err != nil {
			notifyFailure(err.Error())
			return err
		}
		source := asMap(sourceValue)
		savedValue, err := runtime.Invoke(ctx, "onenote.page.save", map[string]any{"accountId": accountID, "mode": map[string]string{"existing": "existing", "new": "new"}[destination], "pageId": pageID, "title": title, "html": renderContent(source, contentMode)})
		if err != nil {
			notifyFailure(err.Error())
			return err
		}
		saved := asMap(savedValue)
		if text(saved["id"]) != "" {
			pageID = text(saved["id"])
		}
		if text(saved["title"]) != "" {
			title = text(saved["title"])
		}
		if eventID == "" {
			eventID = fmt.Sprintf("%d", time.Now().UnixNano())
		}
		link := Link{ID: eventID, SlackAccountID: text(meta["slackAccountId"]), ChannelID: text(meta["channelId"]), MessageTS: text(meta["messageTs"]), ContentMode: contentMode, OneNoteAccountID: accountID, PageID: pageID, PageTitle: title, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
		repo.mu.Lock()
		repo.state.Links = append(repo.state.Links, link)
		sort.Slice(repo.state.Links, func(i, j int) bool { return repo.state.Links[i].CreatedAt > repo.state.Links[j].CreatedAt })
		err = repo.saveLocked()
		repo.mu.Unlock()
		confirmation := "Saved Slack " + contentMode + " to OneNote"
		if title != "" {
			confirmation += " → " + title
		}
		if u := webURL(saved); u != "" {
			confirmation += "\n" + u
		}
		_, _ = runtime.Invoke(ctx, "slack.notify.ephemeral", map[string]any{"accountId": meta["slackAccountId"], "channelId": meta["channelId"], "userId": payload["userId"], "text": confirmation})
		_ = runtime.Log(ctx, "info", "Slack content saved to OneNote", map[string]any{"messageTs": meta["messageTs"], "pageId": pageID, "contentMode": contentMode})
		return err
	})
	if err := runtime.Serve(context.Background()); err != nil {
		log.Fatal(err)
	}
}

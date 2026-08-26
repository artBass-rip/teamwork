package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (k *Kernel) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, k.Status()) })
	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		events, err := k.RecentEvents(50)
		if err != nil {
			writeError(w, 500, err)
			return
		}
		writeJSON(w, 200, map[string]any{"events": events})
	})
	mux.HandleFunc("GET /api/logs", func(w http.ResponseWriter, r *http.Request) {
		logs, err := k.store.RecentLogs(strings.ToLower(r.URL.Query().Get("level")), 200)
		if err != nil {
			writeError(w, 500, err)
			return
		}
		writeJSON(w, 200, map[string]any{"logs": logs, "retentionHours": 48})
	})
	mux.HandleFunc("POST /api/capabilities/", func(w http.ResponseWriter, r *http.Request) {
		capability := strings.TrimPrefix(r.URL.Path, "/api/capabilities/")
		var body struct {
			Payload any `json:"payload"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&body) != nil {
			writeError(w, 400, fmt.Errorf("invalid JSON body"))
			return
		}
		value, err := k.Invoke(r.Context(), capability, body.Payload, "http")
		if err != nil {
			writeError(w, 503, err)
			return
		}
		writeJSON(w, 200, value)
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		value, err := k.Invoke(ctx, "ui.app.render", map[string]any{}, "http")
		if html, ok := value.(string); err == nil && ok {
			w.Header().Set("content-type", "text/html; charset=utf-8")
			fmt.Fprint(w, html)
			return
		}
		w.Header().Set("content-type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<!doctype html><html><meta charset=utf-8><title>TeamWork</title><style>body{font:16px system-ui;max-width:850px;margin:4rem auto;color:#18212b}</style><h1>TeamWork</h1><p>UI module is not ready.</p><p><a href=/api/health>System status</a> · <a href=/api/events>Event journal</a></p></html>")
	})
	return mux
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

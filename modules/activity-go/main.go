package main

import (
	"context"
	"log"
	"sync"

	sdk "teamwork/sdk/go/runtime"
)

func main() {
	runtime := sdk.New()
	var mu sync.Mutex
	events := []map[string]any{}
	runtime.On("*", func(_ context.Context, event map[string]any) error {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
		if len(events) > 100 {
			events = events[len(events)-100:]
		}
		return nil
	})
	runtime.Capability("activity.stats", func(_ context.Context, _ map[string]any) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		var last any
		if len(events) > 0 {
			last = events[len(events)-1]
		}
		return map[string]any{"events_seen": len(events), "last_event": last}, nil
	})
	if err := runtime.Serve(context.Background()); err != nil {
		log.Fatal(err)
	}
}

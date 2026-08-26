package main

import (
	"context"
	"log"

	sdk "teamwork/sdk/go/runtime"
)

func main() {
	runtime := sdk.New()
	runtime.Capability("example.echo", func(ctx context.Context, payload map[string]any) (any, error) {
		result := map[string]any{"echo": payload, "module": "teamwork.echo"}
		if err := runtime.Publish(ctx, "example.echoed", result); err != nil {
			return nil, err
		}
		return result, nil
	})
	if err := runtime.Serve(context.Background()); err != nil {
		log.Fatal(err)
	}
}

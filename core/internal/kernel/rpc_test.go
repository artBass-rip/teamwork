package kernel

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestConnectionRequestStopsWhenModuleDisconnects(t *testing.T) {
	client, server := net.Pipe()
	connection := &connection{conn: client, done: make(chan struct{}), pending: map[string]chan rpcMessage{}}
	go func() {
		buffer := make([]byte, 1024)
		_, _ = server.Read(buffer)
		_ = server.Close()
		connection.doneOnce.Do(func() { close(connection.done) })
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	started := time.Now()
	if _, err := connection.request(ctx, "test", map[string]any{}); err == nil {
		t.Fatal("expected disconnected module error")
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("disconnect took too long: %s", elapsed)
	}
	_ = client.Close()
}

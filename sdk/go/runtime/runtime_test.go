package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"
)

func serveRuntimeResponse(t *testing.T, result any) (*Runtime, <-chan error) {
	t.Helper()
	client, server := net.Pipe()
	go func() {
		defer server.Close()
		scanner := bufio.NewScanner(server)
		if !scanner.Scan() {
			return
		}
		var registration message
		_ = json.Unmarshal(scanner.Bytes(), &registration)
		accepted, _ := json.Marshal(map[string]any{"accepted": true})
		_ = json.NewEncoder(server).Encode(message{JSONRPC: "2.0", ID: registration.ID, Result: accepted})
		if !scanner.Scan() {
			return
		}
		var request message
		_ = json.Unmarshal(scanner.Bytes(), &request)
		encoded, _ := json.Marshal(result)
		_ = json.NewEncoder(server).Encode(message{JSONRPC: "2.0", ID: request.ID, Result: encoded})
	}()
	runtime := New()
	serveDone := make(chan error, 1)
	go func() { serveDone <- runtime.serveConn(context.Background(), client) }()
	return runtime, serveDone
}

func TestRuntimeBecomesReadyAfterRegistration(t *testing.T) {
	t.Setenv("TEAMWORK_MODULE_ID", "test.module")
	t.Setenv("TEAMWORK_MODULE_TOKEN", "token")
	client, server := net.Pipe()

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		defer server.Close()
		scanner := bufio.NewScanner(server)
		if !scanner.Scan() {
			return
		}
		var registration message
		_ = json.Unmarshal(scanner.Bytes(), &registration)
		result, _ := json.Marshal(map[string]any{"accepted": true})
		_ = json.NewEncoder(server).Encode(message{JSONRPC: "2.0", ID: registration.ID, Result: result})
		if !scanner.Scan() {
			return
		}
		var request message
		_ = json.Unmarshal(scanner.Bytes(), &request)
		response, _ := json.Marshal(map[string]any{"available": true})
		_ = json.NewEncoder(server).Encode(message{JSONRPC: "2.0", ID: request.ID, Result: response})
	}()

	runtime := New()
	serveDone := make(chan error, 1)
	go func() { serveDone <- runtime.serveConn(context.Background(), client) }()
	select {
	case <-runtime.Ready():
	case <-time.After(time.Second):
		t.Fatal("runtime did not become ready")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	status, err := runtime.SecretStatus(ctx)
	if err != nil || status["available"] != true {
		t.Fatalf("unexpected status: %#v, %v", status, err)
	}
	<-serverDone
	if err := <-serveDone; err == nil {
		t.Fatal("expected closed core connection error")
	}
}

func TestRequestStopsWhenCoreDisconnects(t *testing.T) {
	runtime := New()
	close(runtime.done)
	runtime.doneOnce.Do(func() {})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := runtime.SecretStatus(ctx); err == nil {
		t.Fatal("expected disconnected runtime error")
	}
}

func TestPublishReportsUnavailableSubscriber(t *testing.T) {
	runtime, serveDone := serveRuntimeResponse(t, PublishResult{EventID: "event-1", Pending: []string{"workflow"}})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runtime.Publish(ctx, "test.event", map[string]any{}); err == nil {
		t.Fatal("expected pending delivery error")
	}
	<-serveDone
}

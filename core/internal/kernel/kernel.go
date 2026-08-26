package kernel

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	"teamwork/core/internal/manifest"
	"teamwork/core/internal/platform"
	"teamwork/core/internal/secrets"
	"teamwork/core/internal/store"
)

type Kernel struct {
	paths         platform.Paths
	store         *store.Store
	secrets       secrets.Store
	secretsError  error
	logger        *slog.Logger
	listener      net.Listener
	supervisor    *supervisor
	ctx           context.Context
	cancel        context.CancelFunc
	mu            sync.RWMutex
	manifests     map[string]manifest.Manifest
	capabilities  map[string]string
	subscriptions map[string]map[string]bool
	connectionsMu sync.RWMutex
	connections   map[string]*connection
}

func New(paths platform.Paths, logger *slog.Logger) (*Kernel, error) {
	if err := paths.Prepare(); err != nil {
		return nil, err
	}
	database, err := store.Open(paths.DB)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	secretStore, secretErr := secrets.Native("TeamWork Integration Hub")
	return &Kernel{paths: paths, store: database, secrets: secretStore, secretsError: secretErr, logger: logger, supervisor: newSupervisor(logger), ctx: ctx, cancel: cancel, manifests: map[string]manifest.Manifest{}, capabilities: map[string]string{}, subscriptions: map[string]map[string]bool{}, connections: map[string]*connection{}}, nil
}

func (k *Kernel) Start() error {
	if err := k.startRPC(); err != nil {
		return err
	}
	if err := k.reconcile(); err != nil {
		return err
	}
	go k.watch()
	return nil
}

func (k *Kernel) Close() error {
	k.cancel()
	k.supervisor.stopAll()
	if k.listener != nil {
		_ = k.listener.Close()
	}
	_ = os.Remove(k.paths.Socket)
	return k.store.Close()
}

func (k *Kernel) authorize(moduleID, token string) bool {
	expected := k.supervisor.token(moduleID)
	return expected != "" && token == expected
}

func (k *Kernel) register(item manifest.Manifest) error {
	k.mu.Lock()
	for _, capability := range item.Provides {
		if owner := k.capabilities[capability]; owner != "" && owner != item.Module.ID {
			k.mu.Unlock()
			return fmt.Errorf("capability %s already provided by %s", capability, owner)
		}
	}
	for _, capability := range item.Provides {
		k.capabilities[capability] = item.Module.ID
	}
	for _, eventType := range item.Subscribes {
		if k.subscriptions[eventType] == nil {
			k.subscriptions[eventType] = map[string]bool{}
		}
		k.subscriptions[eventType][item.Module.ID] = true
	}
	k.manifests[item.Module.ID] = item
	k.mu.Unlock()
	if err := k.supervisor.start(k.ctx, item, k.paths.Socket, k.paths.Data); err != nil {
		k.unregister(item.Module.ID)
		return err
	}
	return nil
}

func (k *Kernel) unregister(moduleID string) {
	k.supervisor.stop(moduleID)
	k.mu.Lock()
	delete(k.manifests, moduleID)
	for capability, owner := range k.capabilities {
		if owner == moduleID {
			delete(k.capabilities, capability)
		}
	}
	for _, subscribers := range k.subscriptions {
		delete(subscribers, moduleID)
	}
	k.mu.Unlock()
}

func (k *Kernel) reconcile() error {
	items, err := manifest.Discover(k.paths.Modules)
	if err != nil {
		return err
	}
	found := map[string]manifest.Manifest{}
	for _, item := range items {
		found[item.Module.ID] = item
	}
	k.mu.RLock()
	current := map[string]manifest.Manifest{}
	for id, item := range k.manifests {
		current[id] = item
	}
	k.mu.RUnlock()
	for id, item := range current {
		next, ok := found[id]
		if !ok || next.Module.Version != item.Module.Version {
			k.unregister(id)
		}
	}
	for id, item := range found {
		if _, ok := current[id]; !ok {
			if err := k.register(item); err != nil {
				return err
			}
		}
	}
	return nil
}

func (k *Kernel) watch() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-k.ctx.Done():
			return
		case <-ticker.C:
			if err := k.reconcile(); err != nil {
				k.logger.Warn("module discovery failed", "error", err)
			}
		}
	}
}

func (k *Kernel) Invoke(ctx context.Context, capability string, payload any, caller string) (any, error) {
	k.mu.RLock()
	provider := k.capabilities[capability]
	k.mu.RUnlock()
	if provider == "" {
		return nil, fmt.Errorf("no provider for capability: %s", capability)
	}
	k.connectionsMu.RLock()
	connection := k.connections[provider]
	k.connectionsMu.RUnlock()
	if connection == nil {
		return nil, fmt.Errorf("provider is not ready: %s", provider)
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	result, err := connection.request(ctx, "capability.invoke", map[string]any{"capability": capability, "payload": payload, "caller": caller})
	if err != nil {
		return nil, err
	}
	var value any
	if err := json.Unmarshal(result, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func (k *Kernel) Publish(ctx context.Context, producer, eventType string, payload any) (map[string]any, error) {
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, err
	}
	eventID := hex.EncodeToString(idBytes)
	if err := k.store.RecordEvent(eventID, producer, eventType, payload); err != nil {
		return nil, err
	}
	k.mu.RLock()
	targets := map[string]bool{}
	for id := range k.subscriptions[eventType] {
		targets[id] = true
	}
	for id := range k.subscriptions["*"] {
		targets[id] = true
	}
	k.mu.RUnlock()
	delivered := []string{}
	for moduleID := range targets {
		k.connectionsMu.RLock()
		connection := k.connections[moduleID]
		k.connectionsMu.RUnlock()
		if connection == nil {
			_ = k.store.Delivery(eventID, moduleID, "pending", "module unavailable")
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, err := connection.request(callCtx, "event.deliver", map[string]any{"id": eventID, "event_type": eventType, "producer": producer, "payload": payload})
		cancel()
		if err != nil {
			_ = k.store.Delivery(eventID, moduleID, "failed", err.Error())
		} else {
			_ = k.store.Delivery(eventID, moduleID, "completed", "")
			delivered = append(delivered, moduleID)
		}
	}
	return map[string]any{"event_id": eventID, "delivered": delivered}, nil
}

func (k *Kernel) Status() map[string]any {
	k.mu.RLock()
	caps := map[string]string{}
	for name, owner := range k.capabilities {
		caps[name] = owner
	}
	k.mu.RUnlock()
	k.connectionsMu.RLock()
	connected := []string{}
	for id := range k.connections {
		connected = append(connected, id)
	}
	k.connectionsMu.RUnlock()
	return map[string]any{"status": "ok", "modules": k.supervisor.status(), "capabilities": caps, "connections": connected}
}
func (k *Kernel) RecentEvents(limit int) ([]store.Event, error) { return k.store.Recent(limit) }

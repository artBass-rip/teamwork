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
	"sort"
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
	remove, start := reconcilePlan(current, found)
	removing := map[string]bool{}
	for _, id := range remove {
		removing[id] = true
		k.unregister(id)
	}
	for _, item := range start {
		if err := k.register(item); err != nil {
			return err
		}
	}
	for id, item := range found {
		if removing[id] || item.Runtime.Restart != "on-failure" || k.supervisor.running(id) {
			continue
		}
		if currentItem, ok := current[id]; ok && currentItem.Module.Version == item.Module.Version {
			if err := k.supervisor.start(k.ctx, item, k.paths.Socket, k.paths.Data); err != nil {
				k.logger.Warn("module restart failed", "module", id, "error", err)
			}
		}
	}
	return nil
}

func reconcilePlan(current, found map[string]manifest.Manifest) ([]string, []manifest.Manifest) {
	remove := []string{}
	startIDs := []string{}
	for id, item := range current {
		next, ok := found[id]
		if !ok || next.Module.Version != item.Module.Version {
			remove = append(remove, id)
		}
	}
	for id, item := range found {
		currentItem, ok := current[id]
		if !ok || currentItem.Module.Version != item.Module.Version {
			startIDs = append(startIDs, id)
		}
	}
	sort.Strings(remove)
	sort.Strings(startIDs)
	start := make([]manifest.Manifest, 0, len(startIDs))
	for _, id := range startIDs {
		start = append(start, found[id])
	}
	return remove, start
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
	pending := []string{}
	failed := map[string]string{}
	for moduleID := range targets {
		k.connectionsMu.RLock()
		connection := k.connections[moduleID]
		k.connectionsMu.RUnlock()
		if connection == nil {
			_ = k.store.Delivery(eventID, moduleID, "pending", "module unavailable")
			pending = append(pending, moduleID)
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, err := connection.request(callCtx, "event.deliver", map[string]any{"id": eventID, "event_type": eventType, "producer": producer, "payload": payload})
		cancel()
		if err != nil {
			_ = k.store.Delivery(eventID, moduleID, "failed", err.Error())
			failed[moduleID] = err.Error()
		} else {
			_ = k.store.Delivery(eventID, moduleID, "completed", "")
			delivered = append(delivered, moduleID)
		}
	}
	sort.Strings(delivered)
	sort.Strings(pending)
	return map[string]any{"event_id": eventID, "delivered": delivered, "pending": pending, "failed": failed}, nil
}

func (k *Kernel) retryDeliveries(connection *connection) {
	events, err := k.store.PendingDeliveries(connection.moduleID, 1000)
	if err != nil {
		k.logger.Error("pending event lookup failed", "module", connection.moduleID, "error", err)
		return
	}
	for _, event := range events {
		ctx, cancel := context.WithTimeout(k.ctx, 30*time.Second)
		_, deliveryErr := connection.request(ctx, "event.deliver", map[string]any{"id": event.ID, "event_type": event.Type, "producer": event.Producer, "payload": event.Payload})
		cancel()
		if deliveryErr != nil {
			_ = k.store.Delivery(event.ID, connection.moduleID, "failed", deliveryErr.Error())
			k.logger.Warn("pending event delivery failed", "module", connection.moduleID, "event", event.ID, "error", deliveryErr)
			continue
		}
		_ = k.store.Delivery(event.ID, connection.moduleID, "completed", "")
		k.logger.Info("pending event delivered", "module", connection.moduleID, "event", event.ID)
	}
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
	sort.Strings(connected)
	return map[string]any{"status": "ok", "modules": k.supervisor.status(), "capabilities": caps, "connections": connected}
}
func (k *Kernel) RecentEvents(limit int) ([]store.Event, error) { return k.store.Recent(limit) }

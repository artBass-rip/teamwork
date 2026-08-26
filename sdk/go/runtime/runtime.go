package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
)

type Handler func(context.Context, map[string]any) (any, error)
type EventHandler func(context.Context, map[string]any) error
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type Runtime struct {
	moduleID, token, socket string
	conn                    net.Conn
	writeMu                 sync.Mutex
	sequence                atomic.Uint64
	pendingMu               sync.Mutex
	pending                 map[string]chan message
	capabilities            map[string]Handler
	events                  map[string][]EventHandler
}

func New() *Runtime {
	return &Runtime{moduleID: os.Getenv("TEAMWORK_MODULE_ID"), token: os.Getenv("TEAMWORK_MODULE_TOKEN"), socket: os.Getenv("TEAMWORK_CORE_SOCKET"), pending: map[string]chan message{}, capabilities: map[string]Handler{}, events: map[string][]EventHandler{}}
}
func (r *Runtime) Capability(name string, handler Handler) { r.capabilities[name] = handler }
func (r *Runtime) On(name string, handler EventHandler) {
	r.events[name] = append(r.events[name], handler)
}
func (r *Runtime) send(value message) error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = r.conn.Write(append(data, '\n'))
	return err
}
func (r *Runtime) Serve(ctx context.Context) error {
	conn, err := net.Dial("unix", r.socket)
	if err != nil {
		return err
	}
	r.conn = conn
	params, _ := json.Marshal(map[string]any{"module_id": r.moduleID, "token": r.token, "protocol_version": "1"})
	if err := r.send(message{JSONRPC: "2.0", ID: "register", Method: "module.register", Params: params}); err != nil {
		return err
	}
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var msg message
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			continue
		}
		if msg.Method == "" {
			r.pendingMu.Lock()
			target := r.pending[msg.ID]
			r.pendingMu.Unlock()
			if target != nil {
				target <- msg
			}
			continue
		}
		go r.dispatch(ctx, msg)
	}
	return scanner.Err()
}
func (r *Runtime) dispatch(ctx context.Context, msg message) {
	var params map[string]any
	_ = json.Unmarshal(msg.Params, &params)
	var result any
	var err error
	switch msg.Method {
	case "capability.invoke":
		name, _ := params["capability"].(string)
		handler := r.capabilities[name]
		if handler == nil {
			err = fmt.Errorf("capability not found: %s", name)
		} else {
			payload, _ := params["payload"].(map[string]any)
			result, err = handler(ctx, payload)
		}
	case "event.deliver":
		eventType, _ := params["event_type"].(string)
		handlers := append(r.events[eventType], r.events["*"]...)
		for _, handler := range handlers {
			if callErr := handler(ctx, params); callErr != nil {
				err = callErr
				break
			}
		}
		result = map[string]any{"ack": err == nil, "handlers": len(handlers)}
	default:
		err = fmt.Errorf("unsupported method: %s", msg.Method)
	}
	encoded, _ := json.Marshal(result)
	response := message{JSONRPC: "2.0", ID: msg.ID, Result: encoded}
	if err != nil {
		response.Result = nil
		response.Error = &rpcError{Code: -32000, Message: err.Error()}
	}
	_ = r.send(response)
}
func (r *Runtime) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := fmt.Sprintf("module-%d", r.sequence.Add(1))
	encoded, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	target := make(chan message, 1)
	r.pendingMu.Lock()
	r.pending[id] = target
	r.pendingMu.Unlock()
	defer func() { r.pendingMu.Lock(); delete(r.pending, id); r.pendingMu.Unlock() }()
	if err := r.send(message{JSONRPC: "2.0", ID: id, Method: method, Params: encoded}); err != nil {
		return nil, err
	}
	select {
	case response := <-target:
		if response.Error != nil {
			return nil, fmt.Errorf("%s", response.Error.Message)
		}
		return response.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (r *Runtime) Publish(ctx context.Context, eventType string, payload any) error {
	_, err := r.request(ctx, "event.publish", map[string]any{"event_type": eventType, "payload": payload})
	return err
}
func (r *Runtime) Log(ctx context.Context, level, message string, fields map[string]any) error {
	_, err := r.request(ctx, "log.write", map[string]any{"level": level, "message": message, "fields": fields})
	return err
}
func (r *Runtime) Invoke(ctx context.Context, capability string, payload any) (any, error) {
	raw, err := r.request(ctx, "capability.invoke", map[string]any{"capability": capability, "payload": payload})
	if err != nil {
		return nil, err
	}
	var value any
	err = json.Unmarshal(raw, &value)
	return value, err
}

func (r *Runtime) SecretStatus(ctx context.Context) (map[string]any, error) {
	raw, err := r.request(ctx, "secret.status", map[string]any{})
	if err != nil {
		return nil, err
	}
	var value map[string]any
	err = json.Unmarshal(raw, &value)
	return value, err
}
func (r *Runtime) SecretPut(ctx context.Context, ref, value string) error {
	_, err := r.request(ctx, "secret.put", map[string]any{"ref": ref, "value": value})
	return err
}
func (r *Runtime) SecretGet(ctx context.Context, ref string) (string, error) {
	raw, err := r.request(ctx, "secret.get", map[string]any{"ref": ref})
	if err != nil {
		return "", err
	}
	var value struct {
		Value string `json:"value"`
	}
	err = json.Unmarshal(raw, &value)
	return value.Value, err
}
func (r *Runtime) SecretDelete(ctx context.Context, ref string) error {
	_, err := r.request(ctx, "secret.delete", map[string]any{"ref": ref})
	return err
}

package kernel

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type rpcMessage struct {
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

type connection struct {
	moduleID  string
	conn      net.Conn
	writeMu   sync.Mutex
	pendingMu sync.Mutex
	pending   map[string]chan rpcMessage
	sequence  atomic.Uint64
}

func (c *connection) send(value rpcMessage) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	_, err = c.conn.Write(encoded)
	return err
}

func (c *connection) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := fmt.Sprintf("core-%d", c.sequence.Add(1))
	encoded, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	response := make(chan rpcMessage, 1)
	c.pendingMu.Lock()
	c.pending[id] = response
	c.pendingMu.Unlock()
	defer func() { c.pendingMu.Lock(); delete(c.pending, id); c.pendingMu.Unlock() }()
	if err := c.send(rpcMessage{JSONRPC: "2.0", ID: id, Method: method, Params: encoded}); err != nil {
		return nil, err
	}
	select {
	case message := <-response:
		if message.Error != nil {
			return nil, errors.New(message.Error.Message)
		}
		return message.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (k *Kernel) startRPC() error {
	_ = os.Remove(k.paths.Socket)
	listener, err := net.Listen("unix", k.paths.Socket)
	if err != nil {
		return err
	}
	if err := os.Chmod(k.paths.Socket, 0600); err != nil {
		listener.Close()
		return err
	}
	k.listener = listener
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go k.serveConnection(conn)
		}
	}()
	return nil
}

func (k *Kernel) serveConnection(raw net.Conn) {
	c := &connection{conn: raw, pending: map[string]chan rpcMessage{}}
	defer func() {
		raw.Close()
		if c.moduleID != "" {
			k.connectionsMu.Lock()
			if k.connections[c.moduleID] == c {
				delete(k.connections, c.moduleID)
			}
			k.connectionsMu.Unlock()
		}
	}()
	scanner := bufio.NewScanner(raw)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var message rpcMessage
		if json.Unmarshal(scanner.Bytes(), &message) != nil {
			return
		}
		if message.Method == "" {
			c.pendingMu.Lock()
			target := c.pending[message.ID]
			c.pendingMu.Unlock()
			if target != nil {
				target <- message
			}
			continue
		}
		if c.moduleID == "" {
			var registration struct {
				ModuleID        string `json:"module_id"`
				Token           string `json:"token"`
				ProtocolVersion string `json:"protocol_version"`
			}
			if message.Method != "module.register" || json.Unmarshal(message.Params, &registration) != nil || !k.authorize(registration.ModuleID, registration.Token) || registration.ProtocolVersion != "1" {
				_ = c.send(rpcMessage{JSONRPC: "2.0", ID: message.ID, Error: &rpcError{Code: -32001, Message: "module registration rejected"}})
				return
			}
			c.moduleID = registration.ModuleID
			k.connectionsMu.Lock()
			k.connections[c.moduleID] = c
			k.connectionsMu.Unlock()
			result, _ := json.Marshal(map[string]any{"accepted": true, "protocolVersion": "1"})
			_ = c.send(rpcMessage{JSONRPC: "2.0", ID: message.ID, Result: result})
			continue
		}
		go k.handleModuleRequest(c, message)
	}
}

func (k *Kernel) handleModuleRequest(c *connection, message rpcMessage) {
	var result any
	var err error
	switch message.Method {
	case "module.heartbeat":
		result = map[string]bool{"ok": true}
	case "capability.invoke":
		var params struct {
			Capability string `json:"capability"`
			Payload    any    `json:"payload"`
		}
		err = json.Unmarshal(message.Params, &params)
		if err == nil {
			result, err = k.Invoke(context.Background(), params.Capability, params.Payload, c.moduleID)
		}
	case "event.publish":
		var params struct {
			EventType string `json:"event_type"`
			Payload   any    `json:"payload"`
		}
		err = json.Unmarshal(message.Params, &params)
		if err == nil {
			result, err = k.Publish(context.Background(), c.moduleID, params.EventType, params.Payload)
		}
	case "log.write":
		var params struct {
			Level   string `json:"level"`
			Message string `json:"message"`
			Fields  any    `json:"fields"`
		}
		err = json.Unmarshal(message.Params, &params)
		params.Level = strings.ToLower(params.Level)
		if err == nil && params.Level != "debug" && params.Level != "info" && params.Level != "warn" && params.Level != "error" {
			err = fmt.Errorf("invalid log level: %s", params.Level)
		}
		if err == nil && strings.TrimSpace(params.Message) == "" {
			err = fmt.Errorf("log message is required")
		}
		if err == nil {
			err = k.store.RecordLog(params.Level, c.moduleID, params.Message, params.Fields)
		}
		result = map[string]bool{"recorded": err == nil}
	case "secret.status":
		result = map[string]any{"available": k.secrets != nil}
		if k.secretsError != nil {
			result.(map[string]any)["message"] = k.secretsError.Error()
		}
	case "secret.put":
		var params struct{ Ref, Value string }
		err = json.Unmarshal(message.Params, &params)
		if err == nil && k.secrets == nil {
			err = fmt.Errorf("system secret store unavailable: %v", k.secretsError)
		}
		if err == nil && (params.Ref == "" || params.Value == "") {
			err = fmt.Errorf("secret ref and value are required")
		}
		if err == nil {
			err = k.secrets.Put(context.Background(), c.moduleID+":"+params.Ref, params.Value)
		}
		result = map[string]bool{"stored": err == nil}
	case "secret.get":
		var params struct{ Ref string }
		err = json.Unmarshal(message.Params, &params)
		if err == nil && k.secrets == nil {
			err = fmt.Errorf("system secret store unavailable: %v", k.secretsError)
		}
		var value string
		if err == nil && params.Ref == "" {
			err = fmt.Errorf("secret ref is required")
		}
		if err == nil {
			value, err = k.secrets.Get(context.Background(), c.moduleID+":"+params.Ref)
		}
		result = map[string]string{"value": value}
	case "secret.delete":
		var params struct{ Ref string }
		err = json.Unmarshal(message.Params, &params)
		if err == nil && k.secrets == nil {
			err = fmt.Errorf("system secret store unavailable: %v", k.secretsError)
		}
		if err == nil && params.Ref == "" {
			err = fmt.Errorf("secret ref is required")
		}
		if err == nil {
			err = k.secrets.Delete(context.Background(), c.moduleID+":"+params.Ref)
		}
		result = map[string]bool{"deleted": err == nil}
	default:
		err = fmt.Errorf("unsupported core method: %s", message.Method)
	}
	response := rpcMessage{JSONRPC: "2.0", ID: message.ID}
	if err != nil {
		response.Error = &rpcError{Code: -32000, Message: err.Error()}
	} else {
		response.Result, _ = json.Marshal(result)
	}
	_ = c.send(response)
}

func requestTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

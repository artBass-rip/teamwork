package kernel

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"teamwork/core/internal/manifest"
)

type processState struct {
	manifest manifest.Manifest
	cmd      *exec.Cmd
	token    string
	done     chan struct{}
}

type supervisor struct {
	mu        sync.Mutex
	processes map[string]*processState
	logger    *slog.Logger
}

func newSupervisor(logger *slog.Logger) *supervisor {
	return &supervisor{processes: map[string]*processState{}, logger: logger}
}

func (s *supervisor) start(ctx context.Context, item manifest.Manifest, socket, dataRoot string) error {
	executable, err := item.Executable()
	if err != nil {
		return err
	}
	info, err := os.Stat(executable)
	if err != nil {
		return fmt.Errorf("module %s executable: %w", item.Module.ID, err)
	}
	if info.Mode()&0111 == 0 {
		return fmt.Errorf("module %s executable is not executable", item.Module.ID)
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return err
	}
	token := hex.EncodeToString(secret)
	cmd := exec.CommandContext(ctx, executable)
	cmd.Dir = filepath.Dir(executable)
	moduleData := filepath.Join(dataRoot, "module-state", item.Module.ID)
	if err := os.MkdirAll(moduleData, 0700); err != nil {
		return err
	}
	cmd.Env = append(os.Environ(), "TEAMWORK_CORE_SOCKET="+socket, "TEAMWORK_MODULE_ID="+item.Module.ID, "TEAMWORK_MODULE_TOKEN="+token, "TEAMWORK_MODULE_DATA="+moduleData)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	state := &processState{manifest: item, cmd: cmd, token: token, done: make(chan struct{})}
	s.mu.Lock()
	s.processes[item.Module.ID] = state
	s.mu.Unlock()
	s.logger.Info("module started", "module", item.Module.ID, "pid", cmd.Process.Pid)
	go func() {
		err := cmd.Wait()
		close(state.done)
		s.logger.Info("module stopped", "module", item.Module.ID, "error", err)
		s.mu.Lock()
		current := s.processes[item.Module.ID]
		if current == state {
			delete(s.processes, item.Module.ID)
		}
		s.mu.Unlock()
	}()
	return nil
}

func (s *supervisor) stop(moduleID string) {
	s.mu.Lock()
	state := s.processes[moduleID]
	delete(s.processes, moduleID)
	s.mu.Unlock()
	if state == nil || state.cmd.Process == nil {
		return
	}
	_ = state.cmd.Process.Signal(os.Interrupt)
	select {
	case <-state.done:
	case <-time.After(5 * time.Second):
		_ = state.cmd.Process.Kill()
		<-state.done
	}
}

func (s *supervisor) stopAll() {
	s.mu.Lock()
	ids := make([]string, 0, len(s.processes))
	for id := range s.processes {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.stop(id)
	}
}

func (s *supervisor) token(moduleID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.processes[moduleID]; p != nil {
		return p.token
	}
	return ""
}

func (s *supervisor) running(moduleID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.processes[moduleID] != nil
}

func (s *supervisor) status() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := []map[string]any{}
	ids := make([]string, 0, len(s.processes))
	for id := range s.processes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		p := s.processes[id]
		result = append(result, map[string]any{"id": id, "version": p.manifest.Module.Version, "pid": p.cmd.Process.Pid, "running": p.cmd.ProcessState == nil})
	}
	return result
}

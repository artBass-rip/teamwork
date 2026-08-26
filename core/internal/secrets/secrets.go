package secrets

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

type Store interface {
	Put(context.Context, string, string) error
	Get(context.Context, string) (string, error)
	Delete(context.Context, string) error
}

func Native(service string) (Store, error) {
	switch runtime.GOOS {
	case "darwin":
		return &keychain{service: service}, nil
	case "linux":
		if _, err := exec.LookPath("secret-tool"); err != nil {
			return nil, fmt.Errorf("Secret Service client secret-tool is unavailable: %w", err)
		}
		return &secretService{service: service}, nil
	default:
		return nil, fmt.Errorf("native secret store unsupported on %s", runtime.GOOS)
	}
}

type keychain struct{ service string }

func (k *keychain) Put(ctx context.Context, key, value string) error {
	return exec.CommandContext(ctx, "security", "add-generic-password", "-U", "-s", k.service, "-a", key, "-w", value).Run()
}
func (k *keychain) Get(ctx context.Context, key string) (string, error) {
	out, err := exec.CommandContext(ctx, "security", "find-generic-password", "-s", k.service, "-a", key, "-w").Output()
	return strings.TrimSpace(string(out)), err
}
func (k *keychain) Delete(ctx context.Context, key string) error {
	return exec.CommandContext(ctx, "security", "delete-generic-password", "-s", k.service, "-a", key).Run()
}

type secretService struct{ service string }

func (s *secretService) Put(ctx context.Context, key, value string) error {
	cmd := exec.CommandContext(ctx, "secret-tool", "store", "--label=TeamWork", "service", s.service, "account", key)
	cmd.Stdin = strings.NewReader(value)
	return cmd.Run()
}
func (s *secretService) Get(ctx context.Context, key string) (string, error) {
	out, err := exec.CommandContext(ctx, "secret-tool", "lookup", "service", s.service, "account", key).Output()
	return strings.TrimSpace(string(out)), err
}
func (s *secretService) Delete(ctx context.Context, key string) error {
	cmd := exec.CommandContext(ctx, "secret-tool", "clear", "service", s.service, "account", key)
	cmd.Stdin = bytes.NewReader(nil)
	return cmd.Run()
}

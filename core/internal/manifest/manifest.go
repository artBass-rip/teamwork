package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var validID = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$`)
var validVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)

type Manifest struct {
	SchemaVersion int `json:"schemaVersion"`
	Module        struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		Version         string `json:"version"`
		ProtocolVersion string `json:"protocolVersion"`
	} `json:"module"`
	Runtime struct {
		Restart string `json:"restart"`
	} `json:"runtime"`
	Executables map[string]string `json:"executables"`
	Provides    []string          `json:"provides"`
	Subscribes  []string          `json:"subscribes"`
	Dir         string            `json:"-"`
}

func Load(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var value Manifest
	if err := json.Unmarshal(data, &value); err != nil {
		return Manifest{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if value.SchemaVersion != 1 || !validID.MatchString(value.Module.ID) || strings.TrimSpace(value.Module.Name) == "" || !validVersion.MatchString(value.Module.Version) || value.Module.ProtocolVersion != "1" {
		return Manifest{}, fmt.Errorf("invalid or incompatible manifest: %s", path)
	}
	if value.Runtime.Restart != "" && value.Runtime.Restart != "never" && value.Runtime.Restart != "on-failure" {
		return Manifest{}, fmt.Errorf("invalid restart policy for module %s: %s", value.Module.ID, value.Runtime.Restart)
	}
	if duplicate := firstDuplicate(value.Provides); duplicate != "" {
		return Manifest{}, fmt.Errorf("module %s provides capability more than once: %s", value.Module.ID, duplicate)
	}
	if duplicate := firstDuplicate(value.Subscribes); duplicate != "" {
		return Manifest{}, fmt.Errorf("module %s subscribes more than once: %s", value.Module.ID, duplicate)
	}
	for _, capability := range value.Provides {
		if !validID.MatchString(capability) {
			return Manifest{}, fmt.Errorf("module %s has invalid capability name: %s", value.Module.ID, capability)
		}
	}
	for _, eventType := range value.Subscribes {
		if eventType != "*" && !validID.MatchString(eventType) {
			return Manifest{}, fmt.Errorf("module %s has invalid event name: %s", value.Module.ID, eventType)
		}
	}
	value.Dir = filepath.Dir(path)
	if _, err := value.Executable(); err != nil {
		return Manifest{}, err
	}
	return value, nil
}

func firstDuplicate(values []string) string {
	seen := map[string]bool{}
	for _, value := range values {
		if seen[value] {
			return value
		}
		seen[value] = true
	}
	return ""
}

func (m Manifest) Executable() (string, error) {
	relative := m.Executables[runtime.GOOS+"-"+runtime.GOARCH]
	if relative == "" {
		return "", fmt.Errorf("module %s does not support %s-%s", m.Module.ID, runtime.GOOS, runtime.GOARCH)
	}
	path := filepath.Clean(filepath.Join(m.Dir, relative))
	rel, err := filepath.Rel(m.Dir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("module executable escapes module directory")
	}
	return path, nil
}

func Discover(directory string) ([]Manifest, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	seen := map[string]bool{}
	result := []Manifest{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(directory, entry.Name(), "module.json")
		if _, err := os.Stat(path); err != nil {
			continue
		}
		value, err := Load(path)
		if err != nil {
			return nil, err
		}
		if seen[value.Module.ID] {
			return nil, fmt.Errorf("duplicate module id: %s", value.Module.ID)
		}
		seen[value.Module.ID] = true
		result = append(result, value)
	}
	return result, nil
}

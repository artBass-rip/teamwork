package manifest

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadSelectsNativeExecutable(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join("bin", runtime.GOOS+"-"+runtime.GOARCH, "example")
	value := `{"schemaVersion":1,"module":{"id":"teamwork.example","name":"Example","version":"1.0.0","protocolVersion":"1"},"executables":{"` + runtime.GOOS + `-` + runtime.GOARCH + `":"` + executable + `"},"provides":["example.echo"],"subscribes":[]}`
	path := filepath.Join(dir, "module.json")
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
	item, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if item.Module.ID != "teamwork.example" {
		t.Fatalf("unexpected id %s", item.Module.ID)
	}
	selected, err := item.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if selected != filepath.Join(dir, executable) {
		t.Fatalf("unexpected executable %s", selected)
	}
}

func TestRejectsEscapingExecutable(t *testing.T) {
	dir := t.TempDir()
	value := `{"schemaVersion":1,"module":{"id":"teamwork.bad","name":"Bad","version":"1","protocolVersion":"1"},"executables":{"` + runtime.GOOS + `-` + runtime.GOARCH + `":"../bad"},"provides":[],"subscribes":[]}`
	path := filepath.Join(dir, "module.json")
	_ = os.WriteFile(path, []byte(value), 0600)
	if _, err := Load(path); err == nil {
		t.Fatal("expected escaping executable rejection")
	}
}

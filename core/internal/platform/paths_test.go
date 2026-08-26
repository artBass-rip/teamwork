package platform

import (
	"path/filepath"
	"testing"
)

func TestHomeOverrideIsSelfContained(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TEAMWORK_HOME", home)
	t.Setenv("TEAMWORK_MODULES_DIR", "")
	paths, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if paths.DB != filepath.Join(home, "core.db") {
		t.Fatalf("unexpected database path %s", paths.DB)
	}
	if paths.Socket != filepath.Join(home, "run", "core.sock") {
		t.Fatalf("unexpected socket path %s", paths.Socket)
	}
}

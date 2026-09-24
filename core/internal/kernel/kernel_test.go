package kernel

import (
	"reflect"
	"testing"

	"teamwork/core/internal/manifest"
)

func testManifest(id, version string) manifest.Manifest {
	value := manifest.Manifest{}
	value.Module.ID = id
	value.Module.Version = version
	return value
}

func TestReconcilePlanRestartsVersionChanges(t *testing.T) {
	current := map[string]manifest.Manifest{
		"removed": testManifest("removed", "1.0.0"),
		"stable":  testManifest("stable", "1.0.0"),
		"updated": testManifest("updated", "1.0.0"),
	}
	found := map[string]manifest.Manifest{
		"added":   testManifest("added", "1.0.0"),
		"stable":  testManifest("stable", "1.0.0"),
		"updated": testManifest("updated", "2.0.0"),
	}

	remove, start := reconcilePlan(current, found)
	if !reflect.DeepEqual(remove, []string{"removed", "updated"}) {
		t.Fatalf("unexpected removals: %#v", remove)
	}
	started := []string{start[0].Module.ID, start[1].Module.ID}
	if !reflect.DeepEqual(started, []string{"added", "updated"}) {
		t.Fatalf("unexpected starts: %#v", started)
	}
}

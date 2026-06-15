package skiller

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFacadeSmoke(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join("..", "..")
	talkingStick := filepath.Join(root, "testdata", "m0", "manifests", "talking-stick.toml")
	runtimeManifest := filepath.Join(root, "testdata", "m0", "manifests", "clawdapus-runtime.toml")
	namespaceCollision := filepath.Join(root, "testdata", "m0", "manifests", "namespace-collision.toml")

	planned, err := Plan(ctx, Options{ManifestPath: talkingStick, Home: t.TempDir()})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if planned.Schema != "skiller-plan.v1" || len(planned.Actions) == 0 {
		t.Fatalf("plan = %#v", planned)
	}

	conflicts, err := Conflicts(ctx, StatusOptions{ManifestPath: namespaceCollision, Home: t.TempDir()})
	if err != nil {
		t.Fatalf("Conflicts: %v", err)
	}
	if len(conflicts.Conflicts) == 0 {
		t.Fatalf("conflicts = %#v, want namespace collision", conflicts)
	}

	home := t.TempDir()
	project := t.TempDir()
	stateDir := t.TempDir()
	result, err := Install(ctx, ApplyOptions{
		ManifestPath:     runtimeManifest,
		Home:             home,
		Project:          project,
		StateDir:         stateDir,
		LockTimeout:      time.Second,
		InstallerVersion: "facade-test",
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if len(result.Actions) != 1 || result.Actions[0].Status != "installed" {
		t.Fatalf("result = %#v", result)
	}
	assertMarkerVersion(t, project, "facade-test")

	report, err := Status(ctx, StatusOptions{
		ManifestPath: runtimeManifest,
		Home:         home,
		Project:      project,
		StateDir:     stateDir,
	})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(report.Items) == 0 {
		t.Fatalf("status = %#v, want installed item", report)
	}

	if err := os.Remove(filepath.Join(stateDir, "state.json")); err != nil {
		t.Fatal(err)
	}
	ledger, err := Repair(ctx, RepairOptions{
		ManifestPath: runtimeManifest,
		Home:         home,
		Project:      project,
		StateDir:     stateDir,
		LockTimeout:  time.Second,
	})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if len(ledger.Installs) != 1 {
		t.Fatalf("ledger installs = %#v, want one", ledger.Installs)
	}
}

func TestFacadeRespectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Plan(ctx, Options{}); err != context.Canceled {
		t.Fatalf("Plan canceled error = %v, want context.Canceled", err)
	}
}

func assertMarkerVersion(t *testing.T, project, want string) {
	t.Helper()
	markerPath := filepath.Join(project, ".claw-skills", "desk-manager", "skills", "clawdapus-cli", ".skiller-install.json")
	data, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatal(err)
	}
	var marker struct {
		Installer struct {
			Version string `json:"version"`
		} `json:"installer"`
	}
	if err := json.Unmarshal(data, &marker); err != nil {
		t.Fatal(err)
	}
	if marker.Installer.Version != want {
		t.Fatalf("marker version = %q, want %q\n%s", marker.Installer.Version, want, data)
	}
}

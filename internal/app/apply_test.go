package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mostlydev/skiller/internal/schemajson"
	"github.com/mostlydev/skiller/pkg/state"
)

func TestApplyCopyPipelineWritesTargetAndState(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	stateDir := t.TempDir()
	manifest := filepath.Join("..", "..", "testdata", "m0", "manifests", "clawdapus-runtime.toml")
	result, err := Apply(context.Background(), ApplyOptions{
		ManifestPath: manifest,
		Home:         home,
		Project:      project,
		StateDir:     stateDir,
		LockTimeout:  time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Actions) != 1 || result.Actions[0].Status != "installed" {
		t.Fatalf("result actions = %#v", result.Actions)
	}
	target := filepath.Join(project, ".claw-skills", "desk-manager", "skills", "clawdapus-cli")
	if _, err := os.Stat(filepath.Join(target, "SKILL.md")); err != nil {
		t.Fatalf("target skill missing: %v", err)
	}
	markerPath := filepath.Join(target, ".skiller-install.json")
	marker, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("marker missing: %v", err)
	}
	if err := schemajson.Validate("marker.schema.json", marker); err != nil {
		t.Fatalf("marker schema: %v\n%s", err, marker)
	}
	loaded, err := state.Load(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Ledger.Installs) != 1 {
		t.Fatalf("installs = %#v, want one install", loaded.Ledger.Installs)
	}
	if loaded.Ledger.Installs[0].TargetPath != target {
		t.Fatalf("target_path = %q, want %q", loaded.Ledger.Installs[0].TargetPath, target)
	}
}

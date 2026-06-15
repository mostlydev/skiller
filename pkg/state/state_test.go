package state

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mostlydev/skiller/internal/contract"
)

func TestLoadCorruptLedgerReturnsEmptyWithRebuildHint(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"schema":`), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Load(dir)
	if err != nil {
		t.Fatalf("Load should not fail on corrupt rebuildable state: %v", err)
	}
	if !result.RebuildRecommended {
		t.Fatal("expected rebuild recommendation")
	}
	if result.Ledger.Schema != "skiller-state.v1" || len(result.Ledger.Installs) != 0 {
		t.Fatalf("ledger = %#v, want empty skiller-state.v1 ledger", result.Ledger)
	}
	if len(result.Diagnostics) != 1 || !strings.Contains(result.Diagnostics[0].Message, "rebuild recommended") {
		t.Fatalf("diagnostics = %#v, want rebuild diagnostic", result.Diagnostics)
	}
}

func TestLoadValidLedger(t *testing.T) {
	dir := t.TempDir()
	data := []byte(`{
  "schema": "skiller-state.v1",
  "sources": [],
  "skills": [],
  "installs": [],
  "extras": [],
  "conflicts": []
}`)
	if err := os.WriteFile(filepath.Join(dir, "state.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if result.RebuildRecommended || len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected rebuild diagnostics: %#v", result)
	}
}

func TestCommitWritesValidatedLedger(t *testing.T) {
	dir := t.TempDir()
	err := Commit(context.Background(), CommitOptions{Dir: dir, LockTimeout: time.Second}, func(ledger *Ledger) error {
		ledger.Conflicts = append(ledger.Conflicts, conflictRecord())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Ledger.Conflicts) != 1 || loaded.Ledger.Conflicts[0].ID != "conflict-001" {
		t.Fatalf("ledger conflicts = %#v", loaded.Ledger.Conflicts)
	}
	data, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
}

func conflictRecord() contract.PlanConflict {
	return contract.PlanConflict{
		ID:                 "conflict-001",
		TargetKind:         "shared",
		TargetID:           "agents",
		EffectiveName:      "demo",
		DesiredCanonicalID: "test:demo",
		Status:             "foreign-target",
		SafeChoices:        []string{"block"},
	}
}

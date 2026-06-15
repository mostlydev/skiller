package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

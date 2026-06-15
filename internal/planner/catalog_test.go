package planner

import (
	"strings"
	"testing"

	"github.com/mostlydev/skiller/internal/contract"
)

// The embedded registries must decode under the strict loader: every key they
// carry has to be declared by the typed contract structs (design doc §6.1).
func TestEmbeddedCatalogLoadsStrictly(t *testing.T) {
	catalog, err := LoadCatalog()
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	if len(catalog.Harnesses) == 0 || len(catalog.Markers) == 0 || len(catalog.Sources) == 0 {
		t.Fatalf("catalog is missing rows: %+v", catalog)
	}
}

func TestStrictTOMLRejectsUnknownField(t *testing.T) {
	const data = `
schema = "skiller-harness-registry.v1"

[[harnesses]]
id = "agents"
kind = "shared"
global_dir = "~/.agents/skills"
typo_field = "should be rejected"
`
	var out contract.Catalog
	err := decodeStrictTOML("test.toml", data, &out)
	if err == nil {
		t.Fatal("expected unknown-field error, got nil")
	}
	if !strings.Contains(err.Error(), "typo_field") {
		t.Fatalf("error should name the unknown field, got: %v", err)
	}
}

func TestStrictTOMLAcceptsKnownFields(t *testing.T) {
	const data = `
schema = "skiller-harness-registry.v1"

[[harnesses]]
id = "agents"
kind = "shared"
readers = ["codex"]
global_dir = "~/.agents/skills"
default_mode = "link"
carries_extras = true
`
	var out contract.Catalog
	if err := decodeStrictTOML("test.toml", data, &out); err != nil {
		t.Fatalf("known fields should decode cleanly, got: %v", err)
	}
	if len(out.Harnesses) != 1 || out.Harnesses[0].ID != "agents" {
		t.Fatalf("unexpected decode: %+v", out.Harnesses)
	}
}

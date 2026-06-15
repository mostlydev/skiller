package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
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
	assertApplyResultSchema(t, result)
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

// Idempotence is a core M2 gate property (§11.4): applying the same manifest twice
// must leave identical state and produce a quiet no-op second run. This also proves
// end-to-end that the copy marker is excluded from the digest — otherwise the marker
// written on run 1 would change the target digest and run 2 would (wrongly) refresh.
func TestApplyIsIdempotent(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	stateDir := t.TempDir()
	manifest := filepath.Join("..", "..", "testdata", "m0", "manifests", "clawdapus-runtime.toml")
	opts := ApplyOptions{
		ManifestPath: manifest,
		Home:         home,
		Project:      project,
		StateDir:     stateDir,
		LockTimeout:  time.Second,
	}

	first, err := Apply(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Actions) != 1 || first.Actions[0].Status != "installed" {
		t.Fatalf("first run actions = %#v, want one installed", first.Actions)
	}
	target := filepath.Join(project, ".claw-skills", "desk-manager", "skills", "clawdapus-cli")
	contentBefore, err := os.ReadFile(filepath.Join(target, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}

	second, err := Apply(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Actions) != 1 || second.Actions[0].Status != "skipped" || second.Actions[0].Action != "no-op" {
		t.Fatalf("second run must be a quiet no-op, got %#v", second.Actions)
	}
	contentAfter, err := os.ReadFile(filepath.Join(target, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(contentBefore) != string(contentAfter) {
		t.Fatalf("target content changed across idempotent re-apply")
	}
	loaded, err := state.Load(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Ledger.Installs) != 1 {
		t.Fatalf("installs = %d, want exactly one (no duplicate on re-apply)", len(loaded.Ledger.Installs))
	}
}

func TestApplySatisfiedByForeignWritesLedgerOnly(t *testing.T) {
	home := t.TempDir()
	stateDir := t.TempDir()
	src := fixtureSource(t, "talking-stick")
	target := filepath.Join(home, ".agents", "skills", "talking-stick")
	copyDir(t, src, target)
	manifest := writeSingleSkillManifest(t, "talking-stick", "mostlydev:talking-stick", "talking-stick", src)

	result, err := Apply(context.Background(), ApplyOptions{
		ManifestPath: manifest,
		Home:         home,
		StateDir:     stateDir,
		LockTimeout:  time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Actions) != 1 || result.Actions[0].Action != "satisfied-by-foreign" || result.Actions[0].Status != "satisfied-by-foreign" {
		t.Fatalf("result actions = %#v, want one satisfied-by-foreign result", result.Actions)
	}
	assertApplyResultSchema(t, result)
	if _, err := os.Stat(filepath.Join(target, ".skiller-install.json")); !os.IsNotExist(err) {
		t.Fatalf("satisfied-by-foreign must not write skiller marker, stat err=%v", err)
	}
	loaded, err := state.Load(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Ledger.Installs) != 1 {
		t.Fatalf("installs = %#v, want one ledger-only install", loaded.Ledger.Installs)
	}
	install := loaded.Ledger.Installs[0]
	if install.TargetPath != target || install.Status != "satisfied-by-foreign" {
		t.Fatalf("install = %#v, want target %q with satisfied-by-foreign status", install, target)
	}
	if install.MarkerPath != "" {
		t.Fatalf("marker_path = %q, want empty for unmanaged foreign target", install.MarkerPath)
	}
}

func TestApplyAdoptExistingWritesLedgerOnly(t *testing.T) {
	home := t.TempDir()
	stateDir := t.TempDir()
	src := fixtureSource(t, "gnit")
	target := filepath.Join(home, ".agents", "skills", "gnit")
	copyDir(t, src, target)
	legacyMarker := filepath.Join(target, ".gnit-skill-managed")
	if err := os.WriteFile(legacyMarker, []byte("gnit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := writeSingleSkillManifest(t, "gnit", "mostlydev:gnit", "gnit", src)

	result, err := Apply(context.Background(), ApplyOptions{
		ManifestPath: manifest,
		Home:         home,
		StateDir:     stateDir,
		LockTimeout:  time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Actions) != 1 || result.Actions[0].Action != "adopt-existing" || result.Actions[0].Status != "skipped" {
		t.Fatalf("result actions = %#v, want one skipped adopt-existing", result.Actions)
	}
	if _, err := os.Stat(filepath.Join(target, ".skiller-install.json")); !os.IsNotExist(err) {
		t.Fatalf("adopt-existing must not write skiller marker, stat err=%v", err)
	}
	loaded, err := state.Load(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Ledger.Installs) != 1 {
		t.Fatalf("installs = %#v, want one ledger-only install", loaded.Ledger.Installs)
	}
	install := loaded.Ledger.Installs[0]
	if install.TargetPath != target || install.Status != "installed" || install.LegacyAdapter != "gnit" || install.MarkerPath != legacyMarker {
		t.Fatalf("install = %#v, want adopted legacy install at %q with marker %q", install, target, legacyMarker)
	}
}

func assertApplyResultSchema(t *testing.T, result any) {
	t.Helper()
	resultJSON, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := schemajson.Validate("apply-result.schema.json", resultJSON); err != nil {
		t.Fatalf("apply result schema: %v\n%s", err, resultJSON)
	}
}

func fixtureSource(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "m0", "sources", name)
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func writeSingleSkillManifest(t *testing.T, name, canonicalID, installSlug, source string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "skiller.toml")
	data := "schema = \"skiller-install.v1\"\n" +
		"owner = \"test-owner\"\n" +
		"namespace = \"mostlydev\"\n" +
		"default_mode = \"link\"\n\n" +
		"[[skills]]\n" +
		"name = " + strconv.Quote(name) + "\n" +
		"canonical_id = " + strconv.Quote(canonicalID) + "\n" +
		"install_slug = " + strconv.Quote(installSlug) + "\n" +
		"source = " + strconv.Quote(source) + "\n" +
		"targets = [\"agents\"]\n" +
		"mode = \"link\"\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	if err := filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	}); err != nil {
		t.Fatal(err)
	}
}

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
	"github.com/mostlydev/skiller/pkg/install"
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

func TestApplyExtraIsIdempotent(t *testing.T) {
	home := t.TempDir()
	stateDir := t.TempDir()
	manifest := filepath.Join("..", "..", "testdata", "m0", "manifests", "talking-stick.toml")
	opts := ApplyOptions{
		ManifestPath: manifest,
		Home:         home,
		StateDir:     stateDir,
		LockTimeout:  time.Second,
	}
	first, err := Apply(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	firstExtra := findResultExtra(first, "grok-session-hook")
	if firstExtra == nil || firstExtra.Status != "installed" {
		t.Fatalf("first extra action = %#v, want installed", firstExtra)
	}

	second, err := Apply(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	secondExtra := findResultExtra(second, "grok-session-hook")
	if secondExtra == nil || secondExtra.Action != "no-op" || secondExtra.Status != "skipped" {
		t.Fatalf("second extra action = %#v, want skipped no-op", secondExtra)
	}
	loaded, err := state.Load(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Ledger.Extras) != 1 {
		t.Fatalf("extras = %#v, want one ledger extra without duplication", loaded.Ledger.Extras)
	}
}

func TestApplyRenameResolutionInstallsExplicitSlug(t *testing.T) {
	home := t.TempDir()
	stateDir := t.TempDir()
	manifest := filepath.Join("..", "..", "testdata", "m0", "manifests", "namespace-collision.toml")
	result, err := Apply(context.Background(), ApplyOptions{
		ManifestPath: manifest,
		Home:         home,
		StateDir:     stateDir,
		OnConflict:   "rename",
		InstallSlug:  "debugging-beta",
		LockTimeout:  time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Actions) != 2 {
		t.Fatalf("actions = %#v, want two installs", result.Actions)
	}
	if findResultSkill(result, "beta:debugging", filepath.Join(home, ".agents/skills/debugging-beta")) == nil {
		t.Fatalf("missing renamed beta install action: %#v", result.Actions)
	}
	if _, err := os.Lstat(filepath.Join(home, ".agents/skills/debugging-beta")); err != nil {
		t.Fatalf("renamed target missing: %v", err)
	}
	loaded, err := state.Load(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Ledger.Installs) != 2 {
		t.Fatalf("installs = %#v, want two ledger installs", loaded.Ledger.Installs)
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

func TestApplyRefreshesStaleLegacyAndRemovesLegacyMarker(t *testing.T) {
	home := t.TempDir()
	stateDir := t.TempDir()
	src := fixtureSource(t, "gnit")
	target := filepath.Join(home, ".agents", "skills", "gnit")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("---\nname: gnit\n---\n\nold\n"), 0o644); err != nil {
		t.Fatal(err)
	}
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
	if len(result.Actions) != 1 || result.Actions[0].Action != "refresh" || result.Actions[0].Status != "updated" {
		t.Fatalf("result actions = %#v, want one updated refresh", result.Actions)
	}
	if _, err := os.Stat(legacyMarker); !os.IsNotExist(err) {
		t.Fatalf("legacy marker should be removed after materialized ownership, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(target, ".skiller-install.json")); err != nil {
		t.Fatalf("skiller marker should be written after legacy refresh: %v", err)
	}
	loaded, err := state.Load(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Ledger.Installs) != 1 || loaded.Ledger.Installs[0].LegacyAdapter != "gnit" {
		t.Fatalf("installs = %#v, want legacy adapter lineage retained", loaded.Ledger.Installs)
	}
}

func TestRepairRebuildsSatisfiedByForeignLedger(t *testing.T) {
	home := t.TempDir()
	stateDir := t.TempDir()
	src := fixtureSource(t, "talking-stick")
	target := filepath.Join(home, ".agents", "skills", "talking-stick")
	copyDir(t, src, target)
	manifest := writeSingleSkillManifest(t, "talking-stick", "mostlydev:talking-stick", "talking-stick", src)

	ledger, err := Repair(context.Background(), RepairOptions{
		ManifestPath: manifest,
		Home:         home,
		StateDir:     stateDir,
		LockTimeout:  time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Installs) != 1 {
		t.Fatalf("installs = %#v, want one repaired install", ledger.Installs)
	}
	install := ledger.Installs[0]
	if install.TargetPath != target || install.Status != "satisfied-by-foreign" || install.MarkerPath != "" {
		t.Fatalf("install = %#v, want unmanaged satisfied-by-foreign repair at %q", install, target)
	}
	if _, err := os.Stat(filepath.Join(target, ".skiller-install.json")); !os.IsNotExist(err) {
		t.Fatalf("repair must not write skiller marker, stat err=%v", err)
	}
}

func TestRepairRebuildsOwnedInstallAfterMissingState(t *testing.T) {
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
	if _, err := Apply(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(stateDir, "state.json")); err != nil {
		t.Fatal(err)
	}

	ledger, err := Repair(context.Background(), RepairOptions{
		ManifestPath: manifest,
		Home:         home,
		Project:      project,
		StateDir:     stateDir,
		LockTimeout:  time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(project, ".claw-skills", "desk-manager", "skills", "clawdapus-cli")
	if len(ledger.Installs) != 1 || ledger.Installs[0].TargetPath != target || ledger.Installs[0].Status != "installed" {
		t.Fatalf("installs = %#v, want repaired installed target %q", ledger.Installs, target)
	}
}

func TestUninstallRemovesOwnedRuntimeCopyAndState(t *testing.T) {
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
	if _, err := Apply(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(project, ".claw-skills", "desk-manager", "skills", "clawdapus-cli")

	result, err := Uninstall(context.Background(), UninstallOptions{
		ManifestPath: manifest,
		Home:         home,
		Project:      project,
		StateDir:     stateDir,
		LockTimeout:  time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Actions) != 1 || result.Actions[0].Action != "remove-owned" || result.Actions[0].Status != "removed" {
		t.Fatalf("result actions = %#v, want one removed action", result.Actions)
	}
	assertApplyResultSchema(t, result)
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("target should be removed, stat err=%v", err)
	}
	loaded, err := state.Load(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Ledger.Installs) != 0 {
		t.Fatalf("installs = %#v, want uninstall to remove install row", loaded.Ledger.Installs)
	}
}

func TestUninstallBlocksModifiedOwnedCopy(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	stateDir := t.TempDir()
	manifest := filepath.Join("..", "..", "testdata", "m0", "manifests", "clawdapus-runtime.toml")
	if _, err := Apply(context.Background(), ApplyOptions{
		ManifestPath: manifest,
		Home:         home,
		Project:      project,
		StateDir:     stateDir,
		LockTimeout:  time.Second,
	}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(project, ".claw-skills", "desk-manager", "skills", "clawdapus-cli")
	if err := os.WriteFile(filepath.Join(target, "local-notes.md"), []byte("operator edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Uninstall(context.Background(), UninstallOptions{
		ManifestPath: manifest,
		Home:         home,
		Project:      project,
		StateDir:     stateDir,
		LockTimeout:  time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Actions) != 1 || result.Actions[0].Status != "blocked" {
		t.Fatalf("result actions = %#v, want blocked modified-copy removal", result.Actions)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("target should be preserved: %v", err)
	}

	forced, err := Uninstall(context.Background(), UninstallOptions{
		ManifestPath: manifest,
		Home:         home,
		Project:      project,
		StateDir:     stateDir,
		Force:        true,
		LockTimeout:  time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(forced.Actions) != 1 || forced.Actions[0].Status != "removed" {
		t.Fatalf("forced actions = %#v, want removed modified copy", forced.Actions)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("target should be removed with force, stat err=%v", err)
	}
}

// The critical destructive-safety invariant (§6.5): --force bypasses the digest-modified
// check but NEVER the ownership check. A target skiller does not own must survive
// uninstall even with --force. The ownership guard sits before the force branch, so a
// foreign/unmarked target is skipped, not deleted.
func TestUninstallForceDoesNotDeleteForeignTarget(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	stateDir := t.TempDir()
	manifest := filepath.Join("..", "..", "testdata", "m0", "manifests", "clawdapus-runtime.toml")
	target := filepath.Join(project, ".claw-skills", "desk-manager", "skills", "clawdapus-cli")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	// A foreign, non-skiller-owned copy already occupies the desired target (no marker).
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("---\nname: clawdapus-cli\n---\n\nforeign content not ours\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Uninstall(context.Background(), UninstallOptions{
		ManifestPath: manifest,
		Home:         home,
		Project:      project,
		StateDir:     stateDir,
		Force:        true,
		LockTimeout:  time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Actions) != 1 || result.Actions[0].Status == "removed" {
		t.Fatalf("--force must never remove a foreign target, got %#v", result.Actions)
	}
	if _, err := os.Stat(filepath.Join(target, "SKILL.md")); err != nil {
		t.Fatalf("foreign target must be preserved even with --force: %v", err)
	}
}

func TestUninstallSkipsSharedUnlessExplicit(t *testing.T) {
	home := t.TempDir()
	stateDir := t.TempDir()
	src := fixtureSource(t, "talking-stick")
	manifest := writeSingleSkillManifest(t, "talking-stick", "mostlydev:talking-stick", "talking-stick", src)
	if _, err := Apply(context.Background(), ApplyOptions{
		ManifestPath: manifest,
		Home:         home,
		StateDir:     stateDir,
		LockTimeout:  time.Second,
	}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, ".agents", "skills", "talking-stick")

	skipped, err := Uninstall(context.Background(), UninstallOptions{
		ManifestPath: manifest,
		Home:         home,
		StateDir:     stateDir,
		LockTimeout:  time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped.Actions) != 1 || skipped.Actions[0].Action != "skip-uninstall" || skipped.Actions[0].Status != "skipped" {
		t.Fatalf("result actions = %#v, want shared skip", skipped.Actions)
	}
	if _, err := os.Lstat(target); err != nil {
		t.Fatalf("shared target should remain: %v", err)
	}

	removed, err := Uninstall(context.Background(), UninstallOptions{
		ManifestPath: manifest,
		Home:         home,
		StateDir:     stateDir,
		Shared:       true,
		LockTimeout:  time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed.Actions) != 1 || removed.Actions[0].Status != "removed" {
		t.Fatalf("result actions = %#v, want explicit shared removal", removed.Actions)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("shared target should be removed, stat err=%v", err)
	}
}

func TestCleanupDuplicatesRemovesOnlyManagedSymlinks(t *testing.T) {
	home := t.TempDir()
	stateDir := t.TempDir()
	src := fixtureSource(t, "talking-stick")
	manifest := writeSingleSkillManifest(t, "talking-stick", "mostlydev:talking-stick", "talking-stick", src)
	managedDuplicate := filepath.Join(home, ".codex", "skills", "talking-stick")
	copyDuplicate := filepath.Join(home, ".grok", "skills", "talking-stick")
	foreignSource := filepath.Join(t.TempDir(), "foreign")
	foreignDuplicate := filepath.Join(home, ".config", "opencode", "skills", "talking-stick")
	if err := os.MkdirAll(filepath.Dir(managedDuplicate), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(src, managedDuplicate); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	copyDir(t, src, copyDuplicate)
	if err := os.MkdirAll(foreignSource, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreignSource, "SKILL.md"), []byte("---\nname: talking-stick\n---\n\nforeign\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(foreignDuplicate), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreignSource, foreignDuplicate); err != nil {
		t.Fatal(err)
	}

	result, err := CleanupDuplicates(context.Background(), CleanupOptions{
		ManifestPath: manifest,
		Home:         home,
		StateDir:     stateDir,
		LockTimeout:  time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertApplyResultSchema(t, result)
	var removed int
	for _, action := range result.Actions {
		if action.Status == "removed" {
			removed++
		}
	}
	if removed != 1 {
		t.Fatalf("removed actions = %d, want exactly one managed duplicate removed; actions=%#v", removed, result.Actions)
	}
	if _, err := os.Lstat(managedDuplicate); !os.IsNotExist(err) {
		t.Fatalf("managed duplicate should be removed, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(copyDuplicate, "SKILL.md")); err != nil {
		t.Fatalf("copy duplicate should be preserved: %v", err)
	}
	if _, err := os.Lstat(foreignDuplicate); err != nil {
		t.Fatalf("foreign symlink should be preserved: %v", err)
	}
}

func TestSyncPrunesUndeclaredMarkerOwnedCopy(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	stateDir := t.TempDir()
	manifest := filepath.Join("..", "..", "testdata", "m0", "manifests", "clawdapus-runtime.toml")
	if _, err := Apply(context.Background(), ApplyOptions{
		ManifestPath: manifest,
		Home:         home,
		Project:      project,
		StateDir:     stateDir,
		LockTimeout:  time.Second,
	}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(project, ".claw-skills", "desk-manager", "skills", "clawdapus-cli")
	emptyManifest := writeEmptyManifest(t, "clawdapus", "mostlydev")

	plan, err := PlanSync(SyncOptions{
		ManifestPath: emptyManifest,
		Home:         home,
		Project:      project,
		StateDir:     stateDir,
		LockTimeout:  time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Action != "remove-owned" || plan.Actions[0].Status != "dry-run" {
		t.Fatalf("sync dry-run actions = %#v, want one remove-owned dry-run", plan.Actions)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("sync dry-run should preserve target: %v", err)
	}

	result, err := Sync(context.Background(), SyncOptions{
		ManifestPath: emptyManifest,
		Home:         home,
		Project:      project,
		StateDir:     stateDir,
		LockTimeout:  time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Actions) != 1 || result.Actions[0].Status != "removed" {
		t.Fatalf("sync actions = %#v, want one removed stale install", result.Actions)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("sync should remove stale marker-owned target, stat err=%v", err)
	}
	loaded, err := state.Load(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Ledger.Installs) != 0 {
		t.Fatalf("installs = %#v, want sync to remove stale install row", loaded.Ledger.Installs)
	}
}

func TestM2RepresentativeManifestsApplyWithoutBlockedActions(t *testing.T) {
	cases := []struct {
		name     string
		manifest string
	}{
		{name: "talking-stick", manifest: "talking-stick.toml"},
		{name: "gnit", manifest: "gnit.toml"},
		{name: "our-ai", manifest: "our-self.toml"},
		{name: "clawdapus-runtime", manifest: "clawdapus-runtime.toml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Apply(context.Background(), ApplyOptions{
				ManifestPath: filepath.Join("..", "..", "testdata", "m0", "manifests", tc.manifest),
				Home:         t.TempDir(),
				Project:      t.TempDir(),
				StateDir:     t.TempDir(),
				LockTimeout:  time.Second,
			})
			if err != nil {
				t.Fatal(err)
			}
			assertApplyResultSchema(t, result)
			for _, action := range result.Actions {
				if action.Status == "failed" || action.Status == "blocked" || action.Status == "partially-satisfied" {
					t.Fatalf("unexpected blocked/failed action: %#v", action)
				}
			}
		})
	}
}

// The §11.4 gate requires idempotence proven across ALL four tool shapes, not just the
// copy-mode runtime case. This generalizes TestApplyIsIdempotent over the representative
// manifests and is the regression guard for the link-mode idempotence fix (an ours-symlink
// that resolves to the managed source must re-plan as a quiet no-op, never a spurious
// refresh). A second apply must leave every SKILL install a no-op with no duplicate ledger
// rows. Extras have separate coverage in TestApplyExtraIsIdempotent because they install
// file targets rather than skill directories.
func TestM2SkillInstallIdempotentAcrossShapes(t *testing.T) {
	cases := []struct{ name, manifest string }{
		{"talking-stick", "talking-stick.toml"},
		{"gnit", "gnit.toml"},
		{"our-ai", "our-self.toml"},
		{"clawdapus-runtime", "clawdapus-runtime.toml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := ApplyOptions{
				ManifestPath: filepath.Join("..", "..", "testdata", "m0", "manifests", tc.manifest),
				Home:         t.TempDir(),
				Project:      t.TempDir(),
				StateDir:     t.TempDir(),
				LockTimeout:  time.Second,
			}
			if _, err := Apply(context.Background(), opts); err != nil {
				t.Fatal(err)
			}
			firstInstalls := countLedgerInstalls(t, opts.StateDir)

			second, err := Apply(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			for _, action := range second.Actions {
				if action.Skill == nil {
					continue
				}
				if action.Status != "skipped" {
					t.Fatalf("re-apply not idempotent for skill action: %#v", action)
				}
			}
			if got := countLedgerInstalls(t, opts.StateDir); got != firstInstalls {
				t.Fatalf("ledger installs changed on re-apply: %d -> %d (duplication)", firstInstalls, got)
			}
		})
	}
}

func countLedgerInstalls(t *testing.T, stateDir string) int {
	t.Helper()
	loaded, err := state.Load(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	return len(loaded.Ledger.Installs)
}

func findResultExtra(result install.Result, id string) *install.ActionResult {
	for i := range result.Actions {
		if result.Actions[i].Extra != nil && result.Actions[i].Extra.ID == id {
			return &result.Actions[i]
		}
	}
	return nil
}

func findResultSkill(result install.Result, canonicalID, targetPath string) *install.ActionResult {
	for i := range result.Actions {
		if result.Actions[i].Skill != nil && result.Actions[i].Skill.CanonicalID == canonicalID && result.Actions[i].Target.Path == targetPath {
			return &result.Actions[i]
		}
	}
	return nil
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

func writeEmptyManifest(t *testing.T, owner, namespace string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "skiller.toml")
	data := "schema = \"skiller-install.v1\"\n" +
		"owner = " + strconv.Quote(owner) + "\n" +
		"namespace = " + strconv.Quote(namespace) + "\n" +
		"default_mode = \"link\"\n"
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

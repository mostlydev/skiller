package planner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mostlydev/skiller/internal/contract"
)

func TestRegistryIncludesVerifiedFleet(t *testing.T) {
	catalog, err := LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, h := range catalog.Harnesses {
		got[h.ID] = true
	}
	for _, id := range []string{"agents", "claude-code", "codex", "antigravity", "grok", "opencode"} {
		if !got[id] {
			t.Fatalf("registry missing %s", id)
		}
	}
}

func TestTalkingStickPlanRepresentsSharedClaudeAndExtra(t *testing.T) {
	home := t.TempDir()
	plan := mustPlan(t, "talking-stick.toml", home)

	assertAction(t, plan, "mostlydev:talking-stick", filepath.Join(home, ".agents/skills/talking-stick"), "install-link")
	assertAction(t, plan, "mostlydev:talking-stick", filepath.Join(home, ".claude/skills/talking-stick"), "install-link")
	if got := findExtraAction(plan, "grok-session-hook"); got == nil || got.Action != "install-extra" {
		t.Fatalf("missing grok-session-hook install-extra action: %#v", got)
	}
}

func TestDigestMatchingDirectInstallIsSatisfiedByForeign(t *testing.T) {
	home := t.TempDir()
	src := fixturePath(t, "sources/talking-stick")
	dst := filepath.Join(home, ".agents/skills/talking-stick")
	copyDir(t, src, dst)

	plan := mustPlan(t, "talking-stick.toml", home)
	action := assertAction(t, plan, "mostlydev:talking-stick", dst, "satisfied-by-foreign")
	if action.Status != "dry-run" {
		t.Fatalf("status = %q, want dry-run", action.Status)
	}
	if action.Ownership.DigestMatch == nil || !*action.Ownership.DigestMatch {
		t.Fatalf("digest_match = %#v, want true", action.Ownership.DigestMatch)
	}
}

func TestProprietaryDuplicateProducesPartialSatisfaction(t *testing.T) {
	home := t.TempDir()
	src := fixturePath(t, "sources/talking-stick")
	copyDir(t, src, filepath.Join(home, ".codex/skills/talking-stick"))

	plan := mustPlan(t, "talking-stick.toml", home)
	action := assertAction(t, plan, "mostlydev:talking-stick", filepath.Join(home, ".agents/skills/talking-stick"), "partially-satisfied")
	if action.Status != "blocked" {
		t.Fatalf("status = %q, want blocked", action.Status)
	}
	if len(plan.Conflicts) == 0 {
		t.Fatal("expected conflict for partial satisfaction")
	}
}

func TestOwnedStaleCopyPlansRefresh(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, ".agents/skills/talking-stick")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("---\nname: talking-stick\n---\n\nold\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldDigest, err := digestPath(target)
	if err != nil {
		t.Fatal(err)
	}
	writeMarker(t, target, "talking-stick", "mostlydev:talking-stick", oldDigest)

	plan := mustPlan(t, "talking-stick.toml", home)
	assertAction(t, plan, "mostlydev:talking-stick", target, "refresh")
}

func TestOwnedModifiedCopyBlocks(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, ".agents/skills/talking-stick")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("---\nname: talking-stick\n---\n\nold\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldDigest, err := digestPath(target)
	if err != nil {
		t.Fatal(err)
	}
	writeMarker(t, target, "talking-stick", "mostlydev:talking-stick", oldDigest)
	if err := os.WriteFile(filepath.Join(target, "local-notes.md"), []byte("operator edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan := mustPlan(t, "talking-stick.toml", home)
	action := assertAction(t, plan, "mostlydev:talking-stick", target, "block-conflict")
	if action.Status != "blocked" {
		t.Fatalf("status = %q, want blocked", action.Status)
	}
}

func TestRuntimeTargetDirDefaultsToCopy(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	manifest := filepath.Join(fixturePath(t, "manifests"), "clawdapus.toml")
	plan, err := Build(Options{ManifestPath: manifest, Home: home, Project: project})
	if err != nil {
		t.Fatal(err)
	}
	SortPlan(&plan)
	runtimePath := filepath.Join(project, ".claw-skills/desk-manager/skills/clawdapus-cli")
	action := assertAction(t, plan, "mostlydev:clawdapus-cli", runtimePath, "install-copy")
	if action.Target.Scope != "runtime" {
		t.Fatalf("scope = %q, want runtime", action.Target.Scope)
	}
}

func TestManifestNamespaceCollisionBlocksSecondPlan(t *testing.T) {
	home := t.TempDir()
	plan := mustPlan(t, "namespace-collision.toml", home)
	var blocked int
	for _, action := range plan.Actions {
		if action.Action == "block-conflict" && action.Status == "blocked" {
			blocked++
		}
	}
	if blocked != 1 {
		t.Fatalf("blocked actions = %d, want 1; plan=%#v", blocked, plan.Actions)
	}
	if len(plan.Conflicts) != 1 || plan.Conflicts[0].Status != "namespace-collision" {
		t.Fatalf("conflicts = %#v, want namespace-collision", plan.Conflicts)
	}
}

func mustPlan(t *testing.T, manifestName, home string) contract.Plan {
	t.Helper()
	manifest := filepath.Join(fixturePath(t, "manifests"), manifestName)
	plan, err := Build(Options{ManifestPath: manifest, Home: home})
	if err != nil {
		t.Fatal(err)
	}
	SortPlan(&plan)
	return plan
}

func fixturePath(t *testing.T, rel string) string {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "m0", filepath.FromSlash(rel))
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func assertAction(t *testing.T, plan contract.Plan, canonicalID, targetPath, actionName string) contract.PlanAction {
	t.Helper()
	for _, action := range plan.Actions {
		if action.Skill == nil {
			continue
		}
		if action.Skill.CanonicalID == canonicalID && action.Target.Path == targetPath {
			if action.Action != actionName {
				t.Fatalf("action for %s at %s = %s, want %s", canonicalID, targetPath, action.Action, actionName)
			}
			return action
		}
	}
	t.Fatalf("missing action %s for %s at %s; actions=%#v", actionName, canonicalID, targetPath, plan.Actions)
	return contract.PlanAction{}
}

func findExtraAction(plan contract.Plan, id string) *contract.PlanAction {
	for i := range plan.Actions {
		if plan.Actions[i].Extra != nil && plan.Actions[i].Extra.ID == id {
			return &plan.Actions[i]
		}
	}
	return nil
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	if err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
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

func writeMarker(t *testing.T, dir, owner, canonicalID, installedDigest string) {
	t.Helper()
	marker := map[string]any{
		"schema": "skiller-install-marker.v1",
		"installer": map[string]string{
			"name":    "skiller",
			"version": "0.0.0-test",
		},
		"owner":                       owner,
		"canonical_id":                canonicalID,
		"name":                        filepath.Base(dir),
		"mode":                        "copy",
		"target_kind":                 "shared",
		"scope":                       "host",
		"installed_at":                "2026-06-15T00:00:00Z",
		"source_digest_at_install":    installedDigest,
		"installed_digest_at_install": installedDigest,
	}
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(dir, ".skiller-install.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

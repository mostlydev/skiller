package planner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/mostlydev/skiller/internal/contract"
	planpkg "github.com/mostlydev/skiller/pkg/plan"
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

func TestExistingExtraTargetIsObserved(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, ".grok/hooks/talking-stick-session.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(`{"existing":true}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan := mustPlan(t, "talking-stick.toml", home)
	action := findExtraAction(plan, "grok-session-hook")
	if action == nil {
		t.Fatal("missing grok-session-hook install-extra action")
	}
	if action.Ownership.Class != "foreign-unmanaged" {
		t.Fatalf("extra ownership class = %q, want foreign-unmanaged; ownership=%#v", action.Ownership.Class, action.Ownership)
	}
	if action.Ownership.Path != target {
		t.Fatalf("extra ownership path = %q, want %q", action.Ownership.Path, target)
	}
}

func TestMatchingExtraTargetPlansNoOp(t *testing.T) {
	home := t.TempDir()
	source := fixturePath(t, "sources/talking-stick/grok-session.json")
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, ".grok/hooks/talking-stick-session.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		t.Fatal(err)
	}

	plan := mustPlan(t, "talking-stick.toml", home)
	action := findExtraAction(plan, "grok-session-hook")
	if action == nil {
		t.Fatal("missing grok-session-hook action")
	}
	if action.Action != "no-op" {
		t.Fatalf("extra action = %q, want no-op; ownership=%#v", action.Action, action.Ownership)
	}
	if action.Ownership.DigestMatch == nil || !*action.Ownership.DigestMatch {
		t.Fatalf("digest_match = %#v, want true", action.Ownership.DigestMatch)
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
	assertStrings(t, action.ConflictModes, []string{"block", "skip", "adopt-existing"})
}

func TestResolutionAdoptsPartialSatisfactionWithoutDuplicate(t *testing.T) {
	home := t.TempDir()
	src := fixturePath(t, "sources/talking-stick")
	duplicate := filepath.Join(home, ".codex/skills/talking-stick")
	copyDir(t, src, duplicate)

	plan := mustPlanWithOptions(t, "talking-stick.toml", home, func(opts *Options) {
		opts.OnConflict = "adopt-existing"
	})
	action := assertAction(t, plan, "mostlydev:talking-stick", duplicate, "adopt-existing")
	if len(action.PlannedWrites) != 0 {
		t.Fatalf("planned writes = %#v, want none for ledger-only duplicate adoption", action.PlannedWrites)
	}
	if action.Ownership.Path != duplicate {
		t.Fatalf("ownership path = %q, want duplicate path %q", action.Ownership.Path, duplicate)
	}
	conflict := assertConflict(t, plan, "partial-satisfaction", "adopt-existing")
	assertStrings(t, conflict.SafeChoices, []string{"block", "skip", "adopt-existing"})
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
	assertStrings(t, action.ConflictModes, []string{"block", "skip", "replace-owned", "force-replace"})
}

func TestResolutionReplacesModifiedOwnedCopy(t *testing.T) {
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

	plan := mustPlanWithOptions(t, "talking-stick.toml", home, func(opts *Options) {
		opts.OnConflict = "replace-owned"
	})
	action := assertAction(t, plan, "mostlydev:talking-stick", target, "refresh")
	if len(action.PlannedWrites) == 0 {
		t.Fatalf("planned writes = %#v, want refresh write", action.PlannedWrites)
	}
	assertConflict(t, plan, "modified-owned-copy", "replace-owned")
}

func TestResolutionAdoptsForeignTargetWithoutClaimingDigestMatch(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, ".agents/skills/talking-stick")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("---\nname: local-talking-stick\n---\n\nlocal\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan := mustPlanWithOptions(t, "talking-stick.toml", home, func(opts *Options) {
		opts.OnConflict = "adopt-existing"
	})
	action := assertAction(t, plan, "mostlydev:talking-stick", target, "adopt-existing")
	if action.Ownership.DigestMatch == nil || *action.Ownership.DigestMatch {
		t.Fatalf("digest_match = %#v, want false for mismatching adopted foreign target", action.Ownership.DigestMatch)
	}
	if len(action.PlannedWrites) != 0 {
		t.Fatalf("planned writes = %#v, want none for ledger-only foreign adoption", action.PlannedWrites)
	}
	conflict := assertConflict(t, plan, "foreign-target", "adopt-existing")
	assertStrings(t, conflict.SafeChoices, []string{"block", "skip", "adopt-existing", "rename", "force-replace"})
}

func TestForceReplaceRequiresForce(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, ".agents/skills/talking-stick")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("---\nname: local-talking-stick\n---\n\nlocal\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan := mustPlanWithOptions(t, "talking-stick.toml", home, func(opts *Options) {
		opts.OnConflict = "force-replace"
	})
	action := assertAction(t, plan, "mostlydev:talking-stick", target, "block-conflict")
	if action.Reason != "force-replace resolution requires --force" {
		t.Fatalf("reason = %q", action.Reason)
	}
	assertConflict(t, plan, "foreign-target", "")
}

func TestForceReplacePlansForeignReplacementWithForce(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, ".agents/skills/talking-stick")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("---\nname: local-talking-stick\n---\n\nlocal\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan := mustPlanWithOptions(t, "talking-stick.toml", home, func(opts *Options) {
		opts.OnConflict = "force-replace"
		opts.Force = true
	})
	action := assertAction(t, plan, "mostlydev:talking-stick", target, "force-replace")
	if !plan.Inputs.Force {
		t.Fatalf("plan input force = false, want true")
	}
	if action.Mode.Effective != "copy" {
		t.Fatalf("effective mode = %q, want copy", action.Mode.Effective)
	}
	assertWrites(t, action.PlannedWrites, []contract.PlannedWrite{
		{Kind: "copy", Path: target},
		{Kind: "marker", Path: filepath.Join(target, ".skiller-install.json")},
	})
	assertConflict(t, plan, "foreign-target", "force-replace")
}

func TestGlobalForceReplaceRefusesMultipleTargets(t *testing.T) {
	home := t.TempDir()
	talkingStick := filepath.Join(home, ".agents/skills/talking-stick")
	gnit := filepath.Join(home, ".agents/skills/gnit")
	if err := os.MkdirAll(talkingStick, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(gnit, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(talkingStick, "SKILL.md"), []byte("---\nname: local-talking-stick\n---\n\nlocal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gnit, "SKILL.md"), []byte("---\nname: local-gnit\n---\n\nlocal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := writeTwoDistinctSkillManifest(t, fixturePath(t, "sources/talking-stick"), fixturePath(t, "sources/gnit"))
	plan, err := Build(Options{
		ManifestPath: manifest,
		Home:         home,
		OnConflict:   "force-replace",
		Force:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	SortPlan(&plan)

	for _, target := range []string{talkingStick, gnit} {
		action := assertAnyActionAt(t, plan, target, "block-conflict")
		if action.Reason != "global force-replace refused for multiple destructive conflicts; use per-conflict resolutions" {
			t.Fatalf("reason = %q", action.Reason)
		}
	}
}

func TestRuntimeTargetDirDefaultsToCopy(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	manifest := filepath.Join(fixturePath(t, "manifests"), "clawdapus-runtime.toml")
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
	assertStrings(t, plan.Conflicts[0].SafeChoices, []string{"block", "skip", "rename"})
}

func TestResolutionSkipsNamespaceCollision(t *testing.T) {
	home := t.TempDir()
	plan := mustPlanWithOptions(t, "namespace-collision.toml", home, func(opts *Options) {
		opts.OnConflict = "skip"
	})
	action := assertAction(t, plan, "beta:debugging", filepath.Join(home, ".agents/skills/debugging"), "no-op")
	if len(action.PlannedWrites) != 0 {
		t.Fatalf("planned writes = %#v, want none for skipped conflict", action.PlannedWrites)
	}
	assertConflict(t, plan, "namespace-collision", "skip")
}

func TestResolutionMapOverridesDefaultPolicyByConflictID(t *testing.T) {
	home := t.TempDir()
	blocked := mustPlan(t, "namespace-collision.toml", home)
	conflict := assertConflict(t, blocked, "namespace-collision", "")

	plan := mustPlanWithOptions(t, "namespace-collision.toml", home, func(opts *Options) {
		opts.OnConflict = "block"
		opts.Resolutions = map[string]planpkg.Resolution{
			conflict.ID: {Policy: "skip"},
		}
	})
	assertAction(t, plan, "beta:debugging", filepath.Join(home, ".agents/skills/debugging"), "no-op")
	assertConflict(t, plan, "namespace-collision", "skip")
}

func TestUnknownResolutionIDWarnsButDoesNotFail(t *testing.T) {
	home := t.TempDir()
	plan := mustPlanWithOptions(t, "talking-stick.toml", home, func(opts *Options) {
		opts.Resolutions = map[string]planpkg.Resolution{
			"missing-conflict-id": {Policy: "skip"},
		}
	})
	if len(plan.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v, want one warning for stale resolution", plan.Diagnostics)
	}
	if plan.Diagnostics[0].Level != "warning" || plan.Diagnostics[0].Path != "missing-conflict-id" {
		t.Fatalf("diagnostic = %#v", plan.Diagnostics[0])
	}
}

func TestInapplicableResolutionRemainsBlocked(t *testing.T) {
	home := t.TempDir()
	plan := mustPlanWithOptions(t, "namespace-collision.toml", home, func(opts *Options) {
		opts.OnConflict = "adopt-existing"
	})
	action := assertAction(t, plan, "beta:debugging", filepath.Join(home, ".agents/skills/debugging"), "block-conflict")
	if action.Status != "blocked" {
		t.Fatalf("status = %q, want blocked", action.Status)
	}
	if action.Reason != "resolution adopt-existing is not applicable to namespace-collision" {
		t.Fatalf("reason = %q", action.Reason)
	}
	assertConflict(t, plan, "namespace-collision", "")
}

func TestRenameResolutionUsesExplicitSlug(t *testing.T) {
	home := t.TempDir()
	plan := mustPlanWithOptions(t, "namespace-collision.toml", home, func(opts *Options) {
		opts.OnConflict = "rename"
		opts.InstallSlug = "debugging-beta"
	})
	assertAction(t, plan, "alpha:debugging", filepath.Join(home, ".agents/skills/debugging"), "install-link")
	renamed := assertAction(t, plan, "beta:debugging", filepath.Join(home, ".agents/skills/debugging-beta"), "install-link")
	if renamed.Skill.InstallSlug != "debugging-beta" {
		t.Fatalf("install_slug = %q, want debugging-beta", renamed.Skill.InstallSlug)
	}
	if plan.Inputs.InstallSlug != "debugging-beta" {
		t.Fatalf("plan input install_slug = %q, want debugging-beta", plan.Inputs.InstallSlug)
	}
	if len(plan.Conflicts) != 0 {
		t.Fatalf("conflicts = %#v, want none after rename", plan.Conflicts)
	}
}

func TestRenameResolutionRequiresInstallSlug(t *testing.T) {
	home := t.TempDir()
	plan := mustPlanWithOptions(t, "namespace-collision.toml", home, func(opts *Options) {
		opts.OnConflict = "rename"
	})
	action := assertAction(t, plan, "beta:debugging", filepath.Join(home, ".agents/skills/debugging"), "block-conflict")
	if action.Reason != "rename resolution requires --install-slug" {
		t.Fatalf("reason = %q", action.Reason)
	}
	assertConflict(t, plan, "namespace-collision", "")
}

func TestRenameResolutionRefusesFrontmatterCollision(t *testing.T) {
	home := t.TempDir()
	source := fixturePath(t, "sources/talking-stick")
	manifest := writeTwoSkillManifest(t, source, source)

	plan, err := Build(Options{
		ManifestPath: manifest,
		Home:         home,
		OnConflict:   "rename",
		InstallSlug:  "talking-stick-beta",
	})
	if err != nil {
		t.Fatal(err)
	}
	SortPlan(&plan)
	action := assertAction(t, plan, "beta:talking-stick", filepath.Join(home, ".agents/skills/talking-stick-beta"), "block-conflict")
	if action.Reason != "renamed target still conflicts after applying install_slug" {
		t.Fatalf("reason = %q", action.Reason)
	}
	conflict := assertConflict(t, plan, "frontmatter-collision", "")
	assertStrings(t, conflict.SafeChoices, []string{"block", "skip"})
}

func mustPlan(t *testing.T, manifestName, home string) contract.Plan {
	t.Helper()
	return mustPlanWithOptions(t, manifestName, home, nil)
}

func mustPlanWithOptions(t *testing.T, manifestName, home string, configure func(*Options)) contract.Plan {
	t.Helper()
	manifest := filepath.Join(fixturePath(t, "manifests"), manifestName)
	opts := Options{ManifestPath: manifest, Home: home}
	if configure != nil {
		configure(&opts)
	}
	plan, err := Build(opts)
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

func assertConflict(t *testing.T, plan contract.Plan, status, resolution string) contract.PlanConflict {
	t.Helper()
	for _, conflict := range plan.Conflicts {
		if conflict.Status == status {
			if conflict.Resolution != resolution {
				t.Fatalf("conflict resolution for %s = %q, want %q; conflict=%#v", status, conflict.Resolution, resolution, conflict)
			}
			return conflict
		}
	}
	t.Fatalf("missing conflict status %q; conflicts=%#v", status, plan.Conflicts)
	return contract.PlanConflict{}
}

func assertStrings(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("strings = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("strings = %#v, want %#v", got, want)
		}
	}
}

func assertWrites(t *testing.T, got, want []contract.PlannedWrite) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("writes = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("writes = %#v, want %#v", got, want)
		}
	}
}

func findExtraAction(plan contract.Plan, id string) *contract.PlanAction {
	for i := range plan.Actions {
		if plan.Actions[i].Extra != nil && plan.Actions[i].Extra.ID == id {
			return &plan.Actions[i]
		}
	}
	return nil
}

func assertAnyActionAt(t *testing.T, plan contract.Plan, targetPath, actionName string) contract.PlanAction {
	t.Helper()
	for _, action := range plan.Actions {
		if action.Target.Path == targetPath {
			if action.Action != actionName {
				t.Fatalf("action at %s = %s, want %s", targetPath, action.Action, actionName)
			}
			return action
		}
	}
	t.Fatalf("missing action %s at %s; actions=%#v", actionName, targetPath, plan.Actions)
	return contract.PlanAction{}
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

func writeTwoSkillManifest(t *testing.T, firstSource, secondSource string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "skiller.toml")
	data := "schema = \"skiller-install.v1\"\n" +
		"owner = \"rename-fixture\"\n" +
		"namespace = \"alpha\"\n" +
		"default_mode = \"link\"\n\n" +
		"[[skills]]\n" +
		"name = \"talking-stick\"\n" +
		"canonical_id = \"alpha:talking-stick\"\n" +
		"install_slug = \"talking-stick\"\n" +
		"source = " + strconv.Quote(firstSource) + "\n" +
		"targets = [\"agents\"]\n" +
		"mode = \"link\"\n\n" +
		"[[skills]]\n" +
		"name = \"talking-stick\"\n" +
		"canonical_id = \"beta:talking-stick\"\n" +
		"install_slug = \"talking-stick\"\n" +
		"source = " + strconv.Quote(secondSource) + "\n" +
		"targets = [\"agents\"]\n" +
		"mode = \"link\"\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeTwoDistinctSkillManifest(t *testing.T, talkingStickSource, gnitSource string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "skiller.toml")
	data := "schema = \"skiller-install.v1\"\n" +
		"owner = \"multi-force-fixture\"\n" +
		"namespace = \"mostlydev\"\n" +
		"default_mode = \"link\"\n\n" +
		"[[skills]]\n" +
		"name = \"talking-stick\"\n" +
		"canonical_id = \"mostlydev:talking-stick\"\n" +
		"install_slug = \"talking-stick\"\n" +
		"source = " + strconv.Quote(talkingStickSource) + "\n" +
		"targets = [\"agents\"]\n" +
		"mode = \"link\"\n\n" +
		"[[skills]]\n" +
		"name = \"gnit\"\n" +
		"canonical_id = \"mostlydev:gnit\"\n" +
		"install_slug = \"gnit\"\n" +
		"source = " + strconv.Quote(gnitSource) + "\n" +
		"targets = [\"agents\"]\n" +
		"mode = \"link\"\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

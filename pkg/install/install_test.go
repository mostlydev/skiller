package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mostlydev/skiller/internal/contract"
	"github.com/mostlydev/skiller/internal/digest"
	"github.com/mostlydev/skiller/internal/fsutil"
	"github.com/mostlydev/skiller/internal/schemajson"
)

func TestApplyInstallCopyWritesMarker(t *testing.T) {
	source := makeSkillSource(t)
	target := filepath.Join(t.TempDir(), "skills", "demo")
	plan := singleSkillPlan(t, source, target, "install-copy", "copy")
	result, err := Apply(plan, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	if result.Actions[0].Status != "installed" {
		t.Fatalf("status = %q, want installed", result.Actions[0].Status)
	}
	assertFile(t, filepath.Join(target, "SKILL.md"), "---\nname: demo\n---\n")
	markerPath := filepath.Join(target, ".skiller-install.json")
	validateMarker(t, markerPath)
	marker := readMarker(t, markerPath)
	if marker["owner"] != "tester" || marker["canonical_id"] != "test:demo" {
		t.Fatalf("unexpected marker: %#v", marker)
	}
}

func TestApplyInstallLinkFallbackWritesCopyMarker(t *testing.T) {
	source := makeSkillSource(t)
	target := filepath.Join(t.TempDir(), "skills", "demo")
	plan := singleSkillPlan(t, source, target, "install-link", "link")
	opts := testOptions()
	opts.FS.Symlink = func(oldname, newname string) error { return os.ErrPermission }
	result, err := Apply(plan, opts)
	if err != nil {
		t.Fatal(err)
	}
	action := result.Actions[0]
	if !action.FallbackApplied || action.EffectiveMode != "copy" {
		t.Fatalf("action = %#v, want copy fallback", action)
	}
	validateMarker(t, filepath.Join(target, ".skiller-install.json"))
}

func TestApplyInstallExtraWritesSidecarMarker(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.json")
	target := filepath.Join(root, "hooks", "hook.json")
	writeFile(t, source, `{"ok":true}`+"\n")
	plan := contract.Plan{
		Schema: "skiller-plan.v1",
		Inputs: contract.PlanInputs{ManifestPath: "/manifest.toml", Namespace: "test", OnConflict: "block"},
		Actions: []contract.PlanAction{{
			ID:        "act-extra",
			Action:    "install-extra",
			Status:    "dry-run",
			Extra:     &contract.PlanExtra{ID: "hook", Source: source, Target: target},
			Target:    contract.PlanTarget{ID: "grok", Kind: "extra", Scope: "host", Root: filepath.Dir(target), Path: target, LockID: "target:extra"},
			Mode:      contract.PlanMode{Requested: "copy", Effective: "copy"},
			Ownership: contract.ObservedOwnership{Class: "absent", Path: target},
			PlannedWrites: []contract.PlannedWrite{
				{Kind: "file", Path: target},
				{Kind: "sidecar-marker", Path: target + ".skiller-install.json"},
			},
		}},
	}
	result, err := Apply(plan, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	if result.Actions[0].Status != "installed" {
		t.Fatalf("status = %q, want installed", result.Actions[0].Status)
	}
	assertFile(t, target, `{"ok":true}`+"\n")
	validateMarker(t, target+".skiller-install.json")
}

func TestApplyNoOpAndBlockedDoNotWrite(t *testing.T) {
	target := filepath.Join(t.TempDir(), "skills", "demo")
	plan := contract.Plan{Actions: []contract.PlanAction{
		{ID: "noop", Action: "no-op", Target: contract.PlanTarget{Path: target}},
		{ID: "blocked", Action: "block-conflict", Target: contract.PlanTarget{Path: target}, Reason: "blocked"},
	}}
	result, err := Apply(plan, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	if result.Actions[0].Status != "skipped" || result.Actions[1].Status != "blocked" {
		t.Fatalf("actions = %#v", result.Actions)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("target should not be written, stat err=%v", err)
	}
}

func singleSkillPlan(t *testing.T, source, target, actionName, mode string) contract.Plan {
	t.Helper()
	sourceDigest, err := digest.Path(source)
	if err != nil {
		t.Fatal(err)
	}
	return contract.Plan{
		Schema: "skiller-plan.v1",
		Inputs: contract.PlanInputs{
			ManifestPath: "/manifest.toml",
			Namespace:    "test",
			OnConflict:   "block",
		},
		Sources: []contract.SourceSnapshot{{
			ID:             "source-001",
			SourceKind:     "file",
			OriginalSpec:   source,
			CanonicalURI:   "file://" + filepath.ToSlash(source),
			SourceKey:      "file:test",
			SourceStatus:   "refreshed",
			LocalCachePath: source,
			SourceRealpath: source,
			SourceDigest:   sourceDigest,
		}},
		Actions: []contract.PlanAction{{
			ID:        "act-demo",
			Action:    actionName,
			Status:    "dry-run",
			Skill:     &contract.PlanSkill{CanonicalID: "test:demo", Namespace: "test", Name: "demo", InstallSlug: "demo", FrontmatterName: "demo"},
			SourceID:  "source-001",
			Target:    contract.PlanTarget{ID: "agents", Kind: "shared", Scope: "host", Root: filepath.Dir(target), Path: target, LockID: "target:demo"},
			Mode:      contract.PlanMode{Requested: mode, Effective: mode},
			Ownership: contract.ObservedOwnership{Class: "absent", Path: target},
			PlannedWrites: []contract.PlannedWrite{
				{Kind: mode, Path: target},
			},
		}},
	}
}

func testOptions() Options {
	return Options{
		Owner:     "tester",
		Namespace: "test",
		Version:   "test",
		Now:       func() time.Time { return time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC) },
		FS:        fsutil.Options{Suffix: func() string { return "test" }},
	}
}

func makeSkillSource(t *testing.T) string {
	t.Helper()
	source := filepath.Join(t.TempDir(), "source")
	writeFile(t, filepath.Join(source, "SKILL.md"), "---\nname: demo\n---\n")
	return source
}

func validateMarker(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := schemajson.Validate("marker.schema.json", data); err != nil {
		t.Fatalf("marker schema validation: %v\n%s", err, data)
	}
}

func readMarker(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var marker map[string]any
	if err := json.Unmarshal(data, &marker); err != nil {
		t.Fatal(err)
	}
	return marker
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, data, want)
	}
}

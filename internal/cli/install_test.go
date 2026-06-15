package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mostlydev/skiller/internal/contract"
	"github.com/mostlydev/skiller/internal/schemajson"
	"github.com/mostlydev/skiller/pkg/install"
	statepkg "github.com/mostlydev/skiller/pkg/state"
)

func TestInstallDryRunMatchesPlan(t *testing.T) {
	manifest := filepath.Join("..", "..", "testdata", "m0", "manifests", "talking-stick.toml")
	home := "/skiller/golden/home"
	var planOut bytes.Buffer
	if err := Run([]string{"plan", "--manifest", manifest, "--home", home, "--json"}, &planOut, &bytes.Buffer{}); err != nil {
		t.Fatalf("plan: %v", err)
	}
	var installOut bytes.Buffer
	if err := Run([]string{"install", "--manifest", manifest, "--home", home, "--dry-run", "--json"}, &installOut, &bytes.Buffer{}); err != nil {
		t.Fatalf("install --dry-run: %v", err)
	}
	if planOut.String() != installOut.String() {
		t.Fatalf("install --dry-run did not match plan\n--- plan ---\n%s\n--- install ---\n%s", planOut.String(), installOut.String())
	}
}

func TestInstallWritesResultAndReturnsErrorForBlockedAction(t *testing.T) {
	manifest := filepath.Join("..", "..", "testdata", "m0", "manifests", "namespace-collision.toml")
	home := t.TempDir()
	stateDir := t.TempDir()
	var stdout bytes.Buffer
	err := Run([]string{"install", "--manifest", manifest, "--home", home, "--state-dir", stateDir, "--json"}, &stdout, &bytes.Buffer{})
	if err == nil {
		t.Fatal("install returned nil error, want non-zero action status error")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("error = %q, want blocked action summary", err.Error())
	}
	if err := schemajson.Validate("apply-result.schema.json", stdout.Bytes()); err != nil {
		t.Fatalf("apply-result schema: %v\n%s", err, stdout.String())
	}
	var result install.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	var blocked int
	for _, action := range result.Actions {
		if action.Status == "blocked" {
			blocked++
		}
	}
	if blocked == 0 {
		t.Fatalf("result has no blocked action: %#v", result.Actions)
	}
}

func TestPlanRenameInstallSlugFlag(t *testing.T) {
	manifest := filepath.Join("..", "..", "testdata", "m0", "manifests", "namespace-collision.toml")
	home := "/skiller/golden/home"
	var stdout bytes.Buffer
	err := Run([]string{
		"plan",
		"--manifest", manifest,
		"--home", home,
		"--on-conflict", "rename",
		"--install-slug", "debugging-beta",
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("plan rename: %v", err)
	}
	if err := schemajson.Validate("plan.schema.json", stdout.Bytes()); err != nil {
		t.Fatalf("plan schema: %v\n%s", err, stdout.String())
	}
	var plan contract.Plan
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Inputs.InstallSlug != "debugging-beta" {
		t.Fatalf("install_slug = %q, want debugging-beta", plan.Inputs.InstallSlug)
	}
	if len(plan.Conflicts) != 0 {
		t.Fatalf("conflicts = %#v, want none after rename", plan.Conflicts)
	}
}

func TestPlanForceFlag(t *testing.T) {
	manifest := filepath.Join("..", "..", "testdata", "m0", "manifests", "talking-stick.toml")
	home := "/skiller/golden/home"
	var stdout bytes.Buffer
	err := Run([]string{
		"plan",
		"--manifest", manifest,
		"--home", home,
		"--on-conflict", "force-replace",
		"--force",
		"--json",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("plan force-replace: %v", err)
	}
	if err := schemajson.Validate("plan.schema.json", stdout.Bytes()); err != nil {
		t.Fatalf("plan schema: %v\n%s", err, stdout.String())
	}
	var plan contract.Plan
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if !plan.Inputs.Force {
		t.Fatalf("force input = false, want true")
	}
}

func TestStateRepairWritesStateLedger(t *testing.T) {
	manifest := filepath.Join("..", "..", "testdata", "m0", "manifests", "clawdapus-runtime.toml")
	home := t.TempDir()
	project := t.TempDir()
	stateDir := t.TempDir()
	var installOut bytes.Buffer
	if err := Run([]string{"install", "--manifest", manifest, "--home", home, "--project", project, "--state-dir", stateDir, "--json"}, &installOut, &bytes.Buffer{}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := os.Remove(filepath.Join(stateDir, "state.json")); err != nil {
		t.Fatal(err)
	}

	var repairOut bytes.Buffer
	if err := Run([]string{"state", "repair", "--manifest", manifest, "--home", home, "--project", project, "--state-dir", stateDir, "--json"}, &repairOut, &bytes.Buffer{}); err != nil {
		t.Fatalf("state repair: %v", err)
	}
	if err := schemajson.Validate("state.schema.json", repairOut.Bytes()); err != nil {
		t.Fatalf("state schema: %v\n%s", err, repairOut.String())
	}
	var ledger statepkg.Ledger
	if err := json.Unmarshal(repairOut.Bytes(), &ledger); err != nil {
		t.Fatal(err)
	}
	if len(ledger.Installs) != 1 || ledger.Installs[0].Status != "installed" {
		t.Fatalf("installs = %#v, want one repaired installed target", ledger.Installs)
	}
}

func TestUninstallRemovesOwnedRuntimeTarget(t *testing.T) {
	manifest := filepath.Join("..", "..", "testdata", "m0", "manifests", "clawdapus-runtime.toml")
	home := t.TempDir()
	project := t.TempDir()
	stateDir := t.TempDir()
	if err := Run([]string{"install", "--manifest", manifest, "--home", home, "--project", project, "--state-dir", stateDir, "--json"}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("install: %v", err)
	}
	target := filepath.Join(project, ".claw-skills", "desk-manager", "skills", "clawdapus-cli")
	var dryRunOut bytes.Buffer
	if err := Run([]string{"uninstall", "--manifest", manifest, "--home", home, "--project", project, "--state-dir", stateDir, "--dry-run", "--json"}, &dryRunOut, &bytes.Buffer{}); err != nil {
		t.Fatalf("uninstall --dry-run: %v", err)
	}
	if err := schemajson.Validate("plan.schema.json", dryRunOut.Bytes()); err != nil {
		t.Fatalf("uninstall dry-run plan schema: %v\n%s", err, dryRunOut.String())
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("dry-run should preserve target: %v", err)
	}

	var uninstallOut bytes.Buffer
	if err := Run([]string{"uninstall", "--manifest", manifest, "--home", home, "--project", project, "--state-dir", stateDir, "--json"}, &uninstallOut, &bytes.Buffer{}); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if err := schemajson.Validate("apply-result.schema.json", uninstallOut.Bytes()); err != nil {
		t.Fatalf("apply-result schema: %v\n%s", err, uninstallOut.String())
	}
	var result install.Result
	if err := json.Unmarshal(uninstallOut.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Actions) != 1 || result.Actions[0].Status != "removed" {
		t.Fatalf("actions = %#v, want one removed action", result.Actions)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("target should be removed, stat err=%v", err)
	}
}

func TestCleanupDuplicatesRemovesManagedSymlink(t *testing.T) {
	manifest := filepath.Join("..", "..", "testdata", "m0", "manifests", "talking-stick.toml")
	src, err := filepath.Abs(filepath.Join("..", "..", "testdata", "m0", "sources", "talking-stick"))
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	stateDir := t.TempDir()
	duplicate := filepath.Join(home, ".codex", "skills", "talking-stick")
	if err := os.MkdirAll(filepath.Dir(duplicate), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(src, duplicate); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	var dryRunOut bytes.Buffer
	if err := Run([]string{"cleanup-duplicates", "--manifest", manifest, "--home", home, "--state-dir", stateDir, "--dry-run", "--json"}, &dryRunOut, &bytes.Buffer{}); err != nil {
		t.Fatalf("cleanup-duplicates --dry-run: %v", err)
	}
	if err := schemajson.Validate("plan.schema.json", dryRunOut.Bytes()); err != nil {
		t.Fatalf("cleanup dry-run plan schema: %v\n%s", err, dryRunOut.String())
	}
	if _, err := os.Lstat(duplicate); err != nil {
		t.Fatalf("dry-run should preserve duplicate: %v", err)
	}

	var stdout bytes.Buffer
	if err := Run([]string{"cleanup-duplicates", "--manifest", manifest, "--home", home, "--state-dir", stateDir, "--json"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("cleanup-duplicates: %v", err)
	}
	if err := schemajson.Validate("apply-result.schema.json", stdout.Bytes()); err != nil {
		t.Fatalf("apply-result schema: %v\n%s", err, stdout.String())
	}
	if _, err := os.Lstat(duplicate); !os.IsNotExist(err) {
		t.Fatalf("managed duplicate should be removed, stat err=%v", err)
	}
}

func TestSyncDryRunAndPrune(t *testing.T) {
	manifest := filepath.Join("..", "..", "testdata", "m0", "manifests", "clawdapus-runtime.toml")
	home := t.TempDir()
	project := t.TempDir()
	stateDir := t.TempDir()
	if err := Run([]string{"install", "--manifest", manifest, "--home", home, "--project", project, "--state-dir", stateDir, "--json"}, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("install: %v", err)
	}
	emptyManifest := filepath.Join(t.TempDir(), "empty.toml")
	if err := os.WriteFile(emptyManifest, []byte("schema = \"skiller-install.v1\"\nowner = \"clawdapus\"\nnamespace = \"mostlydev\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(project, ".claw-skills", "desk-manager", "skills", "clawdapus-cli")

	var dryRunOut bytes.Buffer
	if err := Run([]string{"sync", "--manifest", emptyManifest, "--home", home, "--project", project, "--state-dir", stateDir, "--dry-run", "--json"}, &dryRunOut, &bytes.Buffer{}); err != nil {
		t.Fatalf("sync --dry-run: %v", err)
	}
	if err := schemajson.Validate("plan.schema.json", dryRunOut.Bytes()); err != nil {
		t.Fatalf("sync dry-run plan schema: %v\n%s", err, dryRunOut.String())
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("sync dry-run should preserve target: %v", err)
	}

	var syncOut bytes.Buffer
	if err := Run([]string{"sync", "--manifest", emptyManifest, "--home", home, "--project", project, "--state-dir", stateDir, "--json"}, &syncOut, &bytes.Buffer{}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if err := schemajson.Validate("apply-result.schema.json", syncOut.Bytes()); err != nil {
		t.Fatalf("sync result schema: %v\n%s", err, syncOut.String())
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("sync should remove stale marker-owned target, stat err=%v", err)
	}
}

package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mostlydev/skiller/internal/schemajson"
	"github.com/mostlydev/skiller/pkg/install"
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

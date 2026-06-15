package cli

import (
	"bytes"
	"path/filepath"
	"testing"
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

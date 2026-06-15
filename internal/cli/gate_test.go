package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mostlydev/skiller/internal/schemajson"
	"github.com/mostlydev/skiller/pkg/version"
)

func TestM4StaticBinaryGate(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	bin := filepath.Join(t.TempDir(), "skiller")
	build := exec.Command("go", "build", "-o", bin, "./cmd/skiller")
	build.Dir = repoRoot
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("CGO_ENABLED=0 go build: %v\n%s", err, out)
	}

	versionLine := runGateCommand(t, bin, "--version")
	if !strings.HasPrefix(strings.TrimSpace(string(versionLine)), "skiller ") {
		t.Fatalf("--version output = %q", versionLine)
	}

	versionJSON := runGateCommand(t, bin, "version", "--json")
	if err := schemajson.Validate("version.schema.json", versionJSON); err != nil {
		t.Fatalf("version schema: %v\n%s", err, versionJSON)
	}
	var info version.Info
	if err := json.Unmarshal(versionJSON, &info); err != nil {
		t.Fatal(err)
	}
	if info.Version == "" {
		t.Fatalf("version info = %#v", info)
	}

	registryJSON := runGateCommand(t, bin, "registry", "--json")
	if !json.Valid(registryJSON) {
		t.Fatalf("registry output is not JSON:\n%s", registryJSON)
	}

	manifest := filepath.Join(repoRoot, "testdata", "m0", "manifests", "talking-stick.toml")
	planJSON := runGateCommand(t, bin, "plan", "--manifest", manifest, "--home", t.TempDir(), "--json")
	if err := schemajson.Validate("plan.schema.json", planJSON); err != nil {
		t.Fatalf("plan schema: %v\n%s", err, planJSON)
	}
}

func runGateCommand(t *testing.T, bin string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s %s: %v\nstderr:\n%s\nstdout:\n%s", bin, strings.Join(args, " "), err, stderr.String(), out)
	}
	return out
}

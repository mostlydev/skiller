package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mostlydev/skiller/internal/schemajson"
	"github.com/mostlydev/skiller/pkg/version"
)

func TestVersionFlagPrintsHumanLine(t *testing.T) {
	var stdout bytes.Buffer
	if err := Run([]string{"--version"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("--version: %v", err)
	}
	line := strings.TrimSpace(stdout.String())
	if !strings.HasPrefix(line, "skiller ") {
		t.Fatalf("--version output = %q, want skiller prefix", line)
	}
}

func TestVersionJSONValidatesAgainstSchema(t *testing.T) {
	var stdout bytes.Buffer
	if err := Run([]string{"version", "--json"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("version --json: %v", err)
	}
	if err := schemajson.Validate("version.schema.json", stdout.Bytes()); err != nil {
		t.Fatalf("version schema: %v\n%s", err, stdout.String())
	}
	var info version.Info
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Schema != "skiller-version.v1" || info.Version == "" || info.GoVersion == "" || info.Platform == "" {
		t.Fatalf("Info = %#v", info)
	}
}

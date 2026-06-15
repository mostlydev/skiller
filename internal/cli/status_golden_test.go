package cli

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mostlydev/skiller/internal/schemajson"
)

var updateM1Golden = flag.Bool("update-m1-golden", false, "regenerate M1 status/conflict golden files")

var m1SourceKeyRe = regexp.MustCompile(`"source_key": "file:[0-9a-f]+"`)

func TestM1StatusAndConflictsGoldens(t *testing.T) {
	fixturesAbs, err := filepath.Abs(filepath.Join("..", "..", "testdata", "m0"))
	if err != nil {
		t.Fatal(err)
	}
	fixturesResolved, err := filepath.EvalSymlinks(fixturesAbs)
	if err != nil {
		fixturesResolved = fixturesAbs
	}
	goldenRoot := filepath.Join("..", "..", "testdata", "m1", "golden")
	ledgerStateDir := filepath.Join("..", "..", "testdata", "m1", "state", "conflict-ledger")

	cases := []struct {
		name       string
		args       []string
		schemaName string
	}{
		{
			name: "status-talking-stick",
			args: []string{
				"status",
				"--manifest", filepath.Join(fixturesAbs, "manifests", "talking-stick.toml"),
				"--home", "/skiller/golden/home",
				"--state-dir", "/skiller/golden/state",
				"--json",
			},
			schemaName: "status.schema.json",
		},
		{
			name: "conflicts-namespace",
			args: []string{
				"conflicts", "list",
				"--manifest", filepath.Join(fixturesAbs, "manifests", "namespace-collision.toml"),
				"--home", "/skiller/golden/home",
				"--state-dir", "/skiller/golden/state",
				"--json",
			},
			schemaName: "conflicts.schema.json",
		},
		{
			name: "conflicts-ledger-only",
			args: []string{
				"conflicts", "list",
				"--state-dir", ledgerStateDir,
				"--json",
			},
			schemaName: "conflicts.schema.json",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			if err := Run(tc.args, &stdout, &stderr); err != nil {
				t.Fatalf("Run: %v\nstderr: %s", err, stderr.String())
			}
			if tc.schemaName != "" {
				if err := schemajson.Validate(tc.schemaName, stdout.Bytes()); err != nil {
					t.Fatalf("schema validation: %v\n%s", err, stdout.String())
				}
			}
			norm := stdout.String()
			if fixturesResolved != fixturesAbs {
				norm = strings.ReplaceAll(norm, fixturesResolved, "<FIXTURES>")
			}
			norm = strings.ReplaceAll(norm, fixturesAbs, "<FIXTURES>")
			norm = strings.ReplaceAll(norm, filepath.Clean(ledgerStateDir), "<M1_STATE>/conflict-ledger")
			norm = m1SourceKeyRe.ReplaceAllString(norm, `"source_key": "file:<redacted>"`)
			goldenPath := filepath.Join(goldenRoot, tc.name+".json")
			if *updateM1Golden {
				if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(goldenPath, []byte(norm), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden (regenerate with `go test ./internal/cli -run TestM1StatusAndConflictsGoldens -update-m1-golden`): %v", err)
			}
			if string(want) != norm {
				t.Fatalf("%s output does not match %s\n--- got ---\n%s", tc.name, goldenPath, norm)
			}
		})
	}
}

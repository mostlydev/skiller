package planner

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "regenerate golden plan files")

// source_key hashes the absolute source path, which varies by checkout location;
// everything else in a golden plan is either content-derived (digests) or rooted at
// the fixed placeholder home below, so only this field needs redaction.
var sourceKeyRe = regexp.MustCompile(`"source_key": "file:[0-9a-f]+"`)

// TestGoldenPlans is the executable proof of the M0 gate (design doc §11.2): a single
// read-only `skiller plan --json` shape represents every adopting tool's current
// install intent. Each committed golden under testdata/m0/golden/ is a full, human
// reviewable plan, so the contract is locked by example, not only by field asserts.
//
// Determinism: a fixed non-existent home makes every target `absent` and every derived
// path/lock-id stable; env-home overrides are cleared; the checkout-relative fixtures
// root is normalized to <FIXTURES>; source_key (a hash of the checkout path) is redacted.
// Regenerate with: go test ./internal/planner -run TestGoldenPlans -update
func TestGoldenPlans(t *testing.T) {
	cases := []struct {
		name     string
		manifest string
		project  string
	}{
		{"talking-stick", "talking-stick.toml", ""},
		{"our-self", "our-self.toml", ""},
		{"our-manifest", "our-manifest.toml", ""},
		{"gnit", "gnit.toml", ""},
		{"clawdapus", "clawdapus.toml", ""},
		{"clawdapus-runtime", "clawdapus-runtime.toml", "/skiller/golden/project"},
		{"namespace-collision", "namespace-collision.toml", ""},
	}

	for _, env := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "GROK_HOME"} {
		t.Setenv(env, "")
	}

	fixturesAbs, err := filepath.Abs(filepath.Join("..", "..", "testdata", "m0"))
	if err != nil {
		t.Fatal(err)
	}
	fixturesResolved, err := filepath.EvalSymlinks(fixturesAbs)
	if err != nil {
		fixturesResolved = fixturesAbs
	}

	const home = "/skiller/golden/home"

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manifest := filepath.Join(fixturesAbs, "manifests", tc.manifest)
			plan, err := Build(Options{ManifestPath: manifest, Home: home, Project: tc.project})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			SortPlan(&plan)

			raw, err := json.MarshalIndent(plan, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			norm := string(raw) + "\n"
			if fixturesResolved != fixturesAbs {
				norm = strings.ReplaceAll(norm, fixturesResolved, "<FIXTURES>")
			}
			norm = strings.ReplaceAll(norm, fixturesAbs, "<FIXTURES>")
			norm = sourceKeyRe.ReplaceAllString(norm, `"source_key": "file:<redacted>"`)

			goldenPath := filepath.Join(fixturesAbs, "golden", tc.name+".plan.json")
			if *updateGolden {
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
				t.Fatalf("read golden (regenerate with `go test ./internal/planner -run TestGoldenPlans -update`): %v", err)
			}
			if string(want) != norm {
				t.Fatalf("plan for %s does not match %s; regenerate with -update if intended.\n--- got ---\n%s", tc.name, goldenPath, norm)
			}
		})
	}
}

func TestM3ResolvedPlanGoldens(t *testing.T) {
	cases := []struct {
		name       string
		manifest   string
		onConflict string
	}{
		{"namespace-skip", "namespace-collision.toml", "skip"},
	}

	fixturesAbs, err := filepath.Abs(filepath.Join("..", "..", "testdata", "m0"))
	if err != nil {
		t.Fatal(err)
	}
	fixturesResolved, err := filepath.EvalSymlinks(fixturesAbs)
	if err != nil {
		fixturesResolved = fixturesAbs
	}
	goldenRoot := filepath.Join("..", "..", "testdata", "m3", "golden")

	const home = "/skiller/golden/home"

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manifest := filepath.Join(fixturesAbs, "manifests", tc.manifest)
			plan, err := Build(Options{ManifestPath: manifest, Home: home, OnConflict: tc.onConflict})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			SortPlan(&plan)

			raw, err := json.MarshalIndent(plan, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			norm := string(raw) + "\n"
			if fixturesResolved != fixturesAbs {
				norm = strings.ReplaceAll(norm, fixturesResolved, "<FIXTURES>")
			}
			norm = strings.ReplaceAll(norm, fixturesAbs, "<FIXTURES>")
			norm = sourceKeyRe.ReplaceAllString(norm, `"source_key": "file:<redacted>"`)

			goldenPath := filepath.Join(goldenRoot, tc.name+".plan.json")
			if *updateGolden {
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
				t.Fatalf("read golden (regenerate with `go test ./internal/planner -run TestM3ResolvedPlanGoldens -update`): %v", err)
			}
			if string(want) != norm {
				t.Fatalf("plan for %s does not match %s; regenerate with -update if intended.\n--- got ---\n%s", tc.name, goldenPath, norm)
			}
		})
	}
}

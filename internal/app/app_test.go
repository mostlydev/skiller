package app

import (
	"path/filepath"
	"testing"
)

func talkingStickManifest() string {
	return filepath.Join("..", "..", "testdata", "m0", "manifests", "talking-stick.toml")
}

func claudeCodeTarget(t *testing.T, opts Options) string {
	t.Helper()
	plan, err := Plan(opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	for _, a := range plan.Actions {
		if a.Target.ID == "claude-code" {
			return a.Target.Path
		}
	}
	t.Fatal("no claude-code target action in plan")
	return ""
}

// Env-home overrides (CLAUDE_CONFIG_DIR, CODEX_HOME, GROK_HOME) are read by internal/app
// and passed to pkg/target as a resolved map so pkg/target stays pure (no os.Getenv). The
// M0 golden tests clear those vars, so this is the regression guard proving the hoisted
// wiring actually redirects a proprietary root — i.e. envHomes() reaches the planner.
func TestEnvHomeRedirectsProprietaryRoot(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/custom/claude")
	got := claudeCodeTarget(t, Options{ManifestPath: talkingStickManifest(), Home: "/skiller/test/home"})
	want := filepath.Join("/custom/claude", "skills", "talking-stick")
	if got != want {
		t.Fatalf("claude-code path = %q, want %q (CLAUDE_CONFIG_DIR not honored through internal/app->pkg/target)", got, want)
	}
}

func TestNoEnvHomeFallsBackToGlobalDir(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	got := claudeCodeTarget(t, Options{ManifestPath: talkingStickManifest(), Home: "/skiller/test/home"})
	want := filepath.Join("/skiller/test/home", ".claude", "skills", "talking-stick")
	if got != want {
		t.Fatalf("claude-code path = %q, want %q", got, want)
	}
}

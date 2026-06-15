package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := writeManifest(t, `
schema = "skiller-install.v1"
owner = "tester"

[[skills]]
name = "demo"
source = "../sources/demo"
typo_field = "reject me"
`)
	err := loadErr(path)
	if err == nil {
		t.Fatal("expected unknown-field error")
	}
	if !strings.Contains(err.Error(), "typo_field") {
		t.Fatalf("error should name unknown field, got: %v", err)
	}
}

func TestLoadRejectsMissingRequiredSkillFields(t *testing.T) {
	path := writeManifest(t, `
schema = "skiller-install.v1"
owner = "tester"

[[skills]]
name = "demo"
`)
	err := loadErr(path)
	if err == nil {
		t.Fatal("expected missing source error")
	}
	if !strings.Contains(err.Error(), "skills[0] missing source") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRejectsMissingRequiredExtraFields(t *testing.T) {
	path := writeManifest(t, `
schema = "skiller-install.v1"
owner = "tester"

[[extras]]
id = "hook"
source = "hook.json"
`)
	err := loadErr(path)
	if err == nil {
		t.Fatal("expected missing target error")
	}
	if !strings.Contains(err.Error(), "extras[0] missing source or target") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func loadErr(path string) error {
	_, err := Load(path)
	return err
}

func writeManifest(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "manifest.toml")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyDirStagesAndPromotes(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target", "skill")
	writeFile(t, filepath.Join(source, "SKILL.md"), "new\n")
	result, err := CopyDir(source, target, func(stage string) error {
		return os.WriteFile(filepath.Join(stage, ".skiller-install.json"), []byte("{}\n"), 0o644)
	}, testOptions("one"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Effective != "copy" {
		t.Fatalf("effective = %q, want copy", result.Effective)
	}
	assertFile(t, filepath.Join(target, "SKILL.md"), "new\n")
	assertFile(t, filepath.Join(target, ".skiller-install.json"), "{}\n")
	if _, err := os.Stat(filepath.Join(root, "target", ".skiller-stage-one")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stage dir should be gone, err=%v", err)
	}
}

func TestCopyDirRollsBackWhenPromoteFails(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	writeFile(t, filepath.Join(source, "SKILL.md"), "new\n")
	writeFile(t, filepath.Join(target, "SKILL.md"), "old\n")
	opts := testOptions("rollback")
	opts.Rename = func(oldpath, newpath string) error {
		if strings.Contains(oldpath, ".skiller-stage-") && newpath == target {
			return errors.New("injected promote failure")
		}
		return os.Rename(oldpath, newpath)
	}
	if _, err := CopyDir(source, target, nil, opts); err == nil {
		t.Fatal("expected injected promote failure")
	}
	assertFile(t, filepath.Join(target, "SKILL.md"), "old\n")
	if _, err := os.Stat(filepath.Join(root, ".skiller-backup-rollback")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup should be gone after rollback, err=%v", err)
	}
}

func TestLinkOrCopyFallsBackToCopy(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	writeFile(t, filepath.Join(source, "SKILL.md"), "new\n")
	opts := testOptions("fallback")
	opts.Symlink = func(oldname, newname string) error {
		return os.ErrPermission
	}
	result, err := LinkOrCopyDir(source, target, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.FallbackApplied || result.Requested != "link" || result.Effective != "copy" {
		t.Fatalf("result = %#v, want link->copy fallback", result)
	}
	assertFile(t, filepath.Join(target, "SKILL.md"), "new\n")
}

func TestCopyFileReplacesWithRollbackStrategy(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.json")
	target := filepath.Join(root, "hooks", "hook.json")
	writeFile(t, source, `{"new":true}`+"\n")
	writeFile(t, target, `{"old":true}`+"\n")
	if _, err := CopyFile(source, target, nil, testOptions("file")); err != nil {
		t.Fatal(err)
	}
	assertFile(t, target, `{"new":true}`+"\n")
}

func TestSweepOrphansRemovesOnlySkillerStagesAndBackups(t *testing.T) {
	parent := t.TempDir()
	writeFile(t, filepath.Join(parent, ".skiller-stage-old", "x"), "x")
	writeFile(t, filepath.Join(parent, ".skiller-backup-old", "x"), "x")
	writeFile(t, filepath.Join(parent, "skill", "SKILL.md"), "keep")
	if err := SweepOrphans(parent); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(parent, ".skiller-stage-old")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stage should be removed, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(parent, ".skiller-backup-old")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup should be removed, err=%v", err)
	}
	assertFile(t, filepath.Join(parent, "skill", "SKILL.md"), "keep")
}

func testOptions(suffix string) Options {
	return Options{Suffix: func() string { return suffix }}
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

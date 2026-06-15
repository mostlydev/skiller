package digest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileDigestIgnoresBasename(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "source.json")
	second := filepath.Join(dir, "target-name.json")
	writeFile(t, first, "same\n")
	writeFile(t, second, "same\n")

	firstDigest, err := Path(first)
	if err != nil {
		t.Fatal(err)
	}
	secondDigest, err := Path(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstDigest != secondDigest {
		t.Fatalf("file digests differ for same content under different names: %s != %s", firstDigest, secondDigest)
	}
}

func TestDirectoryDigestIncludesRelativePath(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	writeFile(t, filepath.Join(first, "a.txt"), "same\n")
	writeFile(t, filepath.Join(second, "b.txt"), "same\n")

	firstDigest, err := Path(first)
	if err != nil {
		t.Fatal(err)
	}
	secondDigest, err := Path(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstDigest == secondDigest {
		t.Fatalf("directory digests should include relative paths, both were %s", firstDigest)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

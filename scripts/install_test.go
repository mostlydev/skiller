package scripts

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallScriptInstallsFromFakeRelease(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh is POSIX shell")
	}
	releaseDir := t.TempDir()
	archiveName, archiveBytes := writeFakeArchive(t, releaseDir, "v1.2.3")
	writeChecksums(t, releaseDir, archiveName, archiveBytes)

	binDir := t.TempDir()
	cmd := exec.Command("sh", "./install.sh", "--base-url", releaseDir, "--version", "v1.2.3", "--bin-dir", binDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install.sh: %v\n%s", err, out)
	}
	installed := filepath.Join(binDir, "skiller")
	if _, err := os.Stat(installed); err != nil {
		t.Fatalf("installed binary missing: %v\n%s", err, out)
	}
	run := exec.Command(installed, "version", "--json")
	versionOut, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("installed binary run: %v\n%s", err, versionOut)
	}
	if !strings.Contains(string(versionOut), `"version":"v1.2.3"`) {
		t.Fatalf("version output = %s", versionOut)
	}
	if !strings.Contains(string(out), "Installed skiller to "+installed) {
		t.Fatalf("install output = %s", out)
	}
}

func TestInstallScriptRejectsChecksumMismatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh is POSIX shell")
	}
	releaseDir := t.TempDir()
	archiveName, _ := writeFakeArchive(t, releaseDir, "v1.2.3")
	if err := os.WriteFile(filepath.Join(releaseDir, "checksums.txt"), []byte("0000  "+archiveName+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	cmd := exec.Command("sh", "./install.sh", "--base-url", releaseDir, "--version", "v1.2.3", "--bin-dir", binDir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("install.sh succeeded with bad checksum\n%s", out)
	}
	if !strings.Contains(string(out), "checksum mismatch") {
		t.Fatalf("bad-checksum output = %s", out)
	}
	if _, err := os.Stat(filepath.Join(binDir, "skiller")); !os.IsNotExist(err) {
		t.Fatalf("binary installed despite checksum mismatch, stat err=%v", err)
	}
}

func writeFakeArchive(t *testing.T, releaseDir, version string) (string, []byte) {
	t.Helper()
	osName, arch := goreleaserTarget(t)
	archiveName := fmt.Sprintf("skiller_%s_%s_%s.tar.gz", version, osName, arch)
	archivePath := filepath.Join(releaseDir, archiveName)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"version\" ] && [ \"$2\" = \"--json\" ]; then\n" +
		"  printf '{\"schema\":\"skiller-version.v1\",\"version\":\"" + version + "\",\"commit\":\"\",\"date\":\"\",\"built_by\":\"fake\",\"dirty\":false,\"source\":\"ldflags\",\"go_version\":\"fake\",\"platform\":\"" + osName + "/" + arch + "\"}\\n'\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 0\n"
	if err := tw.WriteHeader(&tar.Header{Name: "skiller", Mode: 0o755, Size: int64(len(script))}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(tw, script); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archivePath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return archiveName, buf.Bytes()
}

func writeChecksums(t *testing.T, releaseDir, archiveName string, archive []byte) {
	t.Helper()
	sum := sha256.Sum256(archive)
	line := fmt.Sprintf("%x  %s\n", sum, archiveName)
	if err := os.WriteFile(filepath.Join(releaseDir, "checksums.txt"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
}

func goreleaserTarget(t *testing.T) (string, string) {
	t.Helper()
	var osName string
	switch runtime.GOOS {
	case "darwin", "linux":
		osName = runtime.GOOS
	default:
		t.Skipf("unsupported test OS %s", runtime.GOOS)
	}
	var arch string
	switch runtime.GOARCH {
	case "amd64", "arm64":
		arch = runtime.GOARCH
	default:
		t.Skipf("unsupported test arch %s", runtime.GOARCH)
	}
	return osName, arch
}

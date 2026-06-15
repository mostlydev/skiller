package bootstrap

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGoBootstrapReferenceWithStubbedSkiller(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is POSIX-only")
	}
	ensureBin := buildGoEnsure(t)
	stub := writeSkillerStub(t, "1.2.3")
	env := append(os.Environ(), "SKILLER_BIN="+stub)

	cmd := exec.Command(ensureBin, "--min-version", "1.0.0")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go ensure success: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "1.2.3") {
		t.Fatalf("success output = %q", out)
	}

	cmd = exec.Command(ensureBin, "--min-version", "9.0.0")
	cmd.Env = env
	out, err = cmd.CombinedOutput()
	if exitCode(err) != 11 {
		t.Fatalf("go ensure old exit = %v, want 11\n%s", err, out)
	}
	if !strings.Contains(string(out), "Install command:") {
		t.Fatalf("old-version output lacks install command:\n%s", out)
	}
}

func TestGoBootstrapDefaultDoesNotRunInstallCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell assertion is POSIX-only")
	}
	ensureBin := buildGoEnsure(t)
	marker := filepath.Join(t.TempDir(), "download-ran")
	pathDir := t.TempDir()
	cmd := exec.Command(ensureBin, "--install-command", "touch "+marker)
	cmd.Env = append(os.Environ(), "SKILLER_BIN=", "PATH="+pathDir, "HOME="+t.TempDir())
	out, err := cmd.CombinedOutput()
	if exitCode(err) != 10 {
		t.Fatalf("go ensure missing exit = %v, want 10\n%s", err, out)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("default path ran install command, stat err=%v", err)
	}
	if !strings.Contains(string(out), "Install command: touch "+marker) {
		t.Fatalf("missing output lacks exact install command:\n%s", out)
	}
}

func buildGoEnsure(t *testing.T) string {
	t.Helper()
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "ensure-skiller-go")
	cmd := exec.Command(goBin, "build", "-o", bin, "./go/ensure.go")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ensure: %v\n%s", err, out)
	}
	return bin
}

func TestNodeBootstrapReferenceWithStubbedSkiller(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is POSIX-only")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	stub := writeSkillerStub(t, "1.2.3")
	cmd := exec.Command(node, "./node/ensure-skiller.mjs", "--min-version", "1.0.0")
	cmd.Env = append(os.Environ(), "SKILLER_BIN="+stub)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node ensure success: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "1.2.3") {
		t.Fatalf("success output = %q", out)
	}
}

func TestRustBootstrapReferenceWithStubbedSkiller(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is POSIX-only")
	}
	rustc, err := exec.LookPath("rustc")
	if err != nil {
		t.Skip("rustc not installed")
	}
	bin := filepath.Join(t.TempDir(), "ensure-skiller-rust")
	compile := exec.Command(rustc, "./rust/ensure_skiller.rs", "-o", bin)
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("rust compile: %v\n%s", err, out)
	}
	stub := writeSkillerStub(t, "1.2.3")
	cmd := exec.Command(bin, "--min-version", "1.0.0")
	cmd.Env = append(os.Environ(), "SKILLER_BIN="+stub)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rust ensure success: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "1.2.3") {
		t.Fatalf("success output = %q", out)
	}
}

func writeSkillerStub(t *testing.T, version string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "skiller")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"version\" ] && [ \"$2\" = \"--json\" ]; then\n" +
		"  printf '{\"schema\":\"skiller-version.v1\",\"version\":\"" + version + "\",\"commit\":\"\",\"date\":\"\",\"built_by\":\"stub\",\"dirty\":false,\"source\":\"ldflags\",\"go_version\":\"go-test\",\"platform\":\"test/test\"}\\n'\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 2\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if !strings.Contains(err.Error(), "exit status") {
		return -1
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return -1
}

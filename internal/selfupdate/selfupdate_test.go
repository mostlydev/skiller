package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mostlydev/skiller/internal/schemajson"
)

func TestCheckReportsUpdateAvailableWithoutFetchingArchive(t *testing.T) {
	client := fakeRelease(t, "1.2.0", []byte("new"))
	result, err := Run(context.Background(), Options{
		Check:          true,
		CurrentVersion: "1.0.0",
		TargetOS:       "darwin",
		TargetArch:     "arm64",
		BaseURL:        "fake://release",
		Client:         client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "update-available" || !result.UpdateAvailable || result.TargetVersion != "1.2.0" {
		t.Fatalf("result = %#v", result)
	}
	if client.fetches["skiller_1.2.0_darwin_arm64.tar.gz"] != 0 {
		t.Fatalf("--check fetched archive: %#v", client.fetches)
	}
	assertSelfupdateSchema(t, result)
}

func TestDryRunDoesNotReplaceExecutable(t *testing.T) {
	client := fakeRelease(t, "1.2.0", []byte("new"))
	exe := writeExecutable(t, "old")
	result, err := Run(context.Background(), Options{
		DryRun:         true,
		ExecutablePath: exe,
		CurrentVersion: "1.0.0",
		TargetOS:       "darwin",
		TargetArch:     "arm64",
		BaseURL:        "fake://release",
		Client:         client,
	})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "dry-run" || result.ExecutablePath != resolved {
		t.Fatalf("result = %#v", result)
	}
	if got := mustRead(t, exe); string(got) != "old" {
		t.Fatalf("executable changed on dry-run: %q", got)
	}
}

func TestRunVerifiesChecksumBeforeSwap(t *testing.T) {
	client := fakeRelease(t, "1.2.0", []byte("new"))
	client.assets["checksums.txt"] = []byte("0000  skiller_1.2.0_darwin_arm64.tar.gz\n")
	exe := writeExecutable(t, "old")
	_, err := Run(context.Background(), Options{
		ExecutablePath: exe,
		CurrentVersion: "1.0.0",
		TargetOS:       "darwin",
		TargetArch:     "arm64",
		BaseURL:        "fake://release",
		Client:         client,
	})
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err = %v, want checksum mismatch", err)
	}
	if got := mustRead(t, exe); string(got) != "old" {
		t.Fatalf("executable changed after bad checksum: %q", got)
	}
}

func TestRunSwapsExecutable(t *testing.T) {
	client := fakeRelease(t, "1.2.0", []byte("new"))
	exe := writeExecutable(t, "old")
	result, err := Run(context.Background(), Options{
		ExecutablePath: exe,
		CurrentVersion: "1.0.0",
		TargetOS:       "darwin",
		TargetArch:     "arm64",
		BaseURL:        "fake://release",
		Client:         client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "updated" || !result.UpdateAvailable {
		t.Fatalf("result = %#v", result)
	}
	if got := mustRead(t, exe); string(got) != "new" {
		t.Fatalf("executable = %q, want new", got)
	}
}

func TestRunRefusesNonRegularExecutable(t *testing.T) {
	dir := t.TempDir()
	_, err := Run(context.Background(), Options{
		DryRun:         true,
		ExecutablePath: dir,
		CurrentVersion: "1.0.0",
		TargetOS:       "darwin",
		TargetArch:     "arm64",
		BaseURL:        "fake://release",
		Client:         fakeRelease(t, "1.2.0", []byte("new")),
	})
	if err == nil || !strings.Contains(err.Error(), "non-regular executable") {
		t.Fatalf("err = %v, want non-regular executable", err)
	}
}

type fakeClient struct {
	assets  map[string][]byte
	fetches map[string]int
}

func (c *fakeClient) Get(_ context.Context, url string) ([]byte, error) {
	name := path.Base(url)
	c.fetches[name]++
	data, ok := c.assets[name]
	if !ok {
		return nil, fmt.Errorf("missing fake asset %s", name)
	}
	return append([]byte(nil), data...), nil
}

func fakeRelease(t *testing.T, version string, binary []byte) *fakeClient {
	t.Helper()
	archiveName := "skiller_" + version + "_darwin_arm64.tar.gz"
	archive := tarGz(t, binary)
	sum := sha256.Sum256(archive)
	return &fakeClient{
		assets: map[string][]byte{
			"checksums.txt": []byte(fmt.Sprintf("%x  %s\n", sum, archiveName)),
			archiveName:     archive,
		},
		fetches: map[string]int{},
	}
}

func tarGz(t *testing.T, binary []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "skiller", Mode: 0o755, Size: int64(len(binary))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeExecutable(t *testing.T, content string) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "skiller")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if err := os.WriteFile(exe, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertSelfupdateSchema(t *testing.T, result Result) {
	t.Helper()
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := schemajson.Validate("selfupdate.schema.json", data); err != nil {
		t.Fatalf("selfupdate schema: %v\n%s", err, data)
	}
}

package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/mostlydev/skiller/internal/fsutil"
	"github.com/mostlydev/skiller/pkg/version"
)

const (
	Schema          = "skiller-selfupdate.v1"
	DefaultRepo     = "mostlydev/skiller"
	DefaultVersion  = "latest"
	defaultHTTPWait = 30 * time.Second
)

type Client interface {
	Get(ctx context.Context, url string) ([]byte, error)
}

type HTTPClient struct {
	Client *http.Client
}

type Options struct {
	Repo           string
	Version        string
	Check          bool
	DryRun         bool
	ExecutablePath string
	CurrentVersion string
	TargetOS       string
	TargetArch     string
	BaseURL        string
	Client         Client
}

type Result struct {
	Schema           string `json:"schema"`
	Repo             string `json:"repo"`
	RequestedVersion string `json:"requested_version"`
	CurrentVersion   string `json:"current_version"`
	TargetVersion    string `json:"target_version"`
	TargetOS         string `json:"target_os"`
	TargetArch       string `json:"target_arch"`
	Archive          string `json:"archive"`
	Checksum         string `json:"checksum"`
	ExecutablePath   string `json:"executable_path,omitempty"`
	UpdateAvailable  bool   `json:"update_available"`
	Check            bool   `json:"check"`
	DryRun           bool   `json:"dry_run"`
	Status           string `json:"status"`
	Message          string `json:"message,omitempty"`
}

func Run(ctx context.Context, opts Options) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	opts = defaults(opts)
	result := Result{
		Schema:           Schema,
		Repo:             opts.Repo,
		RequestedVersion: opts.Version,
		CurrentVersion:   opts.CurrentVersion,
		TargetOS:         opts.TargetOS,
		TargetArch:       opts.TargetArch,
		Check:            opts.Check,
		DryRun:           opts.DryRun,
	}
	if !opts.Check {
		exe, err := resolveExecutable(opts.ExecutablePath)
		if err != nil {
			return result, err
		}
		result.ExecutablePath = exe
	}
	checksums, err := fetchChecksums(ctx, opts)
	if err != nil {
		return result, err
	}
	archive, checksum, err := selectArchive(checksums, opts.TargetOS, opts.TargetArch)
	if err != nil {
		return result, err
	}
	result.Archive = archive
	result.Checksum = checksum
	result.TargetVersion = archiveVersion(archive, opts.TargetOS, opts.TargetArch)
	result.UpdateAvailable = updateAvailable(result.CurrentVersion, result.TargetVersion)
	if opts.Check {
		if result.UpdateAvailable {
			result.Status = "update-available"
			result.Message = "an update is available"
		} else {
			result.Status = "up-to-date"
			result.Message = "skiller is up to date"
		}
		return result, nil
	}
	if opts.DryRun {
		result.Status = "dry-run"
		if result.UpdateAvailable {
			result.Message = "selfupdate would replace the executable"
		} else {
			result.Message = "selfupdate would leave the executable unchanged"
		}
		return result, nil
	}
	if !result.UpdateAvailable {
		result.Status = "up-to-date"
		result.Message = "skiller is up to date"
		return result, nil
	}
	if err := ensureWritableExecutable(result.ExecutablePath); err != nil {
		return result, err
	}
	archiveBytes, err := opts.Client.Get(ctx, joinURL(assetBaseURL(opts), archive))
	if err != nil {
		return result, err
	}
	if got := sha256Hex(archiveBytes); got != checksum {
		return result, fmt.Errorf("checksum mismatch for %s: expected %s, got %s", archive, checksum, got)
	}
	binary, err := extractBinary(archive, archiveBytes, opts.TargetOS)
	if err != nil {
		return result, err
	}
	info, err := os.Stat(result.ExecutablePath)
	if err != nil {
		return result, err
	}
	if _, err := fsutil.WriteFile(result.ExecutablePath, binary, info.Mode().Perm(), fsutil.Options{}); err != nil {
		if runtime.GOOS == "windows" {
			return result, fmt.Errorf("replace running executable: %w (known M4 limitation on Windows)", err)
		}
		return result, err
	}
	result.Status = "updated"
	result.Message = "skiller updated"
	return result, nil
}

func (c HTTPClient) Get(ctx context.Context, url string) ([]byte, error) {
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPWait}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func defaults(opts Options) Options {
	if opts.Repo == "" {
		opts.Repo = DefaultRepo
	}
	if opts.Version == "" {
		opts.Version = DefaultVersion
	}
	if opts.CurrentVersion == "" {
		opts.CurrentVersion = version.Get().Version
	}
	if opts.TargetOS == "" {
		opts.TargetOS = runtime.GOOS
	}
	if opts.TargetArch == "" {
		opts.TargetArch = runtime.GOARCH
	}
	if opts.Client == nil {
		opts.Client = HTTPClient{}
	}
	return opts
}

func fetchChecksums(ctx context.Context, opts Options) ([]byte, error) {
	return opts.Client.Get(ctx, joinURL(assetBaseURL(opts), "checksums.txt"))
}

func assetBaseURL(opts Options) string {
	if opts.BaseURL != "" {
		return opts.BaseURL
	}
	if opts.Version == DefaultVersion {
		return "https://github.com/" + opts.Repo + "/releases/latest/download"
	}
	return "https://github.com/" + opts.Repo + "/releases/download/" + opts.Version
}

func joinURL(base, name string) string {
	return strings.TrimRight(base, "/") + "/" + name
}

func selectArchive(checksums []byte, targetOS, targetArch string) (string, string, error) {
	suffix := "_" + targetOS + "_" + targetArch + ".tar.gz"
	if targetOS == "windows" {
		suffix = "_" + targetOS + "_" + targetArch + ".zip"
	}
	lines := strings.Split(string(checksums), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := fields[1]
		if strings.HasPrefix(name, "skiller_") && strings.HasSuffix(name, suffix) {
			return name, fields[0], nil
		}
	}
	return "", "", fmt.Errorf("no skiller archive found for %s/%s in checksums.txt", targetOS, targetArch)
}

func archiveVersion(archive, targetOS, targetArch string) string {
	suffix := "_" + targetOS + "_" + targetArch + ".tar.gz"
	if targetOS == "windows" {
		suffix = "_" + targetOS + "_" + targetArch + ".zip"
	}
	value := strings.TrimPrefix(archive, "skiller_")
	value = strings.TrimSuffix(value, suffix)
	return value
}

func updateAvailable(current, target string) bool {
	cmp, ok := compareVersions(target, current)
	if !ok {
		return current != target
	}
	return cmp > 0
}

func compareVersions(a, b string) (int, bool) {
	aa, ok := parseVersion(a)
	if !ok {
		return 0, false
	}
	bb, ok := parseVersion(b)
	if !ok {
		return 0, false
	}
	for i := 0; i < 3; i++ {
		if aa[i] > bb[i] {
			return 1, true
		}
		if aa[i] < bb[i] {
			return -1, true
		}
	}
	return 0, true
}

func parseVersion(value string) ([3]int, bool) {
	var out [3]int
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == '.' || r == '-' || r == '+'
	})
	if len(fields) == 0 {
		return out, false
	}
	for i := 0; i < 3; i++ {
		if i >= len(fields) {
			break
		}
		n, err := strconv.Atoi(fields[i])
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

func resolveExecutable(override string) (string, error) {
	exe := override
	if exe == "" {
		var err error
		exe, err = os.Executable()
		if err != nil {
			return "", fmt.Errorf("identify executable: %w", err)
		}
	}
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("resolve executable symlinks: %w", err)
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", fmt.Errorf("stat executable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("refusing to replace non-regular executable: %s", real)
	}
	return real, nil
}

func ensureWritableExecutable(exe string) error {
	info, err := os.Stat(exe)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to replace non-regular executable: %s", exe)
	}
	if info.Mode().Perm()&0o222 == 0 {
		return fmt.Errorf("refusing to replace non-writable executable: %s", exe)
	}
	parent := filepath.Dir(exe)
	probe, err := os.CreateTemp(parent, ".skiller-write-check-")
	if err != nil {
		return fmt.Errorf("executable directory is not writable: %w", err)
	}
	name := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Remove(name)
}

func extractBinary(archive string, data []byte, targetOS string) ([]byte, error) {
	if strings.HasSuffix(archive, ".tar.gz") {
		return extractTarGz(data, targetOS)
	}
	if strings.HasSuffix(archive, ".zip") {
		return extractZip(data, targetOS)
	}
	return nil, fmt.Errorf("unsupported archive format: %s", archive)
}

func extractTarGz(data []byte, targetOS string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		if isBinaryName(header.Name, targetOS) {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("archive did not contain skiller binary")
}

func extractZip(data []byte, targetOS string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	for _, file := range zr.File {
		if file.FileInfo().IsDir() || !isBinaryName(file.Name, targetOS) {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return nil, err
		}
		out, readErr := io.ReadAll(rc)
		closeErr := rc.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		return out, nil
	}
	return nil, fmt.Errorf("archive did not contain skiller binary")
}

func isBinaryName(name, targetOS string) bool {
	base := path.Base(filepath.ToSlash(name))
	if targetOS == "windows" {
		return base == "skiller.exe"
	}
	return base == "skiller"
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

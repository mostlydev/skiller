package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const (
	exitMissing = 10
	exitTooOld  = 11
	exitInvalid = 12
	exitExec    = 13
	exitInstall = 20

	defaultMinVersion     = "0.0.0"
	defaultInstallCommand = "curl -fsSL https://raw.githubusercontent.com/mostlydev/skiller/main/scripts/install.sh | sh"
)

type options struct {
	minVersion     string
	allowDownload  bool
	installCommand string
}

type ensureResult struct {
	ok      bool
	path    string
	version string
	code    int
	message string
}

func main() {
	opts := parseOptions()
	result := ensureSkiller(opts)
	if result.ok {
		fmt.Printf("%s %s\n", result.path, result.version)
		return
	}
	if opts.allowDownload {
		if err := runInstall(opts.installCommand); err != nil {
			fmt.Fprintf(os.Stderr, "skiller install command failed: %s\n", opts.installCommand)
			os.Exit(exitInstall)
		}
		retry := ensureSkiller(options{minVersion: opts.minVersion, installCommand: opts.installCommand})
		if retry.ok {
			fmt.Printf("%s %s\n", retry.path, retry.version)
			return
		}
		fmt.Fprintln(os.Stderr, retry.message)
		fmt.Fprintf(os.Stderr, "Install command: %s\n", opts.installCommand)
		os.Exit(retry.code)
	}
	fmt.Fprintln(os.Stderr, result.message)
	fmt.Fprintf(os.Stderr, "Install command: %s\n", opts.installCommand)
	os.Exit(result.code)
}

func parseOptions() options {
	opts := options{
		minVersion:    firstNonEmpty(os.Getenv("SKILLER_MIN_VERSION"), defaultMinVersion),
		allowDownload: os.Getenv("SKILLER_BOOTSTRAP_ALLOW_DOWNLOAD") == "1",
		installCommand: firstNonEmpty(
			os.Getenv("SKILLER_BOOTSTRAP_INSTALL_COMMAND"),
			defaultInstallCommand,
		),
	}
	flag.StringVar(&opts.minVersion, "min-version", opts.minVersion, "minimum skiller version")
	flag.BoolVar(&opts.allowDownload, "allow-download", opts.allowDownload, "allow running the install command")
	flag.StringVar(&opts.installCommand, "install-command", opts.installCommand, "install command to print or run")
	flag.Parse()
	return opts
}

func ensureSkiller(opts options) ensureResult {
	binary := findBinary()
	if binary == "" {
		return ensureResult{code: exitMissing, message: "skiller binary not found"}
	}
	version, err := readVersion(binary)
	if err != nil {
		if errors.Is(err, errInvalidVersion) {
			return ensureResult{code: exitInvalid, message: err.Error()}
		}
		return ensureResult{code: exitExec, message: err.Error()}
	}
	cmp, ok := compareVersions(version, opts.minVersion)
	if !ok {
		return ensureResult{code: exitInvalid, message: "invalid skiller version: " + version}
	}
	if cmp < 0 {
		return ensureResult{
			code:    exitTooOld,
			message: fmt.Sprintf("skiller %s is older than required %s", version, opts.minVersion),
		}
	}
	return ensureResult{ok: true, path: binary, version: version}
}

func findBinary() string {
	if value := os.Getenv("SKILLER_BIN"); value != "" && isExecutable(value) {
		return value
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		for _, name := range executableNames() {
			candidate := filepath.Join(dir, name)
			if isExecutable(candidate) {
				return candidate
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidate := filepath.Join(home, ".local", "bin", executableNames()[0])
		if isExecutable(candidate) {
			return candidate
		}
	}
	return ""
}

func executableNames() []string {
	if runtime.GOOS == "windows" {
		return []string{"skiller.exe", "skiller"}
	}
	return []string{"skiller"}
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0o111 != 0
}

var errInvalidVersion = errors.New("invalid skiller version JSON")

func readVersion(binary string) (string, error) {
	out, err := exec.Command(binary, "version", "--json").Output()
	if err != nil {
		return "", fmt.Errorf("failed to run %s version --json: %w", binary, err)
	}
	var payload struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return "", fmt.Errorf("%w: %v", errInvalidVersion, err)
	}
	if payload.Version == "" {
		return "", fmt.Errorf("%w: missing version", errInvalidVersion)
	}
	return payload.Version, nil
}

func compareVersions(found, minimum string) (int, bool) {
	a, ok := parseVersion(found)
	if !ok {
		return 0, false
	}
	b, ok := parseVersion(minimum)
	if !ok {
		return 0, false
	}
	for i := 0; i < 3; i++ {
		if a[i] > b[i] {
			return 1, true
		}
		if a[i] < b[i] {
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

func runInstall(command string) error {
	shell, flag := "/bin/sh", "-c"
	if runtime.GOOS == "windows" {
		shell, flag = "cmd", "/C"
	}
	cmd := exec.Command(shell, flag, command)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

package version

import (
	"runtime/debug"
	"strings"
	"testing"
)

func TestGetWithBuildInfoFallback(t *testing.T) {
	oldVersion, oldCommit, oldDate, oldBuiltBy := Version, Commit, Date, BuiltBy
	defer func() {
		Version, Commit, Date, BuiltBy = oldVersion, oldCommit, oldDate, oldBuiltBy
	}()
	Version, Commit, Date, BuiltBy = "dev", "", "", ""

	got := getWithBuildInfo(func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Main: debug.Module{Version: "v1.2.3"},
			Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "0123456789abcdef"},
				{Key: "vcs.time", Value: "2026-06-15T04:00:00Z"},
				{Key: "vcs.modified", Value: "true"},
			},
		}, true
	})

	if got.Version != "v1.2.3" || got.Source != "build-info" {
		t.Fatalf("version/source = %q/%q, want build-info v1.2.3", got.Version, got.Source)
	}
	if got.Commit != "0123456789abcdef" || got.Date != "2026-06-15T04:00:00Z" || !got.Dirty {
		t.Fatalf("build info fields = %#v", got)
	}
}

func TestLdflagsVariablesWin(t *testing.T) {
	oldVersion, oldCommit, oldDate, oldBuiltBy := Version, Commit, Date, BuiltBy
	defer func() {
		Version, Commit, Date, BuiltBy = oldVersion, oldCommit, oldDate, oldBuiltBy
	}()
	Version, Commit, Date, BuiltBy = "9.9.9", "abcdef", "2026-06-15T04:00:00Z", "test"

	got := getWithBuildInfo(func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Main: debug.Module{Version: "v1.2.3"},
			Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "ignored"},
				{Key: "vcs.time", Value: "ignored"},
			},
		}, true
	})

	if got.Version != "9.9.9" || got.Commit != "abcdef" || got.Date != "2026-06-15T04:00:00Z" || got.BuiltBy != "test" {
		t.Fatalf("Info = %#v", got)
	}
	if got.Source != "ldflags" {
		t.Fatalf("source = %q, want ldflags", got.Source)
	}
}

func TestInfoString(t *testing.T) {
	line := (Info{Version: "1.2.3", Commit: "0123456789abcdef", Dirty: true}).String()
	if !strings.Contains(line, "skiller 1.2.3") || !strings.Contains(line, "0123456789ab") || !strings.Contains(line, "dirty") {
		t.Fatalf("line = %q", line)
	}
}

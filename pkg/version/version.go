// Package version reports the skiller build identity.
package version

import (
	"runtime"
	"runtime/debug"
	"strings"
)

const schema = "skiller-version.v1"

// These variables are set by release builds with -ldflags. Development and
// go-install builds fall back to runtime/debug build information when present.
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
	BuiltBy = ""
)

type Info struct {
	Schema    string `json:"schema"`
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	BuiltBy   string `json:"built_by"`
	Dirty     bool   `json:"dirty"`
	Source    string `json:"source"`
	GoVersion string `json:"go_version"`
	Platform  string `json:"platform"`
}

func Get() Info {
	return getWithBuildInfo(debug.ReadBuildInfo)
}

func (i Info) String() string {
	parts := []string{"skiller", firstNonEmpty(i.Version, "dev")}
	if i.Commit != "" {
		parts = append(parts, "("+shortCommit(i.Commit)+")")
	}
	if i.Dirty {
		parts = append(parts, "dirty")
	}
	return strings.Join(parts, " ")
}

func getWithBuildInfo(readBuildInfo func() (*debug.BuildInfo, bool)) Info {
	info := Info{
		Schema:    schema,
		Version:   firstNonEmpty(Version, "dev"),
		Commit:    Commit,
		Date:      Date,
		BuiltBy:   BuiltBy,
		Source:    "ldflags",
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}
	buildInfo, ok := readBuildInfo()
	if info.Version == "dev" {
		info.Source = "dev"
		if ok && buildInfo.Main.Version != "" && buildInfo.Main.Version != "(devel)" {
			info.Version = buildInfo.Main.Version
			info.Source = "build-info"
		}
	}
	if ok {
		settings := map[string]string{}
		for _, setting := range buildInfo.Settings {
			settings[setting.Key] = setting.Value
		}
		if info.Commit == "" {
			info.Commit = settings["vcs.revision"]
		}
		if info.Date == "" {
			info.Date = settings["vcs.time"]
		}
		info.Dirty = settings["vcs.modified"] == "true"
	}
	if info.Version == "" {
		info.Version = "dev"
	}
	return info
}

func shortCommit(commit string) string {
	if len(commit) <= 12 {
		return commit
	}
	return commit[:12]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

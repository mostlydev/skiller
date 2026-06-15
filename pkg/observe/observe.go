package observe

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/mostlydev/skiller/internal/digest"
	"github.com/mostlydev/skiller/pkg/registry"
	"github.com/mostlydev/skiller/pkg/target"
)

type WorldState struct {
	Observed map[string]RawObservation
}

type RawObservation struct {
	Path          string
	Exists        bool
	IsSymlink     bool
	SymlinkTarget string
	Digest        string
	SkillerMarker *MarkerFacts
	LegacyMarker  *LegacyFacts
	OwnershipTest *TestResult
	Err           string
}

type MarkerFacts struct {
	Owner                    string
	CanonicalID              string
	InstalledDigestAtInstall string
	InstallerName            string
	MarkerPath               string
}

type LegacyFacts struct {
	OwnerLabel string
	Class      string
	MarkerPath string
}

type TestResult struct {
	OwnerLabel string
	Class      string
	MarkerPath string
}

type Options struct {
	Home         string
	ExtraTargets []target.Ref
}

func Observe(candidates []target.Candidate, reg registry.Catalog, opts Options) WorldState {
	world := WorldState{Observed: map[string]RawObservation{}}
	for _, candidate := range candidates {
		observeOne(world, reg, opts, candidate.Target.Path, installSlug(candidate), candidate.Target)
		for _, dup := range candidate.Duplicates {
			observeOne(world, reg, opts, dup.Path, installSlug(candidate), dup)
		}
	}
	for _, ref := range opts.ExtraTargets {
		observeOne(world, reg, opts, ref.Path, "", ref)
	}
	return world
}

func observeOne(world WorldState, reg registry.Catalog, opts Options, path, installSlug string, ref target.Ref) {
	if _, ok := world.Observed[path]; ok {
		return
	}
	raw := RawObservation{Path: path}
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			world.Observed[path] = raw
			return
		}
		raw.Err = err.Error()
		world.Observed[path] = raw
		return
	}
	raw.Exists = true
	if d, err := digest.Path(path); err == nil {
		raw.Digest = d
	} else {
		raw.Err = err.Error()
	}
	if info.Mode()&os.ModeSymlink != 0 {
		raw.IsSymlink = true
		if linkTarget, err := filepath.EvalSymlinks(path); err == nil {
			raw.SymlinkTarget = linkTarget
		}
		world.Observed[path] = raw
		return
	}
	if marker, ok := readSkillerMarker(path); ok {
		raw.SkillerMarker = &marker
	}
	for _, marker := range reg.Markers {
		if marker.MarkerFile != "" {
			markerPath := filepath.Join(path, marker.MarkerFile)
			if _, err := os.Stat(markerPath); err == nil {
				raw.LegacyMarker = &LegacyFacts{
					OwnerLabel: marker.OwnerLabel,
					Class:      marker.Class,
					MarkerPath: markerPath,
				}
				break
			}
		}
		if marker.OwnershipTest == "clawdapus-global-marker" && installSlug == "clawdapus-cli" {
			markerPath := filepath.Join(opts.Home, ".claw", "skill-installed")
			if _, err := os.Stat(markerPath); err == nil {
				if strings.Contains(ref.Path, filepath.Join(".agents", "skills")) || strings.Contains(ref.Path, filepath.Join(".claude", "skills")) {
					raw.OwnershipTest = &TestResult{
						OwnerLabel: marker.OwnerLabel,
						Class:      marker.Class,
						MarkerPath: markerPath,
					}
				}
			}
		}
	}
	world.Observed[path] = raw
}

type skillerMarker struct {
	Owner                    string `json:"owner"`
	CanonicalID              string `json:"canonical_id"`
	InstalledDigestAtInstall string `json:"installed_digest_at_install"`
	Installer                struct {
		Name string `json:"name"`
	} `json:"installer"`
}

func readSkillerMarker(dir string) (MarkerFacts, bool) {
	data, err := os.ReadFile(filepath.Join(dir, ".skiller-install.json"))
	if err != nil {
		return MarkerFacts{}, false
	}
	var marker skillerMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		return MarkerFacts{}, false
	}
	if marker.Installer.Name != "skiller" {
		return MarkerFacts{}, false
	}
	return MarkerFacts{
		Owner:                    marker.Owner,
		CanonicalID:              marker.CanonicalID,
		InstalledDigestAtInstall: marker.InstalledDigestAtInstall,
		InstallerName:            marker.Installer.Name,
		MarkerPath:               filepath.Join(dir, ".skiller-install.json"),
	}, true
}

func installSlug(candidate target.Candidate) string {
	if candidate.Skill.InstallSlug != "" {
		return candidate.Skill.InstallSlug
	}
	return candidate.Skill.Name
}

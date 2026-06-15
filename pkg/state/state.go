package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/mostlydev/skiller/internal/contract"
	"github.com/mostlydev/skiller/internal/schemajson"
)

type Ledger struct {
	Schema    string                  `json:"schema"`
	Sources   []SourceRecord          `json:"sources"`
	Skills    []SkillRecord           `json:"skills"`
	Installs  []InstallRecord         `json:"installs"`
	Extras    []ExtraRecord           `json:"extras"`
	Conflicts []contract.PlanConflict `json:"conflicts"`
}

type SourceRecord struct {
	ID                string `json:"id"`
	Owner             string `json:"owner,omitempty"`
	Namespace         string `json:"namespace,omitempty"`
	PackageRef        string `json:"package_ref,omitempty"`
	Version           string `json:"version,omitempty"`
	SourceKind        string `json:"source_kind"`
	OriginalSpec      string `json:"original_spec"`
	CanonicalURI      string `json:"canonical_uri"`
	SourceKey         string `json:"source_key"`
	Subdir            string `json:"subdir,omitempty"`
	PinnedRef         string `json:"pinned_ref,omitempty"`
	RequestedChecksum string `json:"requested_checksum,omitempty"`
	ResolvedRevision  string `json:"resolved_revision,omitempty"`
	SourceStatus      string `json:"source_status"`
	LocalCachePath    string `json:"local_cache_path"`
	SourceRealpath    string `json:"source_realpath,omitempty"`
	SourceDigest      string `json:"source_digest,omitempty"`
	FetchedAt         string `json:"fetched_at,omitempty"`
	DiscoveredAt      string `json:"discovered_at,omitempty"`
	LastSeenAt        string `json:"last_seen_at,omitempty"`
}

type SkillRecord struct {
	ID              string `json:"id"`
	CanonicalID     string `json:"canonical_id"`
	Namespace       string `json:"namespace"`
	Name            string `json:"name"`
	InstallSlug     string `json:"install_slug"`
	FrontmatterName string `json:"frontmatter_name,omitempty"`
	SourceID        string `json:"source_id,omitempty"`
	Description     string `json:"description,omitempty"`
	CreatedAt       string `json:"created_at,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty"`
}

type InstallRecord struct {
	ID                       string `json:"id"`
	SkillID                  string `json:"skill_id"`
	TargetKind               string `json:"target_kind"`
	TargetID                 string `json:"target_id"`
	TargetPath               string `json:"target_path"`
	Mode                     string `json:"mode"`
	Scope                    string `json:"scope"`
	MarkerPath               string `json:"marker_path,omitempty"`
	InstalledDigestAtInstall string `json:"installed_digest_at_install,omitempty"`
	SourceDigestAtInstall    string `json:"source_digest_at_install,omitempty"`
	Status                   string `json:"status"`
	LegacyAdapter            string `json:"legacy_adapter,omitempty"`
	InstalledAt              string `json:"installed_at,omitempty"`
	UpdatedAt                string `json:"updated_at,omitempty"`
	LastSeenAt               string `json:"last_seen_at,omitempty"`
}

type ExtraRecord struct {
	ID          string `json:"id"`
	SourceID    string `json:"source_id"`
	ExtraID     string `json:"extra_id"`
	TargetPath  string `json:"target_path"`
	Mode        string `json:"mode"`
	MarkerPath  string `json:"marker_path,omitempty"`
	InstalledAt string `json:"installed_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
	LastSeenAt  string `json:"last_seen_at,omitempty"`
}

type LoadResult struct {
	Ledger             Ledger
	Path               string
	Diagnostics        []contract.Diagnostic
	RebuildRecommended bool
}

func Load(dir string) (LoadResult, error) {
	resolved, err := ResolveDir(dir)
	if err != nil {
		return LoadResult{}, err
	}
	path := filepath.Join(resolved, "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return rebuildable(path, "state ledger missing; rebuild recommended"), nil
		}
		return rebuildable(path, "state ledger unreadable; rebuild recommended: "+err.Error()), nil
	}
	if err := schemajson.Validate("state.schema.json", data); err != nil {
		return rebuildable(path, "state ledger invalid; rebuild recommended: "+err.Error()), nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var ledger Ledger
	if err := dec.Decode(&ledger); err != nil {
		return rebuildable(path, "state ledger undecodable; rebuild recommended: "+err.Error()), nil
	}
	normalize(&ledger)
	return LoadResult{Ledger: ledger, Path: path}, nil
}

func Empty() Ledger {
	return Ledger{
		Schema:    "skiller-state.v1",
		Sources:   []SourceRecord{},
		Skills:    []SkillRecord{},
		Installs:  []InstallRecord{},
		Extras:    []ExtraRecord{},
		Conflicts: []contract.PlanConflict{},
	}
}

func ResolveDir(explicit string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}
	if env := os.Getenv("SKILLER_STATE_DIR"); env != "" {
		return filepath.Abs(env)
	}
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Abs(filepath.Join(xdg, "skiller"))
	}
	if runtime.GOOS == "darwin" {
		if config, err := os.UserConfigDir(); err == nil && config != "" {
			return filepath.Join(config, "skiller"), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve state dir: %w", err)
	}
	return filepath.Join(home, ".local", "state", "skiller"), nil
}

func rebuildable(path, message string) LoadResult {
	return LoadResult{
		Ledger: Empty(),
		Path:   path,
		Diagnostics: []contract.Diagnostic{{
			Level:   "warning",
			Message: message,
			Path:    path,
		}},
		RebuildRecommended: true,
	}
}

func normalize(ledger *Ledger) {
	if ledger.Schema == "" {
		ledger.Schema = "skiller-state.v1"
	}
	if ledger.Sources == nil {
		ledger.Sources = []SourceRecord{}
	}
	if ledger.Skills == nil {
		ledger.Skills = []SkillRecord{}
	}
	if ledger.Installs == nil {
		ledger.Installs = []InstallRecord{}
	}
	if ledger.Extras == nil {
		ledger.Extras = []ExtraRecord{}
	}
	if ledger.Conflicts == nil {
		ledger.Conflicts = []contract.PlanConflict{}
	}
}

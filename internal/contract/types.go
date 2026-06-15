package contract

type Catalog struct {
	Schema    string       `json:"schema" toml:"schema"`
	Harnesses []Harness    `json:"harnesses,omitempty" toml:"harnesses"`
	Markers   []MarkerRule `json:"markers,omitempty" toml:"markers"`
	Sources   []SourceRule `json:"sources,omitempty" toml:"sources"`
}

type Harness struct {
	ID            string   `json:"id" toml:"id"`
	Kind          string   `json:"kind" toml:"kind"`
	Aliases       []string `json:"aliases,omitempty" toml:"aliases"`
	Readers       []string `json:"readers,omitempty" toml:"readers"`
	PrimaryTarget string   `json:"primary_target,omitempty" toml:"primary_target"`
	GlobalDir     string   `json:"global_dir,omitempty" toml:"global_dir"`
	ProjectDir    string   `json:"project_dir,omitempty" toml:"project_dir"`
	EnvHome       string   `json:"env_home,omitempty" toml:"env_home"`
	DetectDirs    []string `json:"detect_dirs,omitempty" toml:"detect_dirs"`
	DuplicateDirs []string `json:"duplicate_dirs,omitempty" toml:"duplicate_dirs"`
	DefaultMode   string   `json:"default_mode,omitempty" toml:"default_mode"`
	CarriesExtras bool     `json:"carries_extras" toml:"carries_extras"`
}

type MarkerRule struct {
	OwnerLabel    string `json:"owner_label" toml:"owner_label"`
	MarkerFile    string `json:"marker_file,omitempty" toml:"marker_file"`
	OwnershipTest string `json:"ownership_test,omitempty" toml:"ownership_test"`
	Class         string `json:"class" toml:"class"`
}

type SourceRule struct {
	Host           string   `json:"host" toml:"host"`
	Kind           string   `json:"kind" toml:"kind"`
	Shorthand      string   `json:"shorthand,omitempty" toml:"shorthand"`
	WebNormalizers []string `json:"web_normalizers,omitempty" toml:"web_normalizers"`
}

type Manifest struct {
	Schema      string          `toml:"schema" json:"schema"`
	Owner       string          `toml:"owner" json:"owner"`
	Namespace   string          `toml:"namespace" json:"namespace,omitempty"`
	Version     string          `toml:"version" json:"version,omitempty"`
	DefaultMode string          `toml:"default_mode" json:"default_mode,omitempty"`
	Skills      []ManifestSkill `toml:"skills" json:"skills,omitempty"`
	Extras      []ManifestExtra `toml:"extras" json:"extras,omitempty"`
}

type ManifestSkill struct {
	Name        string              `toml:"name" json:"name"`
	CanonicalID string              `toml:"canonical_id" json:"canonical_id,omitempty"`
	InstallSlug string              `toml:"install_slug" json:"install_slug,omitempty"`
	Source      string              `toml:"source" json:"source"`
	Targets     []string            `toml:"targets" json:"targets,omitempty"`
	Mode        string              `toml:"mode" json:"mode,omitempty"`
	TargetDirs  []ManifestTargetDir `toml:"target_dirs" json:"target_dirs,omitempty"`
}

type ManifestTargetDir struct {
	ID     string `toml:"id" json:"id,omitempty"`
	Path   string `toml:"path" json:"path"`
	Scope  string `toml:"scope" json:"scope,omitempty"`
	Mode   string `toml:"mode" json:"mode,omitempty"`
	Kind   string `toml:"kind" json:"kind,omitempty"`
	Reader string `toml:"reader" json:"reader,omitempty"`
}

type ManifestExtra struct {
	ID          string `toml:"id" json:"id"`
	Harness     string `toml:"harness" json:"harness,omitempty"`
	Source      string `toml:"source" json:"source"`
	Target      string `toml:"target" json:"target"`
	Mode        string `toml:"mode" json:"mode,omitempty"`
	OwnedMarker bool   `toml:"owned_marker" json:"owned_marker,omitempty"`
}

type Plan struct {
	Schema      string           `json:"schema"`
	Operation   string           `json:"operation"`
	DryRun      bool             `json:"dry_run"`
	Inputs      PlanInputs       `json:"inputs"`
	Sources     []SourceSnapshot `json:"sources,omitempty"`
	Actions     []PlanAction     `json:"actions"`
	Conflicts   []PlanConflict   `json:"conflicts,omitempty"`
	Diagnostics []Diagnostic     `json:"diagnostics,omitempty"`
}

type PlanInputs struct {
	ManifestPath string `json:"manifest_path,omitempty"`
	Home         string `json:"home,omitempty"`
	Project      string `json:"project,omitempty"`
	Namespace    string `json:"namespace,omitempty"`
	InstallSlug  string `json:"install_slug,omitempty"`
	OnConflict   string `json:"on_conflict"`
}

type SourceSnapshot struct {
	ID                string `json:"id"`
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
	FrontmatterName   string `json:"frontmatter_name,omitempty"`
	Description       string `json:"description,omitempty"`
}

type PlanAction struct {
	ID                  string              `json:"id"`
	Action              string              `json:"action"`
	Status              string              `json:"status"`
	Reason              string              `json:"reason,omitempty"`
	Skill               *PlanSkill          `json:"skill,omitempty"`
	Extra               *PlanExtra          `json:"extra,omitempty"`
	SourceID            string              `json:"source_id,omitempty"`
	Target              PlanTarget          `json:"target"`
	Mode                PlanMode            `json:"mode"`
	Ownership           ObservedOwnership   `json:"ownership"`
	RelatedObservations []ObservedOwnership `json:"related_observations,omitempty"`
	ConflictModes       []string            `json:"conflict_modes,omitempty"`
	PlannedWrites       []PlannedWrite      `json:"planned_writes,omitempty"`
}

type PlanSkill struct {
	CanonicalID      string   `json:"canonical_id"`
	Namespace        string   `json:"namespace"`
	Name             string   `json:"name"`
	InstallSlug      string   `json:"install_slug"`
	FrontmatterName  string   `json:"frontmatter_name,omitempty"`
	Description      string   `json:"description,omitempty"`
	RequestedTargets []string `json:"requested_targets,omitempty"`
}

type PlanExtra struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
}

type PlanTarget struct {
	ID               string   `json:"id"`
	Kind             string   `json:"kind"`
	Scope            string   `json:"scope"`
	Root             string   `json:"root"`
	Path             string   `json:"path"`
	Readers          []string `json:"readers,omitempty"`
	RequestedTargets []string `json:"requested_targets,omitempty"`
	LockID           string   `json:"lock_id"`
}

type PlanMode struct {
	Requested        string `json:"requested"`
	Effective        string `json:"effective"`
	FallbackPossible bool   `json:"fallback_possible,omitempty"`
}

type ObservedOwnership struct {
	Class          string `json:"class"`
	Path           string `json:"path,omitempty"`
	Digest         string `json:"digest,omitempty"`
	DigestMatch    *bool  `json:"digest_match,omitempty"`
	MarkerPath     string `json:"marker_path,omitempty"`
	LegacyAdapter  string `json:"legacy_adapter,omitempty"`
	SourceRealpath string `json:"source_realpath,omitempty"`
	Message        string `json:"message,omitempty"`
}

type PlannedWrite struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
}

type PlanConflict struct {
	ID                  string   `json:"id"`
	TargetKind          string   `json:"target_kind"`
	TargetID            string   `json:"target_id"`
	EffectiveName       string   `json:"effective_name"`
	ExistingCanonicalID string   `json:"existing_canonical_id,omitempty"`
	DesiredCanonicalID  string   `json:"desired_canonical_id"`
	Status              string   `json:"status"`
	Resolution          string   `json:"resolution,omitempty"`
	SafeChoices         []string `json:"safe_choices,omitempty"`
}

type Diagnostic struct {
	Level   string `json:"level"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}

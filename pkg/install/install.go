package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mostlydev/skiller/internal/contract"
	"github.com/mostlydev/skiller/internal/digest"
	"github.com/mostlydev/skiller/internal/fsutil"
)

type Options struct {
	Owner        string
	Namespace    string
	ManifestPath string
	Version      string
	Now          func() time.Time
	FS           fsutil.Options
}

type UninstallOptions struct {
	Owner        string
	RemoveShared bool
	Force        bool
}

type Result struct {
	Schema      string                  `json:"schema"`
	Actions     []ActionResult          `json:"actions"`
	Diagnostics []contract.Diagnostic   `json:"diagnostics,omitempty"`
	Conflicts   []contract.PlanConflict `json:"conflicts,omitempty"`
}

type ActionResult struct {
	ID              string                  `json:"id"`
	Action          string                  `json:"action"`
	Status          string                  `json:"status"`
	Reason          string                  `json:"reason,omitempty"`
	Target          contract.PlanTarget     `json:"target"`
	Skill           *contract.PlanSkill     `json:"skill,omitempty"`
	Extra           *contract.PlanExtra     `json:"extra,omitempty"`
	RequestedMode   string                  `json:"requested_mode,omitempty"`
	EffectiveMode   string                  `json:"effective_mode,omitempty"`
	FallbackApplied bool                    `json:"fallback_applied,omitempty"`
	BackupPath      string                  `json:"backup_path,omitempty"`
	Writes          []contract.PlannedWrite `json:"writes,omitempty"`
	Error           string                  `json:"error,omitempty"`
}

func Apply(plan contract.Plan, opts Options) (Result, error) {
	result := Result{
		Schema:      "skiller-apply-result.v1",
		Actions:     []ActionResult{},
		Diagnostics: append([]contract.Diagnostic(nil), plan.Diagnostics...),
	}
	sources := map[string]contract.SourceSnapshot{}
	for _, source := range plan.Sources {
		sources[source.ID] = source
	}
	for _, action := range plan.Actions {
		actionResult := applyAction(action, sources, plan, opts)
		result.Actions = append(result.Actions, actionResult)
		if isBlockedStatus(actionResult.Status) {
			for _, conflict := range plan.Conflicts {
				if conflict.TargetID == action.Target.ID && conflict.EffectiveName == effectiveName(action) {
					result.Conflicts = append(result.Conflicts, conflict)
				}
			}
		}
	}
	return result, nil
}

func Uninstall(plan contract.Plan, opts UninstallOptions) (Result, error) {
	result := Result{Schema: "skiller-apply-result.v1", Actions: []ActionResult{}}
	sources := map[string]contract.SourceSnapshot{}
	for _, source := range plan.Sources {
		sources[source.ID] = source
	}
	for _, action := range plan.Actions {
		if action.Skill == nil {
			continue
		}
		actionResult := uninstallAction(action, sources[action.SourceID], plan, opts)
		result.Actions = append(result.Actions, actionResult)
	}
	return result, nil
}

func applyAction(action contract.PlanAction, sources map[string]contract.SourceSnapshot, plan contract.Plan, opts Options) ActionResult {
	out := ActionResult{
		ID:            action.ID,
		Action:        action.Action,
		Target:        action.Target,
		Skill:         action.Skill,
		Extra:         action.Extra,
		RequestedMode: action.Mode.Requested,
		EffectiveMode: action.Mode.Effective,
	}
	switch action.Action {
	case "block-conflict":
		out.Status = "blocked"
		out.Reason = action.Reason
		return out
	case "partially-satisfied":
		out.Status = "partially-satisfied"
		out.Reason = action.Reason
		return out
	case "satisfied-by-foreign":
		out.Status = "satisfied-by-foreign"
		out.Reason = action.Reason
		return out
	case "no-op", "adopt-existing":
		out.Status = "skipped"
		out.Reason = action.Reason
		return out
	case "install-link":
		source, ok := sources[action.SourceID]
		if !ok {
			return failed(out, "missing source "+action.SourceID)
		}
		result, err := fsutil.LinkOrCopyDir(source.LocalCachePath, action.Target.Path, markerMutator(action, source, plan, opts), opts.FS)
		if err != nil {
			return failed(out, err.Error())
		}
		out.Status = "installed"
		out.EffectiveMode = result.Effective
		out.FallbackApplied = result.FallbackApplied
		out.Writes = writesFor(action, result.Effective)
		return out
	case "install-copy", "refresh":
		source, ok := sources[action.SourceID]
		if !ok {
			return failed(out, "missing source "+action.SourceID)
		}
		result, err := fsutil.CopyDir(source.LocalCachePath, action.Target.Path, markerMutator(action, source, plan, opts), opts.FS)
		if err != nil {
			return failed(out, err.Error())
		}
		if action.Action == "refresh" {
			out.Status = "updated"
		} else {
			out.Status = "installed"
		}
		out.EffectiveMode = result.Effective
		out.Writes = writesFor(action, "copy")
		return out
	case "force-replace":
		source, ok := sources[action.SourceID]
		if !ok {
			return failed(out, "missing source "+action.SourceID)
		}
		result, err := fsutil.CopyDirRetainBackup(source.LocalCachePath, action.Target.Path, markerMutator(action, source, plan, opts), opts.FS)
		if err != nil {
			return failed(out, err.Error())
		}
		out.Status = "updated"
		out.EffectiveMode = "copy"
		out.BackupPath = result.BackupPath
		out.Writes = writesFor(action, "copy")
		if result.BackupPath != "" {
			out.Writes = append(out.Writes, contract.PlannedWrite{Kind: "retained-backup", Path: result.BackupPath})
		}
		return out
	case "install-extra":
		if action.Extra == nil {
			return failed(out, "install-extra missing extra payload")
		}
		if _, err := fsutil.CopyFile(action.Extra.Source, action.Extra.Target, nil, opts.FS); err != nil {
			return failed(out, err.Error())
		}
		if hasPlannedWrite(action, "sidecar-marker") {
			marker, err := extraMarker(action, opts, plan)
			if err != nil {
				return failed(out, err.Error())
			}
			if _, err := fsutil.WriteFile(action.Extra.Target+".skiller-install.json", marker, 0o644, opts.FS); err != nil {
				return failed(out, err.Error())
			}
		}
		out.Status = "installed"
		out.EffectiveMode = "copy"
		out.Writes = action.PlannedWrites
		return out
	default:
		return failed(out, "unsupported action "+action.Action)
	}
}

func uninstallAction(action contract.PlanAction, source contract.SourceSnapshot, plan contract.Plan, opts UninstallOptions) ActionResult {
	out := ActionResult{
		ID:            action.ID,
		Action:        "remove-owned",
		Target:        action.Target,
		Skill:         action.Skill,
		RequestedMode: action.Mode.Requested,
		EffectiveMode: action.Mode.Effective,
	}
	if action.Ownership.Class == "absent" {
		out.Action = "skip-uninstall"
		out.Status = "skipped"
		out.Reason = "target is not installed"
		return out
	}
	if action.Target.Kind == "shared" && !opts.RemoveShared {
		out.Action = "skip-uninstall"
		out.Status = "skipped"
		out.Reason = "shared target requires --shared or --all"
		return out
	}
	switch action.Ownership.Class {
	case "ours-symlink":
		return removeOwnedSymlink(out, source)
	case "ours-copy":
		return removeOwnedCopy(out, plan, opts)
	default:
		out.Action = "skip-uninstall"
		out.Status = "skipped"
		out.Reason = "target is not skiller-owned"
		return out
	}
}

func removeOwnedSymlink(out ActionResult, source contract.SourceSnapshot) ActionResult {
	info, err := os.Lstat(out.Target.Path)
	if err != nil {
		if os.IsNotExist(err) {
			out.Action = "skip-uninstall"
			out.Status = "skipped"
			out.Reason = "target is not installed"
			return out
		}
		return failed(out, err.Error())
	}
	if info.Mode()&os.ModeSymlink == 0 {
		out.Status = "blocked"
		out.Reason = "target is not a symlink"
		return out
	}
	realpath, err := filepath.EvalSymlinks(out.Target.Path)
	if err != nil {
		return failed(out, err.Error())
	}
	if !samePath(realpath, source.SourceRealpath) {
		out.Status = "blocked"
		out.Reason = "symlink does not resolve to managed source"
		return out
	}
	if err := os.Remove(out.Target.Path); err != nil {
		return failed(out, err.Error())
	}
	out.Status = "removed"
	out.Writes = []contract.PlannedWrite{{Kind: "remove", Path: out.Target.Path}}
	return out
}

func removeOwnedCopy(out ActionResult, plan contract.Plan, opts UninstallOptions) ActionResult {
	marker, err := readMarkerPayload(out.Target.Path)
	if err != nil {
		out.Status = "blocked"
		out.Reason = err.Error()
		return out
	}
	owner := firstNonEmpty(opts.Owner, plan.Inputs.Namespace)
	if marker.Installer.Name != "skiller" || marker.Owner != owner || marker.CanonicalID != out.Skill.CanonicalID {
		out.Status = "blocked"
		out.Reason = "copy marker does not match requested owner and skill"
		return out
	}
	if !opts.Force {
		currentDigest, err := digest.Path(out.Target.Path)
		if err != nil {
			return failed(out, err.Error())
		}
		if currentDigest != marker.InstalledDigestAtInstall {
			out.Status = "blocked"
			out.Reason = "owned copy is modified; refusing conservative delete"
			return out
		}
	}
	if err := os.RemoveAll(out.Target.Path); err != nil {
		return failed(out, err.Error())
	}
	out.Status = "removed"
	out.Writes = []contract.PlannedWrite{{Kind: "remove", Path: out.Target.Path}}
	return out
}

func markerMutator(action contract.PlanAction, source contract.SourceSnapshot, plan contract.Plan, opts Options) func(stage string) error {
	return func(stage string) error {
		installedDigest, err := digest.Path(stage)
		if err != nil {
			return err
		}
		marker, err := skillMarker(action, source, plan, opts, installedDigest)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(stage, ".skiller-install.json"), marker, 0o644)
	}
}

func skillMarker(action contract.PlanAction, source contract.SourceSnapshot, plan contract.Plan, opts Options, installedDigest string) ([]byte, error) {
	if action.Skill == nil {
		return nil, fmt.Errorf("skill marker requires skill payload")
	}
	marker := markerPayload{
		Schema: "skiller-install-marker.v1",
		Installer: markerInstaller{
			Name:    "skiller",
			Version: firstNonEmpty(opts.Version, "dev"),
		},
		Owner:                    firstNonEmpty(opts.Owner, plan.Inputs.Namespace),
		Namespace:                firstNonEmpty(opts.Namespace, action.Skill.Namespace, plan.Inputs.Namespace),
		CanonicalID:              action.Skill.CanonicalID,
		Name:                     action.Skill.Name,
		InstallSlug:              action.Skill.InstallSlug,
		FrontmatterName:          action.Skill.FrontmatterName,
		Mode:                     "copy",
		TargetKind:               action.Target.Kind,
		Scope:                    action.Target.Scope,
		SourceRealpath:           source.SourceRealpath,
		SourceKey:                source.SourceKey,
		CanonicalURI:             source.CanonicalURI,
		ManifestPath:             plan.Inputs.ManifestPath,
		InstalledAt:              now(opts).Format(time.RFC3339),
		SourceDigestAtInstall:    source.SourceDigest,
		InstalledDigestAtInstall: installedDigest,
	}
	return json.MarshalIndent(marker, "", "  ")
}

func extraMarker(action contract.PlanAction, opts Options, plan contract.Plan) ([]byte, error) {
	d, err := digest.Path(action.Extra.Target)
	if err != nil {
		return nil, err
	}
	id := "extra:" + action.Extra.ID
	marker := markerPayload{
		Schema: "skiller-install-marker.v1",
		Installer: markerInstaller{
			Name:    "skiller",
			Version: firstNonEmpty(opts.Version, "dev"),
		},
		Owner:                    firstNonEmpty(opts.Owner, plan.Inputs.Namespace),
		Namespace:                firstNonEmpty(opts.Namespace, plan.Inputs.Namespace),
		CanonicalID:              id,
		Name:                     action.Extra.ID,
		InstallSlug:              action.Extra.ID,
		Mode:                     "copy",
		TargetKind:               "extra",
		Scope:                    "host",
		ManifestPath:             plan.Inputs.ManifestPath,
		InstalledAt:              now(opts).Format(time.RFC3339),
		SourceDigestAtInstall:    d,
		InstalledDigestAtInstall: d,
	}
	return json.MarshalIndent(marker, "", "  ")
}

type markerPayload struct {
	Schema                   string          `json:"schema"`
	Installer                markerInstaller `json:"installer"`
	Owner                    string          `json:"owner"`
	Namespace                string          `json:"namespace,omitempty"`
	CanonicalID              string          `json:"canonical_id"`
	Name                     string          `json:"name"`
	InstallSlug              string          `json:"install_slug,omitempty"`
	FrontmatterName          string          `json:"frontmatter_name,omitempty"`
	Mode                     string          `json:"mode"`
	TargetKind               string          `json:"target_kind"`
	Scope                    string          `json:"scope"`
	SourceRealpath           string          `json:"source_realpath,omitempty"`
	SourceKey                string          `json:"source_key,omitempty"`
	CanonicalURI             string          `json:"canonical_uri,omitempty"`
	ManifestPath             string          `json:"manifest_path,omitempty"`
	InstalledAt              string          `json:"installed_at"`
	SourceDigestAtInstall    string          `json:"source_digest_at_install"`
	InstalledDigestAtInstall string          `json:"installed_digest_at_install"`
}

type markerInstaller struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func readMarkerPayload(dir string) (markerPayload, error) {
	data, err := os.ReadFile(filepath.Join(dir, ".skiller-install.json"))
	if err != nil {
		return markerPayload{}, fmt.Errorf("read copy marker: %w", err)
	}
	var marker markerPayload
	if err := json.Unmarshal(data, &marker); err != nil {
		return markerPayload{}, fmt.Errorf("decode copy marker: %w", err)
	}
	return marker, nil
}

func failed(result ActionResult, message string) ActionResult {
	result.Status = "failed"
	result.Error = message
	return result
}

func isBlockedStatus(status string) bool {
	return status == "blocked" || status == "partially-satisfied"
}

func writesFor(action contract.PlanAction, effective string) []contract.PlannedWrite {
	writes := make([]contract.PlannedWrite, 0, len(action.PlannedWrites))
	for _, write := range action.PlannedWrites {
		if write.Kind == "link" && effective == "copy" {
			writes = append(writes, contract.PlannedWrite{Kind: "copy", Path: write.Path})
			writes = append(writes, contract.PlannedWrite{Kind: "marker", Path: filepath.Join(write.Path, ".skiller-install.json")})
			continue
		}
		writes = append(writes, write)
	}
	return writes
}

func hasPlannedWrite(action contract.PlanAction, kind string) bool {
	for _, write := range action.PlannedWrites {
		if write.Kind == kind {
			return true
		}
	}
	return false
}

func effectiveName(action contract.PlanAction) string {
	if action.Skill != nil {
		return action.Skill.InstallSlug
	}
	if action.Extra != nil {
		return action.Extra.ID
	}
	return ""
}

func now(opts Options) time.Time {
	if opts.Now != nil {
		return opts.Now().UTC()
	}
	return time.Now().UTC()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	aa, err := filepath.Abs(a)
	if err == nil {
		a = aa
	}
	bb, err := filepath.Abs(b)
	if err == nil {
		b = bb
	}
	return filepath.Clean(strings.TrimSpace(a)) == filepath.Clean(strings.TrimSpace(b))
}

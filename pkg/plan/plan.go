package plan

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/mostlydev/skiller/internal/contract"
	"github.com/mostlydev/skiller/internal/hashid"
	"github.com/mostlydev/skiller/pkg/manifest"
	"github.com/mostlydev/skiller/pkg/observe"
	"github.com/mostlydev/skiller/pkg/registry"
	"github.com/mostlydev/skiller/pkg/source"
	"github.com/mostlydev/skiller/pkg/target"
)

type Plan = contract.Plan
type Inputs struct {
	Manifest        manifest.Manifest
	Catalog         registry.Catalog
	Sources         []source.Snapshot
	SourcesBySpec   map[string]source.Snapshot
	Candidates      []target.Candidate
	ExtraCandidates []target.ExtraCandidate
	World           observe.WorldState
	Options         Options
}

type Options struct {
	ManifestPath string
	Home         string
	Project      string
	Namespace    string
	OnConflict   string
	Resolutions  map[string]Resolution
}

type Resolution struct {
	Policy      string
	InstallSlug string
}

var allConflictModes = []string{"block", "skip", "adopt-existing", "replace-owned", "rename", "force-replace"}

type builder struct {
	in            Inputs
	conflicts     []contract.PlanConflict
	actions       []contract.PlanAction
	diagnostics   []contract.Diagnostic
	actionKeys    map[string]bool
	desiredByPath map[string]string
}

func Build(in Inputs) Plan {
	if in.Options.OnConflict == "" {
		in.Options.OnConflict = "block"
	}
	b := &builder{
		in:            in,
		actionKeys:    map[string]bool{},
		desiredByPath: map[string]string{},
	}
	for _, candidate := range in.Candidates {
		b.planCandidate(candidate)
	}
	for _, candidate := range in.ExtraCandidates {
		b.planExtra(candidate)
	}
	return Plan{
		Schema:    "skiller-plan.v1",
		Operation: "plan",
		DryRun:    true,
		Inputs: contract.PlanInputs{
			ManifestPath: in.Options.ManifestPath,
			Home:         in.Options.Home,
			Project:      in.Options.Project,
			Namespace:    b.namespace(),
			OnConflict:   in.Options.OnConflict,
		},
		Sources:     in.Sources,
		Actions:     b.actions,
		Conflicts:   b.conflicts,
		Diagnostics: b.diagnostics,
	}
}

func (b *builder) planCandidate(candidate target.Candidate) {
	skill := candidate.Skill
	snapshot := b.in.SourcesBySpec[skill.Source]
	installSlug := firstNonEmpty(skill.InstallSlug, skill.Name)
	namespace := b.namespace()
	canonicalID := firstNonEmpty(skill.CanonicalID, namespace+":"+skill.Name)
	planSkill := contract.PlanSkill{
		CanonicalID:     canonicalID,
		Namespace:       namespace,
		Name:            skill.Name,
		InstallSlug:     installSlug,
		FrontmatterName: snapshot.FrontmatterName,
		Description:     snapshot.Description,
	}
	mode := b.resolveMode(skill, candidate.Target, snapshot.SourceKind)
	action := b.planTargetAction(snapshot, planSkill, candidate, mode)
	action = b.applyDesiredCollision(action, planSkill, candidate.Target)
	b.addAction(action)
}

func (b *builder) planExtra(candidate target.ExtraCandidate) {
	extra := candidate.Extra
	snapshot := b.in.SourcesBySpec[extra.Source]
	sourcePath := snapshot.LocalCachePath
	if sourcePath == "" {
		sourcePath = resolvePath(extra.Source, filepath.Dir(b.in.Options.ManifestPath), b.in.Options.Home)
	}
	targetRef := candidate.Target
	mode := firstNonEmpty(extra.Mode, "copy")
	ownership := classifyExtra(b.in.World.Observed[targetRef.Path], targetRef.Path, snapshot.SourceDigest)
	action := contract.PlanAction{
		ID:     actionID("extra", extra.ID, targetRef.Path),
		Action: "install-extra",
		Status: "dry-run",
		Extra: &contract.PlanExtra{
			ID:     extra.ID,
			Source: sourcePath,
			Target: targetRef.Path,
		},
		Target:    targetRef,
		Mode:      contract.PlanMode{Requested: mode, Effective: mode},
		Ownership: ownership,
		PlannedWrites: []contract.PlannedWrite{
			{Kind: "file", Path: targetRef.Path},
		},
	}
	if extra.OwnedMarker {
		action.PlannedWrites = append(action.PlannedWrites, contract.PlannedWrite{
			Kind: "sidecar-marker",
			Path: targetRef.Path + ".skiller-install.json",
		})
	}
	if ownership.DigestMatch != nil && *ownership.DigestMatch {
		action.Action = "no-op"
		action.Reason = "extra target already matches desired source"
		action.PlannedWrites = nil
	}
	b.addAction(action)
}

func (b *builder) planTargetAction(snapshot source.Snapshot, skill contract.PlanSkill, candidate target.Candidate, mode contract.PlanMode) contract.PlanAction {
	targetRef := candidate.Target
	skill.RequestedTargets = targetRef.RequestedTargets
	raw := b.in.World.Observed[targetRef.Path]
	ownership := b.classify(raw, targetRef.Path, snapshot.SourceRealpath, snapshot.SourceDigest, b.in.Manifest.Owner, skill.CanonicalID)
	related := b.relatedDuplicateObservations(candidate, snapshot.SourceDigest, b.in.Manifest.Owner, skill.CanonicalID)
	action := contract.PlanAction{
		ID:                  actionID(skill.CanonicalID, targetRef.ID, targetRef.Path),
		Action:              "install-" + mode.Effective,
		Status:              "dry-run",
		Skill:               &skill,
		SourceID:            snapshot.ID,
		Target:              targetRef,
		Mode:                mode,
		Ownership:           ownership,
		RelatedObservations: related,
	}
	if partial := firstMatchingForeign(related); partial != nil && ownership.Class == "absent" {
		action.Action = "partially-satisfied"
		action.Status = "blocked"
		action.Reason = "a selected reader already has a matching foreign/proprietary copy; writing the shared target would create a duplicate for that reader"
		conflict := b.newConflict(targetRef, skill, "", "partial-satisfaction")
		return b.resolveConflict(action, conflict, matchingDuplicateRef(candidate, partial.Path), partial)
	}
	switch ownership.Class {
	case "absent":
		action.PlannedWrites = []contract.PlannedWrite{{Kind: mode.Effective, Path: targetRef.Path}}
		if mode.Effective == "copy" {
			action.PlannedWrites = append(action.PlannedWrites, contract.PlannedWrite{Kind: "marker", Path: filepath.Join(targetRef.Path, ".skiller-install.json")})
		}
	case "ours-symlink":
		// A symlink that resolves to the managed source is always current — it serves
		// the live source, so there is nothing to refresh (design §6.4, "updates for
		// free"). No-op unconditionally; a content digest of a symlink-to-directory is
		// not a reliable freshness signal and must not force a spurious re-link, which
		// would break idempotence for every link-mode install.
		action.Action = "no-op"
		action.Reason = "symlink already resolves to the managed source"
		return action
	case "ours-copy":
		if ownership.DigestMatch != nil && *ownership.DigestMatch {
			action.Action = "no-op"
			action.Reason = "target already matches desired source"
			return action
		}
		if strings.Contains(ownership.Message, "modified") {
			action.Action = "block-conflict"
			action.Status = "blocked"
			action.Reason = ownership.Message
			conflict := b.newConflict(targetRef, skill, "", "modified-owned-copy")
			return b.resolveConflict(action, conflict, target.Ref{}, nil)
		}
		action.Action = "refresh"
		action.PlannedWrites = []contract.PlannedWrite{{Kind: mode.Effective, Path: targetRef.Path}}
	case "ours-legacy":
		if ownership.DigestMatch != nil && *ownership.DigestMatch {
			action.Action = "adopt-existing"
			action.Reason = "legacy marker belongs to an adopting tool; skiller records lineage without mutating the target"
		} else {
			action.Action = "refresh"
			action.Reason = "legacy marker belongs to an adopting tool; skiller would replace as owned on apply"
			action.PlannedWrites = []contract.PlannedWrite{{Kind: mode.Effective, Path: targetRef.Path}}
		}
	case "foreign-known", "foreign-unmanaged":
		if ownership.DigestMatch != nil && *ownership.DigestMatch {
			action.Action = "satisfied-by-foreign"
			action.Reason = "existing foreign target has the desired digest; no duplicate will be created"
			return action
		}
		action.Action = "block-conflict"
		action.Status = "blocked"
		action.Reason = "existing target is not skiller-owned and does not match desired source"
		conflict := b.newConflict(targetRef, skill, "", "foreign-target")
		return b.resolveConflict(action, conflict, target.Ref{}, nil)
	}
	return action
}

func (b *builder) classify(raw observe.RawObservation, path, sourceRealpath, desiredDigest, owner, canonicalID string) contract.ObservedOwnership {
	if !raw.Exists {
		return contract.ObservedOwnership{Class: "absent", Path: path}
	}
	match := raw.Digest != "" && desiredDigest != "" && raw.Digest == desiredDigest
	obs := contract.ObservedOwnership{
		Class:       "foreign-unmanaged",
		Path:        path,
		Digest:      raw.Digest,
		DigestMatch: boolPtr(match),
		Message:     raw.Err,
	}
	if raw.IsSymlink {
		obs.SourceRealpath = raw.SymlinkTarget
		if sourceRealpath != "" && samePath(raw.SymlinkTarget, sourceRealpath) {
			obs.Class = "ours-symlink"
		}
		return obs
	}
	if raw.SkillerMarker != nil {
		obs.MarkerPath = raw.SkillerMarker.MarkerPath
		if raw.SkillerMarker.Owner == owner && (canonicalID == "" || raw.SkillerMarker.CanonicalID == canonicalID) {
			obs.Class = "ours-copy"
			if raw.Digest != "" && raw.SkillerMarker.InstalledDigestAtInstall != "" && raw.Digest != raw.SkillerMarker.InstalledDigestAtInstall && !match {
				obs.Message = "owned copy is modified; preserve unless forced"
			}
		} else {
			obs.Class = "foreign-known"
			obs.Message = "skiller marker belongs to another owner or canonical_id"
		}
		return obs
	}
	if raw.LegacyMarker != nil {
		obs.MarkerPath = raw.LegacyMarker.MarkerPath
		obs.LegacyAdapter = raw.LegacyMarker.OwnerLabel
		if raw.LegacyMarker.Class == "ours-legacy" {
			obs.Class = "ours-legacy"
		} else {
			obs.Class = "foreign-known"
		}
		return obs
	}
	if raw.OwnershipTest != nil {
		obs.MarkerPath = raw.OwnershipTest.MarkerPath
		obs.LegacyAdapter = raw.OwnershipTest.OwnerLabel
		if raw.OwnershipTest.Class == "ours-legacy" {
			obs.Class = "ours-legacy"
		} else {
			obs.Class = "foreign-known"
		}
	}
	return obs
}

func (b *builder) relatedDuplicateObservations(candidate target.Candidate, desiredDigest, owner, canonicalID string) []contract.ObservedOwnership {
	var out []contract.ObservedOwnership
	for _, duplicate := range candidate.Duplicates {
		raw := b.in.World.Observed[duplicate.Path]
		obs := b.classify(raw, duplicate.Path, "", desiredDigest, owner, canonicalID)
		if obs.Class != "absent" {
			out = append(out, obs)
		}
	}
	return out
}

func (b *builder) resolveMode(skill manifest.Skill, targetRef target.Ref, sourceKind string) contract.PlanMode {
	mode := skill.Mode
	if mode == "" {
		mode = b.in.Manifest.DefaultMode
	}
	if mode == "" {
		mode = "link"
	}
	if targetRef.Scope == "runtime" && skill.Mode == "" {
		mode = "copy"
	}
	if sourceKind != "file" && skill.Mode == "" {
		mode = "copy"
	}
	return contract.PlanMode{
		Requested:        firstNonEmpty(skill.Mode, mode),
		Effective:        mode,
		FallbackPossible: mode == "link" && targetRef.Scope == "host",
	}
}

func (b *builder) applyDesiredCollision(action contract.PlanAction, skill contract.PlanSkill, targetRef target.Ref) contract.PlanAction {
	if existing := b.desiredByPath[targetRef.Path]; existing != "" && existing != skill.CanonicalID {
		action.Action = "block-conflict"
		action.Status = "blocked"
		action.Reason = "manifest requests the same harness-visible target path for different canonical IDs"
		conflict := b.newConflict(targetRef, skill, existing, "namespace-collision")
		return b.resolveConflict(action, conflict, target.Ref{}, nil)
	}
	b.desiredByPath[targetRef.Path] = skill.CanonicalID
	return action
}

func (b *builder) namespace() string {
	return firstNonEmpty(b.in.Options.Namespace, b.in.Manifest.Namespace, b.in.Manifest.Owner)
}

func (b *builder) addAction(action contract.PlanAction) {
	key := planActionKey(action)
	if key == "" {
		key = action.ID
	}
	if b.actionKeys[key] {
		return
	}
	b.actionKeys[key] = true
	b.actions = append(b.actions, action)
}

func planActionKey(action contract.PlanAction) string {
	if action.Skill != nil {
		return action.Skill.CanonicalID + "\x00" + action.Target.Path
	}
	if action.Extra != nil {
		return "extra:" + action.Extra.ID + "\x00" + action.Target.Path
	}
	return ""
}

func (b *builder) newConflict(targetRef target.Ref, skill contract.PlanSkill, existingCanonicalID string, status string) contract.PlanConflict {
	return contract.PlanConflict{
		ID:                  actionID("conflict", skill.CanonicalID, targetRef.Path),
		TargetKind:          targetRef.Kind,
		TargetID:            targetRef.ID,
		EffectiveName:       skill.InstallSlug,
		ExistingCanonicalID: existingCanonicalID,
		DesiredCanonicalID:  skill.CanonicalID,
		Status:              status,
		SafeChoices:         applicableConflictModes(status),
	}
}

func (b *builder) addConflict(conflict contract.PlanConflict) {
	b.conflicts = append(b.conflicts, conflict)
}

func (b *builder) resolveConflict(action contract.PlanAction, conflict contract.PlanConflict, duplicateRef target.Ref, duplicateObs *contract.ObservedOwnership) contract.PlanAction {
	action.ConflictModes = conflict.SafeChoices
	policy := b.resolutionPolicy(conflict)
	if policy == "" || policy == "prompt" || policy == "block" {
		b.addConflict(conflict)
		return action
	}
	resolved := conflict
	if !policyApplies(conflict.Status, policy) {
		action.Reason = "resolution " + policy + " is not applicable to " + conflict.Status
		b.addConflict(conflict)
		return action
	}
	resolved.Resolution = policy
	action.ConflictModes = nil
	switch policy {
	case "skip":
		action.Action = "no-op"
		action.Status = "dry-run"
		action.Reason = "conflict skipped by resolution"
		action.PlannedWrites = nil
	case "adopt-existing":
		action.Action = "adopt-existing"
		action.Status = "dry-run"
		action.Reason = "existing target accepted by resolution; skiller records lineage without mutating the target"
		action.PlannedWrites = nil
		if conflict.Status == "partial-satisfaction" && duplicateRef.Path != "" && duplicateObs != nil {
			action.ID = actionID(action.Skill.CanonicalID, duplicateRef.ID, duplicateRef.Path)
			action.Target = duplicateRef
			action.Ownership = *duplicateObs
		}
	case "replace-owned":
		action.Action = "refresh"
		action.Status = "dry-run"
		action.Reason = "owned copy replaced by resolution"
		action.PlannedWrites = []contract.PlannedWrite{{Kind: action.Mode.Effective, Path: action.Target.Path}}
	}
	b.addConflict(resolved)
	return action
}

func (b *builder) resolutionPolicy(conflict contract.PlanConflict) string {
	if b.in.Options.Resolutions != nil {
		if resolution, ok := b.in.Options.Resolutions[conflict.ID]; ok && strings.TrimSpace(resolution.Policy) != "" {
			return strings.TrimSpace(resolution.Policy)
		}
	}
	return strings.TrimSpace(b.in.Options.OnConflict)
}

func applicableConflictModes(status string) []string {
	var modes []string
	switch status {
	case "modified-owned-copy":
		modes = []string{"block", "skip", "replace-owned", "force-replace"}
	case "foreign-target":
		modes = []string{"block", "skip", "adopt-existing", "rename", "force-replace"}
	case "namespace-collision":
		modes = []string{"block", "skip", "rename"}
	case "partial-satisfaction":
		modes = []string{"block", "skip", "adopt-existing"}
	default:
		modes = allConflictModes
	}
	return append([]string(nil), modes...)
}

func policyApplies(status, policy string) bool {
	switch policy {
	case "skip":
		return true
	case "adopt-existing":
		return status == "foreign-target" || status == "partial-satisfaction"
	case "replace-owned":
		return status == "modified-owned-copy"
	default:
		return false
	}
}

func firstMatchingForeign(observations []contract.ObservedOwnership) *contract.ObservedOwnership {
	for i := range observations {
		obs := &observations[i]
		if (obs.Class == "foreign-known" || obs.Class == "foreign-unmanaged") && obs.DigestMatch != nil && *obs.DigestMatch {
			return obs
		}
	}
	return nil
}

func matchingDuplicateRef(candidate target.Candidate, path string) target.Ref {
	for _, duplicate := range candidate.Duplicates {
		if duplicate.Path == path {
			return duplicate
		}
	}
	return target.Ref{}
}

func classifyExtra(raw observe.RawObservation, path, desiredDigest string) contract.ObservedOwnership {
	if !raw.Exists {
		return contract.ObservedOwnership{Class: "absent", Path: path}
	}
	match := raw.Digest != "" && desiredDigest != "" && raw.Digest == desiredDigest
	return contract.ObservedOwnership{Class: "foreign-unmanaged", Path: path, Digest: raw.Digest, DigestMatch: boolPtr(match), Message: raw.Err}
}

func Sort(plan *Plan) {
	sort.Slice(plan.Actions, func(i, j int) bool {
		return plan.Actions[i].ID < plan.Actions[j].ID
	})
	sort.Slice(plan.Conflicts, func(i, j int) bool {
		return plan.Conflicts[i].ID < plan.Conflicts[j].ID
	})
}

func resolvePath(path, base, home string) string {
	path = expandPath(path, home)
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(base, filepath.FromSlash(path)))
}

func expandPath(path, home string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	if path == "~" {
		return home
	}
	return filepath.Clean(filepath.FromSlash(path))
}

func lockID(root string) string {
	return "target:" + hashid.Short(filepath.Clean(root))
}

func actionID(parts ...string) string {
	return "act-" + hashid.Short(strings.Join(parts, "\x00"))
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
	return filepath.Clean(a) == filepath.Clean(b)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func nonEmptyStrings(values ...string) []string {
	var out []string
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}

func boolPtr(value bool) *bool {
	return &value
}

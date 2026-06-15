package planner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mostlydev/skiller/internal/contract"
)

var conflictModes = []string{"block", "skip", "adopt-existing", "replace-owned", "rename", "force-replace"}

type Options struct {
	ManifestPath string
	Home         string
	Project      string
	Namespace    string
	OnConflict   string
}

type planBuilder struct {
	opts          Options
	catalog       contract.Catalog
	manifest      contract.Manifest
	manifestDir   string
	sources       []contract.SourceSnapshot
	conflicts     []contract.PlanConflict
	actions       []contract.PlanAction
	diagnostics   []contract.Diagnostic
	sourceIDs     map[string]string
	actionKeys    map[string]bool
	desiredByPath map[string]string
}

func Build(opts Options) (contract.Plan, error) {
	if opts.OnConflict == "" {
		opts.OnConflict = "block"
	}
	catalog, err := LoadCatalog()
	if err != nil {
		return contract.Plan{}, err
	}
	manifest, err := LoadManifest(opts.ManifestPath)
	if err != nil {
		return contract.Plan{}, err
	}
	absManifest, err := filepath.Abs(opts.ManifestPath)
	if err != nil {
		return contract.Plan{}, err
	}
	home, err := resolveHome(opts.Home)
	if err != nil {
		return contract.Plan{}, err
	}
	opts.Home = home
	if opts.Project != "" {
		if opts.Project, err = filepath.Abs(opts.Project); err != nil {
			return contract.Plan{}, err
		}
	}
	b := &planBuilder{
		opts:          opts,
		catalog:       catalog,
		manifest:      manifest,
		manifestDir:   filepath.Dir(absManifest),
		sourceIDs:     map[string]string{},
		actionKeys:    map[string]bool{},
		desiredByPath: map[string]string{},
	}
	for i, skill := range manifest.Skills {
		if err := b.planSkill(i, skill); err != nil {
			return contract.Plan{}, err
		}
	}
	for i, extra := range manifest.Extras {
		if err := b.planExtra(i, extra); err != nil {
			return contract.Plan{}, err
		}
	}
	return contract.Plan{
		Schema:    "skiller-plan.v1",
		Operation: "plan",
		DryRun:    true,
		Inputs: contract.PlanInputs{
			ManifestPath: absManifest,
			Home:         opts.Home,
			Project:      opts.Project,
			Namespace:    b.namespace(),
			OnConflict:   opts.OnConflict,
		},
		Sources:     b.sources,
		Actions:     b.actions,
		Conflicts:   b.conflicts,
		Diagnostics: b.diagnostics,
	}, nil
}

func (b *planBuilder) planSkill(index int, skill contract.ManifestSkill) error {
	if skill.Name == "" {
		return fmt.Errorf("skills[%d] missing name", index)
	}
	if skill.Source == "" {
		return fmt.Errorf("skills[%d] missing source", index)
	}
	snapshot, err := b.resolveLocalSource(skill.Source)
	if err != nil {
		return err
	}
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
	targets := append([]string(nil), skill.Targets...)
	if len(targets) == 0 && len(skill.TargetDirs) == 0 {
		targets = []string{"agents"}
	}
	for _, targetName := range targets {
		target, err := b.resolveNamedTarget(targetName, installSlug)
		if err != nil {
			return err
		}
		mode := b.resolveMode(skill.Mode, target, snapshot.SourceKind)
		action := b.planTargetAction(snapshot, planSkill, target, mode)
		action = b.applyDesiredCollision(action, planSkill, target)
		b.addAction(action)
	}
	for _, targetDir := range skill.TargetDirs {
		target, err := b.resolveExplicitTargetDir(targetDir, installSlug)
		if err != nil {
			return err
		}
		mode := b.resolveMode(firstNonEmpty(targetDir.Mode, skill.Mode), target, snapshot.SourceKind)
		action := b.planTargetAction(snapshot, planSkill, target, mode)
		action = b.applyDesiredCollision(action, planSkill, target)
		b.addAction(action)
	}
	return nil
}

func (b *planBuilder) applyDesiredCollision(action contract.PlanAction, skill contract.PlanSkill, target contract.PlanTarget) contract.PlanAction {
	if existing := b.desiredByPath[target.Path]; existing != "" && existing != skill.CanonicalID {
		action.Action = "block-conflict"
		action.Status = "blocked"
		action.Reason = "manifest requests the same harness-visible target path for different canonical IDs"
		action.ConflictModes = conflictModes
		b.addConflictWithExisting(target, skill, existing, "namespace-collision")
		return action
	}
	b.desiredByPath[target.Path] = skill.CanonicalID
	return action
}

func (b *planBuilder) planExtra(index int, extra contract.ManifestExtra) error {
	if extra.ID == "" {
		return fmt.Errorf("extras[%d] missing id", index)
	}
	if extra.Source == "" || extra.Target == "" {
		return fmt.Errorf("extras[%d] missing source or target", index)
	}
	source := b.resolvePath(extra.Source, b.manifestDir)
	target := b.expandPath(extra.Target)
	root := filepath.Dir(target)
	mode := firstNonEmpty(extra.Mode, "copy")
	ownership := classifyExtra(target)
	action := contract.PlanAction{
		ID:     actionID("extra", extra.ID, target),
		Action: "install-extra",
		Status: "dry-run",
		Extra: &contract.PlanExtra{
			ID:     extra.ID,
			Source: source,
			Target: target,
		},
		Target: contract.PlanTarget{
			ID:      firstNonEmpty(extra.Harness, "explicit-extra"),
			Kind:    "extra",
			Scope:   "host",
			Root:    root,
			Path:    target,
			LockID:  lockID(root),
			Readers: nonEmptyStrings(extra.Harness),
		},
		Mode:      contract.PlanMode{Requested: mode, Effective: mode},
		Ownership: ownership,
		PlannedWrites: []contract.PlannedWrite{
			{Kind: "file", Path: target},
		},
	}
	if extra.OwnedMarker {
		action.PlannedWrites = append(action.PlannedWrites, contract.PlannedWrite{
			Kind: "sidecar-marker",
			Path: target + ".skiller-install.json",
		})
	}
	b.addAction(action)
	return nil
}

func (b *planBuilder) planTargetAction(source contract.SourceSnapshot, skill contract.PlanSkill, target contract.PlanTarget, mode contract.PlanMode) contract.PlanAction {
	skill.RequestedTargets = target.RequestedTargets
	ownership := b.classifyTarget(target.Path, source.SourceRealpath, source.SourceDigest, b.manifest.Owner, skill.CanonicalID, skill.InstallSlug)
	related := b.relatedDuplicateObservations(target, skill.InstallSlug, source.SourceDigest)
	action := contract.PlanAction{
		ID:                  actionID(skill.CanonicalID, target.ID, target.Path),
		Action:              "install-" + mode.Effective,
		Status:              "dry-run",
		Skill:               &skill,
		SourceID:            source.ID,
		Target:              target,
		Mode:                mode,
		Ownership:           ownership,
		RelatedObservations: related,
	}
	if partial := firstMatchingForeign(related); partial != nil && ownership.Class == "absent" {
		action.Action = "partially-satisfied"
		action.Status = "blocked"
		action.Reason = "a selected reader already has a matching foreign/proprietary copy; writing the shared target would create a duplicate for that reader"
		action.ConflictModes = conflictModes
		b.addConflict(target, skill, "partial-satisfaction")
		return action
	}
	switch ownership.Class {
	case "absent":
		action.PlannedWrites = []contract.PlannedWrite{{Kind: mode.Effective, Path: target.Path}}
		if mode.Effective == "copy" {
			action.PlannedWrites = append(action.PlannedWrites, contract.PlannedWrite{Kind: "marker", Path: filepath.Join(target.Path, ".skiller-install.json")})
		}
	case "ours-symlink", "ours-copy":
		if ownership.DigestMatch != nil && *ownership.DigestMatch {
			action.Action = "no-op"
			action.Reason = "target already matches desired source"
			return action
		}
		if ownership.Class == "ours-copy" && strings.Contains(ownership.Message, "modified") {
			action.Action = "block-conflict"
			action.Status = "blocked"
			action.Reason = ownership.Message
			action.ConflictModes = conflictModes
			b.addConflict(target, skill, "modified-owned-copy")
			return action
		}
		action.Action = "refresh"
		action.PlannedWrites = []contract.PlannedWrite{{Kind: mode.Effective, Path: target.Path}}
	case "ours-legacy":
		if ownership.DigestMatch != nil && *ownership.DigestMatch {
			action.Action = "adopt-existing"
			action.Reason = "legacy marker belongs to an adopting tool; skiller would record lineage and upgrade marker on apply"
		} else {
			action.Action = "refresh"
			action.Reason = "legacy marker belongs to an adopting tool; skiller would replace as owned on apply"
			action.PlannedWrites = []contract.PlannedWrite{{Kind: mode.Effective, Path: target.Path}}
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
		action.ConflictModes = conflictModes
		b.addConflict(target, skill, "foreign-target")
	}
	return action
}

func (b *planBuilder) resolveLocalSource(spec string) (contract.SourceSnapshot, error) {
	sourcePath := b.resolvePath(spec, b.manifestDir)
	realpath, err := filepath.EvalSymlinks(sourcePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return contract.SourceSnapshot{}, fmt.Errorf("source %s does not exist", sourcePath)
		}
		realpath = sourcePath
	}
	digest, err := digestPath(realpath)
	if err != nil {
		return contract.SourceSnapshot{}, err
	}
	key := "file:" + shortHash(realpath)
	if existing := b.sourceIDs[key]; existing != "" {
		for _, source := range b.sources {
			if source.ID == existing {
				return source, nil
			}
		}
	}
	name, description := parseSkillFrontmatter(filepath.Join(realpath, "SKILL.md"))
	source := contract.SourceSnapshot{
		ID:              fmt.Sprintf("source-%03d", len(b.sources)+1),
		SourceKind:      "file",
		OriginalSpec:    spec,
		CanonicalURI:    fileURI(realpath),
		SourceKey:       key,
		SourceStatus:    "refreshed",
		LocalCachePath:  realpath,
		SourceRealpath:  realpath,
		SourceDigest:    digest,
		FrontmatterName: name,
		Description:     description,
	}
	b.sourceIDs[key] = source.ID
	b.sources = append(b.sources, source)
	return source, nil
}

func (b *planBuilder) resolveNamedTarget(name, installSlug string) (contract.PlanTarget, error) {
	h, requested, ok := b.lookupHarness(name)
	if !ok {
		return contract.PlanTarget{}, fmt.Errorf("unknown target %q", name)
	}
	writeHarness := h
	if h.PrimaryTarget != "" {
		var found bool
		writeHarness, _, found = b.lookupHarness(h.PrimaryTarget)
		if !found {
			return contract.PlanTarget{}, fmt.Errorf("harness %q primary_target %q not found", h.ID, h.PrimaryTarget)
		}
	}
	root := b.targetRoot(writeHarness)
	path := filepath.Join(root, installSlug)
	readers := append([]string(nil), h.Readers...)
	if writeHarness.ID == h.ID {
		readers = append([]string(nil), writeHarness.Readers...)
	}
	return contract.PlanTarget{
		ID:               writeHarness.ID,
		Kind:             writeHarness.Kind,
		Scope:            "host",
		Root:             root,
		Path:             path,
		Readers:          readers,
		RequestedTargets: []string{requested},
		LockID:           lockID(root),
	}, nil
}

func (b *planBuilder) resolveExplicitTargetDir(targetDir contract.ManifestTargetDir, installSlug string) (contract.PlanTarget, error) {
	if targetDir.Path == "" {
		return contract.PlanTarget{}, fmt.Errorf("target_dirs entry missing path")
	}
	root := b.resolveTargetDirPath(targetDir.Path)
	scope := firstNonEmpty(targetDir.Scope, "runtime")
	kind := firstNonEmpty(targetDir.Kind, scope)
	id := firstNonEmpty(targetDir.ID, "target-dir:"+root)
	return contract.PlanTarget{
		ID:               id,
		Kind:             kind,
		Scope:            scope,
		Root:             root,
		Path:             filepath.Join(root, installSlug),
		Readers:          nonEmptyStrings(targetDir.Reader),
		RequestedTargets: []string{id},
		LockID:           lockID(root),
	}, nil
}

func (b *planBuilder) resolveMode(requested string, target contract.PlanTarget, sourceKind string) contract.PlanMode {
	mode := requested
	if mode == "" {
		mode = b.manifest.DefaultMode
	}
	if mode == "" {
		mode = "link"
	}
	if target.Scope == "runtime" && requested == "" {
		mode = "copy"
	}
	if sourceKind != "file" && requested == "" {
		mode = "copy"
	}
	return contract.PlanMode{
		Requested:        firstNonEmpty(requested, mode),
		Effective:        mode,
		FallbackPossible: mode == "link" && target.Scope == "host",
	}
}

func (b *planBuilder) namespace() string {
	return firstNonEmpty(b.opts.Namespace, b.manifest.Namespace, b.manifest.Owner)
}

func (b *planBuilder) addAction(action contract.PlanAction) {
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

func (b *planBuilder) addConflict(target contract.PlanTarget, skill contract.PlanSkill, status string) {
	b.addConflictWithExisting(target, skill, "", status)
}

func (b *planBuilder) addConflictWithExisting(target contract.PlanTarget, skill contract.PlanSkill, existingCanonicalID string, status string) {
	b.conflicts = append(b.conflicts, contract.PlanConflict{
		ID:                  actionID("conflict", skill.CanonicalID, target.Path),
		TargetKind:          target.Kind,
		TargetID:            target.ID,
		EffectiveName:       skill.InstallSlug,
		ExistingCanonicalID: existingCanonicalID,
		DesiredCanonicalID:  skill.CanonicalID,
		Status:              status,
		SafeChoices:         conflictModes,
	})
}

func (b *planBuilder) lookupHarness(name string) (contract.Harness, string, bool) {
	for _, h := range b.catalog.Harnesses {
		if h.ID == name {
			return h, name, true
		}
		for _, alias := range h.Aliases {
			if alias == name {
				return h, name, true
			}
		}
	}
	return contract.Harness{}, name, false
}

func (b *planBuilder) targetRoot(h contract.Harness) string {
	if b.opts.Project != "" && h.ProjectDir != "" {
		if filepath.IsAbs(h.ProjectDir) {
			return b.expandPath(h.ProjectDir)
		}
		return filepath.Clean(filepath.Join(b.opts.Project, filepath.FromSlash(h.ProjectDir)))
	}
	if h.EnvHome != "" {
		if value := strings.TrimSpace(os.Getenv(h.EnvHome)); value != "" {
			return filepath.Join(value, "skills")
		}
	}
	return b.expandPath(h.GlobalDir)
}

func (b *planBuilder) resolveTargetDirPath(path string) string {
	expanded := b.expandPath(path)
	if filepath.IsAbs(expanded) {
		return expanded
	}
	base := b.opts.Project
	if base == "" {
		base = b.manifestDir
	}
	return filepath.Clean(filepath.Join(base, filepath.FromSlash(expanded)))
}

func (b *planBuilder) relatedDuplicateObservations(target contract.PlanTarget, installSlug, desiredDigest string) []contract.ObservedOwnership {
	if target.Scope != "host" {
		return nil
	}
	readerSet := map[string]bool{}
	for _, reader := range target.Readers {
		readerSet[reader] = true
	}
	var out []contract.ObservedOwnership
	for _, h := range b.catalog.Harnesses {
		if !readerSet[h.ID] && !anyIn(readerSet, h.Readers) {
			continue
		}
		for _, dir := range h.DuplicateDirs {
			root := b.expandPath(dir)
			path := filepath.Join(root, installSlug)
			if samePath(path, target.Path) {
				continue
			}
			obs := b.classifyTarget(path, "", desiredDigest, b.manifest.Owner, "", installSlug)
			if obs.Class != "absent" {
				out = append(out, obs)
			}
		}
	}
	return out
}

func (b *planBuilder) classifyTarget(path, sourceRealpath, desiredDigest, owner, canonicalID, installSlug string) contract.ObservedOwnership {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return contract.ObservedOwnership{Class: "absent", Path: path}
		}
		return contract.ObservedOwnership{Class: "foreign-unmanaged", Path: path, Message: err.Error()}
	}
	var digest string
	if d, err := digestPath(path); err == nil {
		digest = d
	}
	match := digest != "" && desiredDigest != "" && digest == desiredDigest
	obs := contract.ObservedOwnership{
		Class:       "foreign-unmanaged",
		Path:        path,
		Digest:      digest,
		DigestMatch: boolPtr(match),
	}
	if info.Mode()&os.ModeSymlink != 0 {
		linkTarget, err := filepath.EvalSymlinks(path)
		if err == nil {
			obs.SourceRealpath = linkTarget
			if sourceRealpath != "" && samePath(linkTarget, sourceRealpath) {
				obs.Class = "ours-symlink"
			}
		}
		return obs
	}
	if marker, ok := readSkillerMarker(path); ok {
		obs.MarkerPath = filepath.Join(path, ".skiller-install.json")
		if marker.Owner == owner && (canonicalID == "" || marker.CanonicalID == canonicalID) {
			obs.Class = "ours-copy"
			if digest != "" && marker.InstalledDigestAtInstall != "" && digest != marker.InstalledDigestAtInstall && !match {
				obs.Message = "owned copy is modified; preserve unless forced"
			}
		} else {
			obs.Class = "foreign-known"
			obs.Message = "skiller marker belongs to another owner or canonical_id"
		}
		return obs
	}
	for _, marker := range b.catalog.Markers {
		if marker.MarkerFile != "" {
			markerPath := filepath.Join(path, marker.MarkerFile)
			if _, err := os.Stat(markerPath); err == nil {
				obs.MarkerPath = markerPath
				obs.LegacyAdapter = marker.OwnerLabel
				if marker.Class == "ours-legacy" {
					obs.Class = "ours-legacy"
				} else {
					obs.Class = "foreign-known"
				}
				return obs
			}
		}
		if marker.OwnershipTest == "clawdapus-global-marker" && installSlug == "clawdapus-cli" {
			if _, err := os.Stat(filepath.Join(b.opts.Home, ".claw", "skill-installed")); err == nil {
				if strings.Contains(path, filepath.Join(".agents", "skills")) || strings.Contains(path, filepath.Join(".claude", "skills")) {
					obs.Class = "foreign-known"
					obs.LegacyAdapter = marker.OwnerLabel
					obs.MarkerPath = filepath.Join(b.opts.Home, ".claw", "skill-installed")
					return obs
				}
			}
		}
	}
	return obs
}

type skillerMarker struct {
	Owner                    string `json:"owner"`
	CanonicalID              string `json:"canonical_id"`
	InstalledDigestAtInstall string `json:"installed_digest_at_install"`
	Installer                struct {
		Name string `json:"name"`
	} `json:"installer"`
}

func readSkillerMarker(dir string) (skillerMarker, bool) {
	data, err := os.ReadFile(filepath.Join(dir, ".skiller-install.json"))
	if err != nil {
		return skillerMarker{}, false
	}
	var marker skillerMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		return skillerMarker{}, false
	}
	return marker, marker.Installer.Name == "skiller"
}

func firstMatchingForeign(observations []contract.ObservedOwnership) *contract.ObservedOwnership {
	for i := range observations {
		obs := &observations[i]
		if (obs.Class == "foreign-known" || obs.Class == "foreign-unmanaged" || obs.Class == "satisfied-by-foreign") && obs.DigestMatch != nil && *obs.DigestMatch {
			return obs
		}
	}
	return nil
}

func classifyExtra(path string) contract.ObservedOwnership {
	if _, err := os.Lstat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return contract.ObservedOwnership{Class: "absent", Path: path}
		}
		return contract.ObservedOwnership{Class: "foreign-unmanaged", Path: path, Message: err.Error()}
	}
	return contract.ObservedOwnership{Class: "foreign-unmanaged", Path: path}
}

func digestPath(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	if !info.IsDir() {
		if err := hashFile(hash, path, filepath.Base(path)); err != nil {
			return "", err
		}
		return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
	}
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if isInstallerMetadata(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(path, p)
		if err != nil {
			return err
		}
		return hashFile(hash, p, filepath.ToSlash(rel))
	})
	if err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func hashFile(hash io.Writer, path, name string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := io.WriteString(hash, "file\x00"+name+"\x00"); err != nil {
		return err
	}
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	_, err = io.WriteString(hash, "\x00")
	return err
}

func isInstallerMetadata(name string) bool {
	switch name {
	case ".skiller-install.json", ".our-managed.json", ".gnit-skill-managed":
		return true
	default:
		return false
	}
}

func parseSkillFrontmatter(path string) (string, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	text := string(data)
	if !strings.HasPrefix(text, "---\n") {
		return "", ""
	}
	end := strings.Index(text[4:], "\n---")
	if end < 0 {
		return "", ""
	}
	frontmatter := text[4 : 4+end]
	var name, description string
	for _, line := range strings.Split(frontmatter, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), "\"'")
		switch strings.TrimSpace(key) {
		case "name":
			name = value
		case "description":
			description = value
		}
	}
	return name, description
}

func resolveHome(home string) (string, error) {
	if home == "" {
		return os.UserHomeDir()
	}
	return filepath.Abs(home)
}

func (b *planBuilder) resolvePath(path, base string) string {
	path = b.expandPath(path)
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(base, filepath.FromSlash(path)))
}

func (b *planBuilder) expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(b.opts.Home, path[2:])
	}
	if path == "~" {
		return b.opts.Home
	}
	return filepath.Clean(os.ExpandEnv(filepath.FromSlash(path)))
}

func fileURI(path string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	return u.String()
}

func lockID(root string) string {
	return "target:" + shortHash(filepath.Clean(root))
}

func actionID(parts ...string) string {
	return "act-" + shortHash(strings.Join(parts, "\x00"))
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:16]
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	ar, err := filepath.EvalSymlinks(a)
	if err == nil {
		a = ar
	}
	br, err := filepath.EvalSymlinks(b)
	if err == nil {
		b = br
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

func anyIn(set map[string]bool, values []string) bool {
	for _, value := range values {
		if set[value] {
			return true
		}
	}
	return false
}

func SortPlan(plan *contract.Plan) {
	sort.Slice(plan.Actions, func(i, j int) bool {
		return plan.Actions[i].ID < plan.Actions[j].ID
	})
	sort.Slice(plan.Conflicts, func(i, j int) bool {
		return plan.Conflicts[i].ID < plan.Conflicts[j].ID
	})
}

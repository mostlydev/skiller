package target

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mostlydev/skiller/internal/contract"
	"github.com/mostlydev/skiller/internal/hashid"
	"github.com/mostlydev/skiller/pkg/manifest"
	"github.com/mostlydev/skiller/pkg/registry"
)

type Ref = contract.PlanTarget

type Candidate struct {
	Skill      manifest.Skill
	Target     Ref
	Duplicates []Ref
}

type ExtraCandidate struct {
	Extra  manifest.Extra
	Target Ref
}

type Options struct {
	Home        string
	Project     string
	ManifestDir string
	EnvHomes    map[string]string
}

func Resolve(m manifest.Manifest, reg registry.Catalog, opts Options) ([]Candidate, error) {
	var out []Candidate
	for _, skill := range m.Skills {
		installSlug := firstNonEmpty(skill.InstallSlug, skill.Name)
		targets := append([]string(nil), skill.Targets...)
		if len(targets) == 0 && len(skill.TargetDirs) == 0 {
			targets = []string{"agents"}
		}
		for _, name := range targets {
			ref, err := resolveNamedTarget(reg, opts, name, installSlug)
			if err != nil {
				return nil, err
			}
			out = append(out, Candidate{
				Skill:      skill,
				Target:     ref,
				Duplicates: duplicateRefs(reg, opts, ref, installSlug),
			})
		}
		for _, targetDir := range skill.TargetDirs {
			ref, err := resolveExplicitTargetDir(opts, targetDir, installSlug)
			if err != nil {
				return nil, err
			}
			out = append(out, Candidate{Skill: skill, Target: ref})
		}
	}
	return out, nil
}

func ResolveExtras(m manifest.Manifest, opts Options) ([]ExtraCandidate, error) {
	var out []ExtraCandidate
	for i, extra := range m.Extras {
		if extra.ID == "" {
			return nil, fmt.Errorf("extras[%d] missing id", i)
		}
		if extra.Source == "" || extra.Target == "" {
			return nil, fmt.Errorf("extras[%d] missing source or target", i)
		}
		targetPath := expandPath(extra.Target, opts.Home)
		root := filepath.Dir(targetPath)
		out = append(out, ExtraCandidate{
			Extra: extra,
			Target: Ref{
				ID:      firstNonEmpty(extra.Harness, "explicit-extra"),
				Kind:    "extra",
				Scope:   "host",
				Root:    root,
				Path:    targetPath,
				Readers: nonEmptyStrings(extra.Harness),
				LockID:  lockID(root),
			},
		})
	}
	return out, nil
}

func resolveNamedTarget(reg registry.Catalog, opts Options, name, installSlug string) (Ref, error) {
	h, requested, ok := lookupHarness(reg, name)
	if !ok {
		return Ref{}, fmt.Errorf("unknown target %q", name)
	}
	writeHarness := h
	if h.PrimaryTarget != "" {
		var found bool
		writeHarness, _, found = lookupHarness(reg, h.PrimaryTarget)
		if !found {
			return Ref{}, fmt.Errorf("harness %q primary_target %q not found", h.ID, h.PrimaryTarget)
		}
	}
	root := targetRoot(writeHarness, opts)
	path := filepath.Join(root, installSlug)
	readers := append([]string(nil), h.Readers...)
	if writeHarness.ID == h.ID {
		readers = append([]string(nil), writeHarness.Readers...)
	}
	return Ref{
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

func resolveExplicitTargetDir(opts Options, targetDir manifest.TargetDir, installSlug string) (Ref, error) {
	if targetDir.Path == "" {
		return Ref{}, fmt.Errorf("target_dirs entry missing path")
	}
	root := resolveTargetDirPath(opts, targetDir.Path)
	scope := firstNonEmpty(targetDir.Scope, "runtime")
	kind := firstNonEmpty(targetDir.Kind, scope)
	id := firstNonEmpty(targetDir.ID, "target-dir:"+root)
	return Ref{
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

func duplicateRefs(reg registry.Catalog, opts Options, ref Ref, installSlug string) []Ref {
	if ref.Scope != "host" {
		return nil
	}
	readerSet := map[string]bool{}
	for _, reader := range ref.Readers {
		readerSet[reader] = true
	}
	var out []Ref
	for _, h := range reg.Harnesses {
		if !readerSet[h.ID] && !anyIn(readerSet, h.Readers) {
			continue
		}
		for _, dir := range h.DuplicateDirs {
			root := expandPath(dir, opts.Home)
			path := filepath.Join(root, installSlug)
			if samePath(path, ref.Path) {
				continue
			}
			out = append(out, Ref{
				ID:               h.ID + "-duplicate",
				Kind:             "proprietary",
				Scope:            "host",
				Root:             root,
				Path:             path,
				Readers:          append([]string(nil), h.Readers...),
				RequestedTargets: []string{h.ID},
				LockID:           lockID(root),
			})
		}
	}
	return out
}

func lookupHarness(reg registry.Catalog, name string) (registry.Harness, string, bool) {
	for _, h := range reg.Harnesses {
		if h.ID == name {
			return h, name, true
		}
		for _, alias := range h.Aliases {
			if alias == name {
				return h, name, true
			}
		}
	}
	return registry.Harness{}, name, false
}

func targetRoot(h registry.Harness, opts Options) string {
	if opts.Project != "" && h.ProjectDir != "" {
		if filepath.IsAbs(h.ProjectDir) {
			return expandPath(h.ProjectDir, opts.Home)
		}
		return filepath.Clean(filepath.Join(opts.Project, filepath.FromSlash(h.ProjectDir)))
	}
	if h.EnvHome != "" {
		if value := strings.TrimSpace(opts.EnvHomes[h.EnvHome]); value != "" {
			return filepath.Join(value, "skills")
		}
	}
	return expandPath(h.GlobalDir, opts.Home)
}

func resolveTargetDirPath(opts Options, path string) string {
	expanded := expandPath(path, opts.Home)
	if filepath.IsAbs(expanded) {
		return expanded
	}
	base := opts.Project
	if base == "" {
		base = opts.ManifestDir
	}
	return filepath.Clean(filepath.Join(base, filepath.FromSlash(expanded)))
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

func anyIn(set map[string]bool, values []string) bool {
	for _, value := range values {
		if set[value] {
			return true
		}
	}
	return false
}

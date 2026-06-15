package prune

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mostlydev/skiller/internal/contract"
	"github.com/mostlydev/skiller/internal/hashid"
	"github.com/mostlydev/skiller/pkg/install"
	"github.com/mostlydev/skiller/pkg/source"
	"github.com/mostlydev/skiller/pkg/target"
)

type Inputs struct {
	Candidates    []target.Candidate
	SourcesBySpec map[string]source.Snapshot
	Namespace     string
}

func CleanupDuplicates(in Inputs) install.Result {
	result := install.Result{Schema: "skiller-apply-result.v1", Actions: []install.ActionResult{}}
	for _, candidate := range in.Candidates {
		snapshot := in.SourcesBySpec[candidate.Skill.Source]
		skill := planSkill(candidate, snapshot, in.Namespace)
		for _, duplicate := range candidate.Duplicates {
			result.Actions = append(result.Actions, cleanupDuplicate(skill, duplicate, snapshot, true))
		}
	}
	return result
}

func Plan(in Inputs) []contract.PlanAction {
	var actions []contract.PlanAction
	for _, candidate := range in.Candidates {
		snapshot := in.SourcesBySpec[candidate.Skill.Source]
		skill := planSkill(candidate, snapshot, in.Namespace)
		for _, duplicate := range candidate.Duplicates {
			result := cleanupDuplicate(skill, duplicate, snapshot, false)
			action := contract.PlanAction{
				ID:            result.ID,
				Action:        result.Action,
				Status:        result.Status,
				Reason:        result.Reason,
				Skill:         result.Skill,
				Target:        result.Target,
				Mode:          contract.PlanMode{Requested: result.RequestedMode, Effective: result.EffectiveMode},
				Ownership:     ownershipForDuplicate(result, snapshot),
				PlannedWrites: result.Writes,
			}
			actions = append(actions, action)
		}
	}
	return actions
}

func cleanupDuplicate(skill contract.PlanSkill, duplicate target.Ref, snapshot source.Snapshot, execute bool) install.ActionResult {
	out := install.ActionResult{
		ID:            "cleanup:" + hashid.Short(skill.CanonicalID+"\x00"+duplicate.Path),
		Action:        "skip-duplicate",
		Status:        "skipped",
		Target:        duplicate,
		Skill:         &skill,
		RequestedMode: "link",
		EffectiveMode: "link",
	}
	info, err := os.Lstat(duplicate.Path)
	if err != nil {
		if os.IsNotExist(err) {
			out.Reason = "duplicate not present"
			return out
		}
		return failed(out, err.Error())
	}
	if info.Mode()&os.ModeSymlink == 0 {
		out.Reason = "duplicate is not a symlink; preserved"
		return out
	}
	realpath, err := filepath.EvalSymlinks(duplicate.Path)
	if err != nil {
		return failed(out, err.Error())
	}
	if !samePath(realpath, snapshot.SourceRealpath) {
		out.Reason = "duplicate symlink does not resolve to managed source; preserved"
		return out
	}
	out.Action = "remove-duplicate"
	out.Writes = []contract.PlannedWrite{{Kind: "remove", Path: duplicate.Path}}
	if !execute {
		out.Status = "dry-run"
		return out
	}
	if err := os.Remove(duplicate.Path); err != nil {
		return failed(out, err.Error())
	}
	out.Status = "removed"
	return out
}

func ownershipForDuplicate(result install.ActionResult, snapshot source.Snapshot) contract.ObservedOwnership {
	ownership := contract.ObservedOwnership{Class: "foreign-unmanaged", Path: result.Target.Path}
	if result.Reason == "duplicate not present" {
		ownership.Class = "absent"
		return ownership
	}
	if result.Action == "remove-duplicate" {
		ownership.Class = "ours-symlink"
		ownership.SourceRealpath = snapshot.SourceRealpath
	}
	return ownership
}

func planSkill(candidate target.Candidate, snapshot source.Snapshot, namespace string) contract.PlanSkill {
	installSlug := firstNonEmpty(candidate.Skill.InstallSlug, candidate.Skill.Name)
	return contract.PlanSkill{
		CanonicalID:     firstNonEmpty(candidate.Skill.CanonicalID, namespace+":"+candidate.Skill.Name),
		Namespace:       namespace,
		Name:            candidate.Skill.Name,
		InstallSlug:     installSlug,
		FrontmatterName: snapshot.FrontmatterName,
		Description:     snapshot.Description,
	}
}

func failed(result install.ActionResult, message string) install.ActionResult {
	result.Status = "failed"
	result.Error = message
	return result
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

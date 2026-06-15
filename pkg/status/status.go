package status

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/mostlydev/skiller/internal/contract"
	"github.com/mostlydev/skiller/internal/hashid"
	"github.com/mostlydev/skiller/pkg/state"
)

type Report struct {
	Schema      string                  `json:"schema"`
	Items       []Item                  `json:"items"`
	Conflicts   []contract.PlanConflict `json:"conflicts,omitempty"`
	Diagnostics []contract.Diagnostic   `json:"diagnostics,omitempty"`
}

type Item struct {
	CanonicalID string                     `json:"canonical_id"`
	InstallSlug string                     `json:"install_slug"`
	Target      contract.PlanTarget        `json:"target"`
	Status      string                     `json:"status"`
	Ownership   contract.ObservedOwnership `json:"ownership"`
	Source      *contract.SourceSnapshot   `json:"source,omitempty"`
}

type Inputs struct {
	Plan        *contract.Plan
	Ledger      state.Ledger
	Diagnostics []contract.Diagnostic
}

type ConflictReport struct {
	Schema      string                `json:"schema"`
	Conflicts   []ConflictItem        `json:"conflicts"`
	Diagnostics []contract.Diagnostic `json:"diagnostics,omitempty"`
}

type ConflictItem struct {
	Provenance string                `json:"provenance"`
	Conflict   contract.PlanConflict `json:"conflict"`
}

func Build(in Inputs) Report {
	report := Report{
		Schema:      "skiller-status.v1",
		Items:       []Item{},
		Diagnostics: append([]contract.Diagnostic(nil), in.Diagnostics...),
	}
	seen := map[string]bool{}
	sources := planSources(in.Plan)
	if in.Plan != nil {
		for _, action := range in.Plan.Actions {
			if action.Skill == nil {
				continue
			}
			item := Item{
				CanonicalID: action.Skill.CanonicalID,
				InstallSlug: action.Skill.InstallSlug,
				Target:      action.Target,
				Status:      statusForAction(action),
				Ownership:   action.Ownership,
			}
			if source, ok := sources[action.SourceID]; ok {
				s := source
				item.Source = &s
			}
			report.Items = append(report.Items, item)
			seen[itemKey(item.CanonicalID, item.Target.Path)] = true
		}
		report.Conflicts = append(report.Conflicts, in.Plan.Conflicts...)
	}
	skills := ledgerSkills(in.Ledger)
	for _, install := range in.Ledger.Installs {
		skill, ok := skills[install.SkillID]
		if !ok {
			report.Diagnostics = append(report.Diagnostics, contract.Diagnostic{
				Level:   "warning",
				Message: "state install references unknown skill; reporting as orphaned",
				Path:    install.TargetPath,
			})
			report.Items = append(report.Items, Item{
				CanonicalID: "orphaned:" + install.SkillID,
				InstallSlug: install.SkillID,
				Target:      targetFromInstall(install),
				Status:      "orphaned",
				Ownership:   contract.ObservedOwnership{Class: "absent", Path: install.TargetPath},
			})
			continue
		}
		if seen[itemKey(skill.CanonicalID, install.TargetPath)] {
			continue
		}
		report.Items = append(report.Items, Item{
			CanonicalID: skill.CanonicalID,
			InstallSlug: skill.InstallSlug,
			Target:      targetFromInstall(install),
			Status:      "not-seen",
			Ownership: contract.ObservedOwnership{
				Class:         "absent",
				Path:          install.TargetPath,
				MarkerPath:    install.MarkerPath,
				LegacyAdapter: install.LegacyAdapter,
			},
		})
	}
	report.Conflicts = mergePlanConflicts(report.Conflicts, in.Ledger.Conflicts)
	Sort(&report)
	return report
}

func Conflicts(plan *contract.Plan, ledger state.Ledger, diagnostics []contract.Diagnostic) ConflictReport {
	byID := map[string]ConflictItem{}
	if plan != nil {
		for _, conflict := range plan.Conflicts {
			byID[conflict.ID] = ConflictItem{Provenance: "live-plan", Conflict: conflict}
		}
	}
	for _, conflict := range ledger.Conflicts {
		if item, ok := byID[conflict.ID]; ok {
			item.Provenance = "both"
			byID[conflict.ID] = item
			continue
		}
		byID[conflict.ID] = ConflictItem{Provenance: "ledger", Conflict: conflict}
	}
	out := ConflictReport{
		Schema:      "skiller-conflicts.v1",
		Conflicts:   make([]ConflictItem, 0, len(byID)),
		Diagnostics: append([]contract.Diagnostic(nil), diagnostics...),
	}
	for _, item := range byID {
		out.Conflicts = append(out.Conflicts, item)
	}
	sort.Slice(out.Conflicts, func(i, j int) bool {
		return out.Conflicts[i].Conflict.ID < out.Conflicts[j].Conflict.ID
	})
	return out
}

func Sort(report *Report) {
	sort.Slice(report.Items, func(i, j int) bool {
		if report.Items[i].CanonicalID != report.Items[j].CanonicalID {
			return report.Items[i].CanonicalID < report.Items[j].CanonicalID
		}
		return report.Items[i].Target.Path < report.Items[j].Target.Path
	})
	sort.Slice(report.Conflicts, func(i, j int) bool {
		return report.Conflicts[i].ID < report.Conflicts[j].ID
	})
}

func statusForAction(action contract.PlanAction) string {
	switch action.Action {
	case "no-op", "adopt-existing":
		return "installed"
	case "install-link", "install-copy", "install-extra":
		return "not-installed"
	case "refresh":
		return "stale"
	case "satisfied-by-foreign":
		return "satisfied-by-foreign"
	case "partially-satisfied":
		return "partially-satisfied"
	case "block-conflict":
		if strings.Contains(action.Ownership.Message, "modified") {
			return "modified"
		}
		return "blocked"
	default:
		if action.Status == "blocked" {
			return "blocked"
		}
		return "not-installed"
	}
}

func planSources(plan *contract.Plan) map[string]contract.SourceSnapshot {
	out := map[string]contract.SourceSnapshot{}
	if plan == nil {
		return out
	}
	for _, source := range plan.Sources {
		out[source.ID] = source
	}
	return out
}

func ledgerSkills(ledger state.Ledger) map[string]state.SkillRecord {
	out := map[string]state.SkillRecord{}
	for _, skill := range ledger.Skills {
		out[skill.ID] = skill
	}
	return out
}

func targetFromInstall(install state.InstallRecord) contract.PlanTarget {
	root := filepath.Dir(install.TargetPath)
	return contract.PlanTarget{
		ID:     install.TargetID,
		Kind:   install.TargetKind,
		Scope:  install.Scope,
		Root:   root,
		Path:   install.TargetPath,
		LockID: "target:" + hashid.Short(filepath.Clean(root)),
	}
}

func itemKey(canonicalID, targetPath string) string {
	return canonicalID + "\x00" + targetPath
}

func mergePlanConflicts(live, ledger []contract.PlanConflict) []contract.PlanConflict {
	byID := map[string]contract.PlanConflict{}
	for _, conflict := range live {
		byID[conflict.ID] = conflict
	}
	for _, conflict := range ledger {
		if _, ok := byID[conflict.ID]; !ok {
			byID[conflict.ID] = conflict
		}
	}
	out := make([]contract.PlanConflict, 0, len(byID))
	for _, conflict := range byID {
		out = append(out, conflict)
	}
	return out
}

package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mostlydev/skiller/internal/contract"
	"github.com/mostlydev/skiller/internal/fsutil"
	"github.com/mostlydev/skiller/internal/hashid"
	"github.com/mostlydev/skiller/internal/lock"
	"github.com/mostlydev/skiller/pkg/install"
	"github.com/mostlydev/skiller/pkg/manifest"
	"github.com/mostlydev/skiller/pkg/observe"
	planpkg "github.com/mostlydev/skiller/pkg/plan"
	"github.com/mostlydev/skiller/pkg/prune"
	"github.com/mostlydev/skiller/pkg/registry"
	"github.com/mostlydev/skiller/pkg/source"
	"github.com/mostlydev/skiller/pkg/state"
	"github.com/mostlydev/skiller/pkg/status"
	"github.com/mostlydev/skiller/pkg/target"
)

type Options = planpkg.Options

type StatusOptions struct {
	ManifestPath string
	Home         string
	Project      string
	Namespace    string
	StateDir     string
}

type ApplyOptions struct {
	ManifestPath     string
	Home             string
	Project          string
	Namespace        string
	InstallSlug      string
	Force            bool
	StateDir         string
	OnConflict       string
	LockTimeout      time.Duration
	InstallerVersion string
}

type RepairOptions struct {
	ManifestPath string
	Home         string
	Project      string
	Namespace    string
	StateDir     string
	OnConflict   string
	LockTimeout  time.Duration
}

type UninstallOptions struct {
	ManifestPath string
	Home         string
	Project      string
	Namespace    string
	StateDir     string
	OnConflict   string
	LockTimeout  time.Duration
	Shared       bool
	All          bool
	Force        bool
}

type CleanupOptions struct {
	ManifestPath string
	Home         string
	Project      string
	Namespace    string
	StateDir     string
	OnConflict   string
	LockTimeout  time.Duration
}

type SyncOptions struct {
	ManifestPath string
	Home         string
	Project      string
	Namespace    string
	StateDir     string
	OnConflict   string
	LockTimeout  time.Duration
	Shared       bool
	All          bool
	Force        bool
}

type preparedPlan struct {
	Manifest        manifest.Manifest
	Catalog         registry.Catalog
	Sources         []source.Snapshot
	SourcesBySpec   map[string]source.Snapshot
	Candidates      []target.Candidate
	ExtraCandidates []target.ExtraCandidate
	Options         Options
}

func Plan(opts Options) (contract.Plan, error) {
	prepared, err := preparePlanWithRename(opts)
	if err != nil {
		return contract.Plan{}, err
	}
	world := observePrepared(prepared)
	return buildPreparedPlan(prepared, world), nil
}

func PlanUninstall(opts UninstallOptions) (contract.Plan, error) {
	prepared, err := preparePlan(Options{
		ManifestPath: opts.ManifestPath,
		Home:         opts.Home,
		Project:      opts.Project,
		Namespace:    opts.Namespace,
		OnConflict:   firstNonEmpty(opts.OnConflict, "block"),
	})
	if err != nil {
		return contract.Plan{}, err
	}
	world := observePrepared(prepared)
	plan := buildPreparedPlan(prepared, world)
	planpkg.Sort(&plan)
	return uninstallPlan(prepared, plan, opts), nil
}

func PlanCleanupDuplicates(opts CleanupOptions) (contract.Plan, error) {
	prepared, err := preparePlan(Options{
		ManifestPath: opts.ManifestPath,
		Home:         opts.Home,
		Project:      opts.Project,
		Namespace:    opts.Namespace,
		OnConflict:   firstNonEmpty(opts.OnConflict, "block"),
	})
	if err != nil {
		return contract.Plan{}, err
	}
	plan := baseOperationPlan(prepared, "cleanup-duplicates")
	plan.Actions = prune.Plan(prune.Inputs{
		Candidates:    prepared.Candidates,
		SourcesBySpec: prepared.SourcesBySpec,
		Namespace:     firstNonEmpty(prepared.Options.Namespace, prepared.Manifest.Namespace, prepared.Manifest.Owner),
	})
	planpkg.Sort(&plan)
	return plan, nil
}

func PlanSync(opts SyncOptions) (contract.Plan, error) {
	prepared, desired, loaded, err := prepareSync(opts)
	if err != nil {
		return contract.Plan{}, err
	}
	return syncPlan(prepared, desired, loaded.Ledger, opts), nil
}

func Apply(ctx context.Context, opts ApplyOptions) (install.Result, error) {
	onConflict := opts.OnConflict
	if onConflict == "" {
		onConflict = "block"
	}
	prepared, err := preparePlanWithRename(Options{
		ManifestPath: opts.ManifestPath,
		Home:         opts.Home,
		Project:      opts.Project,
		Namespace:    opts.Namespace,
		InstallSlug:  opts.InstallSlug,
		Force:        opts.Force,
		OnConflict:   onConflict,
	})
	if err != nil {
		return install.Result{}, err
	}
	stateDir, err := state.ResolveDir(opts.StateDir)
	if err != nil {
		return install.Result{}, err
	}
	manager := lock.NewManager(stateDir)
	if opts.LockTimeout > 0 {
		manager = manager.WithTimeout(opts.LockTimeout)
	}
	locks, err := manager.AcquireTargets(ctx, applyLockIDs(prepared))
	if err != nil {
		return install.Result{}, err
	}
	defer locks.Release()
	if err := sweepOrphans(prepared); err != nil {
		return install.Result{}, err
	}
	world := observePrepared(prepared)
	plan := buildPreparedPlan(prepared, world)
	planpkg.Sort(&plan)
	result, err := install.Apply(plan, install.Options{
		Owner:        prepared.Manifest.Owner,
		Namespace:    firstNonEmpty(prepared.Manifest.Namespace, prepared.Manifest.Owner),
		ManifestPath: prepared.Options.ManifestPath,
		Version:      firstNonEmpty(opts.InstallerVersion, "dev"),
		FS:           fsutil.Options{},
	})
	if err != nil {
		return install.Result{}, err
	}
	if err := state.Commit(ctx, state.CommitOptions{Dir: stateDir, LockTimeout: opts.LockTimeout}, func(ledger *state.Ledger) error {
		applyLedgerUpdates(ledger, plan, result)
		return nil
	}); err != nil {
		return install.Result{}, err
	}
	return result, nil
}

func Sync(ctx context.Context, opts SyncOptions) (install.Result, error) {
	prepared, desired, loaded, err := prepareSync(opts)
	if err != nil {
		return install.Result{}, err
	}
	stateDir, err := state.ResolveDir(opts.StateDir)
	if err != nil {
		return install.Result{}, err
	}
	manager := lock.NewManager(stateDir)
	if opts.LockTimeout > 0 {
		manager = manager.WithTimeout(opts.LockTimeout)
	}
	locks, err := manager.AcquireTargets(ctx, syncLockIDs(prepared, loaded.Ledger))
	if err != nil {
		return install.Result{}, err
	}
	defer locks.Release()
	loaded, err = state.Load(stateDir)
	if err != nil {
		return install.Result{}, err
	}
	plan := syncPlan(prepared, desired, loaded.Ledger, opts)
	result, err := install.Uninstall(plan, install.UninstallOptions{
		Owner:        prepared.Manifest.Owner,
		RemoveShared: opts.Shared || opts.All,
		Force:        opts.Force,
	})
	if err != nil {
		return install.Result{}, err
	}
	if err := state.Commit(ctx, state.CommitOptions{Dir: stateDir, LockTimeout: opts.LockTimeout}, func(ledger *state.Ledger) error {
		applyUninstallLedgerUpdates(ledger, result)
		return nil
	}); err != nil {
		return install.Result{}, err
	}
	return result, nil
}

func Uninstall(ctx context.Context, opts UninstallOptions) (install.Result, error) {
	onConflict := opts.OnConflict
	if onConflict == "" {
		onConflict = "block"
	}
	prepared, err := preparePlan(Options{
		ManifestPath: opts.ManifestPath,
		Home:         opts.Home,
		Project:      opts.Project,
		Namespace:    opts.Namespace,
		OnConflict:   onConflict,
	})
	if err != nil {
		return install.Result{}, err
	}
	stateDir, err := state.ResolveDir(opts.StateDir)
	if err != nil {
		return install.Result{}, err
	}
	manager := lock.NewManager(stateDir)
	if opts.LockTimeout > 0 {
		manager = manager.WithTimeout(opts.LockTimeout)
	}
	locks, err := manager.AcquireTargets(ctx, applyLockIDs(prepared))
	if err != nil {
		return install.Result{}, err
	}
	defer locks.Release()
	world := observePrepared(prepared)
	plan := buildPreparedPlan(prepared, world)
	planpkg.Sort(&plan)
	result, err := install.Uninstall(plan, install.UninstallOptions{
		Owner:        prepared.Manifest.Owner,
		RemoveShared: opts.Shared || opts.All,
		Force:        opts.Force,
	})
	if err != nil {
		return install.Result{}, err
	}
	if err := state.Commit(ctx, state.CommitOptions{Dir: stateDir, LockTimeout: opts.LockTimeout}, func(ledger *state.Ledger) error {
		applyUninstallLedgerUpdates(ledger, result)
		return nil
	}); err != nil {
		return install.Result{}, err
	}
	return result, nil
}

func CleanupDuplicates(ctx context.Context, opts CleanupOptions) (install.Result, error) {
	onConflict := opts.OnConflict
	if onConflict == "" {
		onConflict = "block"
	}
	prepared, err := preparePlan(Options{
		ManifestPath: opts.ManifestPath,
		Home:         opts.Home,
		Project:      opts.Project,
		Namespace:    opts.Namespace,
		OnConflict:   onConflict,
	})
	if err != nil {
		return install.Result{}, err
	}
	stateDir, err := state.ResolveDir(opts.StateDir)
	if err != nil {
		return install.Result{}, err
	}
	manager := lock.NewManager(stateDir)
	if opts.LockTimeout > 0 {
		manager = manager.WithTimeout(opts.LockTimeout)
	}
	locks, err := manager.AcquireTargets(ctx, applyLockIDs(prepared))
	if err != nil {
		return install.Result{}, err
	}
	defer locks.Release()
	return prune.CleanupDuplicates(prune.Inputs{
		Candidates:    prepared.Candidates,
		SourcesBySpec: prepared.SourcesBySpec,
		Namespace:     firstNonEmpty(prepared.Options.Namespace, prepared.Manifest.Namespace, prepared.Manifest.Owner),
	}), nil
}

func Repair(ctx context.Context, opts RepairOptions) (state.Ledger, error) {
	onConflict := opts.OnConflict
	if onConflict == "" {
		onConflict = "block"
	}
	prepared, err := preparePlan(Options{
		ManifestPath: opts.ManifestPath,
		Home:         opts.Home,
		Project:      opts.Project,
		Namespace:    opts.Namespace,
		OnConflict:   onConflict,
	})
	if err != nil {
		return state.Ledger{}, err
	}
	stateDir, err := state.ResolveDir(opts.StateDir)
	if err != nil {
		return state.Ledger{}, err
	}
	manager := lock.NewManager(stateDir)
	if opts.LockTimeout > 0 {
		manager = manager.WithTimeout(opts.LockTimeout)
	}
	locks, err := manager.AcquireTargets(ctx, applyLockIDs(prepared))
	if err != nil {
		return state.Ledger{}, err
	}
	defer locks.Release()
	world := observePrepared(prepared)
	plan := buildPreparedPlan(prepared, world)
	planpkg.Sort(&plan)
	if err := state.Commit(ctx, state.CommitOptions{Dir: stateDir, LockTimeout: opts.LockTimeout}, func(ledger *state.Ledger) error {
		*ledger = state.Empty()
		repairLedger(ledger, plan)
		return nil
	}); err != nil {
		return state.Ledger{}, err
	}
	loaded, err := state.Load(stateDir)
	if err != nil {
		return state.Ledger{}, err
	}
	return loaded.Ledger, nil
}

func Status(opts StatusOptions) (status.Report, error) {
	stateLoad, err := state.Load(opts.StateDir)
	if err != nil {
		return status.Report{}, err
	}
	var planned *contract.Plan
	if opts.ManifestPath != "" {
		plan, err := Plan(Options{
			ManifestPath: opts.ManifestPath,
			Home:         opts.Home,
			Project:      opts.Project,
			Namespace:    opts.Namespace,
			OnConflict:   "block",
		})
		if err != nil {
			return status.Report{}, err
		}
		planpkg.Sort(&plan)
		planned = &plan
	}
	return status.Build(status.Inputs{
		Plan:        planned,
		Ledger:      stateLoad.Ledger,
		Diagnostics: stateLoad.Diagnostics,
	}), nil
}

func Conflicts(opts StatusOptions) (status.ConflictReport, error) {
	stateLoad, err := state.Load(opts.StateDir)
	if err != nil {
		return status.ConflictReport{}, err
	}
	var planned *contract.Plan
	if opts.ManifestPath != "" {
		plan, err := Plan(Options{
			ManifestPath: opts.ManifestPath,
			Home:         opts.Home,
			Project:      opts.Project,
			Namespace:    opts.Namespace,
			OnConflict:   "block",
		})
		if err != nil {
			return status.ConflictReport{}, err
		}
		planpkg.Sort(&plan)
		planned = &plan
	}
	return status.Conflicts(planned, stateLoad.Ledger, stateLoad.Diagnostics), nil
}

func preparePlan(opts Options) (preparedPlan, error) {
	if opts.OnConflict == "" {
		opts.OnConflict = "block"
	}
	reg, err := registry.Load()
	if err != nil {
		return preparedPlan{}, err
	}
	m, err := manifest.Load(opts.ManifestPath)
	if err != nil {
		return preparedPlan{}, err
	}
	absManifest, err := filepath.Abs(opts.ManifestPath)
	if err != nil {
		return preparedPlan{}, err
	}
	opts.ManifestPath = absManifest
	home, err := resolveHome(opts.Home)
	if err != nil {
		return preparedPlan{}, err
	}
	opts.Home = home
	if opts.Project != "" {
		if opts.Project, err = filepath.Abs(opts.Project); err != nil {
			return preparedPlan{}, err
		}
	}
	manifestDir := filepath.Dir(absManifest)
	sources, bySpec, err := source.ResolveAll(m, source.Options{
		ManifestDir: manifestDir,
		Home:        opts.Home,
	})
	if err != nil {
		return preparedPlan{}, err
	}
	candidates, err := target.Resolve(m, reg, target.Options{
		Home:        opts.Home,
		Project:     opts.Project,
		ManifestDir: manifestDir,
		EnvHomes:    envHomes(reg),
	})
	if err != nil {
		return preparedPlan{}, err
	}
	extraCandidates, err := target.ResolveExtras(m, target.Options{
		Home:        opts.Home,
		Project:     opts.Project,
		ManifestDir: manifestDir,
	})
	if err != nil {
		return preparedPlan{}, err
	}
	return preparedPlan{
		Manifest:        m,
		Catalog:         reg,
		Sources:         sources,
		SourcesBySpec:   bySpec,
		Candidates:      candidates,
		ExtraCandidates: extraCandidates,
		Options:         opts,
	}, nil
}

func preparePlanWithRename(opts Options) (preparedPlan, error) {
	prepared, err := preparePlan(opts)
	if err != nil {
		return preparedPlan{}, err
	}
	return applyRenameResolutions(prepared)
}

func applyRenameResolutions(prepared preparedPlan) (preparedPlan, error) {
	if !hasRenameResolution(prepared.Options) {
		return prepared, nil
	}
	discovery := prepared
	discovery.Options.OnConflict = "block"
	world := observePrepared(discovery)
	plan := buildPreparedPlan(discovery, world)
	renames := map[string]string{}
	for _, conflict := range plan.Conflicts {
		policy, slug := renameResolutionFor(prepared.Options, conflict)
		if policy != "rename" || strings.TrimSpace(slug) == "" || !renameConflictStatus(conflict.Status) {
			continue
		}
		renames[conflict.DesiredCanonicalID] = strings.TrimSpace(slug)
	}
	if len(renames) == 0 {
		return prepared, nil
	}
	out := prepared
	out.Manifest.Skills = append([]manifest.Skill(nil), prepared.Manifest.Skills...)
	for i := range out.Manifest.Skills {
		canonicalID := firstNonEmpty(out.Manifest.Skills[i].CanonicalID, firstNonEmpty(out.Manifest.Namespace, out.Manifest.Owner)+":"+out.Manifest.Skills[i].Name)
		if slug, ok := renames[canonicalID]; ok {
			out.Manifest.Skills[i].InstallSlug = slug
		}
	}
	manifestDir := filepath.Dir(prepared.Options.ManifestPath)
	candidates, err := target.Resolve(out.Manifest, out.Catalog, target.Options{
		Home:        prepared.Options.Home,
		Project:     prepared.Options.Project,
		ManifestDir: manifestDir,
		EnvHomes:    envHomes(out.Catalog),
	})
	if err != nil {
		return preparedPlan{}, err
	}
	out.Candidates = candidates
	return out, nil
}

func hasRenameResolution(opts Options) bool {
	if strings.TrimSpace(opts.OnConflict) == "rename" {
		return true
	}
	for _, resolution := range opts.Resolutions {
		if strings.TrimSpace(resolution.Policy) == "rename" {
			return true
		}
	}
	return false
}

func renameResolutionFor(opts Options, conflict contract.PlanConflict) (string, string) {
	if resolution, ok := opts.Resolutions[conflict.ID]; ok && strings.TrimSpace(resolution.Policy) != "" {
		return strings.TrimSpace(resolution.Policy), firstNonEmpty(resolution.InstallSlug, opts.InstallSlug)
	}
	return strings.TrimSpace(opts.OnConflict), opts.InstallSlug
}

func renameConflictStatus(status string) bool {
	return status == "namespace-collision" || status == "foreign-target"
}

func observePrepared(prepared preparedPlan) observe.WorldState {
	return observe.Observe(prepared.Candidates, prepared.Catalog, observe.Options{
		Home:         prepared.Options.Home,
		ExtraTargets: extraTargets(prepared.ExtraCandidates),
	})
}

func buildPreparedPlan(prepared preparedPlan, world observe.WorldState) contract.Plan {
	return planpkg.Build(planpkg.Inputs{
		Manifest:        prepared.Manifest,
		Catalog:         prepared.Catalog,
		Sources:         prepared.Sources,
		SourcesBySpec:   prepared.SourcesBySpec,
		Candidates:      prepared.Candidates,
		ExtraCandidates: prepared.ExtraCandidates,
		World:           world,
		Options:         prepared.Options,
	})
}

func prepareSync(opts SyncOptions) (preparedPlan, contract.Plan, state.LoadResult, error) {
	prepared, err := preparePlan(Options{
		ManifestPath: opts.ManifestPath,
		Home:         opts.Home,
		Project:      opts.Project,
		Namespace:    opts.Namespace,
		OnConflict:   firstNonEmpty(opts.OnConflict, "block"),
	})
	if err != nil {
		return preparedPlan{}, contract.Plan{}, state.LoadResult{}, err
	}
	world := observePrepared(prepared)
	desired := buildPreparedPlan(prepared, world)
	planpkg.Sort(&desired)
	loaded, err := state.Load(opts.StateDir)
	if err != nil {
		return preparedPlan{}, contract.Plan{}, state.LoadResult{}, err
	}
	return prepared, desired, loaded, nil
}

func baseOperationPlan(prepared preparedPlan, operation string) contract.Plan {
	return contract.Plan{
		Schema:    "skiller-plan.v1",
		Operation: operation,
		DryRun:    true,
		Inputs: contract.PlanInputs{
			ManifestPath: prepared.Options.ManifestPath,
			Home:         prepared.Options.Home,
			Project:      prepared.Options.Project,
			Namespace:    firstNonEmpty(prepared.Options.Namespace, prepared.Manifest.Namespace, prepared.Manifest.Owner),
			OnConflict:   prepared.Options.OnConflict,
		},
		Sources:     prepared.Sources,
		Actions:     []contract.PlanAction{},
		Conflicts:   []contract.PlanConflict{},
		Diagnostics: []contract.Diagnostic{},
	}
}

func syncPlan(prepared preparedPlan, desired contract.Plan, ledger state.Ledger, opts SyncOptions) contract.Plan {
	plan := baseOperationPlan(prepared, "sync")
	desiredIDs := map[string]bool{}
	for _, action := range desired.Actions {
		if action.Skill != nil {
			desiredIDs["install:"+action.ID] = true
		}
	}
	skills := map[string]state.SkillRecord{}
	for _, skill := range ledger.Skills {
		skills[skill.ID] = skill
	}
	for _, record := range ledger.Installs {
		if desiredIDs[record.ID] {
			continue
		}
		action := syncPruneAction(record, skills[record.SkillID], opts)
		plan.Actions = append(plan.Actions, action)
	}
	planpkg.Sort(&plan)
	return plan
}

func syncPruneAction(record state.InstallRecord, skill state.SkillRecord, opts SyncOptions) contract.PlanAction {
	actionID := strings.TrimPrefix(record.ID, "install:")
	if actionID == record.ID {
		actionID = "sync:" + hashid.Short(record.ID+"\x00"+record.TargetPath)
	}
	targetRef := targetFromInstall(record)
	skillRef := contract.PlanSkill{
		CanonicalID:     firstNonEmpty(skill.CanonicalID, record.SkillID),
		Namespace:       skill.Namespace,
		Name:            firstNonEmpty(skill.Name, skill.InstallSlug, record.SkillID),
		InstallSlug:     firstNonEmpty(skill.InstallSlug, filepath.Base(record.TargetPath)),
		FrontmatterName: skill.FrontmatterName,
		Description:     skill.Description,
	}
	action := contract.PlanAction{
		ID:     actionID,
		Action: "remove-owned",
		Status: "dry-run",
		Skill:  &skillRef,
		Target: targetRef,
		Mode: contract.PlanMode{
			Requested: firstNonEmpty(record.Mode, "copy"),
			Effective: firstNonEmpty(record.Mode, "copy"),
		},
		Ownership: contract.ObservedOwnership{
			Class:      "ours-copy",
			Path:       record.TargetPath,
			MarkerPath: record.MarkerPath,
		},
		PlannedWrites: []contract.PlannedWrite{{Kind: "remove", Path: record.TargetPath}},
	}
	if record.MarkerPath == "" || filepath.Base(record.MarkerPath) != ".skiller-install.json" {
		action.Action = "skip-uninstall"
		action.Status = "dry-run"
		action.Reason = "sync prunes only skiller marker-owned copy installs"
		action.PlannedWrites = nil
		action.Ownership.Class = "foreign-unmanaged"
		return action
	}
	if record.TargetKind == "shared" && !(opts.Shared || opts.All) {
		action.Action = "skip-uninstall"
		action.Status = "dry-run"
		action.Reason = "shared target requires --shared or --all"
		action.PlannedWrites = nil
	}
	return action
}

func uninstallPlan(prepared preparedPlan, observed contract.Plan, opts UninstallOptions) contract.Plan {
	plan := baseOperationPlan(prepared, "uninstall")
	plan.Conflicts = observed.Conflicts
	for _, action := range observed.Actions {
		if action.Skill == nil {
			continue
		}
		plan.Actions = append(plan.Actions, uninstallPlanAction(action, opts))
	}
	return plan
}

func uninstallPlanAction(action contract.PlanAction, opts UninstallOptions) contract.PlanAction {
	out := action
	out.PlannedWrites = nil
	if action.Ownership.Class == "absent" {
		out.Action = "skip-uninstall"
		out.Status = "dry-run"
		out.Reason = "target is not installed"
		return out
	}
	if action.Target.Kind == "shared" && !(opts.Shared || opts.All) {
		out.Action = "skip-uninstall"
		out.Status = "dry-run"
		out.Reason = "shared target requires --shared or --all"
		return out
	}
	switch action.Ownership.Class {
	case "ours-symlink":
		out.Action = "remove-owned"
		out.Status = "dry-run"
		out.PlannedWrites = []contract.PlannedWrite{{Kind: "remove", Path: action.Target.Path}}
	case "ours-copy":
		out.Action = "remove-owned"
		if !opts.Force && action.Ownership.Message != "" {
			out.Status = "blocked"
			out.Reason = action.Ownership.Message
			return out
		}
		out.Status = "dry-run"
		out.PlannedWrites = []contract.PlannedWrite{{Kind: "remove", Path: action.Target.Path}}
	default:
		out.Action = "skip-uninstall"
		out.Status = "dry-run"
		out.Reason = "target is not skiller-owned"
	}
	return out
}

func resolveHome(home string) (string, error) {
	if home == "" {
		return os.UserHomeDir()
	}
	return filepath.Abs(home)
}

func envHomes(reg registry.Catalog) map[string]string {
	out := map[string]string{}
	for _, h := range reg.Harnesses {
		if h.EnvHome == "" {
			continue
		}
		if _, ok := out[h.EnvHome]; !ok {
			out[h.EnvHome] = os.Getenv(h.EnvHome)
		}
	}
	return out
}

func extraTargets(candidates []target.ExtraCandidate) []target.Ref {
	out := make([]target.Ref, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate.Target)
	}
	return out
}

func applyLockIDs(prepared preparedPlan) []string {
	var ids []string
	for _, candidate := range prepared.Candidates {
		ids = append(ids, candidate.Target.LockID)
		for _, duplicate := range candidate.Duplicates {
			ids = append(ids, duplicate.LockID)
		}
	}
	for _, candidate := range prepared.ExtraCandidates {
		ids = append(ids, candidate.Target.LockID)
	}
	return lock.SortedUnique(ids)
}

func syncLockIDs(prepared preparedPlan, ledger state.Ledger) []string {
	ids := applyLockIDs(prepared)
	for _, install := range ledger.Installs {
		ids = append(ids, lockIDForRoot(filepath.Dir(install.TargetPath)))
	}
	return lock.SortedUnique(ids)
}

func lockIDForRoot(root string) string {
	return "target:" + hashid.Short(filepath.Clean(root))
}

func targetFromInstall(install state.InstallRecord) contract.PlanTarget {
	root := filepath.Dir(install.TargetPath)
	return contract.PlanTarget{
		ID:     install.TargetID,
		Kind:   install.TargetKind,
		Scope:  install.Scope,
		Root:   root,
		Path:   install.TargetPath,
		LockID: lockIDForRoot(root),
	}
}

func sweepOrphans(prepared preparedPlan) error {
	parents := map[string]bool{}
	for _, candidate := range prepared.Candidates {
		parents[filepath.Dir(candidate.Target.Path)] = true
		for _, duplicate := range candidate.Duplicates {
			parents[filepath.Dir(duplicate.Path)] = true
		}
	}
	for _, candidate := range prepared.ExtraCandidates {
		parents[filepath.Dir(candidate.Target.Path)] = true
	}
	for parent := range parents {
		if err := fsutil.SweepOrphans(parent); err != nil {
			return err
		}
	}
	return nil
}

func applyLedgerUpdates(ledger *state.Ledger, plan contract.Plan, result install.Result) {
	plannedByID := map[string]contract.PlanAction{}
	for _, action := range plan.Actions {
		plannedByID[action.ID] = action
	}
	sourcesByID := map[string]contract.SourceSnapshot{}
	for _, source := range plan.Sources {
		sourcesByID[source.ID] = source
		upsertSource(ledger, source)
	}
	for _, action := range result.Actions {
		planned := plannedByID[action.ID]
		switch action.Status {
		case "installed", "updated":
			if action.Skill != nil {
				upsertSkill(ledger, *action.Skill)
				upsertInstall(ledger, action, planned, sourcesByID)
			}
			if action.Extra != nil {
				upsertExtra(ledger, action)
			}
		case "skipped", "satisfied-by-foreign":
			if action.Skill != nil && isLedgerOnlyAdoption(action.Action) {
				upsertSkill(ledger, *action.Skill)
				upsertInstall(ledger, action, planned, sourcesByID)
			}
		case "blocked", "partially-satisfied":
			for _, conflict := range result.Conflicts {
				upsertConflict(ledger, conflict)
			}
		}
	}
}

func applyUninstallLedgerUpdates(ledger *state.Ledger, result install.Result) {
	for _, action := range result.Actions {
		if action.Status == "removed" && action.Skill != nil {
			removeInstall(ledger, action)
		}
	}
}

func repairLedger(ledger *state.Ledger, plan contract.Plan) {
	sourcesByID := map[string]contract.SourceSnapshot{}
	for _, source := range plan.Sources {
		sourcesByID[source.ID] = source
		upsertSource(ledger, source)
	}
	for _, action := range plan.Actions {
		if action.Skill == nil {
			continue
		}
		switch action.Action {
		case "no-op":
			upsertSkill(ledger, *action.Skill)
			upsertInstall(ledger, actionResultFromPlan(action, "installed"), action, sourcesByID)
		case "adopt-existing":
			upsertSkill(ledger, *action.Skill)
			upsertInstall(ledger, actionResultFromPlan(action, "skipped"), action, sourcesByID)
		case "satisfied-by-foreign":
			upsertSkill(ledger, *action.Skill)
			upsertInstall(ledger, actionResultFromPlan(action, "satisfied-by-foreign"), action, sourcesByID)
		case "refresh":
			upsertSkill(ledger, *action.Skill)
			upsertInstall(ledger, actionResultFromPlan(action, "stale"), action, sourcesByID)
		}
	}
	for _, conflict := range plan.Conflicts {
		upsertConflict(ledger, conflict)
	}
}

func actionResultFromPlan(action contract.PlanAction, status string) install.ActionResult {
	return install.ActionResult{
		ID:            action.ID,
		Action:        action.Action,
		Status:        status,
		Reason:        action.Reason,
		Target:        action.Target,
		Skill:         action.Skill,
		Extra:         action.Extra,
		RequestedMode: action.Mode.Requested,
		EffectiveMode: action.Mode.Effective,
	}
}

func isLedgerOnlyAdoption(action string) bool {
	return action == "adopt-existing" || action == "satisfied-by-foreign"
}

func upsertSource(ledger *state.Ledger, source contract.SourceSnapshot) {
	record := state.SourceRecord{
		ID:                source.ID,
		SourceKind:        source.SourceKind,
		OriginalSpec:      source.OriginalSpec,
		CanonicalURI:      source.CanonicalURI,
		SourceKey:         source.SourceKey,
		Subdir:            source.Subdir,
		PinnedRef:         source.PinnedRef,
		RequestedChecksum: source.RequestedChecksum,
		ResolvedRevision:  source.ResolvedRevision,
		SourceStatus:      source.SourceStatus,
		LocalCachePath:    source.LocalCachePath,
		SourceRealpath:    source.SourceRealpath,
		SourceDigest:      source.SourceDigest,
	}
	for i := range ledger.Sources {
		if ledger.Sources[i].ID == record.ID {
			ledger.Sources[i] = record
			return
		}
	}
	ledger.Sources = append(ledger.Sources, record)
}

func upsertSkill(ledger *state.Ledger, skill contract.PlanSkill) {
	record := state.SkillRecord{
		ID:              "skill:" + skill.CanonicalID,
		CanonicalID:     skill.CanonicalID,
		Namespace:       skill.Namespace,
		Name:            skill.Name,
		InstallSlug:     skill.InstallSlug,
		FrontmatterName: skill.FrontmatterName,
		Description:     skill.Description,
	}
	for i := range ledger.Skills {
		if ledger.Skills[i].ID == record.ID {
			ledger.Skills[i] = record
			return
		}
	}
	ledger.Skills = append(ledger.Skills, record)
}

func upsertInstall(ledger *state.Ledger, action install.ActionResult, planned contract.PlanAction, sources map[string]contract.SourceSnapshot) {
	source := sources[planned.SourceID]
	record := state.InstallRecord{
		ID:                       "install:" + action.ID,
		SkillID:                  "skill:" + action.Skill.CanonicalID,
		TargetKind:               action.Target.Kind,
		TargetID:                 action.Target.ID,
		TargetPath:               action.Target.Path,
		Mode:                     installMode(action, planned.Ownership),
		Scope:                    action.Target.Scope,
		MarkerPath:               markerPath(action, planned.Ownership),
		InstalledDigestAtInstall: installedDigest(action, planned, source),
		SourceDigestAtInstall:    source.SourceDigest,
		Status:                   installLedgerStatus(action),
		LegacyAdapter:            planned.Ownership.LegacyAdapter,
		BackupPath:               action.BackupPath,
		LastSeenAt:               time.Now().UTC().Format(time.RFC3339),
	}
	for i := range ledger.Installs {
		if ledger.Installs[i].ID == record.ID {
			ledger.Installs[i] = record
			return
		}
	}
	ledger.Installs = append(ledger.Installs, record)
}

func removeInstall(ledger *state.Ledger, action install.ActionResult) {
	id := "install:" + action.ID
	out := ledger.Installs[:0]
	for _, install := range ledger.Installs {
		if install.ID != id {
			out = append(out, install)
		}
	}
	ledger.Installs = out
}

func installMode(action install.ActionResult, ownership contract.ObservedOwnership) string {
	if ownership.Class != "" && ownership.Class != "absent" {
		if ownership.Class == "ours-symlink" || ownership.SourceRealpath != "" {
			return "link"
		}
		return "copy"
	}
	return firstNonEmpty(action.EffectiveMode, action.RequestedMode, "copy")
}

func installLedgerStatus(action install.ActionResult) string {
	if action.Action == "satisfied-by-foreign" {
		return "satisfied-by-foreign"
	}
	if action.Action == "adopt-existing" {
		return "installed"
	}
	return action.Status
}

func installedDigest(action install.ActionResult, planned contract.PlanAction, source contract.SourceSnapshot) string {
	if action.Status == "installed" || action.Status == "updated" {
		return source.SourceDigest
	}
	if planned.Ownership.Digest != "" {
		return planned.Ownership.Digest
	}
	return ""
}

func upsertExtra(ledger *state.Ledger, action install.ActionResult) {
	record := state.ExtraRecord{
		ID:         "extra:" + action.ID,
		SourceID:   "",
		ExtraID:    action.Extra.ID,
		TargetPath: action.Target.Path,
		Mode:       "copy",
		MarkerPath: action.Target.Path + ".skiller-install.json",
		LastSeenAt: time.Now().UTC().Format(time.RFC3339),
	}
	for i := range ledger.Extras {
		if ledger.Extras[i].ID == record.ID {
			ledger.Extras[i] = record
			return
		}
	}
	ledger.Extras = append(ledger.Extras, record)
}

func upsertConflict(ledger *state.Ledger, conflict contract.PlanConflict) {
	for i := range ledger.Conflicts {
		if ledger.Conflicts[i].ID == conflict.ID {
			ledger.Conflicts[i] = conflict
			return
		}
	}
	ledger.Conflicts = append(ledger.Conflicts, conflict)
}

func markerPath(action install.ActionResult, ownership contract.ObservedOwnership) string {
	if action.Action == "adopt-existing" || action.Action == "satisfied-by-foreign" {
		return ownership.MarkerPath
	}
	if action.EffectiveMode == "copy" {
		return filepath.Join(action.Target.Path, ".skiller-install.json")
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func ValidateOptions(opts Options) error {
	if opts.ManifestPath == "" {
		return fmt.Errorf("manifest path is required")
	}
	return nil
}

package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mostlydev/skiller/internal/contract"
	"github.com/mostlydev/skiller/pkg/manifest"
	"github.com/mostlydev/skiller/pkg/observe"
	planpkg "github.com/mostlydev/skiller/pkg/plan"
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

func Plan(opts Options) (contract.Plan, error) {
	if opts.OnConflict == "" {
		opts.OnConflict = "block"
	}
	reg, err := registry.Load()
	if err != nil {
		return contract.Plan{}, err
	}
	m, err := manifest.Load(opts.ManifestPath)
	if err != nil {
		return contract.Plan{}, err
	}
	absManifest, err := filepath.Abs(opts.ManifestPath)
	if err != nil {
		return contract.Plan{}, err
	}
	opts.ManifestPath = absManifest
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
	manifestDir := filepath.Dir(absManifest)
	sources, bySpec, err := source.ResolveAll(m, source.Options{
		ManifestDir: manifestDir,
		Home:        opts.Home,
	})
	if err != nil {
		return contract.Plan{}, err
	}
	candidates, err := target.Resolve(m, reg, target.Options{
		Home:        opts.Home,
		Project:     opts.Project,
		ManifestDir: manifestDir,
		EnvHomes:    envHomes(reg),
	})
	if err != nil {
		return contract.Plan{}, err
	}
	extraCandidates, err := target.ResolveExtras(m, target.Options{
		Home:        opts.Home,
		Project:     opts.Project,
		ManifestDir: manifestDir,
	})
	if err != nil {
		return contract.Plan{}, err
	}
	world := observe.Observe(candidates, reg, observe.Options{
		Home:         opts.Home,
		ExtraTargets: extraTargets(extraCandidates),
	})
	return planpkg.Build(planpkg.Inputs{
		Manifest:        m,
		Catalog:         reg,
		Sources:         sources,
		SourcesBySpec:   bySpec,
		Candidates:      candidates,
		ExtraCandidates: extraCandidates,
		World:           world,
		Options:         opts,
	}), nil
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

func ValidateOptions(opts Options) error {
	if opts.ManifestPath == "" {
		return fmt.Errorf("manifest path is required")
	}
	return nil
}

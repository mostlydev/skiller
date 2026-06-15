package skiller

import (
	"context"
	"time"

	"github.com/mostlydev/skiller/internal/app"
	"github.com/mostlydev/skiller/pkg/install"
	planpkg "github.com/mostlydev/skiller/pkg/plan"
	"github.com/mostlydev/skiller/pkg/state"
	"github.com/mostlydev/skiller/pkg/status"
	"github.com/mostlydev/skiller/pkg/version"
)

type Resolution = planpkg.Resolution

type Options struct {
	ManifestPath string
	Home         string
	Project      string
	Namespace    string
	InstallSlug  string
	Force        bool
	OnConflict   string
	Resolutions  map[string]Resolution
}

type ApplyOptions struct {
	ManifestPath     string
	Home             string
	Project          string
	Namespace        string
	InstallSlug      string
	Force            bool
	Resolutions      map[string]Resolution
	StateDir         string
	OnConflict       string
	LockTimeout      time.Duration
	InstallerVersion string
}

type StatusOptions struct {
	ManifestPath string
	Home         string
	Project      string
	Namespace    string
	StateDir     string
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

func Plan(ctx context.Context, opts Options) (planpkg.Plan, error) {
	if err := contextError(ctx); err != nil {
		return planpkg.Plan{}, err
	}
	return app.Plan(planpkg.Options{
		ManifestPath: opts.ManifestPath,
		Home:         opts.Home,
		Project:      opts.Project,
		Namespace:    opts.Namespace,
		InstallSlug:  opts.InstallSlug,
		Force:        opts.Force,
		OnConflict:   opts.OnConflict,
		Resolutions:  opts.Resolutions,
	})
}

func Install(ctx context.Context, opts ApplyOptions) (install.Result, error) {
	ctx = normalizeContext(ctx)
	return app.Apply(ctx, app.ApplyOptions{
		ManifestPath:     opts.ManifestPath,
		Home:             opts.Home,
		Project:          opts.Project,
		Namespace:        opts.Namespace,
		InstallSlug:      opts.InstallSlug,
		Force:            opts.Force,
		Resolutions:      opts.Resolutions,
		StateDir:         opts.StateDir,
		OnConflict:       opts.OnConflict,
		LockTimeout:      opts.LockTimeout,
		InstallerVersion: firstNonEmpty(opts.InstallerVersion, version.Get().Version),
	})
}

func Status(ctx context.Context, opts StatusOptions) (status.Report, error) {
	if err := contextError(ctx); err != nil {
		return status.Report{}, err
	}
	return app.Status(app.StatusOptions{
		ManifestPath: opts.ManifestPath,
		Home:         opts.Home,
		Project:      opts.Project,
		Namespace:    opts.Namespace,
		StateDir:     opts.StateDir,
	})
}

func Conflicts(ctx context.Context, opts StatusOptions) (status.ConflictReport, error) {
	if err := contextError(ctx); err != nil {
		return status.ConflictReport{}, err
	}
	return app.Conflicts(app.StatusOptions{
		ManifestPath: opts.ManifestPath,
		Home:         opts.Home,
		Project:      opts.Project,
		Namespace:    opts.Namespace,
		StateDir:     opts.StateDir,
	})
}

func Repair(ctx context.Context, opts RepairOptions) (state.Ledger, error) {
	ctx = normalizeContext(ctx)
	return app.Repair(ctx, app.RepairOptions{
		ManifestPath: opts.ManifestPath,
		Home:         opts.Home,
		Project:      opts.Project,
		Namespace:    opts.Namespace,
		StateDir:     opts.StateDir,
		OnConflict:   opts.OnConflict,
		LockTimeout:  opts.LockTimeout,
	})
}

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func contextError(ctx context.Context) error {
	return normalizeContext(ctx).Err()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

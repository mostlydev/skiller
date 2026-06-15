package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/mostlydev/skiller/internal/app"
	"github.com/mostlydev/skiller/internal/planner"
	"github.com/mostlydev/skiller/pkg/install"
)

func Run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usage(stderr)
	}
	switch args[0] {
	case "registry":
		return runRegistry(args[1:], stdout)
	case "plan":
		return runPlan(args[1:], stdout)
	case "install":
		return runInstall(args[1:], stdout)
	case "uninstall":
		return runUninstall(args[1:], stdout)
	case "cleanup-duplicates":
		return runCleanupDuplicates(args[1:], stdout)
	case "status":
		return runStatus(args[1:], stdout)
	case "conflicts":
		return runConflicts(args[1:], stdout)
	case "state":
		return runState(args[1:], stdout)
	case "-h", "--help", "help":
		return usage(stdout)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runRegistry(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("skiller registry", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*jsonOut {
		return fmt.Errorf("registry currently requires --json")
	}
	catalog, err := planner.LoadCatalog()
	if err != nil {
		return err
	}
	return writeJSON(stdout, catalog)
}

func runPlan(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("skiller plan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	manifest := fs.String("manifest", "", "manifest path")
	home := fs.String("home", "", "home directory")
	project := fs.String("project", "", "project directory")
	namespace := fs.String("namespace", "", "namespace override")
	onConflict := fs.String("on-conflict", "block", "conflict mode")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*jsonOut {
		return fmt.Errorf("plan currently requires --json")
	}
	if *manifest == "" {
		return fmt.Errorf("plan requires --manifest")
	}
	plan, err := planner.Build(planner.Options{
		ManifestPath: *manifest,
		Home:         *home,
		Project:      *project,
		Namespace:    *namespace,
		OnConflict:   *onConflict,
	})
	if err != nil {
		return err
	}
	planner.SortPlan(&plan)
	return writeJSON(stdout, plan)
}

func runInstall(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("skiller install", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	manifest := fs.String("manifest", "", "manifest path")
	home := fs.String("home", "", "home directory")
	project := fs.String("project", "", "project directory")
	namespace := fs.String("namespace", "", "namespace override")
	stateDir := fs.String("state-dir", "", "state directory")
	onConflict := fs.String("on-conflict", "block", "conflict mode")
	lockTimeout := fs.Duration("lock-timeout", 5*time.Second, "lock acquisition timeout")
	dryRun := fs.Bool("dry-run", false, "return plan without writes")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*jsonOut {
		return fmt.Errorf("install currently requires --json")
	}
	if *manifest == "" {
		return fmt.Errorf("install requires --manifest")
	}
	if *dryRun {
		plan, err := planner.Build(planner.Options{
			ManifestPath: *manifest,
			Home:         *home,
			Project:      *project,
			Namespace:    *namespace,
			OnConflict:   *onConflict,
		})
		if err != nil {
			return err
		}
		planner.SortPlan(&plan)
		return writeJSON(stdout, plan)
	}
	result, err := app.Apply(context.Background(), app.ApplyOptions{
		ManifestPath: *manifest,
		Home:         *home,
		Project:      *project,
		Namespace:    *namespace,
		StateDir:     *stateDir,
		OnConflict:   *onConflict,
		LockTimeout:  *lockTimeout,
	})
	if err != nil {
		return err
	}
	if err := writeJSON(stdout, result); err != nil {
		return err
	}
	if failed, blocked := countFailedBlocked(result); failed > 0 || blocked > 0 {
		return actionStatusError{failed: failed, blocked: blocked}
	}
	return nil
}

type actionStatusError struct {
	failed  int
	blocked int
}

func (e actionStatusError) Error() string {
	return fmt.Sprintf("operation completed with %d failed and %d blocked action(s)", e.failed, e.blocked)
}

func countFailedBlocked(result install.Result) (failed, blocked int) {
	for _, action := range result.Actions {
		switch action.Status {
		case "failed":
			failed++
		case "blocked", "partially-satisfied":
			blocked++
		}
	}
	return failed, blocked
}

func runUninstall(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("skiller uninstall", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	manifest := fs.String("manifest", "", "manifest path")
	home := fs.String("home", "", "home directory")
	project := fs.String("project", "", "project directory")
	namespace := fs.String("namespace", "", "namespace override")
	stateDir := fs.String("state-dir", "", "state directory")
	onConflict := fs.String("on-conflict", "block", "conflict mode")
	lockTimeout := fs.Duration("lock-timeout", 5*time.Second, "lock acquisition timeout")
	shared := fs.Bool("shared", false, "allow shared target removal")
	all := fs.Bool("all", false, "remove all owned targets, including shared targets")
	force := fs.Bool("force", false, "remove owned copies even when their digest changed")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*jsonOut {
		return fmt.Errorf("uninstall currently requires --json")
	}
	if *manifest == "" {
		return fmt.Errorf("uninstall requires --manifest")
	}
	result, err := app.Uninstall(context.Background(), app.UninstallOptions{
		ManifestPath: *manifest,
		Home:         *home,
		Project:      *project,
		Namespace:    *namespace,
		StateDir:     *stateDir,
		OnConflict:   *onConflict,
		LockTimeout:  *lockTimeout,
		Shared:       *shared,
		All:          *all,
		Force:        *force,
	})
	if err != nil {
		return err
	}
	if err := writeJSON(stdout, result); err != nil {
		return err
	}
	if failed, blocked := countFailedBlocked(result); failed > 0 || blocked > 0 {
		return actionStatusError{failed: failed, blocked: blocked}
	}
	return nil
}

func runCleanupDuplicates(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("skiller cleanup-duplicates", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	manifest := fs.String("manifest", "", "manifest path")
	home := fs.String("home", "", "home directory")
	project := fs.String("project", "", "project directory")
	namespace := fs.String("namespace", "", "namespace override")
	stateDir := fs.String("state-dir", "", "state directory")
	onConflict := fs.String("on-conflict", "block", "conflict mode")
	lockTimeout := fs.Duration("lock-timeout", 5*time.Second, "lock acquisition timeout")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*jsonOut {
		return fmt.Errorf("cleanup-duplicates currently requires --json")
	}
	if *manifest == "" {
		return fmt.Errorf("cleanup-duplicates requires --manifest")
	}
	result, err := app.CleanupDuplicates(context.Background(), app.CleanupOptions{
		ManifestPath: *manifest,
		Home:         *home,
		Project:      *project,
		Namespace:    *namespace,
		StateDir:     *stateDir,
		OnConflict:   *onConflict,
		LockTimeout:  *lockTimeout,
	})
	if err != nil {
		return err
	}
	if err := writeJSON(stdout, result); err != nil {
		return err
	}
	if failed, blocked := countFailedBlocked(result); failed > 0 || blocked > 0 {
		return actionStatusError{failed: failed, blocked: blocked}
	}
	return nil
}

func runStatus(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("skiller status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	manifest := fs.String("manifest", "", "manifest path")
	home := fs.String("home", "", "home directory")
	project := fs.String("project", "", "project directory")
	namespace := fs.String("namespace", "", "namespace override")
	stateDir := fs.String("state-dir", "", "state directory")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*jsonOut {
		return fmt.Errorf("status currently requires --json")
	}
	report, err := app.Status(app.StatusOptions{
		ManifestPath: *manifest,
		Home:         *home,
		Project:      *project,
		Namespace:    *namespace,
		StateDir:     *stateDir,
	})
	if err != nil {
		return err
	}
	return writeJSON(stdout, report)
}

func runConflicts(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("conflicts requires subcommand list")
	}
	switch args[0] {
	case "list":
		return runConflictsList(args[1:], stdout)
	default:
		return fmt.Errorf("unknown conflicts subcommand %q", args[0])
	}
}

func runConflictsList(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("skiller conflicts list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	manifest := fs.String("manifest", "", "manifest path")
	home := fs.String("home", "", "home directory")
	project := fs.String("project", "", "project directory")
	namespace := fs.String("namespace", "", "namespace override")
	stateDir := fs.String("state-dir", "", "state directory")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*jsonOut {
		return fmt.Errorf("conflicts list currently requires --json")
	}
	report, err := app.Conflicts(app.StatusOptions{
		ManifestPath: *manifest,
		Home:         *home,
		Project:      *project,
		Namespace:    *namespace,
		StateDir:     *stateDir,
	})
	if err != nil {
		return err
	}
	return writeJSON(stdout, report)
}

func runState(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("state requires subcommand repair")
	}
	switch args[0] {
	case "repair":
		return runStateRepair(args[1:], stdout)
	default:
		return fmt.Errorf("unknown state subcommand %q", args[0])
	}
}

func runStateRepair(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("skiller state repair", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	manifest := fs.String("manifest", "", "manifest path")
	home := fs.String("home", "", "home directory")
	project := fs.String("project", "", "project directory")
	namespace := fs.String("namespace", "", "namespace override")
	stateDir := fs.String("state-dir", "", "state directory")
	onConflict := fs.String("on-conflict", "block", "conflict mode")
	lockTimeout := fs.Duration("lock-timeout", 5*time.Second, "lock acquisition timeout")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*jsonOut {
		return fmt.Errorf("state repair currently requires --json")
	}
	if *manifest == "" {
		return fmt.Errorf("state repair requires --manifest")
	}
	ledger, err := app.Repair(context.Background(), app.RepairOptions{
		ManifestPath: *manifest,
		Home:         *home,
		Project:      *project,
		Namespace:    *namespace,
		StateDir:     *stateDir,
		OnConflict:   *onConflict,
		LockTimeout:  *lockTimeout,
	})
	if err != nil {
		return err
	}
	return writeJSON(stdout, ledger)
}

func usage(w io.Writer) error {
	_, err := fmt.Fprintln(w, `Usage:
  skiller registry --json
  skiller plan --manifest skiller.toml --json [--home DIR] [--project DIR] [--namespace N] [--on-conflict MODE]
  skiller install --manifest skiller.toml --json [--dry-run] [--state-dir DIR] [--home DIR] [--project DIR] [--namespace N] [--on-conflict MODE] [--lock-timeout DURATION]
  skiller uninstall --manifest skiller.toml --json [--state-dir DIR] [--home DIR] [--project DIR] [--namespace N] [--shared] [--all] [--force] [--lock-timeout DURATION]
  skiller cleanup-duplicates --manifest skiller.toml --json [--state-dir DIR] [--home DIR] [--project DIR] [--namespace N] [--lock-timeout DURATION]
  skiller status --json [--manifest skiller.toml] [--state-dir DIR] [--home DIR] [--project DIR] [--namespace N]
  skiller conflicts list --json [--manifest skiller.toml] [--state-dir DIR] [--home DIR] [--project DIR] [--namespace N]
  skiller state repair --manifest skiller.toml --json [--state-dir DIR] [--home DIR] [--project DIR] [--namespace N] [--on-conflict MODE] [--lock-timeout DURATION]`)
	return err
}

func writeJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/mostlydev/skiller/internal/planner"
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

func usage(w io.Writer) error {
	_, err := fmt.Fprintln(w, `Usage:
  skiller registry --json
  skiller plan --manifest skiller.toml --json [--home DIR] [--project DIR] [--namespace N] [--on-conflict MODE]`)
	return err
}

func writeJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

package planner

import (
	"github.com/mostlydev/skiller/internal/app"
	"github.com/mostlydev/skiller/internal/contract"
	"github.com/mostlydev/skiller/internal/digest"
	planpkg "github.com/mostlydev/skiller/pkg/plan"
)

type Options = app.Options

func Build(opts Options) (contract.Plan, error) {
	return app.Plan(opts)
}

func SortPlan(plan *contract.Plan) {
	planpkg.Sort(plan)
}

func digestPath(path string) (string, error) {
	return digest.Path(path)
}

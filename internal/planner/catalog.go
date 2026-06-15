package planner

import (
	"github.com/mostlydev/skiller/internal/contract"
	"github.com/mostlydev/skiller/pkg/registry"
)

func LoadCatalog() (contract.Catalog, error) {
	return registry.Load()
}

func decodeStrictTOML(name, data string, out any) error {
	return registry.DecodeStrictTOML(name, data, out)
}

package planner

import (
	"github.com/mostlydev/skiller/internal/contract"
	"github.com/mostlydev/skiller/pkg/manifest"
)

func LoadManifest(path string) (contract.Manifest, error) {
	return manifest.Load(path)
}

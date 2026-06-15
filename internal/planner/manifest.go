package planner

import (
	"fmt"

	"github.com/BurntSushi/toml"
	"github.com/mostlydev/skiller/internal/contract"
)

func LoadManifest(path string) (contract.Manifest, error) {
	var manifest contract.Manifest
	if _, err := toml.DecodeFile(path, &manifest); err != nil {
		return contract.Manifest{}, fmt.Errorf("decode manifest %s: %w", path, err)
	}
	if manifest.Schema == "" {
		return contract.Manifest{}, fmt.Errorf("manifest %s missing schema", path)
	}
	if manifest.Owner == "" {
		return contract.Manifest{}, fmt.Errorf("manifest %s missing owner", path)
	}
	return manifest, nil
}

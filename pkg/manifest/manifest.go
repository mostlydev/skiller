package manifest

import (
	"fmt"
	"strings"

	"github.com/mostlydev/skiller/internal/contract"
	"github.com/mostlydev/skiller/internal/stricttoml"
)

type Manifest = contract.Manifest
type Skill = contract.ManifestSkill
type TargetDir = contract.ManifestTargetDir
type Extra = contract.ManifestExtra

func Load(path string) (Manifest, error) {
	var manifest Manifest
	if err := stricttoml.DecodeFile(path, &manifest); err != nil {
		return Manifest{}, err
	}
	if manifest.Schema == "" {
		return Manifest{}, fmt.Errorf("manifest %s missing schema", path)
	}
	if manifest.Owner == "" {
		return Manifest{}, fmt.Errorf("manifest %s missing owner", path)
	}
	for i, skill := range manifest.Skills {
		if strings.TrimSpace(skill.Name) == "" {
			return Manifest{}, fmt.Errorf("skills[%d] missing name", i)
		}
		if strings.TrimSpace(skill.Source) == "" {
			return Manifest{}, fmt.Errorf("skills[%d] missing source", i)
		}
	}
	for i, extra := range manifest.Extras {
		if strings.TrimSpace(extra.ID) == "" {
			return Manifest{}, fmt.Errorf("extras[%d] missing id", i)
		}
		if strings.TrimSpace(extra.Source) == "" || strings.TrimSpace(extra.Target) == "" {
			return Manifest{}, fmt.Errorf("extras[%d] missing source or target", i)
		}
	}
	return manifest, nil
}

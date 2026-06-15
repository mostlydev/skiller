package registry

import (
	"fmt"

	skiller "github.com/mostlydev/skiller"
	"github.com/mostlydev/skiller/internal/contract"
	"github.com/mostlydev/skiller/internal/stricttoml"
)

type Catalog = contract.Catalog
type Harness = contract.Harness
type MarkerRule = contract.MarkerRule
type SourceRule = contract.SourceRule

func Load() (Catalog, error) {
	var harnesses Catalog
	if err := decodeEmbeddedTOML("data/harnesses.toml", &harnesses); err != nil {
		return Catalog{}, err
	}
	var markers Catalog
	if err := decodeEmbeddedTOML("data/markers.toml", &markers); err != nil {
		return Catalog{}, err
	}
	var sources Catalog
	if err := decodeEmbeddedTOML("data/sources.toml", &sources); err != nil {
		return Catalog{}, err
	}
	return Catalog{
		Schema:    "skiller-catalog.v1",
		Harnesses: harnesses.Harnesses,
		Markers:   markers.Markers,
		Sources:   sources.Sources,
	}, nil
}

func DecodeStrictTOML(name, data string, out any) error {
	return stricttoml.Decode(name, data, out)
}

func decodeEmbeddedTOML(name string, out any) error {
	data, err := skiller.EmbeddedFS.ReadFile(name)
	if err != nil {
		return fmt.Errorf("read embedded %s: %w", name, err)
	}
	return DecodeStrictTOML(name, string(data), out)
}

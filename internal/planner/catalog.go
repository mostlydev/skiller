package planner

import (
	"fmt"

	"github.com/BurntSushi/toml"
	skiller "github.com/mostlydev/skiller"
	"github.com/mostlydev/skiller/internal/contract"
)

func LoadCatalog() (contract.Catalog, error) {
	var harnesses contract.Catalog
	if err := decodeEmbeddedTOML("data/harnesses.toml", &harnesses); err != nil {
		return contract.Catalog{}, err
	}
	var markers contract.Catalog
	if err := decodeEmbeddedTOML("data/markers.toml", &markers); err != nil {
		return contract.Catalog{}, err
	}
	var sources contract.Catalog
	if err := decodeEmbeddedTOML("data/sources.toml", &sources); err != nil {
		return contract.Catalog{}, err
	}
	return contract.Catalog{
		Schema:    "skiller-catalog.v1",
		Harnesses: harnesses.Harnesses,
		Markers:   markers.Markers,
		Sources:   sources.Sources,
	}, nil
}

func decodeEmbeddedTOML(name string, out any) error {
	data, err := skiller.EmbeddedFS.ReadFile(name)
	if err != nil {
		return fmt.Errorf("read embedded %s: %w", name, err)
	}
	if _, err := toml.Decode(string(data), out); err != nil {
		return fmt.Errorf("decode embedded %s: %w", name, err)
	}
	return nil
}

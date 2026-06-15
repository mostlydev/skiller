package planner

import (
	"fmt"
	"strings"

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
	return decodeStrictTOML(name, string(data), out)
}

// decodeStrictTOML decodes TOML into a typed struct and rejects any key that the
// struct does not declare. The typed loader is the data contract for the embedded
// registries (design doc §6.1): an unknown or misspelled field is a hard error so
// data/*.toml and the Go types cannot silently drift, rather than a separate JSON
// schema that would be a second source of truth.
func decodeStrictTOML(name, data string, out any) error {
	md, err := toml.Decode(data, out)
	if err != nil {
		return fmt.Errorf("decode %s: %w", name, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, key := range undecoded {
			keys[i] = key.String()
		}
		return fmt.Errorf("%s has unknown field(s): %s", name, strings.Join(keys, ", "))
	}
	return nil
}

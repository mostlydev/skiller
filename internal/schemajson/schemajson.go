package schemajson

import (
	"bytes"
	"fmt"
	"io/fs"
	"path/filepath"

	skiller "github.com/mostlydev/skiller"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const BaseID = "https://github.com/mostlydev/skiller/schema/"

func Validate(schemaName string, data []byte) error {
	compiler, err := compiler()
	if err != nil {
		return err
	}
	schema, err := compiler.Compile(BaseID + schemaName)
	if err != nil {
		return fmt.Errorf("compile %s: %w", schemaName, err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("parse json: %w", err)
	}
	if err := schema.Validate(doc); err != nil {
		return fmt.Errorf("validate %s: %w", schemaName, err)
	}
	return nil
}

func compiler() (*jsonschema.Compiler, error) {
	compiler := jsonschema.NewCompiler()
	paths, err := fs.Glob(skiller.EmbeddedFS, "schema/*.schema.json")
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		data, err := skiller.EmbeddedFS.ReadFile(path)
		if err != nil {
			return nil, err
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("parse schema %s: %w", path, err)
		}
		id := BaseID + filepath.Base(path)
		if err := compiler.AddResource(id, doc); err != nil {
			return nil, fmt.Errorf("add schema resource %s: %w", id, err)
		}
	}
	return compiler, nil
}

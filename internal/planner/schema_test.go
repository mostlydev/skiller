package planner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const schemaBaseID = "https://github.com/mostlydev/skiller/schema/"

func TestGoldenPlansValidateAgainstPlanSchema(t *testing.T) {
	compiler := mustSchemaCompiler(t)
	planSchema, err := compiler.Compile(schemaBaseID + "plan.schema.json")
	if err != nil {
		t.Fatalf("compile plan schema: %v", err)
	}
	plans, err := filepath.Glob(filepath.Join("..", "..", "testdata", "m0", "golden", "*.plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) == 0 {
		t.Fatal("no golden plans found")
	}
	for _, path := range plans {
		t.Run(filepath.Base(path), func(t *testing.T) {
			doc := mustJSONDoc(t, path)
			if err := planSchema.Validate(doc); err != nil {
				t.Fatalf("validate %s: %v", path, err)
			}
		})
	}
}

func TestMarkerFixtureValidatesAgainstMarkerSchema(t *testing.T) {
	compiler := mustSchemaCompiler(t)
	markerSchema, err := compiler.Compile(schemaBaseID + "marker.schema.json")
	if err != nil {
		t.Fatalf("compile marker schema: %v", err)
	}
	path := filepath.Join("..", "..", "testdata", "m0", "markers", "skiller-install.json")
	if err := markerSchema.Validate(mustJSONDoc(t, path)); err != nil {
		t.Fatalf("validate marker fixture: %v", err)
	}
}

func mustSchemaCompiler(t *testing.T) *jsonschema.Compiler {
	t.Helper()
	compiler := jsonschema.NewCompiler()
	paths, err := filepath.Glob(filepath.Join("..", "..", "schema", "*.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no schema files found")
	}
	for _, path := range paths {
		doc := mustJSONDoc(t, path)
		id := schemaBaseID + filepath.Base(path)
		if err := compiler.AddResource(id, doc); err != nil {
			t.Fatalf("add schema resource %s: %v", id, err)
		}
	}
	return compiler
}

func mustJSONDoc(t *testing.T, path string) any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	doc, err := jsonschema.UnmarshalJSON(f)
	if err != nil {
		t.Fatalf("parse json %s: %v", path, err)
	}
	return doc
}

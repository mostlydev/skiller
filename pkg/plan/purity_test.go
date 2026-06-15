package plan

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"
)

func TestPackagePlanHasNoDirectOSOrNetImports(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if filepath.Base(file) == "purity_test.go" {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, imp := range parsed.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatalf("unquote import in %s: %v", file, err)
			}
			if path == "os" || path == "net" {
				t.Fatalf("pkg/plan must stay pure; %s imports %q", file, path)
			}
			if isNetSubpackage(path) {
				t.Fatalf("pkg/plan must stay pure; %s imports %q", file, path)
			}
			if path == "github.com/mostlydev/skiller/internal/digest" {
				t.Fatalf("pkg/plan must stay pure; %s imports filesystem digest package", file)
			}
		}
	}
}

func isNetSubpackage(path string) bool {
	return len(path) > len("net/") && path[:len("net/")] == "net/"
}

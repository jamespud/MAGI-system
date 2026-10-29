package magi_test

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// F59-03: the verifier injector must not exist in the production compile unit.
// TestGoFiles are intentionally excluded; only GoFiles/CgoFiles are inspected.
func TestExposureGateVerifierInjectionAbsentFromProductionPackage(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}
	files := append(append([]string(nil), pkg.GoFiles...), pkg.CgoFiles...)
	if len(files) == 0 {
		t.Fatal("production package has no Go files")
	}
	fset := token.NewFileSet()
	for _, name := range files {
		file, err := parser.ParseFile(fset, filepath.Join(pkg.Dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil {
				continue
			}
			if fn.Name.Name == "SetExposureGateAttestationVerifierForTest" {
				t.Fatalf("verifier injector leaked into production file %s", name)
			}
		}
	}
}

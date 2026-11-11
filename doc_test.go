package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestUndocumentedFuncsFlagsMissingAndMisnamedDocs checks that only missing or misnamed doc comments are flagged.
func TestUndocumentedFuncsFlagsMissingAndMisnamedDocs(t *testing.T) {
	dir := t.TempDir()
	src := `package x

func a() {}

// b does b.
func b() {}

// wrong.
func c() {}

type T int

// m does m.
func (T) m() {}
`
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := undocumentedFuncs(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "x.go") + ":3 a", filepath.Join(dir, "x.go") + ":9 c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestEveryFunctionHasDocComment enforces the doc-comment rule on this package.
func TestEveryFunctionHasDocComment(t *testing.T) {
	got, err := undocumentedFuncs(".")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("functions without a doc comment starting with their name:\n%s", strings.Join(got, "\n"))
	}
}

// undocumentedFuncs returns "<file>:<line> <name>" for every function or method declared in dir/*.go
// (test and build-tagged files included) whose doc comment is missing or does not start with its name.
func undocumentedFuncs(dir string) ([]string, error) {
	files, err := filepath.Glob(dir + "/*.go")
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	var out []string
	for _, file := range files {
		f, err := parser.ParseFile(fset, file, nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fn.Doc == nil || !strings.HasPrefix(fn.Doc.Text(), fn.Name.Name+" ") {
				out = append(out, fmt.Sprintf("%s:%d %s", file, fset.Position(fn.Pos()).Line, fn.Name.Name))
			}
		}
	}
	return out, nil
}

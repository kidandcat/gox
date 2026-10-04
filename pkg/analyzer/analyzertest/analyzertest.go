// Package analyzertest provides helpers for unit-testing individual
// analyzers without going through the loader / `go list` pipeline.
//
// Source code is supplied as a string, parsed with go/parser, type-checked
// with go/types + importer.Default() (so stdlib imports work), and fed to
// the analyzer. The list of reported issues is returned for inspection.
package analyzertest

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"testing"

	"github.com/kidandcat/gox/pkg/analyzer"
)

// Run parses and type-checks `src` (as a single file named "x.go" in package
// "p"), runs the analyzer, and returns every issue it reports, sorted by
// line/column.
func Run(t *testing.T, a *analyzer.Analyzer, src string) []analyzer.Issue {
	t.Helper()
	fset := token.NewFileSet()
	f, parseErr := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
	if parseErr != nil {
		t.Fatalf("parse: %v", parseErr)
	}
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Implicits:  map[ast.Node]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Scopes:     map[ast.Node]*types.Scope{},
	}
	conf := &types.Config{
		Importer: importer.Default(),
		// Allow type errors — analyzers should still see most of the AST.
		Error: func(error) {},
	}
	// Use a domain-style path so analyzers that treat single-segment imports
	// as stdlib (e.g. namedargs) don't accidentally exempt this code.
	pkg, _ := conf.Check( /* path */ "example.test/p" /* fset */, fset, []*ast.File{f}, info) // safe-ignore: type errors are swallowed by conf.Error; analyzers tolerate partial info

	var issues []analyzer.Issue
	pass := &analyzer.Pass{
		Fset:      fset,
		Pkg:       pkg,
		TypesInfo: info,
		Files:     []*ast.File{f},
		Report:    func(i analyzer.Issue) { issues = append(issues, i) },
	}
	a.Run(pass)
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Pos.Line != issues[j].Pos.Line {
			return issues[i].Pos.Line < issues[j].Pos.Line
		}
		return issues[i].Pos.Column < issues[j].Pos.Column
	})
	return issues
}

// AssertLines fails the test if the lines on which issues fired do not
// match `want` exactly. Useful for "must fire at line N, must NOT fire at
// line M" assertions in compact form.
func AssertLines(t *testing.T, issues []analyzer.Issue, want []int) {
	t.Helper()
	got := make([]int, len(issues))
	for i, is := range issues {
		got[i] = is.Pos.Line
	}
	if !equalInts(got, want) {
		t.Errorf("issue lines: got %v, want %v", got, want)
		for _, is := range issues {
			t.Logf("  %s:%d: %s", is.Analyzer, is.Pos.Line, is.Message)
		}
	}
}

// AssertNone fails if any issue was reported.
func AssertNone(t *testing.T, issues []analyzer.Issue) {
	t.Helper()
	if len(issues) > 0 {
		t.Errorf("expected no issues, got %d:", len(issues))
		for _, is := range issues {
			t.Logf("  %s:%d:%d: %s", is.Analyzer, is.Pos.Line, is.Pos.Column, is.Message)
		}
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

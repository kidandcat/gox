// Package nilmap reports writes to a map variable declared with `var m
// map[K]V` and never assigned, which panic at run time.
//
//	var seen map[string]bool
//	seen[id] = true // panic: assignment to entry in nil map
//
// The variable must be local, declared without a value, and never assigned
// or have its address taken anywhere in the function (closures included).
// This is staticcheck's SA5000 on the AST.
//
// Opt out with `// safe-ignore: <reason>`.
package nilmap

import (
	_ "embed"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/kidandcat/gox/pkg/analyzer"
)

//go:embed nilmap.md
var explanation string // global-ok: populated at compile time by //go:embed, never mutated

func init() {
	analyzer.Register(&analyzer.Analyzer{
		Name:        "nilmap",
		Doc:         "write to a map declared with `var m map[K]V` and never initialised",
		Explanation: explanation,
		Run:         run,
	})
}

func run(pass *analyzer.Pass) {
	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			checkFunc(pass, file, fn.Body)
		}
	}
}

func checkFunc(pass *analyzer.Pass, file *ast.File, body *ast.BlockStmt) {
	nilMaps := map[types.Object]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok || len(vs.Values) != 0 {
			return true
		}
		for _, id := range vs.Names {
			obj := pass.TypesInfo.Defs[id]
			if obj == nil {
				continue
			}
			if _, ok := obj.Type().Underlying().(*types.Map); ok {
				nilMaps[obj] = true
			}
		}
		return true
	})
	if len(nilMaps) == 0 {
		return
	}
	var writes []*ast.IndexExpr
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			for _, l := range x.Lhs {
				switch lv := ast.Unparen(l).(type) {
				case *ast.Ident:
					delete(nilMaps, pass.TypesInfo.ObjectOf(lv)) // initialised
				case *ast.IndexExpr:
					writes = append(writes, lv)
				}
			}
		case *ast.IncDecStmt:
			if ix, ok := ast.Unparen(x.X).(*ast.IndexExpr); ok {
				writes = append(writes, ix)
			}
		case *ast.UnaryExpr:
			if id, ok := ast.Unparen(x.X).(*ast.Ident); ok && x.Op == token.AND {
				delete(nilMaps, pass.TypesInfo.ObjectOf(id)) // may be set elsewhere
			}
		case *ast.RangeStmt:
			for _, e := range []ast.Expr{x.Key, x.Value} {
				if id, ok := e.(*ast.Ident); ok && x.Tok == token.ASSIGN {
					delete(nilMaps, pass.TypesInfo.ObjectOf(id))
				}
			}
		}
		return true
	})
	for _, w := range writes {
		id, ok := ast.Unparen(w.X).(*ast.Ident)
		if !ok || !nilMaps[pass.TypesInfo.ObjectOf(id)] {
			continue
		}
		if pass.HasLineAnnotation(file, w.Pos(), analyzer.AnnSafeIgnore) {
			continue
		}
		pass.Report(analyzer.Issue{
			Analyzer: "nilmap",
			Pos:      pass.Fset.Position(w.Pos()),
			Message:  "write to nil map " + id.Name + ": it is declared with `var` and never initialised, so this panics",
			Hint:     id.Name + " := make(" + types.TypeString(pass.TypesInfo.TypeOf(id), types.RelativeTo(pass.Pkg)) + ")",
		})
	}
}

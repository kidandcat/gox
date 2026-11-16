// Package waitgroup reports sync.WaitGroup.Add called inside the goroutine
// it is meant to count.
//
//	go func() {
//		wg.Add(1) // races with wg.Wait: Wait may return before Add runs
//		defer wg.Done()
//		...
//	}()
//
// Only Add calls directly in the body of a `go func() {...}()` literal, on a
// WaitGroup declared outside that literal, are reported: the same shape go
// vet's waitgroup check looks for.
//
// Opt out with `// safe-ignore: <reason>`.
package waitgroup

import (
	_ "embed"
	"go/ast"
	"go/types"

	"github.com/kidandcat/gox/pkg/analyzer"
)

//go:embed waitgroup.md
var explanation string // global-ok: populated at compile time by //go:embed, never mutated

func init() {
	analyzer.Register(&analyzer.Analyzer{
		Name:        "waitgroup",
		Doc:         "sync.WaitGroup.Add called inside the goroutine it counts",
		Explanation: explanation,
		Run:         run,
	})
}

func run(pass *analyzer.Pass) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			g, ok := n.(*ast.GoStmt)
			if !ok {
				return true
			}
			lit, ok := g.Call.Fun.(*ast.FuncLit)
			if !ok {
				return true
			}
			for _, st := range lit.Body.List {
				es, ok := st.(*ast.ExprStmt)
				if !ok {
					continue
				}
				call, ok := es.X.(*ast.CallExpr)
				if !ok || !isWaitGroupAdd(pass, call) || declaredInside(pass, call, lit) {
					continue
				}
				if pass.HasLineAnnotation(file, call.Pos(), analyzer.AnnSafeIgnore) {
					continue
				}
				pass.Report(analyzer.Issue{
					Analyzer: "waitgroup",
					Pos:      pass.Fset.Position(call.Pos()),
					Message:  "WaitGroup.Add inside the goroutine it counts; Wait can return before Add runs",
					Hint:     "call wg.Add before the `go` statement, or use wg.Go(func() { ... }) (Go 1.25+)",
				})
			}
			return true
		})
	}
}

// declaredInside reports whether the WaitGroup is a variable of the
// goroutine itself, which then counts goroutines it starts, not itself.
func declaredInside(pass *analyzer.Pass, call *ast.CallExpr, lit *ast.FuncLit) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	e := sel.X
	for {
		switch x := e.(type) {
		case *ast.SelectorExpr:
			e = x.X
			continue
		case *ast.UnaryExpr:
			e = x.X
			continue
		case *ast.ParenExpr:
			e = x.X
			continue
		case *ast.Ident:
			obj := pass.TypesInfo.ObjectOf(x)
			return obj != nil && obj.Pos() >= lit.Pos() && obj.Pos() < lit.End()
		}
		return false
	}
}

func isWaitGroupAdd(pass *analyzer.Pass, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Add" {
		return false
	}
	t := pass.TypesInfo.TypeOf(sel.X)
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Pkg().Path() == "sync" && named.Obj().Name() == "WaitGroup"
}

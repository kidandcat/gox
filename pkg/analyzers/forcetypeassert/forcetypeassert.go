// Package forcetypeassert forbids type assertions that would panic on failure.
//
// Reported:
//
//	x := v.(T)       // missing ok; panics if v is not T
//	doStuff(v.(T))   // assertion result used directly inside a call
//	x, _ := v.(T)    // discarded ok; mismatch yields the zero value silently
//
// Allowed:
//
//	x, ok := v.(T)
//	var x, ok = v.(T)
//	switch x := v.(type) { ... }   // type switch (handled by the compiler)
package forcetypeassert

import (
	_ "embed"
	"go/ast"

	"github.com/mentasystems/gox/pkg/analyzer"
)

//go:embed forcetypeassert.md
var explanation string // global-ok: populated at compile time by //go:embed, never mutated

func init() {
	analyzer.Register(&analyzer.Analyzer{
		Name:        "forcetypeassert",
		Doc:         "forbids type assertions without the comma-ok form",
		Explanation: explanation,
		Run:         run,
	})
}

func run(pass *analyzer.Pass) {
	for _, file := range pass.Files {
		// First pass: collect assertions that ARE in a comma-ok position so we don't flag them.
		safe := map[*ast.TypeAssertExpr]bool{}
		discardedOK := map[*ast.TypeAssertExpr]bool{}
		classify := func(lhs []ast.Expr, rhs []ast.Expr) {
			if len(rhs) != 1 {
				return
			}
			ta, ok := rhs[0].(*ast.TypeAssertExpr)
			if !ok || ta.Type == nil {
				return // not an assertion, or a type switch guard (already safe)
			}
			if len(lhs) != 2 {
				return
			}
			if id, ok := lhs[1].(*ast.Ident); ok && id.Name != "_" {
				safe[ta] = true
			} else {
				// x, _ := v.(T) is not comma-ok: the failure bit is dropped.
				discardedOK[ta] = true
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch s := n.(type) {
			case *ast.AssignStmt:
				// x, ok := v.(T)  /  x, ok = v.(T)
				classify(s.Lhs, s.Rhs)
			case *ast.ValueSpec:
				// var x, ok = v.(T)
				names := make([]ast.Expr, len(s.Names))
				for i, id := range s.Names {
					names[i] = id
				}
				classify(names, s.Values)
			}
			return true
		})

		// Also: `if x, ok := v.(T); ok { ... }` — covered above because the AssignStmt has 2 LHS.

		// Second pass: report every other assertion.
		ast.Inspect(file, func(n ast.Node) bool {
			ta, ok := n.(*ast.TypeAssertExpr)
			if !ok {
				return true
			}
			if ta.Type == nil {
				return true // type-switch guard
			}
			if safe[ta] {
				return true
			}
			if pass.HasLineAnnotation(file, ta.Pos(), analyzer.AnnSafeIgnore) {
				return true
			}
			msg := "type assertion without comma-ok will panic on mismatch"
			hint := "use `x, ok := v.(T); if !ok { ... }` or, if a panic is intentional, append `// safe-ignore: <reason>`"
			if discardedOK[ta] {
				msg = "type assertion discards ok; mismatch yields the zero value silently"
				hint = "use `x, ok := v.(T); if !ok { ... }` or annotate with `// safe-ignore: <reason>`"
			}
			pass.Report(analyzer.Issue{
				Analyzer: "forcetypeassert",
				Pos:      pass.Fset.Position(ta.Pos()),
				Message:  msg,
				Hint:     hint,
			})
			return true
		})
	}
}

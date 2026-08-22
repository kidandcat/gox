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
		ast.Inspect(file, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			if len(as.Rhs) != 1 {
				return true
			}
			ta, ok := as.Rhs[0].(*ast.TypeAssertExpr)
			if !ok {
				return true
			}
			if ta.Type == nil {
				return true // type switch guard — already safe
			}
			if len(as.Lhs) == 2 {
				if id, ok := as.Lhs[1].(*ast.Ident); ok && id.Name != "_" {
					safe[ta] = true
				} else {
					// x, _ := v.(T) is not comma-ok: the failure bit is dropped.
					discardedOK[ta] = true
				}
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
			if analyzer.HasLineAnnotation(pass.Fset, file, ta.Pos(), analyzer.AnnSafeIgnore) {
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

// Package forcetypeassert forbids type assertions that would panic on failure.
//
// Reported:
//
//	x := v.(T)       // missing ok; panics if v is not T
//	doStuff(v.(T))   // assertion result used directly inside a call
//
// Allowed:
//
//	x, ok := v.(T)
//	switch x := v.(type) { ... }   // type switch (handled by the compiler)
package forcetypeassert

import (
	_ "embed"
	"go/ast"
	"go/token"

	"github.com/kidandcat/gox/pkg/analyzer"
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
				safe[ta] = true
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
			if hasIgnoreOnLine(pass.Fset, file, ta.Pos()) {
				return true
			}
			pass.Report(analyzer.Issue{
				Analyzer: "forcetypeassert",
				Pos:      pass.Fset.Position(ta.Pos()),
				Message:  "type assertion without comma-ok will panic on mismatch",
				Hint:     "use `x, ok := v.(T); if !ok { ... }` or, if a panic is intentional, append `// safe-ignore: <reason>`",
			})
			return true
		})
	}
}

func hasIgnoreOnLine(fset *token.FileSet, file *ast.File, pos token.Pos) bool {
	line := fset.Position(pos).Line
	for _, cg := range file.Comments {
		if fset.Position(cg.Pos()).Line == line {
			if analyzer.HasAnnotation(cg, analyzer.AnnSafeIgnore) {
				return true
			}
		}
	}
	return false
}

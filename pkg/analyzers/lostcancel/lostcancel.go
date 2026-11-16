// Package lostcancel reports cancel functions from context.WithCancel,
// WithTimeout and WithDeadline (and their Cause variants) that are dropped.
//
//	ctx, _ := context.WithTimeout(ctx, time.Second) // timer leaks until it fires
//	ctx, cancel := context.WithCancel(ctx)          // cancel never used
//
// go vet's lostcancel follows control flow to find paths that skip the call.
// This rule stays on the AST: it reports a cancel assigned to `_`, or a cancel
// variable that is never referenced again in the function. A cancel that is
// deferred, called, returned, stored or passed on counts as used.
//
// Opt out with `// safe-ignore: <reason>`.
package lostcancel

import (
	_ "embed"
	"go/ast"
	"go/types"

	"github.com/kidandcat/gox/pkg/analyzer"
)

//go:embed lostcancel.md
var explanation string // global-ok: populated at compile time by //go:embed, never mutated

func init() {
	analyzer.Register(&analyzer.Analyzer{
		Name:        "lostcancel",
		Doc:         "cancel function from context.WithCancel/WithTimeout/WithDeadline discarded or never used",
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
	uses := map[types.Object]int{}
	ast.Inspect(body, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if obj := pass.TypesInfo.Uses[id]; obj != nil {
				uses[obj]++
			}
		}
		return true
	})
	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != 2 || len(as.Rhs) != 1 {
			return true
		}
		call, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok || !isWithCancel(pass, call) {
			return true
		}
		id, ok := as.Lhs[1].(*ast.Ident)
		if !ok {
			return true // stored into a field or element
		}
		var msg string
		if id.Name == "_" {
			msg = "the cancel function returned by context." + calleeName(call) + " is discarded"
		} else {
			obj := pass.TypesInfo.Defs[id]
			if obj == nil {
				obj = pass.TypesInfo.Uses[id]
				if obj != nil && uses[obj] > 1 {
					return true // plain `=` to an existing variable used elsewhere
				}
			}
			if obj == nil || (pass.TypesInfo.Defs[id] != nil && uses[obj] > 0) {
				return true
			}
			msg = "the cancel function " + id.Name + " is never called"
		}
		if pass.HasLineAnnotation(file, as.Pos(), analyzer.AnnSafeIgnore) {
			return true
		}
		pass.Report(analyzer.Issue{
			Analyzer: "lostcancel",
			Pos:      pass.Fset.Position(as.Pos()),
			Message:  msg + "; the context's resources leak until its parent is done",
			Hint:     "keep the cancel func and `defer cancel()` right after the call",
		})
		return true
	})
}

func isWithCancel(pass *analyzer.Pass, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	pn, ok := pass.TypesInfo.Uses[pkg].(*types.PkgName)
	if !ok || pn.Imported().Path() != "context" {
		return false
	}
	switch sel.Sel.Name {
	case "WithCancel", "WithTimeout", "WithDeadline", "WithCancelCause", "WithTimeoutCause", "WithDeadlineCause":
		return true
	}
	return false
}

func calleeName(call *ast.CallExpr) string {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		return sel.Sel.Name
	}
	return "With..."
}

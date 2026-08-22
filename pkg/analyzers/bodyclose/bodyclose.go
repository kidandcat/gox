// Package bodyclose reports HTTP responses whose Body is never closed.
//
// For each assignment whose RHS is a call returning *http.Response, the
// analyzer requires that, somewhere in the same enclosing block (or a
// subordinate one), there exists a statement of the form `X.Body.Close()`
// (optionally inside a defer), where X is the assigned identifier.
//
// Heuristic, not sound. It will miss cases where the response escapes
// through a function call or struct field. Those cases are uncommon enough
// that the false-negative rate is acceptable in exchange for zero false
// positives on idiomatic code.
package bodyclose

import (
	_ "embed"
	"go/ast"
	"go/types"

	"github.com/mentasystems/gox/pkg/analyzer"
)

//go:embed bodyclose.md
var explanation string // global-ok: populated at compile time by //go:embed, never mutated

func init() {
	analyzer.Register(&analyzer.Analyzer{
		Name:        "bodyclose",
		Doc:         "reports *http.Response values whose Body is never closed",
		Explanation: explanation,
		Run:         run,
	})
}

func run(pass *analyzer.Pass) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				return true
			}
			checkBlock(pass, file, fn.Body)
			return true
		})
		// Also handle function literals at top-level (rare but possible).
		ast.Inspect(file, func(n ast.Node) bool {
			fl, ok := n.(*ast.FuncLit)
			if !ok || fl.Body == nil {
				return true
			}
			checkBlock(pass, file, fl.Body)
			return true
		})
	}
}

func checkBlock(pass *analyzer.Pass, file *ast.File, body *ast.BlockStmt) {
	// Find every assignment introducing a *http.Response.
	type respBind struct {
		ident *ast.Ident
		stmt  ast.Node
	}
	var binds []respBind

	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Rhs) != 1 {
			return true
		}
		call, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		callType := pass.TypesInfo.TypeOf(call)
		if callType == nil {
			return true
		}
		tup, isTup := callType.(*types.Tuple)
		// Pair each LHS ident with its type slot.
		var lhsAndType []struct {
			id *ast.Ident
			t  types.Type
		}
		if isTup {
			for i, lhs := range as.Lhs {
				if id, ok := lhs.(*ast.Ident); ok {
					if i < tup.Len() {
						lhsAndType = append(lhsAndType, struct {
							id *ast.Ident
							t  types.Type
						}{id, tup.At(i).Type()})
					}
				}
			}
		} else if len(as.Lhs) == 1 {
			if id, ok := as.Lhs[0].(*ast.Ident); ok {
				lhsAndType = append(lhsAndType, struct {
					id *ast.Ident
					t  types.Type
				}{id, callType})
			}
		}
		for _, p := range lhsAndType {
			if p.id.Name == "_" {
				continue
			}
			if isHTTPResponsePointer(p.t) {
				binds = append(binds, respBind{ident: p.id, stmt: as})
			}
		}
		return true
	})

	if len(binds) == 0 {
		return
	}

	// For each bind, scan the rest of the body for `X.Body.Close()`.
	for _, b := range binds {
		if analyzer.HasLineAnnotation(pass.Fset, file, b.ident.Pos(), analyzer.AnnSafeIgnore) {
			continue
		}
		if hasBodyClose(body, b.ident.Name, pass.TypesInfo) {
			continue
		}
		pass.Report(analyzer.Issue{
			Analyzer: "bodyclose",
			Pos:      pass.Fset.Position(b.ident.Pos()),
			Message:  "*http.Response Body is never closed",
			Hint:     "add `defer " + b.ident.Name + ".Body.Close()` immediately after the call, or annotate the assignment with `// safe-ignore: <reason>`",
		})
	}
}

func isHTTPResponsePointer(t types.Type) bool {
	ptr, ok := t.(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := ptr.Elem().(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	return obj.Pkg().Path() == "net/http" && obj.Name() == "Response"
}

func hasBodyClose(body *ast.BlockStmt, name string, info *types.Info) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		// Want: <name>.Body.Close()
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Close" {
			return true
		}
		inner, ok := sel.X.(*ast.SelectorExpr)
		if !ok || inner.Sel.Name != "Body" {
			return true
		}
		id, ok := inner.X.(*ast.Ident)
		if !ok || id.Name != name {
			return true
		}
		found = true
		return false
	})
	_ = info
	return found
}

// Package bodyclose reports HTTP responses whose Body is never closed.
//
// For each assignment whose RHS is a call returning *http.Response, the
// analyzer requires that, somewhere in the same enclosing block (or a
// subordinate one), there exists a statement of the form `X.Body.Close()`
// (optionally inside a defer), where X is the assigned identifier.
//
// A response that is returned directly to the caller (`return resp, err`)
// is treated as handed off: closing it becomes the caller's job.
//
// Heuristic, not sound. A response that escapes through a function call or
// struct field (e.g. `defer closeBody(resp)`) is still reported; annotate
// those sites with `// safe-ignore: <reason>`.
package bodyclose

import (
	_ "embed"
	"go/ast"
	"go/types"

	"github.com/kidandcat/gox/pkg/analyzer"
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
		// Nested function literals are checked on their own by run(); scanning
		// them here as well reported every leak inside a closure twice.
		if _, isLit := n.(*ast.FuncLit); isLit {
			return false
		}
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

	// One walk for closes and one for returns, keyed by types.Object so a
	// shadowed `resp` in an inner block cannot close the outer one.
	closed := closedResponses(pass, body)
	returned := returnedResponses(pass, body)
	for _, b := range binds {
		if pass.HasLineAnnotation(file, b.ident.Pos(), analyzer.AnnSafeIgnore) {
			continue
		}
		obj := pass.TypesInfo.ObjectOf(b.ident)
		if obj != nil && (closed[obj] || returned[obj]) {
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

// closedResponses collects every object that is the receiver of `.Body.Close()`,
// including closes written inside nested function literals (defer func(){ ... }).
func closedResponses(pass *analyzer.Pass, body *ast.BlockStmt) map[types.Object]bool {
	out := map[types.Object]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Close" {
			return true
		}
		inner, ok := sel.X.(*ast.SelectorExpr)
		if !ok || inner.Sel.Name != "Body" {
			return true
		}
		id, ok := inner.X.(*ast.Ident)
		if !ok {
			return true
		}
		if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
			out[obj] = true
		}
		return true
	})
	return out
}

// returnedResponses collects objects returned as-is from the function that
// owns body. A return inside a nested literal does not hand the response to
// this function's caller.
func returnedResponses(pass *analyzer.Pass, body *ast.BlockStmt) map[types.Object]bool {
	out := map[types.Object]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			for _, r := range v.Results {
				id, ok := r.(*ast.Ident)
				if !ok {
					continue
				}
				if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
					out[obj] = true
				}
			}
		}
		return true
	})
	return out
}

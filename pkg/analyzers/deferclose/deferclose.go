// Package deferclose reports `defer x.Close()` placed before the error of the
// call that produced x is checked.
//
//	f, err := os.Open(name)
//	defer f.Close() // f is nil when err != nil: Close panics or fails
//	if err != nil { return err }
//
// The rule looks at two consecutive statements: an assignment that yields a
// value and an error, then a defer whose receiver is that value (`x.Close()`,
// `x.Body.Close()`). This is staticcheck's SA5001 without SSA.
//
// Opt out with `// safe-ignore: <reason>`.
package deferclose

import (
	_ "embed"
	"fmt"
	"go/ast"
	"go/types"

	"github.com/kidandcat/gox/pkg/analyzer"
)

//go:embed deferclose.md
var explanation string // global-ok: populated at compile time by //go:embed, never mutated

func init() {
	analyzer.Register(&analyzer.Analyzer{
		Name:        "deferclose",
		Doc:         "defer x.Close() before the error from the call that returned x is checked",
		Explanation: explanation,
		Run:         run,
	})
}

func run(pass *analyzer.Pass) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			var list []ast.Stmt
			switch b := n.(type) {
			case *ast.BlockStmt:
				list = b.List
			case *ast.CaseClause:
				list = b.Body
			case *ast.CommClause:
				list = b.Body
			default:
				return true
			}
			for i := 0; i+1 < len(list); i++ {
				check(pass, file, list[i], list[i+1])
			}
			return true
		})
	}
}

func check(pass *analyzer.Pass, file *ast.File, first, second ast.Stmt) {
	as, ok := first.(*ast.AssignStmt)
	if !ok || len(as.Rhs) != 1 || len(as.Lhs) < 2 {
		return
	}
	if _, ok := as.Rhs[0].(*ast.CallExpr); !ok {
		return
	}
	errID, ok := as.Lhs[len(as.Lhs)-1].(*ast.Ident)
	if !ok || errID.Name == "_" || !isError(pass.TypesInfo.TypeOf(errID)) {
		return
	}
	def, ok := second.(*ast.DeferStmt)
	if !ok {
		return
	}
	sel, ok := def.Call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Close" {
		return
	}
	root := rootIdent(sel.X)
	if root == nil {
		return
	}
	rootObj := pass.TypesInfo.ObjectOf(root)
	if rootObj == nil {
		return
	}
	for _, l := range as.Lhs[:len(as.Lhs)-1] {
		id, ok := l.(*ast.Ident)
		if !ok || pass.TypesInfo.ObjectOf(id) != rootObj {
			continue
		}
		if pass.HasLineAnnotation(file, def.Pos(), analyzer.AnnSafeIgnore) {
			return
		}
		pass.Report(analyzer.Issue{
			Analyzer: "deferclose",
			Pos:      pass.Fset.Position(def.Pos()),
			Message:  fmt.Sprintf("defer %s.Close() before %s is checked; %s may be nil", exprString(sel.X), errID.Name, root.Name),
			Hint:     fmt.Sprintf("move the defer after `if %s != nil { ... }`", errID.Name),
		})
		return
	}
}

// rootIdent returns x for x, x.Body, x.a.b.
func rootIdent(e ast.Expr) *ast.Ident {
	for {
		switch x := e.(type) {
		case *ast.Ident:
			return x
		case *ast.SelectorExpr:
			e = x.X
		default:
			return nil
		}
	}
}

func exprString(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return exprString(x.X) + "." + x.Sel.Name
	}
	return "x"
}

func isError(t types.Type) bool {
	if t == nil {
		return false
	}
	return types.Identical(t, types.Universe.Lookup("error").Type())
}

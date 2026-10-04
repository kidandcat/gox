// Package contextcheck enforces context propagation.
//
// Inside a function or function literal that has a `context.Context`
// parameter, any call to `context.Background()` or `context.TODO()` is
// reported, since it discards the caller's cancellation and deadline.
// A nested literal that receives its own context is checked on its own:
// Background inside it is still reported, and it is not also blamed on the
// outer function.
//
// The analyzer does not track which context is passed to other calls.
// Opt out with `// safe-ignore: <reason>`.
package contextcheck

import (
	_ "embed"
	"go/ast"
	"go/types"

	"github.com/kidandcat/gox/pkg/analyzer"
)

//go:embed contextcheck.md
var explanation string // global-ok: populated at compile time by //go:embed, never mutated

func init() {
	analyzer.Register(&analyzer.Analyzer{
		Name:        "contextcheck",
		Doc:         "context.Context must propagate from a function's parameter, not be re-created",
		Explanation: explanation,
		Run:         run,
	})
}

func run(pass *analyzer.Pass) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			switch fn := n.(type) {
			case *ast.FuncDecl:
				if fn.Body != nil && hasContextParam(pass, fn.Type) {
					checkBody(pass, file, fn.Body)
				}
			case *ast.FuncLit:
				if fn.Body != nil && hasContextParam(pass, fn.Type) {
					checkBody(pass, file, fn.Body)
				}
			}
			return true
		})
	}
}

func checkBody(pass *analyzer.Pass, file *ast.File, body *ast.BlockStmt) {
	ast.Inspect(body, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok {
			// A nested literal with its own context is a separate root.
			// One without a context is still this function's body: Background
			// there drops the outer ctx.
			if hasContextParam(pass, lit.Type) {
				return false
			}
			return true
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || !isContextBackgroundOrTODO(pass, call) {
			return true
		}
		if pass.HasLineAnnotation(file, call.Pos(), analyzer.AnnSafeIgnore) {
			return true
		}
		pass.Report(analyzer.Issue{
			Analyzer: "contextcheck",
			Pos:      pass.Fset.Position(call.Pos()),
			Message:  "context.Background()/TODO() inside a function that already receives a context",
			Hint:     "pass the incoming ctx through; if a fresh context is required, annotate with `// safe-ignore: <reason>`",
		})
		return true
	})
}

func hasContextParam(pass *analyzer.Pass, ft *ast.FuncType) bool {
	if ft.Params == nil {
		return false
	}
	for _, f := range ft.Params.List {
		t := pass.TypesInfo.TypeOf(f.Type)
		if isContextType(t) {
			return true
		}
	}
	return false
}

func isContextType(t types.Type) bool {
	if t == nil {
		return false
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	return obj.Pkg().Path() == "context" && obj.Name() == "Context"
}

func isContextBackgroundOrTODO(pass *analyzer.Pass, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	obj := pass.TypesInfo.Uses[pkgIdent]
	pn, ok := obj.(*types.PkgName)
	if !ok || pn.Imported().Path() != "context" {
		return false
	}
	return sel.Sel.Name == "Background" || sel.Sel.Name == "TODO"
}

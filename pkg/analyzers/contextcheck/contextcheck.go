// Package contextcheck enforces context propagation.
//
// Inside a function or function literal that has a `context.Context`
// parameter, any call to `context.Background()` or `context.TODO()` is
// reported, since it discards the caller's cancellation and deadline.
// A nested literal that receives its own context is checked on its own:
// Background inside it is still reported, and it is not also blamed on the
// outer function.
//
// In the same functions it reports calls that take no context although a
// context-aware variant exists: database/sql Exec/Query/QueryRow/Prepare/
// Begin/Ping, http.NewRequest/Get/Post/Head/PostForm (package and
// *http.Client), exec.Command and net.Dial. Without the context those calls
// keep running after the caller gave up.
//
// A function with an *http.Request parameter counts as having a context
// (r.Context()). Function literals started with `go` are skipped for the
// second check: detached work must not inherit a request's cancellation.
//
// The analyzer does not track which context is passed to other calls.
// Opt out with `// safe-ignore: <reason>`.
package contextcheck

import (
	_ "embed"
	"fmt"
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
					checkNoCtxCalls(pass, file, fn.Body)
				}
			case *ast.FuncLit:
				if fn.Body != nil && hasContextParam(pass, fn.Type) {
					checkBody(pass, file, fn.Body)
					checkNoCtxCalls(pass, file, fn.Body)
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

// checkNoCtxCalls reports calls that drop the available context because a
// context-aware variant exists.
func checkNoCtxCalls(pass *analyzer.Pass, file *ast.File, body *ast.BlockStmt) {
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			return !hasContextParam(pass, x.Type)
		case *ast.GoStmt:
			if _, ok := x.Call.Fun.(*ast.FuncLit); ok {
				return false
			}
		case *ast.CallExpr:
			name, alt := noCtxVariant(pass, x)
			if alt == "" || pass.HasLineAnnotation(file, x.Pos(), analyzer.AnnSafeIgnore) {
				return true
			}
			pass.Report(analyzer.Issue{
				Analyzer: "contextcheck",
				Pos:      pass.Fset.Position(x.Pos()),
				Message:  fmt.Sprintf("%s ignores the context this function receives", name),
				Hint:     fmt.Sprintf("use %s with the incoming ctx (or r.Context()); if it must outlive the caller, annotate with `// safe-ignore: <reason>`", alt),
			})
		}
		return true
	})
}

// noCtxVariant returns the printable name of a call that has a context-aware
// variant, and that variant; alt is "" for every other call.
func noCtxVariant(pass *analyzer.Pass, call *ast.CallExpr) (name, alt string) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", ""
	}
	method := sel.Sel.Name
	if id, ok := sel.X.(*ast.Ident); ok {
		if pn, ok := pass.TypesInfo.Uses[id].(*types.PkgName); ok {
			path := pn.Imported().Path()
			switch {
			case path == "net/http" && method == "NewRequest":
				return "http.NewRequest", "http.NewRequestWithContext"
			case path == "net/http" && (method == "Get" || method == "Post" || method == "Head" || method == "PostForm"):
				return "http." + method, "http.NewRequestWithContext + client.Do"
			case path == "os/exec" && method == "Command":
				return "exec.Command", "exec.CommandContext"
			case path == "net" && (method == "Dial" || method == "DialTimeout"):
				return "net." + method, "(&net.Dialer{}).DialContext"
			}
			return "", ""
		}
	}
	if pass.TypesInfo.Selections[sel] == nil {
		return "", ""
	}
	t := pass.TypesInfo.TypeOf(sel.X)
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return "", ""
	}
	recv := named.Obj().Name()
	switch named.Obj().Pkg().Path() {
	case "database/sql":
		switch recv {
		case "DB", "Tx", "Conn", "Stmt":
		default:
			return "", ""
		}
		// Only the methods that have a ...Context twin; the compiler already
		// rules out the ones a receiver does not have.
		switch method {
		case "Exec", "Query", "QueryRow", "Prepare", "Ping", "Begin":
			return recv + "." + method, method + "Context"
		}
	case "net/http":
		if recv == "Client" && (method == "Get" || method == "Post" || method == "Head" || method == "PostForm") {
			return "Client." + method, "http.NewRequestWithContext + client.Do"
		}
	case "net":
		if recv == "Dialer" && method == "Dial" {
			return "Dialer.Dial", "Dialer.DialContext"
		}
	}
	return "", ""
}

func hasContextParam(pass *analyzer.Pass, ft *ast.FuncType) bool {
	if ft.Params == nil {
		return false
	}
	for _, f := range ft.Params.List {
		t := pass.TypesInfo.TypeOf(f.Type)
		if isContextType(t) || isHTTPRequestPtr(t) {
			return true
		}
	}
	return false
}

// isHTTPRequestPtr reports whether t is *http.Request, which carries the
// request context in r.Context().
func isHTTPRequestPtr(t types.Type) bool {
	ptr, ok := t.(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := ptr.Elem().(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Pkg().Path() == "net/http" && named.Obj().Name() == "Request"
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

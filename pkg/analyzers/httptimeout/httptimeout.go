// Package httptimeout reports HTTP calls and clients that have no timeout
// configured.
//
// Bare calls that go through http.DefaultClient (http.Get, http.Post,
// http.Head, http.PostForm, and any method on http.DefaultClient itself)
// inherit the default client's zero-value Timeout, which is "no timeout" —
// a hanging server will block the goroutine forever. The exception is
// `http.DefaultClient.Do(req)` when `req` is assigned from
// `http.NewRequestWithContext` in the same function: the deadline lives on
// the request, not the client.
//
// Likewise, an *http.Client constructed without an explicit Timeout field
// (or with an explicit zero value) will hang on the same scenario — including
// `var c http.Client` and `new(http.Client)`, which are the zero value.
// The idiomatic fix is either:
//
//	client := &http.Client{Timeout: 30 * time.Second}
//
// or, when the caller already controls a context:
//
//	req, err := http.NewRequestWithContext(ctx, ...)
//	resp, err := client.Do(req)
//
// `http.Server` literals missing ReadHeaderTimeout or WriteTimeout (or
// setting either to the constant 0) are also reported — those are the
// Slowloris / hung-write knobs.
//
// The analyzer does not try to follow a request's context across function
// boundaries. To accept a flagged site, annotate the same line with
// `// timeout-ok: <reason>`.
package httptimeout

import (
	_ "embed"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"github.com/mentasystems/gox/pkg/analyzer"
)

//go:embed httptimeout.md
var explanation string // global-ok: populated at compile time by //go:embed, never mutated

func init() {
	analyzer.Register(&analyzer.Analyzer{
		Name:        "httptimeout",
		Doc:         "HTTP clients, servers, and shortcut calls must set an explicit timeout",
		Explanation: explanation,
		Run:         run,
	})
}

// isShortcutFunc reports whether `name` is one of the net/http package-level
// helpers that go through http.DefaultClient (and therefore inherit no
// timeout).
func isShortcutFunc(name string) bool {
	switch name {
	case "Get", "Post", "PostForm", "Head":
		return true
	}
	return false
}

func run(pass *analyzer.Pass) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.FuncDecl:
				if v.Body != nil {
					checkFunc(pass, file, v.Body)
				}
				return false
			case *ast.FuncLit:
				if v.Body != nil {
					checkFunc(pass, file, v.Body)
				}
				return false
			case *ast.CallExpr:
				checkNewClient(pass, file, v)
				checkCall(pass, file, v, nil)
			case *ast.CompositeLit:
				checkLiteral(pass, file, v)
			case *ast.ValueSpec:
				checkVarClient(pass, file, v)
			}
			return true
		})
	}
}

func checkFunc(pass *analyzer.Pass, file *ast.File, body *ast.BlockStmt) {
	ctxReqs := contextRequestNames(pass, body)
	ast.Inspect(body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.FuncLit:
			if v.Body != nil {
				checkFunc(pass, file, v.Body)
			}
			return false
		case *ast.CallExpr:
			checkNewClient(pass, file, v)
			checkCall(pass, file, v, ctxReqs)
		case *ast.CompositeLit:
			checkLiteral(pass, file, v)
		case *ast.ValueSpec:
			checkVarClient(pass, file, v)
		}
		return true
	})
}

func contextRequestNames(pass *analyzer.Pass, body *ast.BlockStmt) map[string]bool {
	names := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Rhs) != 1 || len(as.Lhs) == 0 {
			return true
		}
		call, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok || !isNewRequestWithContext(pass, call) {
			return true
		}
		if id, ok := as.Lhs[0].(*ast.Ident); ok && id.Name != "_" {
			names[id.Name] = true
		}
		return true
	})
	return names
}

func checkCall(pass *analyzer.Pass, file *ast.File, call *ast.CallExpr, ctxReqs map[string]bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}

	// `http.Get(...)` / `http.Post(...)` / `http.Head(...)` / `http.PostForm(...)`.
	if id, ok := sel.X.(*ast.Ident); ok && isShortcutFunc(sel.Sel.Name) {
		if isNetHTTPPackage(pass, id) {
			report(pass, file, call.Pos(),
				/* msg */ "http."+sel.Sel.Name+" uses http.DefaultClient which has no timeout",
				/* hint */ "build an explicit *http.Client with a Timeout, or use http.NewRequestWithContext")
			return
		}
	}

	// `http.DefaultClient.<Method>(...)` — any method on the default client.
	if inner, ok := sel.X.(*ast.SelectorExpr); ok && inner.Sel.Name == "DefaultClient" {
		if pkgID, ok := inner.X.(*ast.Ident); ok && isNetHTTPPackage(pass, pkgID) {
			if sel.Sel.Name == "Do" && requestHasContext(pass, call, ctxReqs) {
				return
			}
			report(pass, file, call.Pos(),
				/* msg */ "http.DefaultClient."+sel.Sel.Name+" has no timeout",
				/* hint */ "build an explicit *http.Client with a Timeout, or use http.NewRequestWithContext")
			return
		}
	}
}

func requestHasContext(pass *analyzer.Pass, call *ast.CallExpr, ctxReqs map[string]bool) bool {
	if len(call.Args) == 0 {
		return false
	}
	arg := call.Args[0]
	if id, ok := arg.(*ast.Ident); ok && ctxReqs[id.Name] {
		return true
	}
	if inner, ok := arg.(*ast.CallExpr); ok && isNewRequestWithContext(pass, inner) {
		return true
	}
	return false
}

func isNewRequestWithContext(pass *analyzer.Pass, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "NewRequestWithContext" {
		return false
	}
	pkgID, ok := sel.X.(*ast.Ident)
	return ok && isNetHTTPPackage(pass, pkgID)
}

func checkLiteral(pass *analyzer.Pass, file *ast.File, lit *ast.CompositeLit) {
	switch {
	case isHTTPClientType(pass, lit.Type):
		checkClientLiteral(pass, file, lit)
	case isHTTPServerType(pass, lit.Type):
		checkServerLiteral(pass, file, lit)
	}
}

func checkClientLiteral(pass *analyzer.Pass, file *ast.File, lit *ast.CompositeLit) {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		k, ok := kv.Key.(*ast.Ident)
		if !ok || k.Name != "Timeout" {
			continue
		}
		// Timeout field is present — check that it isn't a constant zero.
		if isConstZero(pass, kv.Value) {
			report(pass, file, lit.Pos(),
				/* msg */ "http.Client Timeout is set to 0 (no timeout)",
				/* hint */ "use a positive time.Duration, e.g. Timeout: 30 * time.Second")
			return
		}
		// Either a non-zero constant or a non-constant expression — trust the
		// author. The point of this rule is to force a deliberate choice.
		return
	}
	// No Timeout field at all.
	report(pass, file, lit.Pos(),
		/* msg */ "http.Client constructed without an explicit Timeout",
		/* hint */ "add Timeout: 30 * time.Second (or set it from configuration)")
}

func checkServerLiteral(pass *analyzer.Pass, file *ast.File, lit *ast.CompositeLit) {
	hasReadHeader, hasWrite := false, false
	readHeaderZero, writeZero := false, false
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		k, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch k.Name {
		case "ReadHeaderTimeout":
			hasReadHeader = true
			readHeaderZero = isConstZero(pass, kv.Value)
		case "WriteTimeout":
			hasWrite = true
			writeZero = isConstZero(pass, kv.Value)
		}
	}
	var missing []string
	if !hasReadHeader || readHeaderZero {
		missing = append(missing, "ReadHeaderTimeout")
	}
	if !hasWrite || writeZero {
		missing = append(missing, "WriteTimeout")
	}
	if len(missing) == 0 {
		return
	}
	msg := "http.Server constructed without " + joinAnd(missing)
	if readHeaderZero || writeZero {
		msg = "http.Server " + joinAnd(missing) + " is set to 0 (no timeout)"
	}
	report(pass, file, lit.Pos(),
		/* msg */ msg,
		/* hint */ "set ReadHeaderTimeout and WriteTimeout to a positive time.Duration")
}

func checkVarClient(pass *analyzer.Pass, file *ast.File, vs *ast.ValueSpec) {
	if vs.Type == nil || len(vs.Values) != 0 {
		return
	}
	if !isHTTPClientType(pass, vs.Type) {
		return
	}
	for _, name := range vs.Names {
		if name.Name == "_" {
			continue
		}
		report(pass, file, name.Pos(),
			/* msg */ "http.Client variable has zero Timeout",
			/* hint */ "use &http.Client{Timeout: ...} or annotate with `// timeout-ok: <reason>`")
	}
}

func checkNewClient(pass *analyzer.Pass, file *ast.File, call *ast.CallExpr) {
	id, ok := call.Fun.(*ast.Ident)
	if !ok || id.Name != "new" || len(call.Args) != 1 {
		return
	}
	if obj := pass.TypesInfo.Uses[id]; obj != nil {
		if _, isBuiltin := obj.(*types.Builtin); !isBuiltin {
			return
		}
	}
	if !isHTTPClientType(pass, call.Args[0]) {
		return
	}
	report(pass, file, call.Pos(),
		/* msg */ "new(http.Client) has zero Timeout",
		/* hint */ "use &http.Client{Timeout: ...} or annotate with `// timeout-ok: <reason>`")
}

func isConstZero(pass *analyzer.Pass, expr ast.Expr) bool {
	tav, ok := pass.TypesInfo.Types[expr]
	if !ok || tav.Value == nil {
		return false
	}
	return constant.Sign(tav.Value) == 0
}

func joinAnd(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	}
	return parts[0] + ", " + joinAnd(parts[1:])
}

func report(pass *analyzer.Pass, file *ast.File, pos token.Pos, msg, hint string) {
	if analyzer.HasLineAnnotation(pass.Fset, file, pos, analyzer.AnnTimeoutOK) {
		return
	}
	pass.Report(analyzer.Issue{
		Analyzer: "httptimeout",
		Pos:      pass.Fset.Position(pos),
		Message:  msg,
		Hint:     hint,
	})
}

// isNetHTTPPackage reports whether `id` resolves to the imported `net/http`
// package qualifier.
func isNetHTTPPackage(pass *analyzer.Pass, id *ast.Ident) bool {
	obj := pass.TypesInfo.Uses[id]
	if obj == nil {
		return false
	}
	pkgName, ok := obj.(*types.PkgName)
	if !ok {
		return false
	}
	return pkgName.Imported().Path() == "net/http"
}

func isHTTPNamedType(pass *analyzer.Pass, expr ast.Expr, name string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkgID, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return sel.Sel.Name == name && isNetHTTPPackage(pass, pkgID)
}

// isHTTPClientType reports whether `expr` is the AST form `http.Client`
// (which is how a composite literal's type appears for both `http.Client{}`
// and `&http.Client{}`).
func isHTTPClientType(pass *analyzer.Pass, expr ast.Expr) bool {
	return isHTTPNamedType(pass, expr, "Client")
}

func isHTTPServerType(pass *analyzer.Pass, expr ast.Expr) bool {
	return isHTTPNamedType(pass, expr, "Server")
}

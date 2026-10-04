// Package httptimeout reports HTTP calls and clients that have no timeout
// configured.
//
// Bare calls that go through http.DefaultClient (http.Get, http.Post,
// http.Head, http.PostForm, and any method on http.DefaultClient itself)
// inherit the default client's zero-value Timeout, which is "no timeout" —
// a hanging server will block the goroutine forever.
//
// Likewise, an *http.Client constructed without an explicit Timeout field
// (or with an explicit zero value) will hang on the same scenario. The
// idiomatic fix is either:
//
//	client := &http.Client{Timeout: 30 * time.Second}
//
// or, when the caller already controls a context:
//
//	req, _ := http.NewRequestWithContext(ctx, ...)
//	resp, _ := client.Do(req)
//
// The analyzer does not try to follow a request's context across function
// boundaries, so calls on existing *http.Client variables (other than
// http.DefaultClient) are not flagged — the literal where the client was
// constructed is the gate. To accept a flagged site, annotate the same
// line with `// timeout-ok: <reason>`.
package httptimeout

import (
	_ "embed"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"github.com/kidandcat/gox/pkg/analyzer"
)

//go:embed httptimeout.md
var explanation string // global-ok: populated at compile time by //go:embed, never mutated

func init() {
	analyzer.Register(&analyzer.Analyzer{
		Name:        "httptimeout",
		Doc:         "HTTP clients and shortcut calls must set an explicit Timeout",
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
			case *ast.CallExpr:
				checkCall(pass, file, v)
			case *ast.CompositeLit:
				checkLiteral(pass, file, v)
			}
			return true
		})
	}
}

func checkCall(pass *analyzer.Pass, file *ast.File, call *ast.CallExpr) {
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
			report(pass, file, call.Pos(),
				/* msg */ "http.DefaultClient."+sel.Sel.Name+" has no timeout",
				/* hint */ "build an explicit *http.Client with a Timeout, or use http.NewRequestWithContext")
			return
		}
	}
}

func checkLiteral(pass *analyzer.Pass, file *ast.File, lit *ast.CompositeLit) {
	if !isHTTPClientType(pass, lit.Type) {
		return
	}
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
		if tav, ok := pass.TypesInfo.Types[kv.Value]; ok && tav.Value != nil {
			if constant.Sign(tav.Value) == 0 {
				report(pass, file, lit.Pos(),
					/* msg */ "http.Client Timeout is set to 0 (no timeout)",
					/* hint */ "use a positive time.Duration, e.g. Timeout: 30 * time.Second")
				return
			}
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

func report(pass *analyzer.Pass, file *ast.File, pos token.Pos, msg, hint string) {
	if hasAnnOnLine(pass.Fset, file, pos, analyzer.AnnTimeoutOK) {
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

// isHTTPClientType reports whether `expr` is the AST form `http.Client`
// (which is how a composite literal's type appears for both `http.Client{}`
// and `&http.Client{}`).
func isHTTPClientType(pass *analyzer.Pass, expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkgID, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return sel.Sel.Name == "Client" && isNetHTTPPackage(pass, pkgID)
}

// hasAnnOnLine reports whether any comment on the same source line as `pos`
// carries the given annotation prefix with a non-empty reason. Matches the
// helper in the goroutine analyzer so multi-line composite literals can be
// annotated on the opening-brace line.
func hasAnnOnLine(fset *token.FileSet, file *ast.File, pos token.Pos, prefix string) bool {
	line := fset.Position(pos).Line
	for _, cg := range file.Comments {
		if fset.Position(cg.Pos()).Line != line {
			continue
		}
		for _, c := range cg.List {
			text := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
			if !strings.HasPrefix(text, prefix) {
				continue
			}
			rest := strings.TrimSpace(text[len(prefix):])
			if rest != "" {
				return true
			}
		}
	}
	return false
}

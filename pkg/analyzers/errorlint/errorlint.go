// Package errorlint reports incorrect handling of error values.
//
// Three patterns are caught:
//
//  1. `err == X` / `err != X` where both sides are error-typed and X is
//     not the untyped `nil`. These compile but break the moment any caller
//     wraps the underlying error with `fmt.Errorf("...: %w", err)`. The
//     fix is `errors.Is(err, X)`.
//
//  2. `v := err.(*MyError)` — a type assertion on an error-typed
//     expression. Same wrapping problem; the fix is `errors.As(err, &v)`.
//
//  3. `fmt.Errorf("...: %s", err)` / `fmt.Errorf("...: %v", err)`. The
//     resulting error is opaque — `errors.Is` / `errors.As` can no longer
//     reach the inner cause. The fix is `%w` instead of `%s` / `%v`.
//
// Annotate intentional cases with `// safe-ignore: <reason>` on the same
// line (e.g. when comparing against a sentinel that is documented never to
// be wrapped).
package errorlint

import (
	_ "embed"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/mentasystems/gox/pkg/analyzer"
)

//go:embed errorlint.md
var explanation string // global-ok: populated at compile time by //go:embed, never mutated

func init() {
	analyzer.Register(&analyzer.Analyzer{
		Name:        "errorlint",
		Doc:         "errors.Is/errors.As/%w instead of ==/type-assert/%s on errors",
		Explanation: explanation,
		Run:         run,
	})
}

// errorInterface is the builtin `error` interface, resolved once at startup.
// global-ok: read-only reference to a stdlib singleton; populated in init().
var errorInterface *types.Interface

func init() {
	obj := types.Universe.Lookup("error")
	if obj == nil {
		return
	}
	if iface, ok := obj.Type().Underlying().(*types.Interface); ok {
		errorInterface = iface
	}
}

func run(pass *analyzer.Pass) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.BinaryExpr:
				checkComparison(pass, file, x)
			case *ast.TypeAssertExpr:
				checkAssert(pass, file, x)
			case *ast.CallExpr:
				checkErrorf(pass, file, x)
			}
			return true
		})
	}
}

func checkComparison(pass *analyzer.Pass, file *ast.File, b *ast.BinaryExpr) {
	if b.Op != token.EQL && b.Op != token.NEQ {
		return
	}
	if isNilLiteral(b.X) || isNilLiteral(b.Y) {
		return
	}
	tx := pass.TypesInfo.TypeOf(b.X)
	ty := pass.TypesInfo.TypeOf(b.Y)
	if !implementsError(tx) || !implementsError(ty) {
		return
	}
	if hasSafeIgnoreOnLine(pass.Fset, file, b.Pos()) {
		return
	}
	pass.Report(analyzer.Issue{
		Analyzer: "errorlint",
		Pos:      pass.Fset.Position(b.OpPos),
		Message:  "comparison of error values with == / != breaks under wrapping",
		Hint:     "use `errors.Is(err, target)`; if comparing against a never-wrapped sentinel, annotate with `// safe-ignore: <reason>`",
	})
}

func checkAssert(pass *analyzer.Pass, file *ast.File, ta *ast.TypeAssertExpr) {
	if ta.Type == nil {
		return // type switch guard, handled by forcetypeassert
	}
	tx := pass.TypesInfo.TypeOf(ta.X)
	if !implementsError(tx) {
		return
	}
	// Exempt error-implementing concrete types: `e := wrappedErr.(SomeInterface)`
	// where SomeInterface is exactly `error` would be silly, but a more
	// specific error interface is occasionally legitimate. We still flag
	// because errors.As is the right tool either way.
	if hasSafeIgnoreOnLine(pass.Fset, file, ta.Pos()) {
		return
	}
	pass.Report(analyzer.Issue{
		Analyzer: "errorlint",
		Pos:      pass.Fset.Position(ta.Pos()),
		Message:  "type assertion on an error value breaks under wrapping",
		Hint:     "use `var target *MyError; if errors.As(err, &target) { ... }`",
	})
}

func checkErrorf(pass *analyzer.Pass, file *ast.File, call *ast.CallExpr) {
	if !isFmtErrorf(pass, call) {
		return
	}
	if len(call.Args) < 2 {
		return
	}
	// Format string is the first arg, must be a string literal we can read.
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return
	}
	format, ok := unquoteString(lit.Value)
	if !ok {
		return
	}
	verbs := parseVerbs(format)
	for i, v := range verbs {
		argIdx := i + 1 // call.Args[0] is the format string
		if argIdx >= len(call.Args) {
			break
		}
		if v != 's' && v != 'v' {
			continue
		}
		argType := pass.TypesInfo.TypeOf(call.Args[argIdx])
		if !implementsError(argType) {
			continue
		}
		if hasSafeIgnoreOnLine(pass.Fset, file, call.Pos()) {
			return
		}
		pass.Report(analyzer.Issue{
			Analyzer: "errorlint",
			Pos:      pass.Fset.Position(call.Args[argIdx].Pos()),
			Message:  fmt.Sprintf("error formatted with %%%c — the cause is lost for errors.Is/As", v),
			Hint:     "use `%w` to wrap, so `errors.Is`/`errors.As` can still reach the inner error",
		})
		return
	}
}

func implementsError(t types.Type) bool {
	if t == nil || errorInterface == nil {
		return false
	}
	// `nil` literal in some positions reports as untyped — ignore.
	if _, isBasic := t.(*types.Basic); isBasic {
		return false
	}
	return types.Implements(t, errorInterface)
}

func isNilLiteral(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "nil"
}

func isFmtErrorf(pass *analyzer.Pass, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Errorf" {
		return false
	}
	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	obj := pass.TypesInfo.Uses[pkgIdent]
	if obj == nil {
		return false
	}
	pn, ok := obj.(*types.PkgName)
	if !ok {
		return false
	}
	return pn.Imported().Path() == "fmt"
}

// parseVerbs returns the verb character (e.g. 's', 'v', 'd', 'w') for each
// fmt verb in `s`, in order of appearance. Flags and widths (%5.2f, %+v)
// are skipped. Literal `%%` is skipped. Verbs we can't parse default to 0.
func parseVerbs(s string) []byte {
	out := make([]byte, 0, 4)
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			continue
		}
		i++
		if i >= len(s) {
			break
		}
		if s[i] == '%' {
			continue // literal %%
		}
		// skip flags
		for i < len(s) && isFlag(s[i]) {
			i++
		}
		// skip width
		for i < len(s) && (isDigit(s[i]) || s[i] == '*') {
			i++
		}
		// skip precision
		if i < len(s) && s[i] == '.' {
			i++
			for i < len(s) && (isDigit(s[i]) || s[i] == '*') {
				i++
			}
		}
		if i >= len(s) {
			break
		}
		out = append(out, s[i])
	}
	return out
}

func isFlag(c byte) bool {
	return c == '+' || c == '-' || c == '#' || c == ' ' || c == '0'
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// unquoteString removes the surrounding quotes of a string literal token.
// Returns (value, true) for `"..."` and “...“; (..., false) otherwise.
func unquoteString(lit string) (string, bool) {
	if len(lit) < 2 {
		return "", false
	}
	first, last := lit[0], lit[len(lit)-1]
	if first == '"' && last == '"' {
		// We don't process escapes — for our purposes %s/%v are not escaped.
		return lit[1 : len(lit)-1], true
	}
	if first == '`' && last == '`' {
		return lit[1 : len(lit)-1], true
	}
	return "", false
}

func hasSafeIgnoreOnLine(fset *token.FileSet, file *ast.File, pos token.Pos) bool {
	return analyzer.HasLineAnnotation(fset, file, pos, analyzer.AnnSafeIgnore)
}

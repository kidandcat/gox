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
	"strconv"

	"github.com/kidandcat/gox/pkg/analyzer"
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
	if pass.HasLineAnnotation(file, b.Pos(), analyzer.AnnSafeIgnore) {
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
	if pass.HasLineAnnotation(file, ta.Pos(), analyzer.AnnSafeIgnore) {
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
	for _, v := range errorVerbs(format) {
		argIdx := v.index + 1 // call.Args[0] is the format string
		if argIdx < 1 || argIdx >= len(call.Args) {
			continue
		}
		arg := call.Args[argIdx]
		if !implementsError(pass.TypesInfo.TypeOf(arg)) {
			continue
		}
		if pass.HasLineAnnotation(file, call.Pos(), analyzer.AnnSafeIgnore) {
			return
		}
		pass.Report(analyzer.Issue{
			Analyzer: "errorlint",
			Pos:      pass.Fset.Position(arg.Pos()),
			Message:  fmt.Sprintf("error formatted with %%%c — the cause is lost for errors.Is/As", v.verb),
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

// fmtVerb is one `%s` or `%v` in a format string. index is the 0-based
// operand it consumes (the format string itself is not an operand).
type fmtVerb struct {
	verb  byte
	index int
}

// errorVerbs returns every `%s` and `%v` in format, with the operand index
// fmt would pass to that verb. Explicit indexes (`%[2]v`) and `*` width or
// precision (each consumes an operand) follow the same rules as fmt.Printf.
// `%w` is not returned: wrapping keeps the cause.
func errorVerbs(format string) []fmtVerb {
	var out []fmtVerb
	argNum := 0
	for i := 0; i < len(format); {
		if format[i] != '%' {
			i++
			continue
		}
		i++
		if i >= len(format) {
			break
		}
		if format[i] == '%' {
			i++
			continue
		}
		afterIndex := false
		for i < len(format) && isFlag(format[i]) {
			i++
		}
		argNum, i, afterIndex = parseIndex(format, i, argNum)

		if i < len(format) && format[i] == '*' {
			i++
			argNum++
			afterIndex = false
		} else {
			i = skipNum(format, i)
		}

		if i < len(format) && format[i] == '.' {
			i++
			argNum, i, afterIndex = parseIndex(format, i, argNum)
			if i < len(format) && format[i] == '*' {
				i++
				argNum++
				afterIndex = false
			} else {
				i = skipNum(format, i)
			}
		}

		if !afterIndex {
			// The verb's own index is consumed here. Nothing after the verb
			// reads the flag, so the bool is discarded.
			argNum, i, _ = parseIndex(format, i, argNum)
		}
		if i >= len(format) {
			break
		}
		verb := format[i]
		i++
		if verb == '%' {
			continue
		}
		used := argNum
		argNum++
		if verb == 's' || verb == 'v' {
			out = append(out, fmtVerb{verb: verb, index: used})
		}
	}
	return out
}

// parseIndex consumes a `[n]` argument index. n is 1-based; the returned
// argNum is 0-based. found is false when format[i] is not '['.
func parseIndex(format string, i, argNum int) (int, int, bool) {
	if i >= len(format) || format[i] != '[' {
		return argNum, i, false
	}
	j := i + 1
	for j < len(format) && format[j] != ']' {
		j++
	}
	if j >= len(format) {
		return argNum, i + 1, false
	}
	n := 0
	ok := j > i+1
	for k := i + 1; k < j; k++ {
		if format[k] < '0' || format[k] > '9' {
			ok = false
			break
		}
		n = n*10 + int(format[k]-'0')
	}
	if !ok || n <= 0 {
		return argNum, j + 1, false
	}
	return n - 1, j + 1, true
}

func skipNum(s string, i int) int {
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return i
}

func isFlag(c byte) bool {
	return c == '+' || c == '-' || c == '#' || c == ' ' || c == '0'
}

// unquoteString returns the string value of a Go string literal token.
func unquoteString(lit string) (string, bool) {
	if len(lit) < 2 {
		return "", false
	}
	s, err := strconv.Unquote(lit)
	if err != nil {
		return "", false
	}
	return s, true
}

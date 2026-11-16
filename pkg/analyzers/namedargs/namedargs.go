// Package namedargs reports call sites whose arguments look swapped.
//
// `transfer(toID, fromID)` against `func transfer(fromID, toID string)`
// compiles and means the opposite of what the names say. The rule reports an
// argument when its name says it belongs to a different parameter:
//
//   - the argument is an identifier or field (`toID`, `req.ToID`) whose name
//     equals, ignoring case, the name of another parameter of the callee and
//     not the name of its own parameter;
//   - the argument carries a `/* name */` label that names another parameter.
//
// The value must also be assignable to that other parameter, otherwise the
// swap would not compile and there is nothing to report. One-letter names
// are ignored: `less(j, i)` is a deliberate reversal far more often than a
// bug.
//
// Earlier versions required a `/* name */` label on every argument of a
// same-typed parameter run. That produced thousands of labels, fought with
// gofmt, and in practice never caught a swap; labels that already exist are
// still checked.
//
// Opt out with `// safe-ignore: <reason>` on the argument or call line.
package namedargs

import (
	_ "embed"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"github.com/kidandcat/gox/pkg/analyzer"
)

//go:embed namedargs.md
var explanation string // global-ok: populated at compile time by //go:embed, never mutated

func init() {
	analyzer.Register(&analyzer.Analyzer{
		Name:        "namedargs",
		Doc:         "reports arguments whose name or /* label */ matches a different parameter (likely swapped)",
		Explanation: explanation,
		Run:         run,
	})
}

func run(pass *analyzer.Pass) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				checkCall(pass, file, call)
			}
			return true
		})
	}
}

func checkCall(pass *analyzer.Pass, file *ast.File, call *ast.CallExpr) {
	sig := calleeSignature(pass, call)
	if sig == nil {
		return
	}
	params := sig.Params()
	n := params.Len()
	if sig.Variadic() {
		n-- // the variadic tail has one name for many arguments
	}
	if n < 2 || len(call.Args) < n {
		return
	}
	pnames := make([]string, n)
	byName := make(map[string]int, n)
	for i := 0; i < n; i++ {
		name := params.At(i).Name()
		if name == "" || name == "_" {
			continue
		}
		pnames[i] = strings.ToLower(name)
		byName[pnames[i]] = i
	}
	if len(byName) < 2 {
		return
	}
	anames := make([]string, n)
	labels := make([]string, n)
	prevEnd := call.Lparen
	for i := 0; i < n; i++ {
		anames[i] = strings.ToLower(argName(call.Args[i]))
		labels[i] = strings.ToLower(leadingArgComment(pass.Fset, file, call.Args[i].Pos(), prevEnd))
		prevEnd = call.Args[i].End()
	}
	assignable := func(i, j int) bool {
		t := pass.TypesInfo.TypeOf(call.Args[i])
		return t != nil && types.AssignableTo(t, params.At(j).Type())
	}
	other := func(key string, i int) (int, bool) {
		if len(key) < 2 || key == pnames[i] {
			return 0, false
		}
		j, ok := byName[key]
		return j, ok && j != i
	}

	for i := 0; i < n; i++ {
		arg := call.Args[i]
		var what string
		j := -1
		// A label is an explicit claim about this argument: one that names
		// another parameter is wrong on its own.
		if lk, ok := other(labels[i], i); ok && assignable(i, lk) {
			what, j = fmt.Sprintf("label /* %s */", labels[i]), lk
		} else if nk, ok := other(anames[i], i); ok && anames[nk] == pnames[i] && assignable(i, nk) {
			// A name alone matches another parameter by chance too often
			// (`role` passed as `primary` next to a `role` parameter). Report
			// only a crossed pair: this argument is named after parameter k
			// and argument k is named after this parameter.
			what, j = "argument "+argName(arg), nk
		}
		if j < 0 {
			continue
		}
		if pass.HasLineAnnotation(file, arg.Pos(), analyzer.AnnSafeIgnore) ||
			pass.HasLineAnnotation(file, call.End(), analyzer.AnnSafeIgnore) {
			continue
		}
		pass.Report(analyzer.Issue{
			Analyzer: "namedargs",
			Pos:      pass.Fset.Position(arg.Pos()),
			Message:  fmt.Sprintf("%s is passed as parameter %q, but the callee also has a parameter %q; arguments look swapped", what, params.At(i).Name(), params.At(j).Name()),
			Hint:     "check the argument order against the signature; if the order is intended, add `// safe-ignore: <reason>`",
		})
	}
}

// argName returns the name an argument carries in source: an identifier, or
// the last selector of a field or method value (`req.ToID` -> "ToID").
func argName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return x.Sel.Name
	case *ast.StarExpr:
		return argName(x.X)
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return argName(x.X)
		}
	case *ast.ParenExpr:
		return argName(x.X)
	}
	return ""
}

// calleeSignature returns the *types.Signature for the callee of a CallExpr,
// or nil for type conversions and built-in calls.
func calleeSignature(pass *analyzer.Pass, call *ast.CallExpr) *types.Signature {
	tv, ok := pass.TypesInfo.Types[call.Fun]
	if !ok || tv.IsType() || tv.IsBuiltin() {
		return nil
	}
	sig, ok := tv.Type.(*types.Signature)
	if !ok {
		return nil
	}
	return sig
}

// leadingArgComment returns the trimmed text of the /* ... */ comment that
// ends right before pos on the same line and after the call's open paren,
// or "".
func leadingArgComment(fset *token.FileSet, file *ast.File, pos, lparen token.Pos) string {
	line := fset.Position(pos).Line
	var best *ast.Comment
	for _, cg := range file.Comments {
		if cg.End() > pos || cg.Pos() < lparen {
			continue
		}
		if fset.Position(cg.End()).Line != line {
			continue
		}
		c := cg.List[len(cg.List)-1]
		if best == nil || c.End() > best.End() {
			best = c
		}
	}
	if best == nil || !strings.HasPrefix(best.Text, "/*") || !strings.HasSuffix(best.Text, "*/") {
		return ""
	}
	return strings.TrimSpace(best.Text[2 : len(best.Text)-2])
}

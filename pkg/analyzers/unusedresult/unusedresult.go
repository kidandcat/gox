// Package unusedresult reports calls to side-effect-free functions whose
// result is thrown away.
//
//	fmt.Sprintf("user %s", id)      // meant fmt.Printf, or forgot `msg :=`
//	errors.New("not found")         // meant `return errors.New(...)`
//	strings.TrimSpace(s)            // strings are immutable; s is unchanged
//	context.WithValue(ctx, k, v)    // ctx is unchanged
//
// The list covers fmt.Sprint*, fmt.Errorf, errors.New/Join/Unwrap/Is,
// every package-level function of strings and bytes, and the context.With*
// constructors. This is go vet's
// unusedresult plus staticcheck's SA4017 for that list.
package unusedresult

import (
	_ "embed"
	"go/ast"
	"go/types"

	"github.com/kidandcat/gox/pkg/analyzer"
)

//go:embed unusedresult.md
var explanation string // global-ok: populated at compile time by //go:embed, never mutated

func init() {
	analyzer.Register(&analyzer.Analyzer{
		Name:        "unusedresult",
		Doc:         "result of a side-effect-free call (fmt.Sprintf, errors.New, strings.*, context.With*) is discarded",
		Explanation: explanation,
		Run:         run,
	})
}

func run(pass *analyzer.Pass) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			es, ok := n.(*ast.ExprStmt)
			if !ok {
				return true
			}
			call, ok := ast.Unparen(es.X).(*ast.CallExpr)
			if !ok {
				return true
			}
			name := pureCall(pass, call)
			if name == "" || pass.HasLineAnnotation(file, call.Pos(), analyzer.AnnSafeIgnore) {
				return true
			}
			pass.Report(analyzer.Issue{
				Analyzer: "unusedresult",
				Pos:      pass.Fset.Position(call.Pos()),
				Message:  "result of " + name + " is discarded; the call has no other effect",
				Hint:     "use the returned value, or delete the call",
			})
			return true
		})
	}
}

// pureCall returns "pkg.Func" when call is a package-level function on the
// list, or "".
func pureCall(pass *analyzer.Pass, call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	pn, ok := pass.TypesInfo.Uses[id].(*types.PkgName)
	if !ok {
		return ""
	}
	fn := sel.Sel.Name
	full := pn.Imported().Name() + "." + fn
	switch pn.Imported().Path() {
	case "fmt":
		switch fn {
		case "Sprint", "Sprintf", "Sprintln", "Errorf", "Append", "Appendf", "Appendln":
			return full
		}
	case "errors":
		switch fn {
		case "New", "Join", "Unwrap", "Is":
			return full
		}
	case "context":
		switch fn {
		case "WithValue", "WithCancel", "WithTimeout", "WithDeadline", "WithCancelCause",
			"WithTimeoutCause", "WithDeadlineCause", "WithoutCancel":
			return full
		}
	case "strings", "bytes":
		// Every package-level function of both packages only returns a
		// value; none writes into its arguments.
		return full
	}
	return ""
}

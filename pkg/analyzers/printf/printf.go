// Package printf checks constant format strings of the fmt, log and testing
// printf-style functions against their arguments.
//
//	fmt.Printf("%s: %d\n", name)         // missing argument
//	fmt.Sprintf("user %s", id, extra)    // extra argument
//	log.Printf("failed: %w", err)        // %w only works in fmt.Errorf
//
// It counts verbs (a `*` width or precision takes an argument too) and
// compares them with the arguments. Formats that use explicit indexes
// (`%[2]d`) or a spread argument (`args...`) are skipped. Argument types are
// not checked; that is go vet's printf, which also runs in `go test`.
package printf

import (
	_ "embed"
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"strings"

	"github.com/kidandcat/gox/pkg/analyzer"
)

//go:embed printf.md
var explanation string // global-ok: populated at compile time by //go:embed, never mutated

func init() {
	analyzer.Register(&analyzer.Analyzer{
		Name:        "printf",
		Doc:         "printf-style format verbs do not match the number of arguments, or %w outside fmt.Errorf",
		Explanation: explanation,
		Run:         run,
	})
}

func run(pass *analyzer.Pass) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || call.Ellipsis.IsValid() {
				return true
			}
			name, fmtIdx, wrapOK := printfFunc(pass, call)
			if name == "" || len(call.Args) <= fmtIdx {
				return true
			}
			tv := pass.TypesInfo.Types[call.Args[fmtIdx]]
			if tv.Value == nil || tv.Value.Kind() != constant.String {
				return true
			}
			verbs, hasW, indexed := countVerbs(constant.StringVal(tv.Value))
			if indexed {
				return true
			}
			args := len(call.Args) - fmtIdx - 1
			var msg string
			switch {
			case hasW && !wrapOK:
				msg = name + " does not support %w; only fmt.Errorf wraps errors"
			case verbs != args:
				msg = fmt.Sprintf("%s format has %d verb(s) but %d argument(s)", name, verbs, args)
			default:
				return true
			}
			if pass.HasLineAnnotation(file, call.Pos(), analyzer.AnnSafeIgnore) {
				return true
			}
			pass.Report(analyzer.Issue{
				Analyzer: "printf",
				Pos:      pass.Fset.Position(call.Pos()),
				Message:  msg,
				Hint:     "make every verb match one argument; use %v for errors outside fmt.Errorf",
			})
			return true
		})
	}
}

// printfFunc identifies a printf-style callee: its printable name, the index
// of the format argument, and whether %w is allowed.
func printfFunc(pass *analyzer.Pass, call *ast.CallExpr) (string, int, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", 0, false
	}
	fn := sel.Sel.Name
	if id, ok := sel.X.(*ast.Ident); ok {
		if pn, ok := pass.TypesInfo.Uses[id].(*types.PkgName); ok {
			switch pn.Imported().Path() {
			case "fmt":
				switch fn {
				case "Printf", "Sprintf":
					return "fmt." + fn, 0, false
				case "Errorf":
					return "fmt.Errorf", 0, true
				case "Fprintf", "Appendf":
					return "fmt." + fn, 1, false
				}
			case "log":
				switch fn {
				case "Printf", "Fatalf", "Panicf":
					return "log." + fn, 0, false
				}
			}
			return "", 0, false
		}
	}
	t := pass.TypesInfo.TypeOf(sel.X)
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return "", 0, false
	}
	recv := named.Obj().Pkg().Path() + "." + named.Obj().Name()
	switch recv {
	case "log.Logger":
		switch fn {
		case "Printf", "Fatalf", "Panicf":
			return "Logger." + fn, 0, false
		}
	case "testing.T", "testing.B", "testing.F", "testing.TB":
		switch fn {
		case "Errorf", "Fatalf", "Logf", "Skipf":
			return named.Obj().Name() + "." + fn, 0, false
		}
	}
	return "", 0, false
}

// countVerbs returns how many arguments a format consumes, whether it uses
// %w, and whether it uses explicit argument indexes.
func countVerbs(format string) (n int, hasW, indexed bool) {
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		i++
		// flags
		for i < len(format) && strings.IndexByte("+-# 0", format[i]) >= 0 {
			i++
		}
		if i < len(format) && format[i] == '[' {
			return n, hasW, true
		}
		// width
		if i < len(format) && format[i] == '*' {
			n++
			i++
		}
		for i < len(format) && format[i] >= '0' && format[i] <= '9' {
			i++
		}
		// precision
		if i < len(format) && format[i] == '.' {
			i++
			if i < len(format) && format[i] == '*' {
				n++
				i++
			}
			for i < len(format) && format[i] >= '0' && format[i] <= '9' {
				i++
			}
		}
		if i < len(format) && format[i] == '[' {
			return n, hasW, true
		}
		if i >= len(format) {
			break
		}
		switch format[i] {
		case '%':
		case 'w':
			hasW = true
			n++
		default:
			n++
		}
	}
	return n, hasW, false
}

// Package errcheck reports calls to functions returning `error` whose error
// value is silently dropped.
//
// Caught patterns:
//
//	foo()              // foo returns error, value discarded
//	x := foo()         // foo returns (T, error), error component discarded
//	_ = foo()          // explicit blank without // safe-ignore: annotation
//
// To intentionally ignore an error, write the line as:
//
//	_ = foo() // safe-ignore: reason here
package errcheck

import (
	_ "embed"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/kidandcat/gox/internal/astutil"
	"github.com/kidandcat/gox/pkg/analyzer"
)

//go:embed errcheck.md
var explanation string // global-ok: populated at compile time by //go:embed, never mutated

func init() {
	analyzer.Register(&analyzer.Analyzer{
		Name:        "errcheck",
		Doc:         "reports calls to error-returning functions whose error is dropped",
		Explanation: explanation,
		Run:         run,
	})
}

func run(pass *analyzer.Pass) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			switch stmt := n.(type) {
			case *ast.ExprStmt:
				if call, ok := stmt.X.(*ast.CallExpr); ok {
					checkBareCall(pass, file, call)
				}
			case *ast.AssignStmt:
				checkAssign(pass, file, stmt)
			}
			return true
		})
	}
}

// checkBareCall handles `someFunc()` as a statement. If any return value is
// `error`, that value is being dropped on the floor.
func checkBareCall(pass *analyzer.Pass, file *ast.File, call *ast.CallExpr) {
	tv, ok := pass.TypesInfo.Types[call]
	if !ok || tv.Type == nil {
		return
	}
	if !returnsError(tv.Type) {
		return
	}
	if isBuiltinAllowedToDropErr(pass, call) {
		return
	}
	// Allow when the trailing comment contains a safe-ignore annotation.
	if tc := astutil.TrailingComment(pass.Fset, file, call.End()); tc != nil {
		if analyzer.HasAnnotation(tc, analyzer.AnnSafeIgnore) {
			return
		}
	}
	name := callee(pass, call)
	pass.Report(analyzer.Issue{
		Analyzer: "errcheck",
		Pos:      pass.Fset.Position(call.Pos()),
		Message:  fmt.Sprintf("dropped error from %s", name),
		Hint:     "assign and handle: if err := " + name + "(...); err != nil { ... }; or annotate with `// safe-ignore: <reason>`",
	})
}

// checkAssign handles `_, _ = foo()` or `_, x := foo()` where one of the
// blank LHS slots corresponds to an error return value.
func checkAssign(pass *analyzer.Pass, file *ast.File, as *ast.AssignStmt) {
	if as.Tok != token.ASSIGN && as.Tok != token.DEFINE {
		return
	}
	if len(as.Rhs) != 1 {
		// like `x, y := a, b` — every RHS is its own expression; covered when each is a call statement
		return
	}
	call, ok := as.Rhs[0].(*ast.CallExpr)
	if !ok {
		return
	}
	tv, ok := pass.TypesInfo.Types[call]
	if !ok || tv.Type == nil {
		return
	}
	tup, ok := tv.Type.(*types.Tuple)
	if !ok {
		// single return value: handled if LHS is blank
		if len(as.Lhs) == 1 {
			if id, ok := as.Lhs[0].(*ast.Ident); ok && id.Name == "_" && isErrorType(tv.Type) {
				maybeReportBlankErr(pass, file, as, callee(pass, call))
			}
		}
		return
	}
	for i := 0; i < tup.Len(); i++ {
		if !isErrorType(tup.At(i).Type()) {
			continue
		}
		if i >= len(as.Lhs) {
			continue
		}
		id, ok := as.Lhs[i].(*ast.Ident)
		if !ok || id.Name != "_" {
			continue
		}
		maybeReportBlankErr(pass, file, as, callee(pass, call))
	}
}

func maybeReportBlankErr(pass *analyzer.Pass, file *ast.File, as *ast.AssignStmt, name string) {
	if tc := astutil.TrailingComment(pass.Fset, file, as.End()); tc != nil {
		if analyzer.HasAnnotation(tc, analyzer.AnnSafeIgnore) {
			return
		}
	}
	pass.Report(analyzer.Issue{
		Analyzer: "errcheck",
		Pos:      pass.Fset.Position(as.Pos()),
		Message:  fmt.Sprintf("error from %s assigned to _ without justification", name),
		Hint:     "if the drop is intentional, append `// safe-ignore: <reason>` to this line",
	})
}

func returnsError(t types.Type) bool {
	switch tt := t.(type) {
	case *types.Tuple:
		for i := 0; i < tt.Len(); i++ {
			if isErrorType(tt.At(i).Type()) {
				return true
			}
		}
	default:
		return isErrorType(t)
	}
	return false
}

func isErrorType(t types.Type) bool {
	if t == nil {
		return false
	}
	named, ok := t.(*types.Named)
	if !ok {
		// could be an alias or interface literal — check Underlying
		if iface, ok := t.Underlying().(*types.Interface); ok {
			return iface.NumMethods() == 1 && iface.Method(0).Name() == "Error"
		}
		return false
	}
	obj := named.Obj()
	return obj != nil && obj.Pkg() == nil && obj.Name() == "error"
}

// callee returns a short, printable name of the call's target.
func callee(_ *analyzer.Pass, call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		return fn.Sel.Name
	}
	return "call"
}

// isBuiltinAllowedToDropErr returns true for the small set of stdlib functions
// where dropping the return is the documented idiom — either because the
// docs say the error is always nil (bytes.Buffer, strings.Builder) or
// because the call is conventionally fire-and-forget (fmt.Print*).
func isBuiltinAllowedToDropErr(pass *analyzer.Pass, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	// Package-level calls: fmt.Println etc.
	if pkgIdent, ok := sel.X.(*ast.Ident); ok {
		if obj := pass.TypesInfo.Uses[pkgIdent]; obj != nil {
			if pn, ok := obj.(*types.PkgName); ok {
				if pn.Imported().Path() == "fmt" {
					switch sel.Sel.Name {
					case "Print", "Printf", "Println", "Fprint", "Fprintf", "Fprintln":
						return true
					}
				}
			}
		}
	}
	// Method calls on documented-nil-error stdlib types.
	recvType := pass.TypesInfo.TypeOf(sel.X)
	if recvType == nil {
		return false
	}
	// Unwrap pointer.
	if ptr, ok := recvType.(*types.Pointer); ok {
		recvType = ptr.Elem()
	}
	named, ok := recvType.(*types.Named)
	if !ok {
		return false
	}
	tn := named.Obj()
	if tn == nil || tn.Pkg() == nil {
		return false
	}
	pkgPath := tn.Pkg().Path()
	typeName := tn.Name()
	method := sel.Sel.Name
	switch pkgPath {
	case "bytes":
		if typeName == "Buffer" {
			switch method {
			case "Write", "WriteString", "WriteByte", "WriteRune":
				return true
			}
		}
	case "strings":
		if typeName == "Builder" {
			switch method {
			case "Write", "WriteString", "WriteByte", "WriteRune":
				return true
			}
		}
	case "hash":
		// hash.Hash.Write is documented to never return an error.
		if method == "Write" {
			return true
		}
	}
	return false
}

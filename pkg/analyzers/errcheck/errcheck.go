// Package errcheck reports calls to functions returning `error` whose error
// value is silently dropped.
//
// Caught patterns:
//
//	foo()              // foo returns error, value discarded
//	defer foo()        // same, including defer f.Close()
//	go foo()           // same; the goroutine's error is unrecoverable
//	x := foo()         // foo returns (T, error), error component discarded
//	_ = foo()          // explicit blank without // safe-ignore: annotation
//
// An explicit blank is accepted without annotation for cleanup calls
// (`_ = rows.Close()`, `_ = tx.Rollback()`, `_ = os.Remove(tmp)`) and for the
// calls whose error is documented to be nil (bytes.Buffer, strings.Builder,
// hash.Hash writes, crypto/rand.Read, fmt.Print*). Writing `_ =` there is
// already the statement that the error does not matter.
//
// Closing what was only read is allowlisted in every form: `rows.Close()` on
// *sql.Rows (failures surface through rows.Err) and `resp.Body.Close()`.
//
// Any other `defer f.Close()` is flagged. Close can fail (flush, short write,
// filesystem error). Handle it in a closure or annotate:
//
//	defer f.Close() // safe-ignore: read-only file
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
			case *ast.DeferStmt:
				checkBareCall(pass, file, stmt.Call)
			case *ast.GoStmt:
				checkBareCall(pass, file, stmt.Call)
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
	if pass.HasLineAnnotation(file, call.End(), analyzer.AnnSafeIgnore) {
		return
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
				maybeReportBlankErr(pass, file, as, call)
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
		maybeReportBlankErr(pass, file, as, call)
	}
}

func maybeReportBlankErr(pass *analyzer.Pass, file *ast.File, as *ast.AssignStmt, call *ast.CallExpr) {
	if isBuiltinAllowedToDropErr(pass, call) || isCleanupCall(pass, call) {
		return
	}
	if pass.HasLineAnnotation(file, as.End(), analyzer.AnnSafeIgnore) {
		return
	}
	name := callee(pass, call)
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

// errorIface is the builtin error interface.
// global-ok: read-only reference to a stdlib singleton; populated in init().
var errorIface *types.Interface

func init() {
	obj := types.Universe.Lookup("error")
	if obj == nil {
		return
	}
	if iface, ok := obj.Type().Underlying().(*types.Interface); ok {
		errorIface = iface
	}
}

// isErrorType reports whether t is the builtin error type or any type whose
// method set implements it, including concrete results such as
// *ValidationError. An anonymous interface whose method happens to be named
// Error but is not `Error() string` does not qualify.
func isErrorType(t types.Type) bool {
	if t == nil || errorIface == nil {
		return false
	}
	t = types.Unalias(t)
	return types.Implements(t, errorIface)
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
				switch pn.Imported().Path() {
				case "fmt":
					switch sel.Sel.Name {
					case "Print", "Printf", "Println", "Fprint", "Fprintf", "Fprintln":
						return true
					}
				case "crypto/rand":
					// Since Go 1.24 rand.Read never returns an error; it
					// crashes the program instead.
					if sel.Sel.Name == "Read" {
						return true
					}
				}
			}
		}
	}
	if isResponseBodyClose(pass, sel) {
		return true
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
	case "database/sql":
		// rows.Close only releases the connection; a failed iteration is
		// reported by rows.Err, which the rowserr rule checks.
		if typeName == "Rows" && method == "Close" {
			return true
		}
	}
	return false
}

// isResponseBodyClose matches resp.Body.Close() on an *http.Response or
// *http.Request: the body was only read, so a close error carries nothing the
// caller can act on.
func isResponseBodyClose(pass *analyzer.Pass, sel *ast.SelectorExpr) bool {
	if sel.Sel.Name != "Close" {
		return false
	}
	body, ok := sel.X.(*ast.SelectorExpr)
	if !ok || body.Sel.Name != "Body" {
		return false
	}
	t := pass.TypesInfo.TypeOf(body.X)
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != "net/http" {
		return false
	}
	return named.Obj().Name() == "Response" || named.Obj().Name() == "Request"
}

// isCleanupCall reports whether call releases a resource: a Close or Rollback
// method, or os.Remove/os.RemoveAll. Their errors are routinely discarded on
// error paths and in defers; an explicit `_ =` is enough of a statement.
func isCleanupCall(pass *analyzer.Pass, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if pkgIdent, ok := sel.X.(*ast.Ident); ok {
		if pn, ok := pass.TypesInfo.Uses[pkgIdent].(*types.PkgName); ok {
			return pn.Imported().Path() == "os" && (sel.Sel.Name == "Remove" || sel.Sel.Name == "RemoveAll")
		}
	}
	if pass.TypesInfo.Selections[sel] == nil {
		return false
	}
	switch sel.Sel.Name {
	case "Close", "Rollback":
		return len(call.Args) == 0
	}
	return false
}

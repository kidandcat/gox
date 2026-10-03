// Package namedargs requires call sites to name their arguments when two or
// more consecutive parameters share the same type.
//
// Rationale: `transfer(userID, orderID)` and `transfer(orderID, userID)` are
// both compilable Go but mean opposite things; this is one of the highest-
// frequency silent bug classes when code is written without supervision.
//
// Required form at the call site — each labelled argument on its own line:
//
//	transfer(
//		/* userID */ a,
//		/* orderID */ b,
//	)
//
// The own-line placement matters: on a single line `gofmt` relocates the
// block comment so it trails the *previous* argument
// (`a, /* orderID */ b` -> `a /* orderID */, b`), silently mis-naming the
// value — the very swap-bug this rule exists to prevent. Only the own-line
// form (and the first argument, which has no predecessor on its line) is a
// `gofmt` fixed point, so the rule accepts only those.
//
// The comment text must match the parameter name from the declaration. The
// rule fires only when the function has 2+ consecutive parameters of the same
// underlying type (or one is assignable to the other), so calls like
// `Add(2, 3)` to a `func Add(int, int)` still trigger but `Open(path)` does
// not.
//
// Variadic, single-typed-group calls (e.g. `fmt.Println(a, b, c)`) are
// exempt: the parameters share a type by design and the call site does not
// distinguish them.
package namedargs

import (
	_ "embed"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"github.com/mentasystems/gox/pkg/analyzer"
)

//go:embed namedargs.md
var explanation string // global-ok: populated at compile time by //go:embed, never mutated

func init() {
	analyzer.Register(&analyzer.Analyzer{
		Name:        "namedargs",
		Doc:         "requires inline `/* paramName */` comments when consecutive params share a type",
		Explanation: explanation,
		Run:         run,
		OptIn:       true,
	})
}

func run(pass *analyzer.Pass) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			checkCall(pass, file, call)
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
	if params.Len() < 2 {
		return
	}

	// Compute parameter names — skip if any are blank/anonymous (we have nothing to match against).
	names := make([]string, params.Len())
	for i := 0; i < params.Len(); i++ {
		n := params.At(i).Name()
		if n == "" || n == "_" {
			return
		}
		names[i] = n
	}

	// Find consecutive groups of compatible-typed params.
	flagged := make([]bool, params.Len())
	for i := 0; i < params.Len()-1; i++ {
		ti := params.At(i).Type()
		tj := params.At(i + 1).Type()
		if compatible(ti, tj) {
			flagged[i] = true
			flagged[i+1] = true
		}
	}
	// Variadic last group of same type: exempt the variadic position itself.
	if sig.Variadic() {
		flagged[params.Len()-1] = false
	}

	anyFlagged := false
	for _, b := range flagged {
		if b {
			anyFlagged = true
			break
		}
	}
	if !anyFlagged {
		return
	}

	// Respect an explicit opt-out. The annotation may live on the line where
	// the call opens (call.Pos) or where it closes (call.End) — multi-line
	// calls commonly carry it on the closing-paren line.
	if hasSafeIgnore(pass, file, call.Pos()) || hasSafeIgnore(pass, file, call.End()) {
		return
	}

	// Match arguments to parameters (variadic flattens the tail).
	for i, arg := range call.Args {
		if i >= params.Len() {
			break // variadic tail
		}
		if !flagged[i] {
			continue
		}
		// Trivially-named arguments (an identifier whose source name matches
		// the parameter name) are self-documenting and don't need a comment.
		if id, ok := arg.(*ast.Ident); ok && id.Name == names[i] {
			continue
		}
		// Per-argument opt-out (for multi-line calls where the annotation sits
		// on a specific argument's line).
		if hasSafeIgnore(pass, file, arg.Pos()) {
			continue
		}
		wantedName := names[i]
		labelled := leadingArgComment(pass.Fset, file, arg.Pos()) == wantedName

		// A `/* name */ arg` label only stays attached to its argument under
		// `gofmt` when the argument does not share a source line with the
		// argument before it. On a single line `gofmt` relocates the block
		// comment so it trails the *previous* argument
		// (`a, /* to */ b` -> `a /* to */, b`), silently mis-naming the value.
		// The first argument has no predecessor on its line, so its label is
		// always stable; every later argument must sit on its own line.
		stablePlacement := i == 0 || !sharesLineWithPrev(pass.Fset, call.Args[i-1], arg)

		switch {
		case labelled && stablePlacement:
			// Correctly labelled in a gofmt-stable position.
		case labelled && !stablePlacement:
			// Label is present but gofmt will relocate it (or already has):
			// turn this otherwise-silent breakage into a visible finding.
			pass.Report(analyzer.Issue{
				Analyzer: "namedargs",
				Pos:      pass.Fset.Position(arg.Pos()),
				Message:  fmt.Sprintf("/* %s */ label shares a line with the previous argument; gofmt relocates it and silently mis-names the argument", wantedName),
				Hint:     fmt.Sprintf("put the labelled argument on its own line: /* %s */ %s,", wantedName, exprSrc(pass.Fset, arg)),
			})
		default:
			pass.Report(analyzer.Issue{
				Analyzer: "namedargs",
				Pos:      pass.Fset.Position(arg.Pos()),
				Message:  fmt.Sprintf("argument shares a type with an adjacent argument; prefix with /* %s */ on its own line", wantedName),
				Hint:     fmt.Sprintf("change to a labelled argument on its own line: /* %s */ %s,", wantedName, exprSrc(pass.Fset, arg)),
			})
		}
	}
}

// sharesLineWithPrev reports whether `arg` begins on the same source line that
// the preceding argument `prev` ends on. When two arguments share a line, a
// leading /* name */ comment on the later one is not a gofmt fixed point:
// gofmt moves the comment back so it trails `prev`.
func sharesLineWithPrev(fset *token.FileSet, prev, arg ast.Expr) bool {
	return fset.Position(prev.End()).Line == fset.Position(arg.Pos()).Line
}

// hasSafeIgnore reports whether the source line containing `pos` carries a
// `// safe-ignore: <reason>` annotation. namedargs reports at an argument
// column inside a call expression, so a line-based lookup is used rather than
// a strict trailing one.
func hasSafeIgnore(pass *analyzer.Pass, file *ast.File, pos token.Pos) bool {
	return pass.HasLineAnnotation(file, pos, analyzer.AnnSafeIgnore)
}

// calleeSignature returns the *types.Signature for the callee of a CallExpr,
// or nil for type conversions / built-in calls we don't want to enforce.
func calleeSignature(pass *analyzer.Pass, call *ast.CallExpr) *types.Signature {
	tv, ok := pass.TypesInfo.Types[call.Fun]
	if !ok {
		return nil
	}
	if tv.IsType() {
		return nil // T(x) conversion
	}
	if tv.IsBuiltin() {
		return nil
	}
	sig, ok := tv.Type.(*types.Signature)
	if !ok {
		return nil
	}
	if isStdlibCallee(pass, call) {
		// Stdlib has stable conventions every Go developer (and trained model)
		// knows by heart. Enforcing namedargs there is pure noise.
		return nil
	}
	return sig
}

// isStdlibCallee reports whether call.Fun resolves to a function/method whose
// package belongs to the Go standard library.
func isStdlibCallee(pass *analyzer.Pass, call *ast.CallExpr) bool {
	var obj types.Object
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		obj = pass.TypesInfo.ObjectOf(fn.Sel)
	case *ast.Ident:
		obj = pass.TypesInfo.ObjectOf(fn)
	}
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	return isStdlibPath(pass, obj.Pkg().Path())
}

// isStdlibPath reports whether path is a standard-library import path.
// The loader's IsStdlib (go list's Standard field) is authoritative: a
// dotless module such as `module myapp` is user code even though its path
// has no dot. Without that information, fall back to the dot heuristic, but
// never treat the package under analysis as stdlib.
func isStdlibPath(pass *analyzer.Pass, path string) bool {
	if pass.IsStdlib != nil {
		return pass.IsStdlib(path)
	}
	if pass.Pkg != nil {
		pkgPath := pass.Pkg.Path()
		if path == pkgPath || strings.HasPrefix(path, pkgPath+"/") {
			return false
		}
	}
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			return true
		}
		if path[i] == '.' {
			return false
		}
	}
	return true
}

// compatible reports whether swapping two adjacent argument positions would
// silently compile. We restrict the rule to basic types (string, integer,
// float, bool) — pointers and structs that share a kind are almost always
// caught by the compiler the moment they are swapped, so the noise/value
// trade-off there is poor.
func compatible(a, b types.Type) bool {
	ba, aOK := a.Underlying().(*types.Basic)
	bb, bOK := b.Underlying().(*types.Basic)
	if !aOK || !bOK {
		return false
	}
	if !isBugProneBasic(ba) || !isBugProneBasic(bb) {
		return false
	}
	// Same basic kind (e.g. both string, both int) — bug-prone if swapped.
	return ba.Kind() == bb.Kind() ||
		(isIntegerBasic(ba) && isIntegerBasic(bb)) ||
		(isFloatBasic(ba) && isFloatBasic(bb))
}

func isBugProneBasic(b *types.Basic) bool {
	info := b.Info()
	if info&types.IsString != 0 {
		return true
	}
	if info&types.IsNumeric != 0 {
		return true
	}
	if info&types.IsBoolean != 0 {
		return true
	}
	return false
}

func isIntegerBasic(b *types.Basic) bool { return b.Info()&types.IsInteger != 0 }
func isFloatBasic(b *types.Basic) bool   { return b.Info()&types.IsFloat != 0 }

// leadingArgComment returns the trimmed text of a single /* ... */ comment
// that immediately precedes `pos` on the same line, or "".
func leadingArgComment(fset *token.FileSet, file *ast.File, pos token.Pos) string {
	line := fset.Position(pos).Line
	var best *ast.CommentGroup
	for _, cg := range file.Comments {
		end := fset.Position(cg.End())
		if end.Line != line {
			continue
		}
		if cg.End() > pos {
			continue
		}
		if best == nil || cg.End() > best.End() {
			best = cg
		}
	}
	if best == nil {
		return ""
	}
	// Take the last comment in the group, expect /* ... */ form
	c := best.List[len(best.List)-1]
	if !strings.HasPrefix(c.Text, "/*") || !strings.HasSuffix(c.Text, "*/") {
		return ""
	}
	inner := strings.TrimSpace(c.Text[2 : len(c.Text)-2])
	return inner
}

// exprSrc renders an expression position back to a short hint string.
func exprSrc(_ *token.FileSet, e ast.Expr) string {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return "..."
}

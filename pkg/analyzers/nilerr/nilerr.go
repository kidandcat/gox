// Package nilerr reports `return ..., nil` inside `if err != nil { ... }`
// when err is not used anywhere in that block.
//
//	if err != nil {
//		return nil, nil // the failure disappears
//	}
//
// Returning nil there is sometimes intended (not found is not an error), but
// then the block normally looks at err (errors.Is, a log line), explains
// itself in a comment, or returns a value built from a call that reports the
// failure in band. A block that does none of that and reports success is the
// bug the nilerr linter catches.
//
// Opt out with `// safe-ignore: <reason>`.
package nilerr

import (
	_ "embed"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/kidandcat/gox/pkg/analyzer"
)

//go:embed nilerr.md
var explanation string // global-ok: populated at compile time by //go:embed, never mutated

func init() {
	analyzer.Register(&analyzer.Analyzer{
		Name:        "nilerr",
		Doc:         "returns a nil error inside `if err != nil` without using err",
		Explanation: explanation,
		Run:         run,
	})
}

func run(pass *analyzer.Pass) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			var ft *ast.FuncType
			var body *ast.BlockStmt
			switch f := n.(type) {
			case *ast.FuncDecl:
				ft, body = f.Type, f.Body
			case *ast.FuncLit:
				ft, body = f.Type, f.Body
			default:
				return true
			}
			if body != nil && returnsErrorLast(pass, ft) {
				checkBody(pass, file, body)
			}
			return true
		})
	}
}

func checkBody(pass *analyzer.Pass, file *ast.File, body *ast.BlockStmt) {
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false // checked as its own function
		}
		ifs, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		errObj := checkedErr(pass, ifs.Cond)
		if errObj == nil || usesObj(pass, ifs.Body, errObj) || hasComment(file, ifs.Body) {
			return true
		}
		ast.Inspect(ifs.Body, func(m ast.Node) bool {
			if _, ok := m.(*ast.FuncLit); ok {
				return false
			}
			ret, ok := m.(*ast.ReturnStmt)
			if !ok || len(ret.Results) == 0 {
				return true
			}
			last, ok := ret.Results[len(ret.Results)-1].(*ast.Ident)
			if !ok || last.Name != "nil" || reportsInBand(ret) {
				return true
			}
			if pass.HasLineAnnotation(file, ret.Pos(), analyzer.AnnSafeIgnore) {
				return true
			}
			pass.Report(analyzer.Issue{
				Analyzer: "nilerr",
				Pos:      pass.Fset.Position(ret.Pos()),
				Message:  "returns a nil error inside `if " + errObj.Name() + " != nil` without using " + errObj.Name() + "; the failure is lost",
				Hint:     "return " + errObj.Name() + " (wrapped if useful); if success is intended, check it with errors.Is or annotate with `// safe-ignore: <reason>`",
			})
			return true
		})
		return true
	})
}

// checkedErr returns the error variable of a `x != nil` condition.
func checkedErr(pass *analyzer.Pass, cond ast.Expr) types.Object {
	be, ok := ast.Unparen(cond).(*ast.BinaryExpr)
	if !ok || be.Op != token.NEQ {
		return nil
	}
	x, y := ast.Unparen(be.X), ast.Unparen(be.Y)
	if id, ok := x.(*ast.Ident); ok && id.Name == "nil" {
		x, y = y, x
	}
	if nilID, ok := y.(*ast.Ident); !ok || nilID.Name != "nil" {
		return nil
	}
	id, ok := x.(*ast.Ident)
	if !ok {
		return nil
	}
	obj := pass.TypesInfo.ObjectOf(id)
	if obj == nil || !types.Identical(obj.Type(), types.Universe.Lookup("error").Type()) {
		return nil
	}
	return obj
}

// hasComment reports whether the block carries a comment: an explained
// early return is a decision, not an accident.
func hasComment(file *ast.File, body *ast.BlockStmt) bool {
	for _, cg := range file.Comments {
		if cg.Pos() > body.Lbrace && cg.End() < body.Rbrace {
			return true
		}
	}
	return false
}

// reportsInBand reports whether another result is built by a call, such as
// an error payload for a protocol response (`return errorResult(...), nil`).
func reportsInBand(ret *ast.ReturnStmt) bool {
	for _, r := range ret.Results[:len(ret.Results)-1] {
		if _, ok := ast.Unparen(r).(*ast.CallExpr); ok {
			return true
		}
	}
	return false
}

func usesObj(pass *analyzer.Pass, n ast.Node, obj types.Object) bool {
	found := false
	ast.Inspect(n, func(m ast.Node) bool {
		if id, ok := m.(*ast.Ident); ok && pass.TypesInfo.ObjectOf(id) == obj {
			found = true
		}
		return !found
	})
	return found
}

func returnsErrorLast(pass *analyzer.Pass, ft *ast.FuncType) bool {
	if ft.Results == nil || len(ft.Results.List) == 0 {
		return false
	}
	last := ft.Results.List[len(ft.Results.List)-1]
	t := pass.TypesInfo.TypeOf(last.Type)
	return t != nil && types.Identical(t, types.Universe.Lookup("error").Type())
}

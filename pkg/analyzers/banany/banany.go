// Package banany forbids `any` / `interface{}` in declarations without an
// `// any-ok: <reason>` annotation on the same line (or on a doc comment).
//
// Covered positions:
//   - function parameter types
//   - function result types
//   - struct field types
//   - method receiver types are not covered (Go does not allow `any` there)
//
// `map[any]V` / `[]any` / `*any` are all reported.
package banany

import (
	_ "embed"
	"go/ast"

	"github.com/kidandcat/gox/pkg/analyzer"
)

//go:embed banany.md
var explanation string // global-ok: populated at compile time by //go:embed, never mutated

func init() {
	analyzer.Register(&analyzer.Analyzer{
		Name:        "banany",
		Doc:         "forbids `any` / `interface{}` without an // any-ok: justification",
		Explanation: explanation,
		Run:         run,
		OptIn:       true,
	})
}

func run(pass *analyzer.Pass) {
	for _, file := range pass.Files {
		// Pre-walk: collect FuncType nodes that are the .Type of a FuncDecl, so
		// the FuncType visit below skips them (the FuncDecl branch handles those).
		ownedByDecl := map[*ast.FuncType]bool{}
		for _, decl := range file.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Type != nil {
				ownedByDecl[fd.Type] = true
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch d := n.(type) {
			case *ast.FuncDecl:
				if d.Type != nil {
					checkFieldList(pass, file, d.Type.Params, d.Doc)
					checkFieldList(pass, file, d.Type.Results, d.Doc)
				}
			case *ast.FuncType:
				if ownedByDecl[d] {
					return true
				}
				checkFieldList(pass, file, d.Params, nil)
				checkFieldList(pass, file, d.Results, nil)
			case *ast.StructType:
				checkFieldList(pass, file, d.Fields, nil)
			}
			return true
		})
	}
}

func checkFieldList(pass *analyzer.Pass, file *ast.File, fl *ast.FieldList, doc *ast.CommentGroup) {
	if fl == nil {
		return
	}
	for _, field := range fl.List {
		if !exprMentionsAny(field.Type) {
			continue
		}
		if analyzer.HasAnnotation(doc, analyzer.AnnAnyOK) ||
			analyzer.HasAnnotation(field.Doc, analyzer.AnnAnyOK) ||
			analyzer.HasAnnotation(field.Comment, analyzer.AnnAnyOK) ||
			pass.HasLineAnnotation(file, field.End(), analyzer.AnnAnyOK) {
			continue
		}
		pass.Report(analyzer.Issue{
			Analyzer: "banany",
			Pos:      pass.Fset.Position(field.Type.Pos()),
			Message:  "use of `any` (interface{}) without justification",
			Hint:     "use a concrete type, or annotate with `// any-ok: <reason>`",
		})
	}
}

func exprMentionsAny(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Ident:
			if x.Name == "any" {
				found = true
				return false
			}
		case *ast.InterfaceType:
			if x.Methods == nil || len(x.Methods.List) == 0 {
				found = true
				return false
			}
		}
		return !found
	})
	return found
}

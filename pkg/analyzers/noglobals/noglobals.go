// Package noglobals forbids package-level mutable `var` declarations.
//
// `const` is allowed. A `var` declaration is also allowed when it carries a
// trailing `// global-ok: <reason>` annotation, which the author must use to
// justify the shared mutable state.
//
// Rationale: package-level mutable state introduces non-determinism, makes
// testing harder, and breaks the property that the same input produces the
// same output across a process. Forbid by default, opt in explicitly.
package noglobals

import (
	_ "embed"
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"github.com/kidandcat/gox/pkg/analyzer"
)

//go:embed noglobals.md
var explanation string // global-ok: populated at compile time by //go:embed, never mutated

func init() {
	analyzer.Register(&analyzer.Analyzer{
		Name:        "noglobals",
		Doc:         "forbids mutable package-level var declarations without justification",
		Explanation: explanation,
		Run:         run,
		OptIn:       true,
	})
}

func run(pass *analyzer.Pass) {
	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec) // safe-ignore: GenDecl.Tok == VAR implies ValueSpec
				for _, name := range vs.Names {
					if name.Name == "_" {
						continue
					}
					if hasGlobalOKOnSpec(pass.Fset, file, vs) || hasGlobalOKOnGenDecl(pass.Fset, file, gd) {
						continue
					}
					pass.Report(analyzer.Issue{
						Analyzer: "noglobals",
						Pos:      pass.Fset.Position(name.Pos()),
						Message:  fmt.Sprintf("package-level mutable var %q is forbidden", name.Name),
						Hint:     "convert to const, move into a function, or annotate with `// global-ok: <reason>`",
					})
				}
			}
		}
	}
}

func hasGlobalOKOnSpec(fset *token.FileSet, file *ast.File, vs *ast.ValueSpec) bool {
	line := fset.Position(vs.End()).Line
	for _, cg := range file.Comments {
		if fset.Position(cg.Pos()).Line == line {
			if containsAnn(cg.List, analyzer.AnnGlobalOK) {
				return true
			}
		}
	}
	if vs.Doc != nil && containsAnn(vs.Doc.List, analyzer.AnnGlobalOK) {
		return true
	}
	if vs.Comment != nil && containsAnn(vs.Comment.List, analyzer.AnnGlobalOK) {
		return true
	}
	return false
}

func hasGlobalOKOnGenDecl(fset *token.FileSet, file *ast.File, gd *ast.GenDecl) bool {
	if gd.Doc != nil && containsAnn(gd.Doc.List, analyzer.AnnGlobalOK) {
		return true
	}
	line := fset.Position(gd.TokPos).Line
	for _, cg := range file.Comments {
		if fset.Position(cg.Pos()).Line == line {
			if containsAnn(cg.List, analyzer.AnnGlobalOK) {
				return true
			}
		}
	}
	return false
}

func containsAnn(comments []*ast.Comment, prefix string) bool {
	for _, c := range comments {
		text := strings.TrimPrefix(c.Text, "//")
		text = strings.TrimSpace(text)
		if strings.HasPrefix(text, prefix) {
			rest := strings.TrimSpace(text[len(prefix):])
			if rest != "" {
				return true
			}
		}
	}
	return false
}

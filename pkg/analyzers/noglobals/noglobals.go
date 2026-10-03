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

	"github.com/mentasystems/gox/pkg/analyzer"
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
					if hasGlobalOKOnSpec(pass, file, vs) || hasGlobalOKOnGenDecl(pass, file, gd) {
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

func hasGlobalOKOnSpec(pass *analyzer.Pass, file *ast.File, vs *ast.ValueSpec) bool {
	return pass.HasLineAnnotation(file, vs.End(), analyzer.AnnGlobalOK) ||
		analyzer.HasAnnotation(vs.Doc, analyzer.AnnGlobalOK) ||
		analyzer.HasAnnotation(vs.Comment, analyzer.AnnGlobalOK)
}

func hasGlobalOKOnGenDecl(pass *analyzer.Pass, file *ast.File, gd *ast.GenDecl) bool {
	return analyzer.HasAnnotation(gd.Doc, analyzer.AnnGlobalOK) ||
		pass.HasLineAnnotation(file, gd.TokPos, analyzer.AnnGlobalOK)
}

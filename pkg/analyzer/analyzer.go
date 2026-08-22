// Package analyzer defines the core types every gox analyzer implements.
//
// An Analyzer inspects a parsed and type-checked Go package and reports
// Issues. The runner (pkg/loader) is responsible for parsing, type-checking,
// and invoking every registered analyzer.
package analyzer

import (
	"go/ast"
	"go/token"
	"go/types"
)

// Issue is a single problem reported by an analyzer.
type Issue struct {
	Analyzer string
	Pos      token.Position
	Message  string
	Hint     string
}

// Pass is the input given to an analyzer.
type Pass struct {
	Fset      *token.FileSet
	Pkg       *types.Package
	TypesInfo *types.Info
	Files     []*ast.File
	Report    func(Issue)
}

// Analyzer is a single rule.
type Analyzer struct {
	Name string
	// Doc is a one-line summary surfaced by `gox list`.
	Doc string
	// Explanation is the long-form markdown surfaced by `gox explain <name>`.
	// Each analyzer embeds its own .md file via //go:embed and assigns it here
	// at init time. May be empty.
	Explanation string
	Run         func(*Pass)
	// OptIn excludes the rule from the default `gox check` run; it only runs
	// with `gox check --all` (or when named explicitly). Style-tier rules are
	// opt-in: enforced at agent turn end they overwhelmingly produce
	// suppression annotations instead of fixes, drowning the bug-tier rules.
	OptIn bool
}

// Annotation prefixes recognised on trailing line comments to opt out of a rule.
//
// Convention: //<prefix> <reason>
const (
	AnnSafeIgnore = "safe-ignore:"
	AnnGlobalOK   = "global-ok:"
	AnnAnyOK      = "any-ok:"
	AnnPanicOK    = "panic-ok:"
	AnnTimeoutOK  = "timeout-ok:"
)

// HasAnnotation reports whether a line comment group contains the given prefix
// and the prefix is followed by a non-empty reason.
func HasAnnotation(cg *ast.CommentGroup, prefix string) bool {
	if cg == nil {
		return false
	}
	for _, c := range cg.List {
		text := c.Text
		if len(text) >= 2 && text[:2] == "//" {
			text = text[2:]
		}
		// trim leading spaces
		i := 0
		for i < len(text) && (text[i] == ' ' || text[i] == '\t') {
			i++
		}
		text = text[i:]
		if len(text) >= len(prefix) && text[:len(prefix)] == prefix {
			rest := text[len(prefix):]
			// require at least one non-space char as the reason
			for _, r := range rest {
				if r != ' ' && r != '\t' {
					return true
				}
			}
		}
	}
	return false
}

// HasLineAnnotation reports whether any comment on the same source line as
// pos carries the given prefix with a non-empty reason. This is the shared
// opt-out check used by rules that honor // safe-ignore: (and similar)
// annotations; empty reasons do not suppress.
func HasLineAnnotation(fset *token.FileSet, file *ast.File, pos token.Pos, prefix string) bool {
	if file == nil {
		return false
	}
	line := fset.Position(pos).Line
	for _, cg := range file.Comments {
		if fset.Position(cg.Pos()).Line != line && fset.Position(cg.End()).Line != line {
			continue
		}
		if HasAnnotation(cg, prefix) {
			return true
		}
	}
	return false
}

// Package analyzer defines the core types every gox analyzer implements.
//
// An Analyzer inspects a parsed and type-checked Go package and reports
// Issues. Parsing and type-checking live in pkg/loader; pkg/analyzer.Run
// invokes every registered analyzer.
package analyzer

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
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

	// ModulePath is the module that owns Pkg (`go list`'s Module.Path).
	// Empty when the package is outside a module or the pass was built by a
	// unit test that does not go through the loader. Rules that look at other
	// packages (exhaustive) treat an empty ModulePath as "this package only".
	ModulePath string

	// IsStdlib reports whether an import path is in the standard library,
	// using `go list`'s Standard field. Nil falls back to the
	// first-segment-has-no-dot heuristic, which mis-classifies dotless
	// modules (`module myapp`); the loader always sets this.
	IsStdlib func(importPath string) bool

	// anns caches trimmed comment text per file line for HasLineAnnotation.
	anns map[*ast.File]map[int][]string
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
	AnnSafeIgnore   = "safe-ignore:"
	AnnGlobalOK     = "global-ok:"
	AnnAnyOK        = "any-ok:"
	AnnTimeoutOK    = "timeout-ok:"
	AnnGoroutineOK  = "goroutine-ok:"
	AnnExhaustiveOK = "exhaustive-ok:"
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
//
// A comment group matches when it starts or ends on that line, so a trailing
// annotation on a multi-line call is seen from both ends.
func HasLineAnnotation(fset *token.FileSet, file *ast.File, pos token.Pos, prefix string) bool {
	if file == nil || fset == nil {
		return false
	}
	return textsHavePrefix(commentTextsOnLine(fset, file, fset.Position(pos).Line), prefix)
}

// HasLineAnnotation is HasLineAnnotation with a per-pass cache, so each file's
// comments are indexed once instead of once per finding.
func (p *Pass) HasLineAnnotation(file *ast.File, pos token.Pos, prefix string) bool {
	if p == nil || file == nil || p.Fset == nil {
		return false
	}
	line := p.Fset.Position(pos).Line
	return textsHavePrefix(p.commentTexts(file)[line], prefix)
}

func (p *Pass) commentTexts(file *ast.File) map[int][]string {
	if p.anns == nil {
		p.anns = map[*ast.File]map[int][]string{}
	}
	if idx, ok := p.anns[file]; ok {
		return idx
	}
	idx := indexCommentTexts(p.Fset, file)
	p.anns[file] = idx
	return idx
}

func commentTextsOnLine(fset *token.FileSet, file *ast.File, line int) []string {
	return indexCommentTexts(fset, file)[line]
}

// indexCommentTexts maps a source line to the trimmed bodies of every comment
// group that starts or ends on it. Bodies have the leading "//" removed.
func indexCommentTexts(fset *token.FileSet, file *ast.File) map[int][]string {
	idx := map[int][]string{}
	if file == nil || fset == nil {
		return idx
	}
	for _, cg := range file.Comments {
		texts := make([]string, len(cg.List))
		for i, c := range cg.List {
			texts[i] = trimComment(c.Text)
		}
		start := fset.Position(cg.Pos()).Line
		end := fset.Position(cg.End()).Line
		idx[start] = append(idx[start], texts...)
		if end != start {
			idx[end] = append(idx[end], texts...)
		}
	}
	return idx
}

func textsHavePrefix(texts []string, prefix string) bool {
	for _, text := range texts {
		if hasPrefixReason(text, prefix) {
			return true
		}
	}
	return false
}

func trimComment(text string) string {
	if len(text) >= 2 && text[:2] == "//" {
		text = text[2:]
	}
	return strings.TrimSpace(text)
}

func hasPrefixReason(text, prefix string) bool {
	if len(text) < len(prefix) || text[:len(prefix)] != prefix {
		return false
	}
	rest := text[len(prefix):]
	for _, r := range rest {
		if r != ' ' && r != '\t' {
			return true
		}
	}
	return false
}

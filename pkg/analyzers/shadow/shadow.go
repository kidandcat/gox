// Package shadow reports variable shadowing introduced by `:=`.
//
// A short variable declaration that introduces a name already visible in an
// enclosing scope is treated as an error. This is the classic source of bugs
// where `err :=` inside a nested block silently masks the outer `err`.
//
// Special-cased: re-declaring the same name in the same scope (Go's "at least
// one new variable" rule) is not shadowing and is not reported.
//
// A declaration in the init of an `if` or `switch` (`if _, err := f(); err !=
// nil`) lives only inside that statement. It is reported only when the next
// mention of the outer variable after the statement is a read: that read
// sees the old value where the author may expect the inner one. When the
// outer variable is assigned again first (`n, err = g()`), nothing is lost.
// Order is by source position, so loops and branches are approximated.
package shadow

import (
	_ "embed"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"

	"github.com/kidandcat/gox/pkg/analyzer"
)

//go:embed shadow.md
var explanation string // global-ok: populated at compile time by //go:embed, never mutated

func init() {
	analyzer.Register(&analyzer.Analyzer{
		Name:        "shadow",
		Doc:         "reports variables that shadow names from an outer scope",
		Explanation: explanation,
		Run:         run,
		OptIn:       true,
	})
}

func run(pass *analyzer.Pass) {
	var events map[types.Object][]event // built on first if/switch-init shadow
	for _, file := range pass.Files {
		inits := map[*ast.AssignStmt]ast.Stmt{}
		ast.Inspect(file, func(n ast.Node) bool {
			switch s := n.(type) {
			case *ast.IfStmt:
				if as, ok := s.Init.(*ast.AssignStmt); ok {
					inits[as] = s
				}
			case *ast.SwitchStmt:
				if as, ok := s.Init.(*ast.AssignStmt); ok {
					inits[as] = s
				}
			case *ast.TypeSwitchStmt:
				if as, ok := s.Init.(*ast.AssignStmt); ok {
					inits[as] = s
				}
			}
			as, ok := n.(*ast.AssignStmt)
			if !ok || as.Tok != token.DEFINE {
				return true
			}
			for _, lhs := range as.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok || id.Name == "_" {
					continue
				}
				// `ok` is the universal Go comma-ok idiom name. Flagging its
				// reuse in nested scopes produces only noise. Real bugs in
				// this space (re-declaring `err`, `result`, etc.) are still
				// caught.
				if id.Name == "ok" {
					continue
				}
				obj := pass.TypesInfo.Defs[id]
				if obj == nil {
					continue // pre-existing (re-assignment of an outer name)
				}
				scope := obj.Parent()
				if scope == nil {
					continue
				}
				outer := scope.Parent()
				if outer == nil {
					continue
				}
				if outerObj := lookupVarOrParam(outer, id.Name, id.Pos()); outerObj != nil {
					if owner, ok := inits[as]; ok {
						if events == nil {
							events = mentions(pass)
						}
						if !readNext(events[outerObj], owner.End()) {
							continue // outer value is reassigned or unused after the statement
						}
					}
					// Respect an explicit opt-out on the declaration line.
					if hasSafeIgnore(pass, file, id.Pos()) {
						continue
					}
					pass.Report(analyzer.Issue{
						Analyzer: "shadow",
						Pos:      pass.Fset.Position(id.Pos()),
						Message:  fmt.Sprintf("declaration of %q shadows outer variable", id.Name),
						Hint:     "rename the inner variable, or use plain `=` if you intend to reassign the outer one",
					})
				}
			}
			return true
		})
	}
}

// event is one mention of a variable: a read, or a write on the left-hand
// side of an assignment.
type event struct {
	pos   token.Pos
	write bool
}

// mentions lists, per object, every use in source order.
func mentions(pass *analyzer.Pass) map[types.Object][]event {
	writes := map[*ast.Ident]bool{}
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			if as, ok := n.(*ast.AssignStmt); ok {
				for _, l := range as.Lhs {
					if id, ok := l.(*ast.Ident); ok {
						writes[id] = true
					}
				}
			}
			return true
		})
	}
	out := map[types.Object][]event{}
	for id, obj := range pass.TypesInfo.Uses {
		if _, ok := obj.(*types.Var); ok {
			out[obj] = append(out[obj], event{pos: id.Pos(), write: writes[id]})
		}
	}
	for _, evs := range out {
		sort.Slice(evs, func(i, j int) bool { return evs[i].pos < evs[j].pos })
	}
	return out
}

// readNext reports whether the first mention after pos is a read.
func readNext(evs []event, after token.Pos) bool {
	i := sort.Search(len(evs), func(i int) bool { return evs[i].pos > after })
	return i < len(evs) && !evs[i].write
}

// hasSafeIgnore reports whether the source line containing `pos` carries a
// `// safe-ignore: <reason>` annotation. shadow reports at the identifier
// column (which may sit in the middle of a line, e.g. inside an `if`-init
// clause), so a line-based lookup is used rather than a strict trailing one.
func hasSafeIgnore(pass *analyzer.Pass, file *ast.File, pos token.Pos) bool {
	return pass.HasLineAnnotation(file, pos, analyzer.AnnSafeIgnore)
}

// lookupVarOrParam walks scopes outward looking for a *types.Var (variable or
// parameter) with the given name visible at `pos`. It ignores package-level
// scope to avoid flagging legitimate uses of common stdlib names.
func lookupVarOrParam(scope *types.Scope, name string, pos token.Pos) types.Object {
	for s := scope; s != nil; s = s.Parent() {
		if s.Parent() == nil {
			// package-level scope: skip to keep noise low
			return nil
		}
		if _, obj := s.LookupParent(name, pos); obj != nil {
			if _, ok := obj.(*types.Var); ok {
				return obj
			}
			return nil
		}
	}
	return nil
}

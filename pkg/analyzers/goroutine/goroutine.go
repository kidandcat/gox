// Package goroutine reports `go` statements whose lifetime is not bound to a
// visible coordination primitive.
//
// A `go funcCall(...)` is accepted when the enclosing function (anywhere in
// its lexical scope up to and including the go statement) defines or
// receives any of the following:
//   - a variable of type *errgroup.Group (golang.org/x/sync/errgroup)
//   - a variable of type sync.WaitGroup
//   - a context.CancelFunc (i.e. a cancellation handle exists)
//
// The intent is not to ban goroutines but to require that something in the
// scope is responsible for their lifecycle. To accept a fire-and-forget
// spawn, annotate with `// goroutine-ok: <reason>` on the same line as `go`.
package goroutine

import (
	_ "embed"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/kidandcat/gox/pkg/analyzer"
)

//go:embed goroutine.md
var explanation string // global-ok: populated at compile time by //go:embed, never mutated

func init() {
	analyzer.Register(&analyzer.Analyzer{
		Name:        "goroutine",
		Doc:         "goroutines must run in a scope with a visible lifecycle primitive",
		Explanation: explanation,
		Run:         run,
		OptIn:       true,
	})
}

func run(pass *analyzer.Pass) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				return true
			}
			scopeHas := scopeHasLifecycleVar(pass, fn.Type, fn.Body)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				gs, ok := n.(*ast.GoStmt)
				if !ok {
					return true
				}
				if pass.HasLineAnnotation(file, gs.Pos(), analyzer.AnnGoroutineOK) {
					return true
				}
				if scopeHas {
					return true
				}
				pass.Report(analyzer.Issue{
					Analyzer: "goroutine",
					Pos:      pass.Fset.Position(gs.Pos()),
					Message:  "goroutine spawned without a visible *errgroup.Group, sync.WaitGroup, or CancelFunc in scope",
					Hint:     "wrap with errgroup.WithContext / use sync.WaitGroup / capture context.WithCancel, or annotate with `// goroutine-ok: <reason>`",
				})
				return true
			})
			return true
		})
	}
}

// scopeHasLifecycleVar reports whether the function parameter list or body
// declares any value whose type is one of the recognised lifecycle primitives.
func scopeHasLifecycleVar(pass *analyzer.Pass, ft *ast.FuncType, body *ast.BlockStmt) bool {
	// Check parameters.
	if ft.Params != nil {
		for _, p := range ft.Params.List {
			if t := pass.TypesInfo.TypeOf(p.Type); isLifecycleType(t) {
				return true
			}
		}
	}
	// Check body declarations.
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch d := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range d.Lhs {
				if id, ok := lhs.(*ast.Ident); ok {
					if obj := pass.TypesInfo.Defs[id]; obj != nil {
						if isLifecycleType(obj.Type()) {
							found = true
							return false
						}
					}
				}
				_ = i
			}
		case *ast.DeclStmt:
			gd, ok := d.Decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				return true
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec) // safe-ignore: GenDecl.Tok == VAR guarantees ValueSpec
				for _, name := range vs.Names {
					if obj := pass.TypesInfo.Defs[name]; obj != nil {
						if isLifecycleType(obj.Type()) {
							found = true
							return false
						}
					}
				}
			}
		}
		return !found
	})
	return found
}

func isLifecycleType(t types.Type) bool {
	if t == nil {
		return false
	}
	// errgroup.Group (pointer or value).
	switch tt := t.(type) {
	case *types.Pointer:
		return isLifecycleType(tt.Elem())
	case *types.Named:
		obj := tt.Obj()
		if obj == nil || obj.Pkg() == nil {
			return false
		}
		switch obj.Pkg().Path() {
		case "golang.org/x/sync/errgroup":
			return obj.Name() == "Group"
		case "sync":
			return obj.Name() == "WaitGroup"
		}
	case *types.Signature:
		// context.CancelFunc is `type CancelFunc func()` — detect by name only via underlying isn't reliable.
		return false
	}
	// context.CancelFunc named type is *types.Named with package context.
	if named, ok := t.(*types.Named); ok {
		obj := named.Obj()
		if obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == "context" && obj.Name() == "CancelFunc" {
			return true
		}
	}
	return false
}

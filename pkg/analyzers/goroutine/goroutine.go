// Package goroutine reports `go` statements whose lifetime is not bound to a
// visible coordination primitive.
//
// A `go` statement is accepted when the statement itself (the call, its
// arguments, or the body of a `go func() {...}()` literal) refers to:
//   - a variable of type *errgroup.Group (golang.org/x/sync/errgroup)
//   - a variable of type sync.WaitGroup
//   - a context.CancelFunc
//   - a context.Context, when the enclosing function also holds a
//     context.CancelFunc (`ctx, cancel := context.WithCancel(...)`)
//
// A primitive that merely exists in the function does not count: a
// WaitGroup that tracks other goroutines says nothing about this one.
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
			hasCancel := scopeHasCancel(pass, fn.Type, fn.Body)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				gs, ok := n.(*ast.GoStmt)
				if !ok {
					return true
				}
				if pass.HasLineAnnotation(file, gs.Pos(), analyzer.AnnGoroutineOK) {
					return true
				}
				if bound(pass, gs, hasCancel) {
					return true
				}
				pass.Report(analyzer.Issue{
					Analyzer: "goroutine",
					Pos:      pass.Fset.Position(gs.Pos()),
					Message:  "goroutine does not use an *errgroup.Group, sync.WaitGroup, CancelFunc, or cancellable context",
					Hint:     "wrap with errgroup.WithContext / use sync.WaitGroup / capture context.WithCancel, or annotate with `// goroutine-ok: <reason>`",
				})
				return true
			})
			return true
		})
	}
}

// bound reports whether the go statement refers to a lifecycle primitive,
// or to a context.Context while the function holds a CancelFunc.
func bound(pass *analyzer.Pass, gs *ast.GoStmt, hasCancel bool) bool {
	found := false
	ast.Inspect(gs.Call, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || found {
			return !found
		}
		v, ok := pass.TypesInfo.ObjectOf(id).(*types.Var)
		if !ok {
			return true
		}
		if isLifecycleType(v.Type()) || (hasCancel && isContext(v.Type())) {
			found = true
		}
		return !found
	})
	return found
}

func isContext(t types.Type) bool {
	n, ok := t.(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "context" && n.Obj().Name() == "Context"
}

func isCancelFunc(t types.Type) bool {
	n, ok := t.(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "context" && n.Obj().Name() == "CancelFunc"
}

// scopeHasCancel reports whether the function parameter list or body
// declares a context.CancelFunc.
func scopeHasCancel(pass *analyzer.Pass, ft *ast.FuncType, body *ast.BlockStmt) bool {
	// Check parameters.
	if ft.Params != nil {
		for _, p := range ft.Params.List {
			if t := pass.TypesInfo.TypeOf(p.Type); t != nil && isCancelFunc(t) {
				return true
			}
		}
	}
	// Check body declarations.
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch d := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range d.Lhs {
				if id, ok := lhs.(*ast.Ident); ok {
					if obj := pass.TypesInfo.Defs[id]; obj != nil {
						if isCancelFunc(obj.Type()) {
							found = true
							return false
						}
					}
				}
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
						if isCancelFunc(obj.Type()) {
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

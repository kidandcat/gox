// Package copylock reports values that contain a lock (sync.Mutex, RWMutex,
// WaitGroup, Once, Cond, sync/atomic types, or anything with a noCopy
// guard) being copied.
//
// A copied mutex is a different mutex: the copy and the original no longer
// exclude each other, and a copy taken while locked stays locked forever.
//
// Reported copies: value receivers and parameters, `range` value variables,
// assignments and `var` initialisations from an existing value, call
// arguments, and return values. Composite literals, function call results
// and `new` are fresh values and are fine. This is go vet's copylocks on the
// AST.
//
// Opt out with `// safe-ignore: <reason>`.
package copylock

import (
	_ "embed"
	"fmt"
	"go/ast"
	"go/types"

	"github.com/kidandcat/gox/pkg/analyzer"
)

//go:embed copylock.md
var explanation string // global-ok: populated at compile time by //go:embed, never mutated

func init() {
	analyzer.Register(&analyzer.Analyzer{
		Name:        "copylock",
		Doc:         "values containing a sync.Mutex / WaitGroup / atomic are copied",
		Explanation: explanation,
		Run:         run,
	})
}

type checker struct {
	pass *analyzer.Pass
	file *ast.File
	memo map[types.Type]string
}

func run(pass *analyzer.Pass) {
	c := &checker{pass: pass, memo: map[types.Type]string{}}
	for _, file := range pass.Files {
		c.file = file
		ast.Inspect(file, c.visit)
	}
}

func (c *checker) visit(n ast.Node) bool {
	switch x := n.(type) {
	case *ast.FuncDecl:
		if x.Recv != nil {
			c.fields(x.Recv, "receiver")
		}
		c.fields(x.Type.Params, "parameter")
	case *ast.FuncLit:
		c.fields(x.Type.Params, "parameter")
	case *ast.RangeStmt:
		if x.Value != nil {
			if id, ok := x.Value.(*ast.Ident); !ok || id.Name != "_" {
				c.check(x.Value, c.pass.TypesInfo.TypeOf(x.Value), "range value copies")
			}
		}
	case *ast.AssignStmt:
		for i, r := range x.Rhs {
			if len(x.Lhs) == len(x.Rhs) {
				if id, ok := x.Lhs[i].(*ast.Ident); ok && id.Name == "_" {
					continue // `_ = v` keeps no copy
				}
			}
			c.checkExpr(r, "assignment copies")
		}
	case *ast.ValueSpec:
		for _, v := range x.Values {
			c.checkExpr(v, "variable declaration copies")
		}
	case *ast.ReturnStmt:
		for _, r := range x.Results {
			c.checkExpr(r, "return copies")
		}
	case *ast.CallExpr:
		if tv, ok := c.pass.TypesInfo.Types[x.Fun]; ok && (tv.IsType() || tv.IsBuiltin()) {
			return true // conversion, new, len, ...
		}
		for _, a := range x.Args {
			c.checkExpr(a, "call argument copies")
		}
	case *ast.CompositeLit:
		for _, e := range x.Elts {
			if kv, ok := e.(*ast.KeyValueExpr); ok {
				e = kv.Value
			}
			c.checkExpr(e, "composite literal copies")
		}
	}
	return true
}

func (c *checker) fields(fl *ast.FieldList, what string) {
	if fl == nil {
		return
	}
	for _, f := range fl.List {
		t := c.pass.TypesInfo.TypeOf(f.Type)
		c.check(f.Type, t, what+" passes by value")
	}
}

// checkExpr reports e when it reads an existing value that holds a lock.
func (c *checker) checkExpr(e ast.Expr, what string) {
	switch x := ast.Unparen(e).(type) {
	case *ast.CompositeLit, *ast.CallExpr, *ast.FuncLit, *ast.BasicLit:
		return // a fresh value
	case *ast.UnaryExpr:
		return // &x and friends do not copy
	case *ast.Ident:
		if x.Name == "_" || x.Name == "nil" {
			return
		}
	}
	c.check(e, c.pass.TypesInfo.TypeOf(e), what)
}

func (c *checker) check(at ast.Node, t types.Type, what string) {
	if t == nil {
		return
	}
	path := c.lockPath(t, 0)
	if path == "" {
		return
	}
	if c.pass.HasLineAnnotation(c.file, at.Pos(), analyzer.AnnSafeIgnore) {
		return
	}
	c.pass.Report(analyzer.Issue{
		Analyzer: "copylock",
		Pos:      c.pass.Fset.Position(at.Pos()),
		Message:  fmt.Sprintf("%s a value containing %s", what, path),
		Hint:     "use a pointer (*T) so every user shares the same lock",
	})
}

// lockPath returns the lock type found inside t ("sync.Mutex"), or "".
func (c *checker) lockPath(t types.Type, depth int) string {
	if depth > 8 {
		return ""
	}
	if p, ok := c.memo[t]; ok {
		return p
	}
	c.memo[t] = "" // cycle guard
	var p string
	if isLock(t) {
		p = typeName(t)
	} else {
		switch u := t.Underlying().(type) {
		case *types.Struct:
			for i := 0; i < u.NumFields() && p == ""; i++ {
				p = c.lockPath(u.Field(i).Type(), depth+1)
			}
		case *types.Array:
			p = c.lockPath(u.Elem(), depth+1)
		}
	}
	// Name the public sync type (sync.WaitGroup, atomic.Int64) rather than
	// the guard field hidden inside it.
	if p != "" && (isNamedIn(t, "sync") || isNamedIn(t, "sync/atomic")) {
		p = typeName(t)
	}
	c.memo[t] = p
	return p
}

// isLock reports whether *t has Lock and Unlock methods while t itself does
// not: the shape of sync.Mutex and of the noCopy guard.
func isLock(t types.Type) bool {
	if _, ok := t.(*types.Named); !ok {
		return false
	}
	ptr := types.NewMethodSet(types.NewPointer(t))
	return hasMethod(ptr, "Lock") && hasMethod(ptr, "Unlock") && !hasMethod(types.NewMethodSet(t), "Lock")
}

func hasMethod(ms *types.MethodSet, name string) bool {
	for i := 0; i < ms.Len(); i++ {
		if ms.At(i).Obj().Name() == name {
			return true
		}
	}
	return false
}

func isNamedIn(t types.Type, pkg string) bool {
	n, ok := t.(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == pkg
}

func typeName(t types.Type) string {
	n, ok := t.(*types.Named)
	if !ok {
		return t.String()
	}
	if n.Obj().Pkg() == nil {
		return n.Obj().Name()
	}
	return n.Obj().Pkg().Name() + "." + n.Obj().Name()
}

// Package rowserr reports *sql.Rows iterated without a rows.Err() check.
//
//	rows, err := db.QueryContext(ctx, q)
//	...
//	for rows.Next() { ... }
//	return nil // a dropped connection mid-iteration looks like "no more rows"
//
// rows.Next returns false both at the end of the result set and on an
// error; only rows.Err tells them apart. The rule reports a *sql.Rows
// variable that is used only through Next, Scan, Close, Columns and
// ColumnTypes in its function and never through Err. A variable that is
// returned, passed to another function or stored is skipped: someone else
// may check it. This is the rowserrcheck linter without SSA.
//
// Opt out with `// safe-ignore: <reason>` on the assignment line.
package rowserr

import (
	_ "embed"
	"go/ast"
	"go/types"

	"github.com/kidandcat/gox/pkg/analyzer"
)

//go:embed rowserr.md
var explanation string // global-ok: populated at compile time by //go:embed, never mutated

func init() {
	analyzer.Register(&analyzer.Analyzer{
		Name:        "rowserr",
		Doc:         "*sql.Rows iterated without checking rows.Err()",
		Explanation: explanation,
		Run:         run,
	})
}

func run(pass *analyzer.Pass) {
	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			checkFunc(pass, file, fn.Body)
		}
	}
}

type state struct {
	def     *ast.Ident
	checked bool // rows.Err seen
	escapes bool // used in a way the rule cannot follow
	next    bool // iterated
}

func checkFunc(pass *analyzer.Pass, file *ast.File, body *ast.BlockStmt) {
	vars := map[types.Object]*state{}
	var order []types.Object
	ast.Inspect(body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		if obj := pass.TypesInfo.Defs[id]; obj != nil && isRows(obj.Type()) {
			if _, seen := vars[obj]; !seen {
				vars[obj] = &state{def: id}
				order = append(order, obj)
			}
		}
		return true
	})
	if len(vars) == 0 {
		return
	}
	// Classify every use by its parent node.
	var stack []ast.Node
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if id, ok := n.(*ast.Ident); ok {
			if st := vars[pass.TypesInfo.Uses[id]]; st != nil && len(stack) > 0 {
				classify(st, id, stack[len(stack)-1])
			}
		}
		stack = append(stack, n)
		return true
	})
	for _, obj := range order {
		st := vars[obj]
		if st.checked || st.escapes || !st.next {
			continue
		}
		if pass.HasLineAnnotation(file, st.def.Pos(), analyzer.AnnSafeIgnore) {
			continue
		}
		pass.Report(analyzer.Issue{
			Analyzer: "rowserr",
			Pos:      pass.Fset.Position(st.def.Pos()),
			Message:  st.def.Name + ".Next() loop without " + st.def.Name + ".Err(); an error mid-iteration looks like the end of the result set",
			Hint:     "after the loop: if err := " + st.def.Name + ".Err(); err != nil { return err }",
		})
	}
}

func classify(st *state, id *ast.Ident, parent ast.Node) {
	switch p := parent.(type) {
	case *ast.SelectorExpr:
		if p.X != id {
			st.escapes = true
			return
		}
		switch p.Sel.Name {
		case "Err":
			st.checked = true
		case "Next", "NextResultSet":
			st.next = true
		case "Scan", "Close", "Columns", "ColumnTypes":
		default:
			st.escapes = true
		}
	case *ast.AssignStmt:
		// `rows, err = db.Query(...)` re-assigns; reading rows on the right
		// side hands it to someone else.
		for _, r := range p.Rhs {
			if r == id {
				st.escapes = true
			}
		}
	case *ast.BinaryExpr:
		// rows != nil
	default:
		st.escapes = true
	}
}

func isRows(t types.Type) bool {
	p, ok := t.(*types.Pointer)
	if !ok {
		return false
	}
	n, ok := p.Elem().(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "database/sql" && n.Obj().Name() == "Rows"
}

// Package exhaustive enforces switch exhaustiveness over enums and sealed
// interfaces defined in the analyzed packages.
//
// An *enum* is a named integer type with two or more distinct constant
// values, declared in the package under analysis or in another package of
// the same module. Constants that share a value are one variant. Enums from
// the standard library and from other modules are not checked.
//
// A *sealed interface* is one whose method set contains at least one
// unexported method, meaning its concrete implementations must live in the
// same package as the interface. Type switches over such an interface must
// list every concrete implementer.
//
// To accept a partial switch, add `default: ...` AND a `// exhaustive-ok:`
// annotation on the same line as `default`.
package exhaustive

import (
	_ "embed"
	"fmt"
	"go/ast"
	"go/types"
	"strings"

	"github.com/kidandcat/gox/pkg/analyzer"
)

//go:embed exhaustive.md
var explanation string // global-ok: populated at compile time by //go:embed, never mutated

func init() {
	analyzer.Register(&analyzer.Analyzer{
		Name:        "exhaustive",
		Doc:         "requires switch exhaustiveness over iota enums and sealed interfaces",
		Explanation: explanation,
		Run:         run,
	})
}

func run(pass *analyzer.Pass) {
	sealed := collectSealedImpls(pass.Pkg)
	enums := map[string][]*types.Const{}

	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			switch s := n.(type) {
			case *ast.SwitchStmt:
				checkValueSwitch(pass, file, s, enums)
			case *ast.TypeSwitchStmt:
				checkTypeSwitch(pass, file, s, sealed)
			}
			return true
		})
	}
}

func checkValueSwitch(pass *analyzer.Pass, file *ast.File, s *ast.SwitchStmt, enums map[string][]*types.Const) {
	if s.Tag == nil {
		return // bare switch — boolean form, not enum
	}
	tagType := pass.TypesInfo.TypeOf(s.Tag)
	if tagType == nil {
		return
	}
	named, ok := tagType.(*types.Named)
	if !ok {
		return
	}
	key := typeKey(named)
	members, ok := enums[key]
	if !ok {
		members = enumMembers(pass, named)
		enums[key] = members
	}
	if len(members) == 0 {
		return
	}
	have, hasDefault := caseValueNames(pass, s.Body)
	if hasDefault && hasAnnotationOnSwitch(pass, file, s) {
		return
	}
	missing := missingNames(members, have)
	if len(missing) == 0 {
		return
	}
	pass.Report(analyzer.Issue{
		Analyzer: "exhaustive",
		Pos:      pass.Fset.Position(s.Switch),
		Message:  fmt.Sprintf("non-exhaustive switch on enum %s: missing %s", named.Obj().Name(), strings.Join(missing, ", ")),
		Hint:     "add cases for every constant, or accept partial coverage with `default: // exhaustive-ok: <reason>`",
	})
}

func checkTypeSwitch(pass *analyzer.Pass, file *ast.File, s *ast.TypeSwitchStmt, sealed map[string][]types.Type) {
	// Get the interface type being switched on.
	var ifaceType types.Type
	switch a := s.Assign.(type) {
	case *ast.AssignStmt:
		if len(a.Rhs) == 1 {
			if ta, ok := a.Rhs[0].(*ast.TypeAssertExpr); ok {
				ifaceType = pass.TypesInfo.TypeOf(ta.X)
			}
		}
	case *ast.ExprStmt:
		if ta, ok := a.X.(*ast.TypeAssertExpr); ok {
			ifaceType = pass.TypesInfo.TypeOf(ta.X)
		}
	}
	if ifaceType == nil {
		return
	}
	named, ok := ifaceType.(*types.Named)
	if !ok {
		return
	}
	if _, ok := named.Underlying().(*types.Interface); !ok {
		return
	}
	key := typeKey(named)
	impls, ok := sealed[key]
	if !ok {
		return
	}
	have, hasDefault := caseTypes(pass, s.Body)
	if hasDefault && hasAnnotationOnTypeSwitch(pass, file, s) {
		return
	}
	missing := missingTypes(impls, have)
	if len(missing) == 0 {
		return
	}
	pass.Report(analyzer.Issue{
		Analyzer: "exhaustive",
		Pos:      pass.Fset.Position(s.Switch),
		Message:  fmt.Sprintf("non-exhaustive type switch on sealed interface %s: missing %s", named.Obj().Name(), strings.Join(missing, ", ")),
		Hint:     "add a case for every implementer, or accept partial coverage with `default: // exhaustive-ok: <reason>`",
	})
}

// enumMembers returns the constants of named when it is an integer enum
// (two or more distinct values) declared in the package under analysis or
// in another package of the same module. Enums from the standard library
// and from third-party modules are ignored: a switch on reflect.Kind is not
// what this rule is for, and dependency enums would light up existing code.
//
// Constants that share a value (const Default = Off) are one variant. The
// caller reports a value as missing only when none of its names is covered.
func enumMembers(pass *analyzer.Pass, named *types.Named) []*types.Const {
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return nil
	}
	pkg := obj.Pkg()
	if pkg != pass.Pkg && !inModule(
		/* pkgPath */ pkg.Path(),
		/* modulePath */ pass.ModulePath,
	) {
		return nil
	}
	basic, ok := named.Underlying().(*types.Basic)
	if !ok || basic.Info()&types.IsInteger == 0 {
		return nil
	}
	scope := pkg.Scope()
	var out []*types.Const
	for _, name := range scope.Names() {
		c, isConst := scope.Lookup(name).(*types.Const)
		if !isConst || c.Val() == nil {
			continue
		}
		ct, isNamed := c.Type().(*types.Named)
		if !isNamed || ct.Obj() != obj {
			continue
		}
		out = append(out, c)
	}
	if distinctValues(out) < 2 {
		return nil
	}
	return out
}

func inModule(pkgPath, modulePath string) bool {
	if modulePath == "" || pkgPath == "" {
		return false
	}
	return pkgPath == modulePath || strings.HasPrefix(pkgPath, modulePath+"/")
}

func distinctValues(members []*types.Const) int {
	seen := map[string]struct{}{}
	for _, c := range members {
		seen[valueKey(c)] = struct{}{}
	}
	return len(seen)
}

func valueKey(c *types.Const) string {
	if c == nil || c.Val() == nil {
		return ""
	}
	return c.Val().ExactString()
}

// collectSealedImpls scans the package for interface types with at least one
// unexported method, and finds every concrete type in the same package that
// implements them.
func collectSealedImpls(pkg *types.Package) map[string][]types.Type {
	out := map[string][]types.Type{}
	if pkg == nil {
		return out
	}
	scope := pkg.Scope()
	var ifaces []*types.Named
	var concretes []*types.Named
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		tn, ok := obj.(*types.TypeName)
		if !ok {
			continue
		}
		named, ok := tn.Type().(*types.Named)
		if !ok {
			continue
		}
		if iface, ok := named.Underlying().(*types.Interface); ok {
			if hasUnexportedMethod(iface) {
				ifaces = append(ifaces, named)
			}
			continue
		}
		concretes = append(concretes, named)
	}
	for _, iface := range ifaces {
		ut := iface.Underlying().(*types.Interface) // safe-ignore: collected only when Underlying is Interface above
		key := typeKey(iface)
		for _, c := range concretes {
			// Prefer the value form if it satisfies the interface; otherwise
			// fall back to the pointer form. We don't list both — that would
			// produce false positives when the case clause uses one form.
			if types.Implements(c, ut) {
				out[key] = append(out[key], c)
			} else if types.Implements(types.NewPointer(c), ut) {
				out[key] = append(out[key], types.NewPointer(c))
			}
		}
	}
	return out
}

func hasUnexportedMethod(iface *types.Interface) bool {
	for i := 0; i < iface.NumMethods(); i++ {
		if !iface.Method(i).Exported() {
			return true
		}
	}
	return false
}

func typeKey(n *types.Named) string {
	obj := n.Obj()
	if obj.Pkg() == nil {
		return obj.Name()
	}
	return obj.Pkg().Path() + "." + obj.Name()
}

func caseValueNames(pass *analyzer.Pass, body *ast.BlockStmt) (map[string]bool, bool) {
	have := map[string]bool{}
	hasDefault := false
	for _, stmt := range body.List {
		cc, ok := stmt.(*ast.CaseClause)
		if !ok {
			continue
		}
		if cc.List == nil {
			hasDefault = true
			continue
		}
		for _, e := range cc.List {
			id, ok := unwrapIdent(e)
			if !ok {
				continue
			}
			obj := pass.TypesInfo.ObjectOf(id)
			if obj == nil {
				continue
			}
			if c, ok := obj.(*types.Const); ok {
				have[c.Name()] = true
			}
		}
	}
	return have, hasDefault
}

func unwrapIdent(e ast.Expr) (*ast.Ident, bool) {
	switch v := e.(type) {
	case *ast.Ident:
		return v, true
	case *ast.SelectorExpr:
		return v.Sel, true
	}
	return nil, false
}

func caseTypes(pass *analyzer.Pass, body *ast.BlockStmt) (map[string]bool, bool) {
	have := map[string]bool{}
	hasDefault := false
	for _, stmt := range body.List {
		cc, ok := stmt.(*ast.CaseClause)
		if !ok {
			continue
		}
		if cc.List == nil {
			hasDefault = true
			continue
		}
		for _, e := range cc.List {
			t := pass.TypesInfo.TypeOf(e)
			if t == nil {
				continue
			}
			have[typeString(t)] = true
		}
	}
	return have, hasDefault
}

func typeString(t types.Type) string { return t.String() }

func missingNames(members []*types.Const, have map[string]bool) []string {
	covered := map[string]bool{}
	for _, c := range members {
		if have[c.Name()] {
			covered[valueKey(c)] = true
		}
	}
	reported := map[string]bool{}
	var out []string
	for _, c := range members {
		k := valueKey(c)
		if covered[k] || reported[k] {
			continue
		}
		reported[k] = true
		out = append(out, c.Name())
	}
	return out
}

func missingTypes(impls []types.Type, have map[string]bool) []string {
	var out []string
	for _, t := range impls {
		if !have[typeString(t)] {
			out = append(out, typeString(t))
		}
	}
	return out
}

func hasAnnotationOnSwitch(pass *analyzer.Pass, file *ast.File, s *ast.SwitchStmt) bool {
	return defaultCaseAnnotation(pass, file, s.Body)
}
func hasAnnotationOnTypeSwitch(pass *analyzer.Pass, file *ast.File, s *ast.TypeSwitchStmt) bool {
	return defaultCaseAnnotation(pass, file, s.Body)
}

func defaultCaseAnnotation(pass *analyzer.Pass, file *ast.File, body *ast.BlockStmt) bool {
	for _, stmt := range body.List {
		cc, ok := stmt.(*ast.CaseClause)
		if !ok || cc.List != nil {
			continue
		}
		if pass.HasLineAnnotation(file, cc.Pos(), analyzer.AnnExhaustiveOK) {
			return true
		}
	}
	return false
}

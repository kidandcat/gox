// Package loader parses and type-checks Go packages using only the standard
// library. It deliberately avoids golang.org/x/tools/go/packages so gox has
// zero external dependencies.
//
// The loader shells out to
//
//	go list -json -e -export -compiled -deps -test
//
// to discover the import graph and compile export data, then drives go/parser
// + go/types directly. List is split from LoadPackage so callers can cheaply
// enumerate packages (and their file metadata) without paying for parse +
// type-check on cache hits.
//
// -export is required so type-aware rules work on a fresh clone: importer.Default
// only finds compiler .a files under GOROOT/GOPATH, not the module build cache,
// so local imports go blind unless something has already run `go build`.
// go list -export writes export data as a side effect and returns its path.
// -compiled prefers the files the compiler actually type-checks (cgo output).
// -test includes *_test.go via the package's test variants. -deps fills the
// export map for imports of the matched packages (those packages are not
// themselves analyzed).
package loader

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// PackageInfo is the lightweight package metadata produced by List.
type PackageInfo struct {
	ImportPath string
	Dir        string
	GoFiles    []string // paths as reported by `go list` (basename or absolute)

	// ForTest is the package under test when this info describes a test
	// variant (`foo [foo.test]` or `foo_test [foo.test]`). Empty for a
	// regular package.
	ForTest string

	// ModulePath is the module that contains this package. Empty outside a module.
	ModulePath string

	importMap      map[string]string
	exports        map[string]string
	deps           []string
	stdlib         map[string]bool
	reportTestOnly bool
	// table is the gc importer shared by every package from the same go list.
	// *types.Package values are only identical within one importer.
	table *exportTable
}

// IsStdlib reports whether importPath is a standard-library package, according
// to the Standard field of the `go list` that produced this info. Paths that
// were not part of that list (including the package under analysis, when it
// is not itself standard) report false.
func (p *PackageInfo) IsStdlib(importPath string) bool {
	if p == nil || p.stdlib == nil {
		return false
	}
	return p.stdlib[importPath]
}

// AbsFiles returns the absolute paths of the package's .go files.
func (p *PackageInfo) AbsFiles() []string {
	out := make([]string, len(p.GoFiles))
	for i, name := range p.GoFiles {
		out[i] = resolveFile(
			/* dir */ p.Dir,
			/* name */ name,
		)
	}
	return out
}

// DepExports returns one "importpath=exportfile" entry per transitive
// dependency, sorted. `go list -export` names export files by content hash,
// so the list changes whenever any dependency's exported API changes; it is
// meant to be folded into cache keys.
func (p *PackageInfo) DepExports() []string {
	out := make([]string, 0, len(p.deps))
	for _, d := range p.deps {
		out = append(out, d+"="+p.exports[d])
	}
	sort.Strings(out)
	return out
}

// ShouldReport reports whether an issue in filename should be surfaced.
// Internal test packages type-check production files together with *_test.go
// so tests can see unexported identifiers; those production files are already
// analyzed as the regular package, so only *_test.go findings are kept.
func (p *PackageInfo) ShouldReport(filename string) bool {
	if p == nil || !p.reportTestOnly {
		return true
	}
	return strings.HasSuffix(filename, "_test.go")
}

func resolveFile(dir, name string) string {
	if filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(dir, name)
}

// Package is a fully-parsed and type-checked Go package.
type Package struct {
	Info       *PackageInfo
	Fset       *token.FileSet
	Files      []*ast.File
	Pkg        *types.Package
	TypesInfo  *types.Info
	TypeErrors []error
}

// listEntry mirrors the subset of `go list -json` output we need.
type listEntry struct {
	ImportPath      string
	Dir             string
	Name            string
	GoFiles         []string
	CompiledGoFiles []string
	Export          string
	ImportMap       map[string]string
	Deps            []string
	DepOnly         bool
	ForTest         string
	Standard        bool
	Module          *struct {
		Path string
	}
	Error *struct{ Err string }
}

func (e listEntry) files() []string {
	if len(e.CompiledGoFiles) > 0 {
		return e.CompiledGoFiles
	}
	return e.GoFiles
}

// isTestMain reports the synthetic `foo.test` main generated for `go test`.
// Its sources live in the build cache and must not be analyzed.
func (e listEntry) isTestMain() bool {
	return e.Name == "main" && strings.HasSuffix(e.ImportPath, ".test") && !strings.Contains(e.ImportPath, "[")
}

// PackageError is a package that `go list` could not load (syntax error,
// type error, missing dependency, ...). Such a package is not analyzed.
type PackageError struct {
	ImportPath string
	Err        string
}

func (e PackageError) Error() string { return e.ImportPath + ": " + e.Err }

// List enumerates the packages matched by the patterns and returns their
// file metadata without parsing them. Dependencies are queried only to
// populate export-data paths for the type checker.
//
// Packages that fail to load are printed to stderr and skipped. Callers that
// must not silently pass on broken code should use ListWithErrors.
func List(patterns ...string) ([]*PackageInfo, error) {
	infos, pkgErrs, err := ListWithErrors(patterns...)
	for _, pe := range pkgErrs {
		fmt.Fprintf(os.Stderr, "gox: %s: %s\n", pe.ImportPath, pe.Err)
	}
	return infos, err
}

// ListWithErrors is List, but returns the packages that failed to load
// instead of printing them, so the caller can fail closed.
func ListWithErrors(patterns ...string) ([]*PackageInfo, []PackageError, error) {
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	args := append([]string{"list", "-json", "-e", "-export", "-compiled", "-deps", "-test"}, patterns...)
	// A stuck `go list` (module download, dead proxy) should fail the run
	// instead of hanging the hook. Cold lists on large modules can take
	// minutes; ten minutes is the ceiling, not the expected duration.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Stderr = os.Stderr
	out, runErr := cmd.Output()
	if ctx.Err() != nil {
		return nil, nil, fmt.Errorf("go list: %w", ctx.Err())
	}
	if runErr != nil {
		return nil, nil, fmt.Errorf("go list: %w", runErr)
	}

	exports := map[string]string{}
	table := newExportTable(exports)
	stdlib := map[string]bool{}
	var infos []*PackageInfo
	var pkgErrs []PackageError
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var e listEntry
		if decErr := dec.Decode(&e); decErr != nil {
			return nil, nil, fmt.Errorf("decode go list: %w", decErr)
		}
		if e.Export != "" {
			exports[e.ImportPath] = e.Export
		}
		if e.Standard {
			stdlib[e.ImportPath] = true
		}
		if e.Error != nil {
			pkgErrs = append(pkgErrs, PackageError{ImportPath: e.ImportPath, Err: e.Error.Err})
			continue
		}
		if e.DepOnly || e.isTestMain() {
			continue
		}
		files := e.files()
		if len(files) == 0 {
			continue
		}
		modulePath := ""
		if e.Module != nil {
			modulePath = e.Module.Path
		}
		info := &PackageInfo{
			ImportPath:     e.ImportPath,
			Dir:            e.Dir,
			GoFiles:        files,
			ForTest:        e.ForTest,
			ModulePath:     modulePath,
			importMap:      e.ImportMap,
			exports:        exports,
			deps:           e.Deps,
			stdlib:         stdlib,
			reportTestOnly: e.ForTest != "" && !strings.HasSuffix(e.Name, "_test"),
			table:          table,
		}
		infos = append(infos, info)
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].ImportPath < infos[j].ImportPath })
	return infos, pkgErrs, nil
}

// LoadPackage parses and type-checks a single package.
func LoadPackage(info *PackageInfo) (*Package, error) {
	fset := token.NewFileSet()
	files := make([]*ast.File, 0, len(info.GoFiles))
	for _, name := range info.GoFiles {
		path := resolveFile(
			/* dir */ info.Dir,
			/* name */ name,
		)
		f, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if parseErr != nil {
			return nil, fmt.Errorf("parse %s: %w", path, parseErr)
		}
		files = append(files, f)
	}

	tInfo := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Implicits:  map[ast.Node]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Scopes:     map[ast.Node]*types.Scope{},
	}

	var typeErrs []error
	conf := &types.Config{
		Importer: newExportImporter(info.table, info.importMap),
		Error:    func(err error) { typeErrs = append(typeErrs, err) },
	}
	pkg, checkErr := conf.Check(checkPath(info.ImportPath), fset, files, tInfo)
	if checkErr != nil && !errors.As(checkErr, new(types.Error)) {
		return nil, checkErr
	}

	return &Package{
		Info:       info,
		Fset:       fset,
		Files:      files,
		Pkg:        pkg,
		TypesInfo:  tInfo,
		TypeErrors: typeErrs,
	}, nil
}

// checkPath strips the ` [foo.test]` suffix that `go list -test` appends so
// go/types sees a clean import path.
func checkPath(importPath string) string {
	if i := strings.Index(importPath, " ["); i >= 0 {
		return importPath[:i]
	}
	return importPath
}

// exportTable is one gc importer for the export files of a single `go list`.
// Packages are decoded once and then reused. They must stay in this importer:
// a *types.Package copied into another importer has a different type identity,
// so time.Duration inside context no longer matches time.Duration from time
// and go/types rejects a program that compiles.
//
// A non-empty ImportMap (vendoring, the synthetic test main) gets a private
// importer instead, so one package's path rewriting cannot poison the rest.
type exportTable struct {
	mu      sync.Mutex
	exports map[string]string
	gc      types.Importer
}

func newExportTable(exports map[string]string) *exportTable {
	if exports == nil {
		exports = map[string]string{}
	}
	t := &exportTable{exports: exports}
	// Imported objects live in this FileSet, not in the FileSet of the package
	// under analysis. Analyzers report positions of the package's own AST.
	t.gc = importer.ForCompiler(token.NewFileSet(), "gc", func(path string) (io.ReadCloser, error) {
		filename := exports[path]
		if filename == "" {
			return nil, fmt.Errorf("no export data for %s", path)
		}
		return os.Open(filename)
	})
	return t
}

// importPath decodes path once. gcimporter mutates its package map, so
// concurrent LoadPackage calls must take turns. The lock covers only the
// import, not the type-check of the package body.
func (t *exportTable) importPath(path string) (*types.Package, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.gc.Import(path)
}

// exportImporter resolves imports from `go list -export` data. importer.Default
// cannot see the module build cache, so type-aware rules would go blind on a
// fresh clone (or any unbuilt local package) without this.
type exportImporter struct {
	table    *exportTable
	private  types.Importer
	fallback types.Importer
}

func newExportImporter(table *exportTable, importMap map[string]string) types.Importer {
	if table == nil {
		table = newExportTable(nil)
	}
	if len(importMap) > 0 {
		return newPrivateImporter(table.exports, importMap)
	}
	return &exportImporter{
		table:    table,
		fallback: importer.Default(),
	}
}

func newPrivateImporter(exports, importMap map[string]string) types.Importer {
	lookup := func(path string) (io.ReadCloser, error) {
		if mapped, ok := importMap[path]; ok {
			path = mapped
		}
		filename := exports[path]
		if filename == "" {
			return nil, fmt.Errorf("no export data for %s", path)
		}
		return os.Open(filename)
	}
	return &exportImporter{
		private:  importer.ForCompiler(token.NewFileSet(), "gc", lookup),
		fallback: importer.Default(),
	}
}

func (e *exportImporter) Import(path string) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	if e.private != nil {
		pkg, err := e.private.Import(path)
		if err != nil {
			return e.fallback.Import(path)
		}
		return pkg, nil
	}
	if e.table.exports[path] == "" {
		return e.fallback.Import(path)
	}
	pkg, err := e.table.importPath(path)
	if err != nil {
		return e.fallback.Import(path)
	}
	return pkg, nil
}

// Load is a convenience wrapper that lists and fully loads every package
// matched by patterns. Use List + LoadPackage when you want per-package
// control (e.g. to consult a cache before parsing).
func Load(patterns ...string) ([]*Package, error) {
	infos, listErr := List(patterns...)
	if listErr != nil {
		return nil, listErr
	}
	pkgs := make([]*Package, 0, len(infos))
	for _, info := range infos {
		p, loadErr := LoadPackage(info)
		if loadErr != nil {
			return nil, fmt.Errorf("%s: %w", info.ImportPath, loadErr)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, nil
}

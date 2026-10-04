package analyzer

import (
	"fmt"
	"go/ast"
	"os"
	"runtime"
	"sort"
	"sync"

	"github.com/kidandcat/gox/pkg/loader"
)

// RunOptions controls the behavior of Run.
type RunOptions struct {
	UseCache bool
	// CacheGet/Put are injected to avoid the analyzer package depending on
	// pkg/cache (which itself imports pkg/analyzer for the Issue type). The
	// CLI wires them up.
	CacheKey func(info *loader.PackageInfo) (string, error)
	CacheGet func(key string) ([]Issue, bool)
	CachePut func(key string, issues []Issue) error

	// Workers caps the number of packages processed in parallel. Zero means
	// runtime.GOMAXPROCS(0).
	Workers int
}

// Stats reports cache hit/miss counts for a Run.
type Stats struct {
	PackagesTotal int
	CacheHits     int
	CacheMisses   int
	// LoadErrors counts packages that could not be listed or parsed, and
	// packages go/types rejected. A list or parse failure was not analyzed.
	// A type-check failure was analyzed with partial info and was not cached.
	// Either way the report is incomplete and callers should fail closed.
	LoadErrors int
}

// pkgResult is the per-package output sent from worker → collector.
type pkgResult struct {
	issues  []Issue
	hit     bool
	loadErr bool
}

// Run loads (or cache-replays) every package matched by patterns and applies
// every registered analyzer. Packages are processed in parallel.
func Run(patterns []string, analyzers []*Analyzer, opts RunOptions) ([]Issue, Stats, error) {
	infos, pkgErrs, listErr := loader.ListWithErrors(patterns...)
	if listErr != nil {
		return nil, Stats{}, listErr
	}
	for _, pe := range pkgErrs {
		fmt.Fprintf(os.Stderr, "gox: %s: %s\n", pe.ImportPath, pe.Err)
	}

	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	if workers > len(infos) {
		workers = len(infos)
	}
	if workers < 1 {
		workers = 1
	}

	jobs := make(chan *loader.PackageInfo)
	results := make(chan pkgResult)

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for info := range jobs {
				results <- processPackage( /* info */ info /* analyzers */, analyzers, opts)
			}
		}()
	}

	go func() {
		for _, info := range infos {
			jobs <- info
		}
		close(jobs)
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	stats := Stats{PackagesTotal: len(infos), LoadErrors: len(pkgErrs)}
	var issues []Issue
	for r := range results {
		issues = append(issues, r.issues...)
		if r.loadErr {
			stats.LoadErrors++
		}
		if r.hit {
			stats.CacheHits++
		} else {
			stats.CacheMisses++
		}
	}

	sort.Slice(issues, func(i, j int) bool {
		a, b := issues[i].Pos, issues[j].Pos
		if a.Filename != b.Filename {
			return a.Filename < b.Filename
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Column != b.Column {
			return a.Column < b.Column
		}
		return issues[i].Analyzer < issues[j].Analyzer
	})
	return issues, stats, nil
}

// processPackage runs the cache lookup → parse → type-check → analyze →
// cache-put cycle for a single package. Safe to call concurrently from
// multiple goroutines because every analyzer.Pass it builds is local.
func processPackage(info *loader.PackageInfo, analyzers []*Analyzer, opts RunOptions) pkgResult {
	// Cache lookup
	var cacheKey string
	if opts.UseCache && opts.CacheKey != nil && opts.CacheGet != nil {
		key, keyErr := opts.CacheKey(info)
		if keyErr == nil {
			cacheKey = key
			if cached, ok := opts.CacheGet(key); ok {
				return pkgResult{issues: cached, hit: true}
			}
		} else {
			fmt.Fprintf(os.Stderr, "gox cache: key %s: %v\n", info.ImportPath, keyErr)
		}
	}

	// Cache miss → parse + type-check + analyze
	pkg, loadErr := loader.LoadPackage(info)
	if loadErr != nil {
		fmt.Fprintf(os.Stderr, "gox: %s: %v\n", info.ImportPath, loadErr)
		return pkgResult{hit: false, loadErr: true}
	}

	// go list accepted the package but go/types did not. Analysis below sees
	// a partial type info; count it as not fully checked and do not cache it.
	partial := len(pkg.TypeErrors) > 0
	if partial {
		fmt.Fprintf(os.Stderr, "gox: %s: type check incomplete (%d error(s)); results are partial\n", info.ImportPath, len(pkg.TypeErrors))
		fmt.Fprintf(os.Stderr, "gox: %s: %v\n", info.ImportPath, pkg.TypeErrors[0])
	}

	// Precompute generated-file set so analyzers can still see all symbols
	// (needed for cross-file type info) but issues reported from generated
	// files are dropped. ast.IsGenerated applies the exact Go convention
	// (marker before the package clause, any header length) on the already
	// parsed files, so no file is re-opened.
	generated := map[string]bool{}
	for _, f := range pkg.Files {
		if ast.IsGenerated(f) {
			if tf := pkg.Fset.File(f.Pos()); tf != nil {
				generated[tf.Name()] = true
			}
		}
	}

	// Internal test variants re-typecheck production files so *_test.go can
	// see unexported names. Those production files are already analyzed as
	// the regular package; don't walk them again.
	files := pkg.Files
	if info.ForTest != "" {
		kept := make([]*ast.File, 0, len(pkg.Files))
		for _, f := range pkg.Files {
			name := ""
			if tf := pkg.Fset.File(f.Pos()); tf != nil {
				name = tf.Name()
			}
			if info.ShouldReport(name) {
				kept = append(kept, f)
			}
		}
		files = kept
	}

	var pkgIssues []Issue
	report := func(i Issue) {
		if generated[i.Pos.Filename] {
			return
		}
		if !info.ShouldReport(i.Pos.Filename) {
			return
		}
		pkgIssues = append(pkgIssues, i)
	}
	pass := &Pass{
		Fset:       pkg.Fset,
		Pkg:        pkg.Pkg,
		TypesInfo:  pkg.TypesInfo,
		Files:      files,
		Report:     report,
		ModulePath: info.ModulePath,
		IsStdlib:   info.IsStdlib,
	}
	for _, a := range analyzers {
		a.Run(pass)
	}

	if !partial && opts.UseCache && opts.CachePut != nil && cacheKey != "" {
		if putErr := opts.CachePut(cacheKey, pkgIssues); putErr != nil {
			fmt.Fprintf(os.Stderr, "gox cache: put %s: %v\n", info.ImportPath, putErr)
		}
	}
	return pkgResult{issues: pkgIssues, hit: false, loadErr: partial}
}

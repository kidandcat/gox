package loader_test

import (
	"strings"
	"testing"

	"github.com/kidandcat/gox/pkg/loader"
)

func TestList_includesExternalTests(t *testing.T) {
	infos, err := loader.List("github.com/kidandcat/gox/pkg/analyzers/errcheck")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var sawPkg, sawTest, sawTestMain bool
	for _, info := range infos {
		switch {
		case info.ImportPath == "github.com/kidandcat/gox/pkg/analyzers/errcheck":
			sawPkg = true
		case strings.Contains(info.ImportPath, "errcheck_test"):
			sawTest = true
			if info.ForTest == "" {
				t.Fatalf("test package %s missing ForTest", info.ImportPath)
			}
			var hasTestFile bool
			for _, f := range info.AbsFiles() {
				if strings.HasSuffix(f, "_test.go") {
					hasTestFile = true
				}
			}
			if !hasTestFile {
				t.Fatalf("test package %s has no *_test.go: %v", info.ImportPath, info.AbsFiles())
			}
		case strings.HasSuffix(info.ImportPath, ".test") && !strings.Contains(info.ImportPath, "["):
			sawTestMain = true
		}
	}
	if !sawPkg {
		t.Fatal("List did not return the production package")
	}
	if !sawTest {
		t.Fatal("List did not return the external test package (go list -test)")
	}
	if sawTestMain {
		t.Fatal("List returned the synthetic foo.test main; it must be skipped")
	}
}

func TestLoadPackage_localImportHasTypes(t *testing.T) {
	// errcheck imports pkg/analyzer (a local package). Without -export,
	// importer.Default() cannot see the module build cache and TypesInfo
	// goes blind. After this load, selections on analyzer types must resolve.
	infos, err := loader.List("github.com/kidandcat/gox/pkg/analyzers/errcheck")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var info *loader.PackageInfo
	for _, i := range infos {
		if i.ImportPath == "github.com/kidandcat/gox/pkg/analyzers/errcheck" && i.ForTest == "" {
			info = i
			break
		}
	}
	if info == nil {
		t.Fatal("production errcheck package not listed")
	}
	pkg, err := loader.LoadPackage(info)
	if err != nil {
		t.Fatalf("LoadPackage: %v", err)
	}
	if len(pkg.TypeErrors) != 0 {
		t.Fatalf("type errors on a known-good package: %v", pkg.TypeErrors)
	}
	if pkg.Pkg == nil {
		t.Fatal("nil types.Package")
	}
	if pkg.Pkg.Scope().Lookup("Run") == nil && pkg.TypesInfo == nil {
		t.Fatal("missing type info")
	}
	// Confirm an import of the local analyzer package resolved.
	foundAnalyzer := false
	for _, imp := range pkg.Pkg.Imports() {
		if imp.Path() == "github.com/kidandcat/gox/pkg/analyzer" {
			foundAnalyzer = true
			if !imp.Complete() {
				t.Fatal("imported pkg/analyzer is incomplete — export data missing")
			}
		}
	}
	if !foundAnalyzer {
		t.Fatal("pkg/analyzer import missing from type-checked package")
	}
}

func TestAbsFiles_joinsRelative(t *testing.T) {
	infos, err := loader.List("github.com/kidandcat/gox/pkg/loader")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var info *loader.PackageInfo
	for _, i := range infos {
		if i.ImportPath == "github.com/kidandcat/gox/pkg/loader" && i.ForTest == "" {
			info = i
			break
		}
	}
	if info == nil {
		t.Fatal("loader package not listed")
	}
	files := info.AbsFiles()
	if len(files) == 0 {
		t.Fatal("no files")
	}
	for _, f := range files {
		if !strings.HasPrefix(f, "/") {
			t.Fatalf("AbsFiles returned non-absolute path %q", f)
		}
		if strings.HasSuffix(f, "_test.go") {
			t.Fatalf("production package should not include test files, got %q", f)
		}
	}
}

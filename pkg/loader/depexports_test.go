package loader_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kidandcat/gox/pkg/loader"
)

func depExportsOf(t *testing.T, importPath string) []string {
	t.Helper()
	infos, err := loader.List("./...")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, info := range infos {
		if info.ImportPath == importPath {
			return info.DepExports()
		}
	}
	t.Fatalf("package %s not listed", importPath)
	return nil
}

func TestDepExports_changesWithDependencyAPI(t *testing.T) {
	dir := t.TempDir()
	// write takes a {name, body} pair.
	write := func(file [2]string) {
		t.Helper()
		name, body := file[0], file[1]
		path := filepath.Join(dir, name)
		if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
			t.Fatal(mkErr)
		}
		if wErr := os.WriteFile(path, []byte(body), 0o644); wErr != nil {
			t.Fatal(wErr)
		}
	}
	write([2]string{"go.mod", "module example.test/m\n\ngo 1.25\n"})
	write([2]string{"b/b.go", "package b\n\nfunc F() {}\n"})
	write([2]string{"a/a.go", "package a\n\nimport \"example.test/m/b\"\n\nfunc G() { b.F() }\n"})
	t.Chdir(dir)

	before := depExportsOf(t, "example.test/m/a")
	if !slices.ContainsFunc(before, func(s string) bool {
		return strings.HasPrefix(s, "example.test/m/b=") && len(s) > len("example.test/m/b=")
	}) {
		t.Fatalf("DepExports of a should include b's export file, got %v", before)
	}
	if !slices.IsSorted(before) {
		t.Errorf("DepExports not sorted: %v", before)
	}

	// Same package, different exported API: callers must get a new key.
	write([2]string{"b/b.go", "package b\n\nimport \"errors\"\n\nfunc F() error { return errors.New(\"x\") }\n"})
	after := depExportsOf(t, "example.test/m/a")
	if slices.Equal(before, after) {
		t.Fatalf("DepExports unchanged after dependency API change: %v", after)
	}
}

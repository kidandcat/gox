package loader_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kidandcat/gox/pkg/loader"
)

// writeModule creates a throwaway module under t.TempDir and chdirs into it.
func writeModule(t *testing.T, files map[string]string) {
	t.Helper()
	dir := t.TempDir()
	files["go.mod"] = "module example.test/m\n\ngo 1.25\n"
	for name, body := range files {
		path := filepath.Join(dir, name)
		if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
			t.Fatal(mkErr)
		}
		if wErr := os.WriteFile(path, []byte(body), 0o644); wErr != nil {
			t.Fatal(wErr)
		}
	}
	t.Chdir(dir)
}

func TestListWithErrors_reportsBrokenPackages(t *testing.T) {
	writeModule(t, map[string]string{
		"ok/ok.go":     "package ok\n\nfunc F() {}\n",
		"syntax/s.go":  "package syntax\n\nfunc F() { (\n",
		"typeerr/t.go": "package typeerr\n\nvar X int = \"s\"\n",
		"missing/m.go": "package missing\n\nimport _ \"example.invalid/nope\"\n",
	})
	infos, pkgErrs, err := loader.ListWithErrors("./...")
	if err != nil {
		t.Fatalf("ListWithErrors: %v", err)
	}
	broken := map[string]bool{}
	for _, pe := range pkgErrs {
		broken[pe.ImportPath] = true
		if pe.Err == "" {
			t.Errorf("%s: empty error text", pe.ImportPath)
		}
	}
	for _, want := range []string{"example.test/m/syntax", "example.test/m/typeerr"} {
		if !broken[want] {
			t.Errorf("expected %s among package errors, got %v", want, pkgErrs)
		}
	}
	if len(pkgErrs) < 3 {
		t.Errorf("expected the missing-import package to be reported too, got %v", pkgErrs)
	}
	var sawOK bool
	for _, info := range infos {
		if broken[info.ImportPath] {
			t.Errorf("broken package %s must not be returned for analysis", info.ImportPath)
		}
		if info.ImportPath == "example.test/m/ok" {
			sawOK = true
		}
	}
	if !sawOK {
		t.Error("healthy package missing from infos")
	}
}

func TestListWithErrors_cleanModuleHasNoErrors(t *testing.T) {
	writeModule(t, map[string]string{
		"a/a.go":      "package a\n\nfunc F() int { return 1 }\n",
		"a/a_test.go": "package a\n\nimport \"testing\"\n\nfunc TestF(t *testing.T) { _ = F() }\n",
	})
	_, pkgErrs, err := loader.ListWithErrors("./...")
	if err != nil {
		t.Fatalf("ListWithErrors: %v", err)
	}
	if len(pkgErrs) != 0 {
		t.Fatalf("unexpected package errors: %v", pkgErrs)
	}
}

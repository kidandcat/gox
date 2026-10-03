package baseline_test

import (
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mentasystems/gox/pkg/analyzer"
	"github.com/mentasystems/gox/pkg/baseline"
)

func TestModuleRoot_findsThisModule(t *testing.T) {
	root, err := baseline.ModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "module github.com/mentasystems/gox") {
		t.Fatalf("ModuleRoot %s is not the gox module", root)
	}
}

func TestBuildFilter_duplicateLinesConsumeOnceEach(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	if err := os.WriteFile(path, []byte("package a\n\nvar x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	issue := func() analyzer.Issue {
		return analyzer.Issue{
			Analyzer: "noglobals",
			Pos:      token.Position{Filename: path, Line: 3, Column: 1},
			Message:  "var x",
		}
	}
	bf, err := baseline.Build(
		[]analyzer.Issue{issue(), issue()},
		/* moduleRoot */ dir,
		/* analyzersVersion */ "test",
	)
	if err != nil {
		t.Fatal(err)
	}
	if bf.IssueCount != 2 {
		t.Fatalf("IssueCount %d, want 2 (line cache must hash both)", bf.IssueCount)
	}
	if left := bf.Filter([]analyzer.Issue{issue(), issue()}, dir); len(left) != 0 {
		t.Fatalf("two baselined copies must consume two identical issues, left %d", len(left))
	}
	if left := bf.Filter([]analyzer.Issue{issue(), issue(), issue()}, dir); len(left) != 1 {
		t.Fatalf("a third copy is new, left %d", len(left))
	}
}

func TestBuild_crlfHashesLikeLF(t *testing.T) {
	dir := t.TempDir()
	lf := filepath.Join(dir, "lf.go")
	cr := filepath.Join(dir, "cr.go")
	if err := os.WriteFile(lf, []byte("package a\nvar x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cr, []byte("package a\r\nvar x = 1\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mk := func(path string) analyzer.Issue {
		return analyzer.Issue{
			Analyzer: "noglobals",
			Pos:      token.Position{Filename: path, Line: 2, Column: 1},
		}
	}
	bf, err := baseline.Build(
		[]analyzer.Issue{mk(lf), mk(cr)},
		/* moduleRoot */ dir,
		/* analyzersVersion */ "test",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(bf.Entries) != 2 {
		t.Fatalf("entries %d", len(bf.Entries))
	}
	if bf.Entries[0].LineHash != bf.Entries[1].LineHash {
		t.Fatalf("CRLF hash %s != LF hash %s", bf.Entries[0].LineHash, bf.Entries[1].LineHash)
	}
	if len(bf.Entries[0].LineHash) != 16 {
		t.Fatalf("line hash %q, want 16 hex chars", bf.Entries[0].LineHash)
	}
}

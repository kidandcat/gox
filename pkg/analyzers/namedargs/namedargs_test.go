package namedargs_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kidandcat/gox/pkg/analyzer"
	"github.com/kidandcat/gox/pkg/analyzer/analyzertest"
)

func TestNamedArgs_twoStringsAdjacent(t *testing.T) {
	const src = `package p
func transfer(userID, orderID string) {}
func _() {
	transfer("u-1", "o-2")
}`
	issues := analyzertest.Run(t, get(), src)
	// Both args are flagged because they're adjacent same-type without comments.
	if len(issues) != 2 {
		t.Fatalf("got %d issues, want 2", len(issues))
	}
}

func TestNamedArgs_ownLineCommentsSatisfy(t *testing.T) {
	// The gofmt-stable form: each labelled argument on its own line.
	const src = `package p
func transfer(userID, orderID string) {}
func _() {
	transfer(
		/* userID */ "u-1",
		/* orderID */ "o-2",
	)
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestNamedArgs_firstArgSharesLparenLineSatisfies(t *testing.T) {
	// gofmt fixed point too: the first argument may sit on the opening-paren
	// line because it has no predecessor whose label gofmt could relocate.
	const src = `package p
func transfer(userID, orderID string) {}
func _() {
	transfer( /* userID */ "u-1",
		/* orderID */ "o-2",
	)
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestNamedArgs_singleLineMangledIsFlagged(t *testing.T) {
	// The shape gofmt produces from a single-line labelled call. The relocated
	// /* orderID */ comment used to satisfy the rule silently; it must now fire
	// on the second argument (the first argument's label is still stable).
	const src = `package p
func transfer(userID, orderID string) {}
func _() {
	transfer( /* userID */ "u-1" /* orderID */, "o-2")
}`
	issues := analyzertest.Run(t, get(), src)
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1 (the relocated label must be flagged)", len(issues))
	}
}

func TestNamedArgs_singleLineCorrectPlacementIsFlagged(t *testing.T) {
	// Even a correctly-placed single-line label is not a gofmt fixed point —
	// gofmt would relocate it — so it must be flagged before that happens.
	const src = `package p
func transfer(userID, orderID string) {}
func _() {
	transfer( /* userID */ "u-1", /* orderID */ "o-2")
}`
	issues := analyzertest.Run(t, get(), src)
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1 (single-line label is not gofmt-stable)", len(issues))
	}
}

func TestNamedArgs_identNameMatchesParam(t *testing.T) {
	const src = `package p
func transfer(userID, orderID string) {}
func _() {
	var userID, orderID string
	transfer(userID, orderID)
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestNamedArgs_distinctTypesIsFine(t *testing.T) {
	const src = `package p
func open(path string, perm int) {}
func _() {
	open("/x", 644)
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestNamedArgs_stdlibCalleeExempt(t *testing.T) {
	const src = `package p
import "strings"
func _() {
	_ = strings.HasPrefix("hello world", "hello")
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestNamedArgs_safeIgnoreSameLine(t *testing.T) {
	const src = `package p
func transfer(userID, orderID string) {}
func _() {
	transfer("u-1", "o-2") // safe-ignore: positional args fixed by call site
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestNamedArgs_safeIgnoreOnClosingLine(t *testing.T) {
	const src = `package p
func transfer(userID, orderID string) {}
func _() {
	transfer(
		"u-1",
		"o-2",
	) // safe-ignore: positional args fixed by call site
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestNamedArgs_safeIgnoreWithoutReasonStillFires(t *testing.T) {
	const src = `package p
func transfer(userID, orderID string) {}
func _() {
	transfer("u-1", "o-2") // safe-ignore:
}`
	issues := analyzertest.Run(t, get(), src)
	if len(issues) != 2 {
		t.Fatalf("got %d issues, want 2 (bare safe-ignore must not suppress)", len(issues))
	}
}

// A dotless module path used to look like the standard library, so calls in
// `module myapp` were silently exempt. go list's Standard field is the
// authority; strings.HasPrefix stays exempt.
func TestNamedArgs_dotlessModuleIsNotStdlib(t *testing.T) {
	dir := t.TempDir()
	writeFile(t,
		/* path */ filepath.Join(dir, "go.mod"),
		/* body */ "module myapp\n\ngo 1.26.8\n",
	)
	writeFile(t,
		/* path */ filepath.Join(dir, "p", "p.go"),
		/* body */ `package p

import "strings"

func transfer(from, to string) {}

func use(a, b string) bool {
	transfer(a, b)
	return strings.HasPrefix(a, b)
}
`)
	t.Chdir(dir)
	issues, stats, err := analyzer.Run([]string{"./..."}, []*analyzer.Analyzer{get()}, analyzer.RunOptions{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.LoadErrors != 0 {
		t.Fatalf("load errors: %d", stats.LoadErrors)
	}
	if len(issues) != 2 {
		for _, is := range issues {
			t.Logf("%s:%d: %s", is.Pos.Filename, is.Pos.Line, is.Message)
		}
		t.Fatalf("got %d issues, want 2 on transfer and none on HasPrefix", len(issues))
	}
	for _, is := range issues {
		if !strings.HasSuffix(is.Pos.Filename, filepath.Join("p", "p.go")) {
			t.Fatalf("issue file %s", is.Pos.Filename)
		}
		if is.Pos.Line != 8 {
			t.Fatalf("issue line %d, want the transfer call", is.Pos.Line)
		}
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func get() *analyzer.Analyzer {
	for _, a := range analyzer.All() {
		if a.Name == "namedargs" {
			return a
		}
	}
	panic("namedargs analyzer not registered")
}

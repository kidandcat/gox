package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kidandcat/gox/pkg/analyzer"
)

// global-ok: standard `go test -update` golden-file flag.
var updateGolden = flag.Bool("update", false, "rewrite testdata/bad/expected.txt")

// TestFixture_bad runs every analyzer (bug + style tier) end to end — go
// list, loader, type-check, runner — over testdata/bad and compares the
// findings with testdata/bad/expected.txt. Regenerate after an intended
// change with:
//
//	go test ./cmd/gox -run TestFixture_bad -update
func TestFixture_bad(t *testing.T) {
	fixture, absErr := filepath.Abs(filepath.Join("..", "..", "testdata", "bad"))
	if absErr != nil {
		t.Fatal(absErr)
	}
	goldenPath := filepath.Join(fixture, "expected.txt")
	t.Chdir(fixture)

	issues, stats, runErr := analyzer.Run([]string{"./..."}, analyzer.All(), analyzer.RunOptions{})
	if runErr != nil {
		t.Fatalf("Run: %v", runErr)
	}
	if stats.PackagesTotal == 0 {
		t.Fatal("fixture produced no packages")
	}
	var b strings.Builder
	for _, is := range issues {
		rel, relErr := filepath.Rel(fixture, is.Pos.Filename)
		if relErr != nil {
			t.Fatal(relErr)
		}
		fmt.Fprintf(&b, "%s:%d:%d: %s: %s\n", filepath.ToSlash(rel), is.Pos.Line, is.Pos.Column, is.Analyzer, is.Message)
	}
	got := b.String()

	if *updateGolden {
		if wErr := os.WriteFile(goldenPath, []byte(got), 0o644); wErr != nil {
			t.Fatal(wErr)
		}
		return
	}
	want, readErr := os.ReadFile(goldenPath)
	if readErr != nil {
		t.Fatalf("read golden (run with -update to create it): %v", readErr)
	}
	if got != string(want) {
		t.Errorf("findings differ from %s (re-run with -update if intended)\n--- got ---\n%s--- want ---\n%s", goldenPath, got, want)
	}
}

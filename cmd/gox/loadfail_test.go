package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/kidandcat/gox/pkg/baseline"
)

// writeBrokenModule creates a module where one package does not type-check
// and another depends on a module replaced by a directory that does not
// exist (the "replace => /Users/someone/dep" case on another machine).
func writeBrokenModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.test/m\n\ngo 1.25\n\n" +
			"require example.test/dep v0.0.0\n\n" +
			"replace example.test/dep => " + filepath.Join(dir, "does-not-exist") + "\n",
		"ok/ok.go":     "package ok\n\nfunc F() {}\n",
		"typeerr/t.go": "package typeerr\n\nvar X int = \"s\"\n",
		"usesdep/u.go": "package usesdep\n\nimport _ \"example.test/dep\"\n",
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
			t.Fatal(mkErr)
		}
		if wErr := os.WriteFile(path, []byte(body), 0o644); wErr != nil {
			t.Fatal(wErr)
		}
	}
	return dir
}

func TestCheckFailsClosedOnLoadErrors(t *testing.T) {
	t.Setenv("GOX_ALL", "")
	t.Setenv("GOX_SKIP", "")
	t.Setenv("GOX_MAX_ISSUES", "")
	t.Chdir(writeBrokenModule(t))

	if code := runCheck([]string{"--no-cache", "--no-baseline", "./..."}); code != 2 {
		t.Fatalf("gox check on a module with unloadable packages: exit %d, want 2", code)
	}
}

func TestBaselineRefusesIncompleteSnapshot(t *testing.T) {
	t.Setenv("GOX_ALL", "")
	t.Setenv("GOX_SKIP", "")
	dir := writeBrokenModule(t)
	t.Chdir(dir)

	if code := runBaseline([]string{"--no-cache", "./..."}); code != 2 {
		t.Fatalf("gox baseline on a module with unloadable packages: exit %d, want 2", code)
	}
	if _, statErr := os.Stat(filepath.Join(dir, baseline.Filename)); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("baseline file must not be written, stat err = %v", statErr)
	}
}

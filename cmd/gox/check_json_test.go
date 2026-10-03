package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckJSON(t *testing.T) {
	t.Setenv("GOX_ALL", "")
	t.Setenv("GOX_SKIP", "")
	t.Setenv("GOX_MAX_ISSUES", "")

	fixture, err := filepath.Abs(filepath.Join("..", "..", "testdata", "bad"))
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile(filepath.Join(fixture, "expected.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want := 0
	for _, line := range strings.Split(strings.TrimSuffix(string(golden), "\n"), "\n") {
		if line != "" {
			want++
		}
	}
	t.Chdir(fixture)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })

	var buf bytes.Buffer
	var copyErr error
	done := make(chan struct{})
	go func() { // goroutine-ok: drain stdout so runCheck cannot block on a full pipe
		_, copyErr = io.Copy(&buf, r)
		close(done)
	}()

	code := runCheck([]string{"--json", "--no-cache", "--no-baseline", "--all", "--max-issues=0", "./..."})
	if closeErr := w.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	os.Stdout = orig
	<-done
	if closeErr := r.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if copyErr != nil {
		t.Fatal(copyErr)
	}

	if code != 1 {
		t.Fatalf("exit %d, want 1; stdout:\n%s", code, buf.String())
	}
	dec := json.NewDecoder(&buf)
	var rep checkJSONReport
	if decErr := dec.Decode(&rep); decErr != nil {
		t.Fatalf("decode: %v\n%s", decErr, buf.String())
	}
	if dec.More() {
		t.Fatalf("stdout is not a single JSON object:\n%s", buf.String())
	}
	if rep.LoadErrors != 0 {
		t.Fatalf("load_errors %d", rep.LoadErrors)
	}
	if rep.IssueCount != len(rep.Issues)+rep.Hidden {
		t.Fatalf("issue_count %d != len(issues) %d + hidden %d", rep.IssueCount, len(rep.Issues), rep.Hidden)
	}
	if rep.IssueCount != want || len(rep.Issues) != want || rep.Hidden != 0 {
		t.Fatalf("got issue_count=%d len=%d hidden=%d, golden has %d", rep.IssueCount, len(rep.Issues), rep.Hidden, want)
	}
	for _, is := range rep.Issues {
		if !strings.HasSuffix(is.File, "bad.go") {
			t.Fatalf("issue file %s", is.File)
		}
		if is.Analyzer == "" || is.Message == "" || is.Line == 0 {
			t.Fatalf("incomplete issue: %+v", is)
		}
	}
}

func TestPatternDir(t *testing.T) {
	dir := t.TempDir()
	if got := patternDir(nil); got != "" {
		t.Fatalf("nil patterns: %q", got)
	}
	if got := patternDir([]string{"./..."}); got != "" {
		t.Fatalf("./...: %q", got)
	}
	if got := patternDir([]string{"example.com/foo"}); got != "" {
		t.Fatalf("import path: %q", got)
	}
	if got := patternDir([]string{filepath.Join(dir, "...")}); got != dir {
		t.Fatalf("abs/...: got %q want %q", got, dir)
	}
	file := filepath.Join(dir, "a.go")
	if err := os.WriteFile(file, []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := patternDir([]string{file}); got != dir {
		t.Fatalf("file pattern: got %q want %q", got, dir)
	}
}

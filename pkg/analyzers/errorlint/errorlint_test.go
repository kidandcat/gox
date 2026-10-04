package errorlint_test

import (
	"testing"

	"github.com/kidandcat/gox/pkg/analyzer"
	"github.com/kidandcat/gox/pkg/analyzer/analyzertest"
)

func TestErrorLint_sentinelComparison(t *testing.T) {
	const src = `package p
import "io"
func _(err error) bool {
	return err == io.EOF
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{4})
}

func TestErrorLint_nilCompareIsFine(t *testing.T) {
	const src = `package p
func _(err error) bool {
	return err == nil
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestErrorLint_typeAssertOnError(t *testing.T) {
	const src = `package p
type myErr struct{}
func (myErr) Error() string { return "x" }
func _(err error) string {
	e := err.(myErr)
	return e.Error()
}`
	issues := analyzertest.Run(t, get(), src)
	// also forcetypeassert may fire on same line, but this test only runs errorlint
	gotEL := 0
	for _, is := range issues {
		if is.Analyzer == "errorlint" {
			gotEL++
		}
	}
	if gotEL != 1 {
		t.Fatalf("expected 1 errorlint issue, got %d", gotEL)
	}
}

func TestErrorLint_errorfVerbV(t *testing.T) {
	const src = `package p
import "fmt"
func _(err error) error {
	return fmt.Errorf("bad: %v", err)
}`
	issues := analyzertest.Run(t, get(), src)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
}

func TestErrorLint_errorfVerbW_isFine(t *testing.T) {
	const src = `package p
import "fmt"
func _(err error) error {
	return fmt.Errorf("bad: %w", err)
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestErrorLint_errorfVerbS(t *testing.T) {
	const src = `package p
import "fmt"
func _(err error) error {
	return fmt.Errorf("bad: %s", err)
}`
	issues := analyzertest.Run(t, get(), src)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
}

func TestErrorLint_safeIgnoreSuppresses(t *testing.T) {
	const src = `package p
import "io"
func _(err error) bool {
	return err == io.EOF // safe-ignore: io.EOF documented never wrapped
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func get() *analyzer.Analyzer {
	for _, a := range analyzer.All() {
		if a.Name == "errorlint" {
			return a
		}
	}
	panic("errorlint analyzer not registered")
}

package errcheck_test

import (
	"testing"

	"github.com/kidandcat/gox/pkg/analyzer"
	"github.com/kidandcat/gox/pkg/analyzer/analyzertest"
)

func TestErrcheck_bareCall(t *testing.T) {
	const src = `package p
func mayFail() error { return nil }
func _() {
	mayFail()
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{4})
}

func TestErrcheck_blankWithoutAnnotation(t *testing.T) {
	const src = `package p
func mayFail() error { return nil }
func _() {
	_ = mayFail()
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{4})
}

func TestErrcheck_blankWithAnnotation(t *testing.T) {
	const src = `package p
func mayFail() error { return nil }
func _() {
	_ = mayFail() // safe-ignore: deliberately fire and forget
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestErrcheck_tupleBlank(t *testing.T) {
	const src = `package p
func split() (int, error) { return 0, nil }
func _() {
	x, _ := split()
	_ = x
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{4})
}

func TestErrcheck_handledErrIsFine(t *testing.T) {
	const src = `package p
func mayFail() error { return nil }
func _() {
	if err := mayFail(); err != nil {
		panic(err)
	}
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestErrcheck_deferCall(t *testing.T) {
	const src = `package p
func mayFail() error { return nil }
func _() {
	defer mayFail()
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{4})
}

func TestErrcheck_deferCloseIsFlagged(t *testing.T) {
	const src = `package p
type closer struct{}
func (closer) Close() error { return nil }
func _() {
	var c closer
	defer c.Close()
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{6})
}

func TestErrcheck_deferCloseWithAnnotation(t *testing.T) {
	const src = `package p
type closer struct{}
func (closer) Close() error { return nil }
func _() {
	var c closer
	defer c.Close() // safe-ignore: read-only file
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestErrcheck_goCall(t *testing.T) {
	const src = `package p
func mayFail() error { return nil }
func _() {
	go mayFail()
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{4})
}

func TestErrcheck_goWithAnnotation(t *testing.T) {
	const src = `package p
func mayFail() error { return nil }
func _() {
	go mayFail() // safe-ignore: fire and forget
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestErrcheck_fmtPrintExempt(t *testing.T) {
	const src = `package p
import "fmt"
func _() {
	fmt.Println("hello")
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestErrcheck_bytesBufferExempt(t *testing.T) {
	const src = `package p
import "bytes"
func _() {
	var buf bytes.Buffer
	buf.WriteString("hi")
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestErrcheck_wrongErrorSignatureNotFlagged(t *testing.T) {
	const src = `package p
type weird interface{ Error() int }
func f() weird { return nil }
func _() { f() }`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestErrcheck_concreteErrorDropped(t *testing.T) {
	const src = `package p
type ValidationError struct{}
func (ValidationError) Error() string { return "nope" }
func validate() *ValidationError { return nil }
func _() { validate() }`
	issues := analyzertest.Run(t, get(), src)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
}

func get() *analyzer.Analyzer {
	for _, a := range analyzer.All() {
		if a.Name == "errcheck" {
			return a
		}
	}
	panic("errcheck analyzer not registered")
}

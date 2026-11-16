package nilerr_test

import (
	"testing"

	"github.com/kidandcat/gox/pkg/analyzer"
	"github.com/kidandcat/gox/pkg/analyzer/analyzertest"
)

func TestNilErr_returnsNilOnError(t *testing.T) {
	const src = `package p
func get() (int, error) { return 0, nil }
func _() (int, error) {
	v, err := get()
	if err != nil {
		return 0, nil
	}
	return v, nil
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{6})
}

func TestNilErr_handledIsFine(t *testing.T) {
	const src = `package p
import (
	"errors"
	"log"
)
var errNotFound = errors.New("nf")
func get() (int, error) { return 0, nil }
func a() (int, error) {
	v, err := get()
	if err != nil {
		if errors.Is(err, errNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return v, nil
}
func b() error {
	_, err := get()
	if err != nil {
		log.Printf("ignored: %v", err)
		return nil
	}
	return nil
}
func d() (string, error) {
	_, err := get()
	if err != nil {
		// a missing row is fine here
		return "", nil
	}
	_, err = get()
	if err != nil {
		return describe("failed"), nil
	}
	return "", nil
}
func describe(s string) string { return s }
func c() int {
	v, err := get()
	if err != nil {
		return 0
	}
	return v
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestNilErr_safeIgnore(t *testing.T) {
	const src = `package p
func get() (int, error) { return 0, nil }
func _() error {
	_, err := get()
	if err != nil {
		return nil // safe-ignore: best-effort warmup
	}
	return nil
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func get() *analyzer.Analyzer {
	for _, a := range analyzer.All() {
		if a.Name == "nilerr" {
			return a
		}
	}
	panic("nilerr analyzer not registered")
}

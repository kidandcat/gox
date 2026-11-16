package deferclose_test

import (
	"testing"

	"github.com/kidandcat/gox/pkg/analyzer"
	"github.com/kidandcat/gox/pkg/analyzer/analyzertest"
)

func TestDeferClose_beforeErrCheck(t *testing.T) {
	const src = `package p
import (
	"net/http"
	"os"
)
func _(c *http.Client, req *http.Request) error {
	f, err := os.Open("x")
	defer f.Close()
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	defer resp.Body.Close()
	return err
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{8, 13})
}

func TestDeferClose_afterErrCheckIsFine(t *testing.T) {
	const src = `package p
import "os"
func _() error {
	f, err := os.Open("x")
	if err != nil {
		return err
	}
	defer f.Close()
	g, _ := os.Open("y")
	defer g.Close()
	return nil
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestDeferClose_otherValueIsFine(t *testing.T) {
	const src = `package p
import "os"
func _(h *os.File) error {
	f, err := os.Open("x")
	defer h.Close()
	_ = f
	return err
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestDeferClose_safeIgnore(t *testing.T) {
	const src = `package p
import "os"
func _() error {
	f, err := os.Open("x")
	defer f.Close() // safe-ignore: Close on nil *os.File returns ErrInvalid
	return err
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func get() *analyzer.Analyzer {
	for _, a := range analyzer.All() {
		if a.Name == "deferclose" {
			return a
		}
	}
	panic("deferclose analyzer not registered")
}

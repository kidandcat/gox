package noglobals_test

import (
	"testing"

	"github.com/kidandcat/gox/pkg/analyzer"
	"github.com/kidandcat/gox/pkg/analyzer/analyzertest"
)

func TestNoGlobals_plainVar(t *testing.T) {
	const src = `package p
var counter int
`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{2})
}

func TestNoGlobals_constIsFine(t *testing.T) {
	const src = `package p
const Max = 42
`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestNoGlobals_globalOkAnnotation(t *testing.T) {
	const src = `package p
// global-ok: process-level singleton by design.
var registry = map[string]int{}
`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestNoGlobals_emptyAnnotationStillFires(t *testing.T) {
	const src = `package p
var counter int // global-ok:
`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{2})
}

func get() *analyzer.Analyzer {
	for _, a := range analyzer.All() {
		if a.Name == "noglobals" {
			return a
		}
	}
	panic("noglobals analyzer not registered")
}

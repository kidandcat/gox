package banany_test

import (
	"testing"

	"github.com/kidandcat/gox/pkg/analyzer"
	"github.com/kidandcat/gox/pkg/analyzer/analyzertest"
)

func TestBanAny_funcParam(t *testing.T) {
	const src = `package p
func _(v any) {}
`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{2})
}

func TestBanAny_interfaceLiteral(t *testing.T) {
	const src = `package p
func _(v interface{}) {}
`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{2})
}

func TestBanAny_structField(t *testing.T) {
	const src = `package p
type S struct {
	Value any
}
`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{3})
}

func TestBanAny_concreteIsFine(t *testing.T) {
	const src = `package p
func _(v string) {}
`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestBanAny_annotationSuppresses(t *testing.T) {
	const src = `package p
func _(v any) {} // any-ok: this is a deliberate boundary
`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func get() *analyzer.Analyzer {
	for _, a := range analyzer.All() {
		if a.Name == "banany" {
			return a
		}
	}
	panic("banany analyzer not registered")
}

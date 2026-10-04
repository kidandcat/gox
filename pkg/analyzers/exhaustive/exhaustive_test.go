package exhaustive_test

import (
	"testing"

	"github.com/kidandcat/gox/pkg/analyzer"
	"github.com/kidandcat/gox/pkg/analyzer/analyzertest"
)

func TestExhaustive_enumMissingCase(t *testing.T) {
	const src = `package p

type Color int
const (
	Red Color = iota
	Green
	Blue
)

func _(c Color) string {
	switch c {
	case Red:
		return "red"
	case Green:
		return "green"
	}
	return ""
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{11})
}

func TestExhaustive_enumComplete(t *testing.T) {
	const src = `package p

type Color int
const (
	Red Color = iota
	Green
	Blue
)

func _(c Color) string {
	switch c {
	case Red:
		return "r"
	case Green:
		return "g"
	case Blue:
		return "b"
	}
	return ""
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestExhaustive_defaultWithAnnotation(t *testing.T) {
	const src = `package p

type Color int
const (
	Red Color = iota
	Green
	Blue
)

func _(c Color) string {
	switch c {
	case Red:
		return "r"
	default: // exhaustive-ok: future variants intentionally fall through
		return "unknown"
	}
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestExhaustive_sealedInterfaceMissingImpl(t *testing.T) {
	const src = `package p

type Shape interface{ area() float64 }
type Circle struct{}
func (c Circle) area() float64 { return 0 }
type Square struct{}
func (s Square) area() float64 { return 0 }

func _(s Shape) string {
	switch s.(type) {
	case Circle:
		return "c"
	}
	return ""
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{10})
}

func TestExhaustive_unrelatedSwitchIgnored(t *testing.T) {
	const src = `package p
func _(x int) string {
	switch x {
	case 1:
		return "a"
	}
	return ""
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func get() *analyzer.Analyzer {
	for _, a := range analyzer.All() {
		if a.Name == "exhaustive" {
			return a
		}
	}
	panic("exhaustive analyzer not registered")
}

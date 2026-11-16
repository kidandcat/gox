package nilmap_test

import (
	"testing"

	"github.com/kidandcat/gox/pkg/analyzer"
	"github.com/kidandcat/gox/pkg/analyzer/analyzertest"
)

func TestNilMap_write(t *testing.T) {
	const src = `package p
func _(words []string) map[string]int {
	var counts map[string]int
	for _, w := range words {
		counts[w]++
	}
	var seen map[string]bool
	seen["x"] = true
	return counts
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{5, 8})
}

func TestNilMap_initialisedIsFine(t *testing.T) {
	const src = `package p
func fill(m *map[string]int) { *m = map[string]int{} }
func _(cond bool) {
	var a map[string]int
	if cond {
		a = make(map[string]int)
	}
	a["x"] = 1
	var b map[string]int
	fill(&b)
	b["x"] = 1
	var c map[string]int
	_ = c["x"]
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func get() *analyzer.Analyzer {
	for _, a := range analyzer.All() {
		if a.Name == "nilmap" {
			return a
		}
	}
	panic("nilmap analyzer not registered")
}

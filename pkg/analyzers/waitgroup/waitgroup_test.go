package waitgroup_test

import (
	"testing"

	"github.com/kidandcat/gox/pkg/analyzer"
	"github.com/kidandcat/gox/pkg/analyzer/analyzertest"
)

func TestWaitGroup_addInsideGoroutine(t *testing.T) {
	const src = `package p
import "sync"
func _(wg *sync.WaitGroup) {
	var local sync.WaitGroup
	go func() {
		wg.Add(1)
		defer wg.Done()
	}()
	go func() {
		local.Add(1)
		defer local.Done()
	}()
	local.Wait()
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{6, 10})
}

func TestWaitGroup_addBeforeGoIsFine(t *testing.T) {
	const src = `package p
import "sync"
func _() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		var inner sync.WaitGroup
		inner.Add(1)
		go func() { defer inner.Done() }()
		inner.Wait()
	}()
	wg.Wait()
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func get() *analyzer.Analyzer {
	for _, a := range analyzer.All() {
		if a.Name == "waitgroup" {
			return a
		}
	}
	panic("waitgroup analyzer not registered")
}

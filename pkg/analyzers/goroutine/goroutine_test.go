package goroutine_test

import (
	"testing"

	"github.com/kidandcat/gox/pkg/analyzer"
	"github.com/kidandcat/gox/pkg/analyzer/analyzertest"
)

func TestGoroutine_bareGoFires(t *testing.T) {
	const src = `package p
import "fmt"
func _() {
	go fmt.Println("hi")
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{4})
}

func TestGoroutine_unrelatedWaitGroupDoesNotSilence(t *testing.T) {
	const src = `package p
import (
	"fmt"
	"sync"
)
func work() {}
func _() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
	}()
	go func() {
		for {
			work()
		}
	}()
	wg.Wait()
	go fmt.Println("hi")
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{13, 19})
}

func TestGoroutine_boundGoroutinesAccepted(t *testing.T) {
	const src = `package p
import (
	"context"
	"sync"
)
func worker(ctx context.Context, wg *sync.WaitGroup) {}
func loop(ctx context.Context) {}
func _(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(1)
	go worker(ctx, &wg)
	wg.Wait()
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go loop(cctx)
	go func() {
		<-cctx.Done()
	}()
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestGoroutine_contextWithoutCancelFires(t *testing.T) {
	const src = `package p
import "context"
func loop(ctx context.Context) {}
func _(ctx context.Context) {
	go loop(ctx)
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{5})
}

func TestGoroutine_annotationSuppresses(t *testing.T) {
	const src = `package p
import "fmt"
func _() {
	go fmt.Println("hi") // goroutine-ok: deliberate fire-and-forget
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func get() *analyzer.Analyzer {
	for _, a := range analyzer.All() {
		if a.Name == "goroutine" {
			return a
		}
	}
	panic("goroutine analyzer not registered")
}

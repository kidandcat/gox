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

func TestGoroutine_waitGroupInScope(t *testing.T) {
	const src = `package p
import (
	"fmt"
	"sync"
)
func _() {
	var wg sync.WaitGroup
	_ = wg
	go fmt.Println("hi")
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestGoroutine_cancelFuncInScope(t *testing.T) {
	const src = `package p
import (
	"context"
	"fmt"
)
func _() {
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	go fmt.Println("hi")
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
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

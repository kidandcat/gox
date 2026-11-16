package copylock_test

import (
	"testing"

	"github.com/kidandcat/gox/pkg/analyzer"
	"github.com/kidandcat/gox/pkg/analyzer/analyzertest"
)

func TestCopyLock_copies(t *testing.T) {
	const src = `package p
import (
	"sync"
	"sync/atomic"
)
type Counter struct {
	mu sync.Mutex
	n  int
}
type Stats struct{ hits atomic.Int64 }
func (c Counter) Get() int { return c.n }
func use(s Stats) {}
func _(cs []Counter, c *Counter, wg sync.WaitGroup) Counter {
	for _, x := range cs {
		_ = x
	}
	d := *c
	_ = d
	use(Stats{})
	var st Stats
	use(st)
	return *c
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{11, 12, 13, 14, 17, 21, 22})
}

func TestCopyLock_pointersAndFreshValuesAreFine(t *testing.T) {
	const src = `package p
import "sync"
type Counter struct {
	mu sync.Mutex
	n  int
}
func newCounter() Counter { return Counter{} }
func (c *Counter) Inc() { c.mu.Lock(); c.n++; c.mu.Unlock() }
func _(cs []Counter, p *Counter) {
	for i := range cs {
		cs[i].Inc()
	}
	q := p
	c := newCounter()
	_ = &c
	_ = q
	var mu sync.Mutex
	_ = &mu
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestCopyLock_safeIgnore(t *testing.T) {
	const src = `package p
import "sync"
type Counter struct{ mu sync.Mutex }
func _(c *Counter) {
	snapshot := *c // safe-ignore: c is never locked here, snapshot only read
	_ = snapshot
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func get() *analyzer.Analyzer {
	for _, a := range analyzer.All() {
		if a.Name == "copylock" {
			return a
		}
	}
	panic("copylock analyzer not registered")
}

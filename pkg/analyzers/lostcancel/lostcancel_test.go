package lostcancel_test

import (
	"testing"

	"github.com/kidandcat/gox/pkg/analyzer"
	"github.com/kidandcat/gox/pkg/analyzer/analyzertest"
)

func TestLostCancel_discardedAndUnused(t *testing.T) {
	const src = `package p
import (
	"context"
	"time"
)
func _(ctx context.Context) {
	a, _ := context.WithTimeout(ctx, time.Second)
	b, cancel := context.WithCancel(ctx)
	_, _ = a, b
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{7, 8})
}

func TestLostCancel_usedIsFine(t *testing.T) {
	const src = `package p
import "context"
type S struct{ stop context.CancelFunc }
func a(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	_ = ctx
}
func b(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	return ctx, cancel
}
func c(ctx context.Context, s *S) {
	ctx, s.stop = context.WithCancel(ctx)
	_ = ctx
}
func d(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	go func() { <-ctx.Done(); cancel() }()
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestLostCancel_safeIgnore(t *testing.T) {
	const src = `package p
import "context"
func _(ctx context.Context) context.Context {
	ctx, _ = context.WithCancel(ctx) // safe-ignore: parent is cancelled right after
	return ctx
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func get() *analyzer.Analyzer {
	for _, a := range analyzer.All() {
		if a.Name == "lostcancel" {
			return a
		}
	}
	panic("lostcancel analyzer not registered")
}

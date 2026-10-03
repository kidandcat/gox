package contextcheck_test

import (
	"testing"

	"github.com/mentasystems/gox/pkg/analyzer"
	"github.com/mentasystems/gox/pkg/analyzer/analyzertest"
)

func TestContextCheck_backgroundInsideCtxFn(t *testing.T) {
	const src = `package p
import "context"
func _(ctx context.Context) {
	_ = ctx
	_ = context.Background()
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{5})
}

func TestContextCheck_todoInsideCtxFn(t *testing.T) {
	const src = `package p
import "context"
func _(ctx context.Context) {
	_ = ctx
	_ = context.TODO()
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{5})
}

func TestContextCheck_safeIgnore(t *testing.T) {
	const src = `package p
import "context"
func _(ctx context.Context) {
	_ = ctx
	_ = context.Background() // safe-ignore: detached cleanup must outlive request
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestContextCheck_emptyReasonDoesNotSuppress(t *testing.T) {
	const src = `package p
import "context"
func _(ctx context.Context) {
	_ = ctx
	_ = context.Background() // safe-ignore:
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{5})
}

func TestContextCheck_literalWithOwnContextIsARoot(t *testing.T) {
	const src = `package p
import "context"
func outer() {
	_ = func(ctx context.Context) {
		_ = ctx
		_ = context.Background()
	}
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{6})
}

func TestContextCheck_nestedLiteralReportedOnce(t *testing.T) {
	const src = `package p
import "context"
func outer(ctx context.Context) {
	_ = ctx
	_ = func(ctx context.Context) {
		_ = ctx
		_ = context.Background()
	}
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{7})
}

func TestContextCheck_nestedLiteralWithoutContextUsesOuter(t *testing.T) {
	const src = `package p
import "context"
func outer(ctx context.Context) {
	_ = ctx
	_ = func() {
		_ = context.Background()
	}
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{6})
}

func TestContextCheck_noCtxParam_allowed(t *testing.T) {
	const src = `package p
import "context"
func _() {
	_ = context.Background()
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func get() *analyzer.Analyzer {
	for _, a := range analyzer.All() {
		if a.Name == "contextcheck" {
			return a
		}
	}
	panic("contextcheck analyzer not registered")
}

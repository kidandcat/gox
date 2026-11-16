package contextcheck_test

import (
	"testing"

	"github.com/kidandcat/gox/pkg/analyzer"
	"github.com/kidandcat/gox/pkg/analyzer/analyzertest"
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

func TestContextCheck_noCtxCallsWithCtxAvailable(t *testing.T) {
	const src = `package p
import (
	"context"
	"database/sql"
	"net/http"
	"os/exec"
)
func _(ctx context.Context, db *sql.DB, c *http.Client) {
	_, _ = db.Exec("x")
	_, _ = db.ExecContext(ctx, "x")
	_ = db.QueryRow("x")
	_, _ = http.NewRequest("GET", "u", nil)
	_, _ = http.NewRequestWithContext(ctx, "GET", "u", nil)
	_, _ = c.Get("u")
	_ = exec.Command("ls")
	_ = exec.CommandContext(ctx, "ls")
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{9, 11, 12, 14, 15})
}

func TestContextCheck_httpHandlerHasContext(t *testing.T) {
	const src = `package p
import (
	"database/sql"
	"net/http"
)
func handler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _ = db.Exec("x")
	}
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{8})
}

func TestContextCheck_noCtxCallsOutsideCtxFnOrInGoroutine(t *testing.T) {
	const src = `package p
import (
	"context"
	"database/sql"
)
func a(db *sql.DB) {
	_, _ = db.Exec("x")
}
func b(ctx context.Context, db *sql.DB) {
	go func() {
		_, _ = db.Exec("detached")
	}()
	_, _ = db.Exec("y") // safe-ignore: must finish even if ctx is cancelled
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

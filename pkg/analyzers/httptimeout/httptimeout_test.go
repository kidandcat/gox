package httptimeout_test

import (
	"testing"

	"github.com/kidandcat/gox/pkg/analyzer"
	"github.com/kidandcat/gox/pkg/analyzer/analyzertest"

	_ "github.com/kidandcat/gox/pkg/analyzers/httptimeout"
)

func get() *analyzer.Analyzer {
	for _, a := range analyzer.All() {
		if a.Name == "httptimeout" {
			return a
		}
	}
	panic("httptimeout analyzer not registered")
}

func onlyHTTPTimeout(issues []analyzer.Issue) []analyzer.Issue {
	out := issues[:0:0]
	for _, is := range issues {
		if is.Analyzer == "httptimeout" {
			out = append(out, is)
		}
	}
	return out
}

func TestShortcut_Get(t *testing.T) {
	const src = `package p
import "net/http"
func _() {
	_, _ = http.Get("http://x")
}`
	issues := onlyHTTPTimeout(analyzertest.Run(t, get(), src))
	if len(issues) != 1 {
		t.Fatalf("want 1 httptimeout issue, got %d", len(issues))
	}
}

func TestShortcut_Post(t *testing.T) {
	const src = `package p
import (
	"net/http"
	"strings"
)
func _() {
	_, _ = http.Post("http://x", "text/plain", strings.NewReader(""))
}`
	issues := onlyHTTPTimeout(analyzertest.Run(t, get(), src))
	if len(issues) != 1 {
		t.Fatalf("want 1 httptimeout issue, got %d", len(issues))
	}
}

func TestShortcut_AllFour(t *testing.T) {
	const src = `package p
import (
	"net/http"
	"net/url"
	"strings"
)
func _() {
	_, _ = http.Get("http://x")
	_, _ = http.Head("http://x")
	_, _ = http.Post("http://x", "text/plain", strings.NewReader(""))
	_, _ = http.PostForm("http://x", url.Values{})
}`
	issues := onlyHTTPTimeout(analyzertest.Run(t, get(), src))
	if len(issues) != 4 {
		t.Fatalf("want 4 httptimeout issues, got %d", len(issues))
	}
}

func TestDefaultClient_Do(t *testing.T) {
	const src = `package p
import "net/http"
func _() {
	req, _ := http.NewRequest("GET", "http://x", nil)
	_, _ = http.DefaultClient.Do(req)
}`
	issues := onlyHTTPTimeout(analyzertest.Run(t, get(), src))
	if len(issues) != 1 {
		t.Fatalf("want 1 httptimeout issue, got %d", len(issues))
	}
}

func TestDefaultClient_Get(t *testing.T) {
	const src = `package p
import "net/http"
func _() {
	_, _ = http.DefaultClient.Get("http://x")
}`
	issues := onlyHTTPTimeout(analyzertest.Run(t, get(), src))
	if len(issues) != 1 {
		t.Fatalf("want 1 httptimeout issue, got %d", len(issues))
	}
}

func TestLiteral_Empty(t *testing.T) {
	const src = `package p
import "net/http"
func _() {
	_ = &http.Client{}
}`
	issues := onlyHTTPTimeout(analyzertest.Run(t, get(), src))
	if len(issues) != 1 {
		t.Fatalf("want 1 httptimeout issue, got %d", len(issues))
	}
}

func TestLiteral_ValueForm(t *testing.T) {
	const src = `package p
import "net/http"
func _() {
	_ = http.Client{}
}`
	issues := onlyHTTPTimeout(analyzertest.Run(t, get(), src))
	if len(issues) != 1 {
		t.Fatalf("want 1 httptimeout issue, got %d", len(issues))
	}
}

func TestLiteral_OnlyTransport(t *testing.T) {
	const src = `package p
import "net/http"
func _() {
	_ = &http.Client{Transport: http.DefaultTransport}
}`
	issues := onlyHTTPTimeout(analyzertest.Run(t, get(), src))
	if len(issues) != 1 {
		t.Fatalf("want 1 httptimeout issue (Transport set, Timeout missing), got %d", len(issues))
	}
}

func TestLiteral_TimeoutZero(t *testing.T) {
	const src = `package p
import "net/http"
func _() {
	_ = &http.Client{Timeout: 0}
}`
	issues := onlyHTTPTimeout(analyzertest.Run(t, get(), src))
	if len(issues) != 1 {
		t.Fatalf("want 1 httptimeout issue (Timeout: 0), got %d", len(issues))
	}
}

func TestLiteral_TimeoutSet_OK(t *testing.T) {
	const src = `package p
import (
	"net/http"
	"time"
)
func _() {
	_ = &http.Client{Timeout: 30 * time.Second}
}`
	if got := onlyHTTPTimeout(analyzertest.Run(t, get(), src)); len(got) != 0 {
		t.Fatalf("want 0 httptimeout issues, got %d: %v", len(got), got)
	}
}

func TestLiteral_TimeoutFromVariable_OK(t *testing.T) {
	// Non-constant expression — trust the author.
	const src = `package p
import (
	"net/http"
	"time"
)
func _(d time.Duration) {
	_ = &http.Client{Timeout: d}
}`
	if got := onlyHTTPTimeout(analyzertest.Run(t, get(), src)); len(got) != 0 {
		t.Fatalf("want 0 httptimeout issues, got %d: %v", len(got), got)
	}
}

func TestAnnotation_SuppressesShortcut(t *testing.T) {
	const src = `package p
import "net/http"
func _() {
	_, _ = http.Get("http://x") // timeout-ok: probe with a long-lived transport elsewhere
}`
	if got := onlyHTTPTimeout(analyzertest.Run(t, get(), src)); len(got) != 0 {
		t.Fatalf("want annotation to suppress, got %d issues", len(got))
	}
}

func TestAnnotation_SuppressesLiteral_OpenBraceLine(t *testing.T) {
	const src = `package p
import "net/http"
func _() {
	_ = &http.Client{ // timeout-ok: tests use a mock RoundTripper
		Transport: http.DefaultTransport,
	}
}`
	if got := onlyHTTPTimeout(analyzertest.Run(t, get(), src)); len(got) != 0 {
		t.Fatalf("want annotation to suppress, got %d issues", len(got))
	}
}

func TestAnnotation_EmptyReason_DoesNotSuppress(t *testing.T) {
	const src = `package p
import "net/http"
func _() {
	_, _ = http.Get("http://x") // timeout-ok:
}`
	if got := onlyHTTPTimeout(analyzertest.Run(t, get(), src)); len(got) != 1 {
		t.Fatalf("want empty-reason annotation ignored, got %d issues", len(got))
	}
}

func TestNotNetHTTP_NotFlagged(t *testing.T) {
	// A different package called `http` with a Get function should not trigger.
	const src = `package p
type fakeHTTP struct{}
func (fakeHTTP) Get(string) {}
var http = fakeHTTP{}
func _() {
	http.Get("http://x")
}`
	if got := onlyHTTPTimeout(analyzertest.Run(t, get(), src)); len(got) != 0 {
		t.Fatalf("want 0 httptimeout issues for non-net/http package, got %d", len(got))
	}
}

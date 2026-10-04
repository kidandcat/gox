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

func TestDefaultClient_Do_NewRequestWithContext_OK(t *testing.T) {
	const src = `package p
import (
	"context"
	"net/http"
)
func _(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://x", nil)
	if err != nil { return }
	_, _ = http.DefaultClient.Do(req)
}`
	if got := onlyHTTPTimeout(analyzertest.Run(t, get(), src)); len(got) != 0 {
		t.Fatalf("want 0 httptimeout issues for Do+NewRequestWithContext, got %d: %v", len(got), got)
	}
}

func TestDefaultClient_Do_NewRequestStillFlagged(t *testing.T) {
	const src = `package p
import "net/http"
func _() {
	req, _ := http.NewRequest("GET", "http://x", nil)
	_, _ = http.DefaultClient.Do(req)
}`
	if got := onlyHTTPTimeout(analyzertest.Run(t, get(), src)); len(got) != 1 {
		t.Fatalf("want 1 httptimeout issue for Do+NewRequest, got %d", len(got))
	}
}

func TestVarClient(t *testing.T) {
	const src = `package p
import "net/http"
func _() {
	var c http.Client
	_ = c
}`
	if got := onlyHTTPTimeout(analyzertest.Run(t, get(), src)); len(got) != 1 {
		t.Fatalf("want 1 httptimeout issue for var http.Client, got %d", len(got))
	}
}

func TestNewClient(t *testing.T) {
	const src = `package p
import "net/http"
func _() {
	_ = new(http.Client)
}`
	if got := onlyHTTPTimeout(analyzertest.Run(t, get(), src)); len(got) != 1 {
		t.Fatalf("want 1 httptimeout issue for new(http.Client), got %d", len(got))
	}
}

func TestServer_Empty(t *testing.T) {
	const src = `package p
import "net/http"
func _() {
	_ = &http.Server{}
}`
	if got := onlyHTTPTimeout(analyzertest.Run(t, get(), src)); len(got) != 1 {
		t.Fatalf("want 1 httptimeout issue for empty http.Server, got %d", len(got))
	}
}

func TestServer_OnlyReadHeader(t *testing.T) {
	const src = `package p
import (
	"net/http"
	"time"
)
func _() {
	_ = &http.Server{ReadHeaderTimeout: time.Second}
}`
	if got := onlyHTTPTimeout(analyzertest.Run(t, get(), src)); len(got) != 1 {
		t.Fatalf("want 1 httptimeout issue (WriteTimeout missing), got %d", len(got))
	}
}

func TestServer_ZeroReadHeader(t *testing.T) {
	const src = `package p
import (
	"net/http"
	"time"
)
func _() {
	_ = &http.Server{ReadHeaderTimeout: 0, WriteTimeout: time.Second}
}`
	if got := onlyHTTPTimeout(analyzertest.Run(t, get(), src)); len(got) != 1 {
		t.Fatalf("want 1 httptimeout issue (ReadHeaderTimeout: 0), got %d", len(got))
	}
}

func TestServer_BothSet_OK(t *testing.T) {
	const src = `package p
import (
	"net/http"
	"time"
)
func _() {
	_ = &http.Server{ReadHeaderTimeout: time.Second, WriteTimeout: time.Second}
}`
	if got := onlyHTTPTimeout(analyzertest.Run(t, get(), src)); len(got) != 0 {
		t.Fatalf("want 0 httptimeout issues, got %d: %v", len(got), got)
	}
}

func TestAnnotation_SuppressesVarClient(t *testing.T) {
	const src = `package p
import "net/http"
func _() {
	var c http.Client // timeout-ok: Timeout assigned from config below
	_ = c
}`
	if got := onlyHTTPTimeout(analyzertest.Run(t, get(), src)); len(got) != 0 {
		t.Fatalf("want annotation to suppress var client, got %d issues", len(got))
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

func TestBareServerFuncs(t *testing.T) {
	const src = `package p
import (
	"net/http"
	"time"
)
func _() error {
	if err := http.ListenAndServe(":8080", nil); err != nil {
		return err
	}
	if err := http.ListenAndServeTLS(":443", "c", "k", nil); err != nil {
		return err
	}
	if err := http.Serve(nil, nil); err != nil {
		return err
	}
	if err := http.ServeTLS(nil, "c", "k", nil); err != nil {
		return err
	}
	s := &http.Server{
		ReadHeaderTimeout: time.Second,
		WriteTimeout:      time.Second,
	}
	return s.ListenAndServe()
}`
	got := onlyHTTPTimeout(analyzertest.Run(t, get(), src))
	if len(got) != 4 {
		for _, is := range got {
			t.Logf("L%d: %s", is.Pos.Line, is.Message)
		}
		t.Fatalf("got %d httptimeout issues, want the four package-level server funcs", len(got))
	}
	for _, is := range got {
		if is.Pos.Line < 7 || is.Pos.Line > 16 {
			t.Fatalf("line %d is not one of the four package-level calls", is.Pos.Line)
		}
	}
}

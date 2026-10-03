package bodyclose_test

import (
	"testing"

	"github.com/mentasystems/gox/pkg/analyzer"
	"github.com/mentasystems/gox/pkg/analyzer/analyzertest"
)

func TestBodyClose_leak(t *testing.T) {
	const src = `package p
import "net/http"
func _() {
	resp, err := http.Get("http://x")
	if err != nil { return }
	_ = resp
}`
	issues := analyzertest.Run(t, get(), src)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
}

func TestBodyClose_closureReportedOnce(t *testing.T) {
	const src = `package p
import "net/http"
func _(c *http.Client, req *http.Request) {
	f := func() {
		resp, err := c.Do(req)
		if err != nil { return }
		_ = resp
	}
	f()
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{5})
}

func TestBodyClose_closedInsideDeferredClosure(t *testing.T) {
	const src = `package p
import "net/http"
func _(c *http.Client, req *http.Request) {
	resp, err := c.Do(req)
	if err != nil { return }
	defer func() { _ = resp.Body.Close() }()
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestBodyClose_shadowedNameDoesNotCloseOuter(t *testing.T) {
	const src = `package p
import "net/http"
func _(c *http.Client, req *http.Request) {
	resp, err := c.Do(req)
	if err != nil { return }
	{
		resp, err := c.Do(req)
		if err != nil { return }
		_ = resp.Body.Close()
	}
	_ = resp
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{4})
}

func TestBodyClose_returnedToCaller(t *testing.T) {
	const src = `package p
import "net/http"
func get(c *http.Client, req *http.Request) (*http.Response, error) {
	resp, err := c.Do(req)
	return resp, err
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestBodyClose_returnedFromClosureOnlyDoesNotExemptOuter(t *testing.T) {
	const src = `package p
import "net/http"
func _(c *http.Client, req *http.Request) {
	resp, err := c.Do(req)
	if err != nil { return }
	f := func() *http.Response { return resp }
	_ = f
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{4})
}

func TestBodyClose_safeIgnore(t *testing.T) {
	const src = `package p
import "net/http"
func _() {
	resp, err := http.Get("http://x") // safe-ignore: probe — body discarded by transport
	if err != nil { return }
	_ = resp
}`
	for _, is := range analyzertest.Run(t, get(), src) {
		if is.Analyzer == "bodyclose" {
			t.Fatalf("annotation should suppress bodyclose: %v", is.Message)
		}
	}
}

func TestBodyClose_emptyReasonDoesNotSuppress(t *testing.T) {
	const src = `package p
import "net/http"
func _() {
	resp, err := http.Get("http://x") // safe-ignore:
	if err != nil { return }
	_ = resp
}`
	got := 0
	for _, is := range analyzertest.Run(t, get(), src) {
		if is.Analyzer == "bodyclose" {
			got++
		}
	}
	if got != 1 {
		t.Fatalf("empty reason must not suppress, got %d bodyclose issues", got)
	}
}

func TestBodyClose_deferCloseIsFine(t *testing.T) {
	const src = `package p
import "net/http"
func _() {
	resp, _ := http.Get("http://x") // safe-ignore: ok
	defer resp.Body.Close()         // safe-ignore: ok
}`
	issues := analyzertest.Run(t, get(), src)
	// only bodyclose under test — other rules' errors don't count
	gotBC := 0
	for _, is := range issues {
		if is.Analyzer == "bodyclose" {
			gotBC++
		}
	}
	if gotBC != 0 {
		t.Fatalf("expected 0 bodyclose issues, got %d", gotBC)
	}
}

func TestBodyClose_immediateCloseIsFine(t *testing.T) {
	const src = `package p
import "net/http"
func _() {
	resp, _ := http.Get("http://x") // safe-ignore: ok
	resp.Body.Close()               // safe-ignore: ok
}`
	for _, is := range analyzertest.Run(t, get(), src) {
		if is.Analyzer == "bodyclose" {
			t.Fatalf("unexpected bodyclose: %v", is.Message)
		}
	}
}

func get() *analyzer.Analyzer {
	for _, a := range analyzer.All() {
		if a.Name == "bodyclose" {
			return a
		}
	}
	panic("bodyclose analyzer not registered")
}

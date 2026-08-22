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

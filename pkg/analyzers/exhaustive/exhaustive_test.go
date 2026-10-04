package exhaustive_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kidandcat/gox/pkg/analyzer"
	"github.com/kidandcat/gox/pkg/analyzer/analyzertest"
)

func TestExhaustive_enumMissingCase(t *testing.T) {
	const src = `package p

type Color int
const (
	Red Color = iota
	Green
	Blue
)

func _(c Color) string {
	switch c {
	case Red:
		return "red"
	case Green:
		return "green"
	}
	return ""
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{11})
}

func TestExhaustive_enumComplete(t *testing.T) {
	const src = `package p

type Color int
const (
	Red Color = iota
	Green
	Blue
)

func _(c Color) string {
	switch c {
	case Red:
		return "r"
	case Green:
		return "g"
	case Blue:
		return "b"
	}
	return ""
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestExhaustive_defaultWithAnnotation(t *testing.T) {
	const src = `package p

type Color int
const (
	Red Color = iota
	Green
	Blue
)

func _(c Color) string {
	switch c {
	case Red:
		return "r"
	default: // exhaustive-ok: future variants intentionally fall through
		return "unknown"
	}
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestExhaustive_sealedInterfaceMissingImpl(t *testing.T) {
	const src = `package p

type Shape interface{ area() float64 }
type Circle struct{}
func (c Circle) area() float64 { return 0 }
type Square struct{}
func (s Square) area() float64 { return 0 }

func _(s Shape) string {
	switch s.(type) {
	case Circle:
		return "c"
	}
	return ""
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{10})
}

func TestExhaustive_unrelatedSwitchIgnored(t *testing.T) {
	const src = `package p
func _(x int) string {
	switch x {
	case 1:
		return "a"
	}
	return ""
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestExhaustive_aliasIsNotASeparateCase(t *testing.T) {
	const src = `package p

type Mode int

const (
	Off Mode = iota
	On
	Default = Off
)

func covered(m Mode) {
	switch m {
	case Off, On:
	}
}

func missing(m Mode) {
	switch m {
	case Off:
	}
}
`
	issues := analyzertest.Run(t, get(), src)
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1:\n%v", len(issues), issues)
	}
	if issues[0].Pos.Line != 18 {
		t.Fatalf("line %d, want the switch that omits On", issues[0].Pos.Line)
	}
	if !strings.Contains(issues[0].Message, "missing On") {
		t.Fatalf("message %q, want missing On", issues[0].Message)
	}
	if strings.Contains(issues[0].Message, "Default") {
		t.Fatalf("alias Default shares Off's value and must not be reported: %s", issues[0].Message)
	}
}

func TestExhaustive_sameModuleNotStdlib(t *testing.T) {
	dir := t.TempDir()
	writeFile(t,
		/* path */ filepath.Join(dir, "go.mod"),
		/* body */ "module example.com/app\n\ngo 1.26.8\n",
	)
	writeFile(t,
		/* path */ filepath.Join(dir, "model", "kind.go"),
		/* body */ `package model

type Kind int

const (
	A Kind = iota
	B
	C
)
`)
	writeFile(t,
		/* path */ filepath.Join(dir, "service", "svc.go"),
		/* body */ `package service

import "example.com/app/model"

func label(k model.Kind) string {
	switch k {
	case model.A:
		return "a"
	}
	return ""
}
`)
	writeFile(t,
		/* path */ filepath.Join(dir, "reflectcase", "r.go"),
		/* body */ `package reflectcase

import "reflect"

func name(k reflect.Kind) string {
	switch k {
	case reflect.Int:
		return "int"
	}
	return ""
}
`)
	t.Chdir(dir)
	issues, stats, err := analyzer.Run([]string{"./..."}, []*analyzer.Analyzer{get()}, analyzer.RunOptions{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.LoadErrors != 0 {
		t.Fatalf("load errors: %d", stats.LoadErrors)
	}
	var onKind, onReflect int
	for _, is := range issues {
		switch {
		case strings.HasSuffix(is.Pos.Filename, filepath.Join("service", "svc.go")):
			onKind++
			if !strings.Contains(is.Message, "missing B, C") {
				t.Fatalf("message %q, want missing B, C", is.Message)
			}
		case strings.Contains(is.Pos.Filename, "reflectcase"):
			onReflect++
			t.Errorf("reflect.Kind must not be treated as an enum: %s", is.Message)
		default:
			t.Errorf("unexpected issue %s:%d: %s", is.Pos.Filename, is.Pos.Line, is.Message)
		}
	}
	if onKind != 1 {
		t.Fatalf("got %d issues on model.Kind, want 1", onKind)
	}
	if onReflect != 0 {
		t.Fatalf("got %d issues on reflect.Kind", onReflect)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func get() *analyzer.Analyzer {
	for _, a := range analyzer.All() {
		if a.Name == "exhaustive" {
			return a
		}
	}
	panic("exhaustive analyzer not registered")
}

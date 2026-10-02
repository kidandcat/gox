package forcetypeassert_test

import (
	"testing"

	"github.com/mentasystems/gox/pkg/analyzer"
	"github.com/mentasystems/gox/pkg/analyzer/analyzertest"
)

func TestForcetypeassert_panicking(t *testing.T) {
	const src = `package p
func _(v any) string {
	return v.(string)
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{3})
}

func TestForcetypeassert_commaOkIsFine(t *testing.T) {
	const src = `package p
func _(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestForcetypeassert_typeSwitchIsFine(t *testing.T) {
	const src = `package p
func _(v any) string {
	switch x := v.(type) {
	case string:
		return x
	}
	return ""
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestForcetypeassert_varCommaOkIsFine(t *testing.T) {
	const src = `package p
func _(v any) string {
	var s, ok = v.(string)
	if !ok {
		return ""
	}
	return s
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestForcetypeassert_varDiscardedOK(t *testing.T) {
	const src = `package p
func _(v any) string {
	var s, _ = v.(string)
	return s
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{3})
	if len(issues) == 1 && issues[0].Message != "type assertion discards ok; mismatch yields the zero value silently" {
		t.Errorf("unexpected message: %q", issues[0].Message)
	}
}

func TestForcetypeassert_varPanicking(t *testing.T) {
	const src = `package p
func _(v any) string {
	var s = v.(string)
	return s
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{3})
}

func TestForcetypeassert_discardedOK(t *testing.T) {
	const src = `package p
func _(v any) string {
	s, _ := v.(string)
	return s
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{3})
}

func TestForcetypeassert_discardedOKAnnotated(t *testing.T) {
	const src = `package p
func _(v any) string {
	s, _ := v.(string) // safe-ignore: caller guarantees type
	return s
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestForcetypeassert_safeIgnore(t *testing.T) {
	const src = `package p
func _(v any) string {
	return v.(string) // safe-ignore: caller guarantees type
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func get() *analyzer.Analyzer {
	for _, a := range analyzer.All() {
		if a.Name == "forcetypeassert" {
			return a
		}
	}
	panic("forcetypeassert analyzer not registered")
}

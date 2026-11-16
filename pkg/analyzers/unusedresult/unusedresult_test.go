package unusedresult_test

import (
	"testing"

	"github.com/kidandcat/gox/pkg/analyzer"
	"github.com/kidandcat/gox/pkg/analyzer/analyzertest"
)

func TestUnusedResult_discarded(t *testing.T) {
	const src = `package p
import (
	"context"
	"errors"
	"fmt"
	"strings"
)
func _(ctx context.Context, s string) {
	fmt.Sprintf("x %s", s)
	errors.New("boom")
	strings.TrimSpace(s)
	context.WithValue(ctx, "k", 1)
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{9, 10, 11, 12})
}

func TestUnusedResult_usedOrImpure(t *testing.T) {
	const src = `package p
import (
	"errors"
	"fmt"
	"strings"
)
func _(s string) error {
	fmt.Printf("x %s", s)
	var b strings.Builder
	b.WriteString(s)
	s = strings.TrimSpace(s)
	_ = s
	return errors.New("boom")
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func get() *analyzer.Analyzer {
	for _, a := range analyzer.All() {
		if a.Name == "unusedresult" {
			return a
		}
	}
	panic("unusedresult analyzer not registered")
}

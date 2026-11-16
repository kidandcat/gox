package printf_test

import (
	"testing"

	"github.com/kidandcat/gox/pkg/analyzer"
	"github.com/kidandcat/gox/pkg/analyzer/analyzertest"
)

func TestPrintf_mismatch(t *testing.T) {
	const src = `package p
import (
	"fmt"
	"log"
	"os"
)
func _(id, name string, err error) {
	log.Printf("sync %s failed after %d tries", id)
	_ = fmt.Sprintf("user %s", id, name)
	log.Printf("save: %w", err)
	fmt.Fprintf(os.Stderr, "%*d\n", 3)
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{8, 9, 10, 11})
}

func TestPrintf_correct(t *testing.T) {
	const src = `package p
import (
	"fmt"
	"log"
	"os"
)
const f = "%s=%d"
func _(id string, n int, err error, args []any) error {
	log.Printf("100%% of %s", id)
	fmt.Fprintf(os.Stderr, "%-*d|%.2f\n", 3, n, 1.5)
	_ = fmt.Sprintf(f, id, n)
	_ = fmt.Sprintf("%[1]s %[1]s", id)
	log.Printf("%s %d", args...)
	return fmt.Errorf("load %s: %w", id, err)
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func get() *analyzer.Analyzer {
	for _, a := range analyzer.All() {
		if a.Name == "printf" {
			return a
		}
	}
	panic("printf analyzer not registered")
}

package namedargs_test

import (
	"testing"

	"github.com/kidandcat/gox/pkg/analyzer"
	"github.com/kidandcat/gox/pkg/analyzer/analyzertest"
)

func TestNamedArgs_swappedIdentifiers(t *testing.T) {
	const src = `package p
func transfer(fromID, toID string, amount int64) {}
func _(fromID, toID string) {
	transfer(toID, fromID, 1)
	transfer(fromID, toID, 1)
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{4, 4})
}

func TestNamedArgs_swappedFieldsIgnoreCase(t *testing.T) {
	const src = `package p
type req struct{ FromID, ToID string }
func transfer(fromID, toID string) {}
func _(r req) {
	transfer(r.ToID, r.FromID)
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{5, 5})
}

func TestNamedArgs_labelNamesOtherParam(t *testing.T) {
	const src = `package p
func transfer(fromID, toID string) {}
func _(a, b string) {
	transfer( /* toID */ a, /* fromID */ b)
	transfer(
		/* fromID */ a,
		/* toID */ b,
	)
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{4, 4})
}

func TestNamedArgs_gofmtRelocatedLabelIsFine(t *testing.T) {
	// gofmt turns transfer(/* fromID */ a, /* toID */ b) into this form.
	const src = `package p
func transfer(fromID, toID string) {}
func _(a, b string) {
	transfer( /* fromID */ a /* toID */, b)
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestNamedArgs_unrelatedNamesAndLiterals(t *testing.T) {
	const src = `package p
func transfer(fromID, toID string) {}
func _(src, dst string) {
	transfer(src, dst)
	transfer("u-1", "o-2")
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestNamedArgs_notAssignableIsNotASwap(t *testing.T) {
	const src = `package p
type ID string
func link(name string, id ID) {}
func _(id string, name ID) {
	link(id, name)
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestNamedArgs_oneLetterNamesIgnored(t *testing.T) {
	const src = `package p
func less(i, j int) bool { return i < j }
func _(i, j int) bool { return less(j, i) }`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestNamedArgs_stdlibSwapIsCaught(t *testing.T) {
	const src = `package p
import "strings"
func _(s, old, new string) string {
	return strings.Replace(s, new, old, -1)
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{4, 4})
}

func TestNamedArgs_variadicTailIgnored(t *testing.T) {
	const src = `package p
func join(sep string, parts ...string) string { return sep }
func _(sep, parts string) {
	_ = join(sep, parts, sep)
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestNamedArgs_safeIgnore(t *testing.T) {
	const src = `package p
func transfer(fromID, toID string) {}
func _(fromID, toID string) {
	transfer(toID, fromID) // safe-ignore: refund goes back the other way
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestNamedArgs_safeIgnoreWithoutReasonStillFires(t *testing.T) {
	const src = `package p
func transfer(fromID, toID string) {}
func _(fromID, toID string) {
	transfer(toID, fromID) // safe-ignore:
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{4, 4})
}

func TestNamedArgs_labelOnlyCoversItsOwnArgument(t *testing.T) {
	const src = `package p
type act struct{ ID string }
func setResult(id, status, result string) {}
func _(a act, status, result string) {
	setResult( /* id */ a.ID, status, result)
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestNamedArgs_gofmtRelocatedLabelsInLongCall(t *testing.T) {
	const src = `package p
func createUser(email, phone, password, fullName string) {}
func _(email, password string) {
	createUser( /* email */ email /* phone */, "" /* password */, password /* fullName */, "Admin")
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func TestNamedArgs_oneSidedNameMatchIsNotASwap(t *testing.T) {
	// "role" passed as "primary" next to a "role" parameter, and the same
	// value passed twice: neither is a crossed pair.
	const src = `package p
func holds(primary, role string) bool { return primary == role }
func dayOf(clientTS, serverTS int64) int64 { return clientTS }
func _(role, admin string, serverTS int64) {
	_ = holds(role, admin)
	_ = dayOf(serverTS, serverTS)
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func get() *analyzer.Analyzer {
	for _, a := range analyzer.All() {
		if a.Name == "namedargs" {
			return a
		}
	}
	panic("namedargs analyzer not registered")
}

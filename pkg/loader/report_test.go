package loader

import "testing"

func TestShouldReport_internalTestFiltersProduction(t *testing.T) {
	regular := &PackageInfo{}
	if !regular.ShouldReport("foo.go") || !regular.ShouldReport("foo_test.go") {
		t.Fatal("regular package should report every file")
	}
	internal := &PackageInfo{reportTestOnly: true}
	if internal.ShouldReport("foo.go") {
		t.Fatal("internal test package must not re-report production files")
	}
	if !internal.ShouldReport("foo_test.go") {
		t.Fatal("internal test package should report *_test.go")
	}
}

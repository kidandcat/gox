package cache_test

import (
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/kidandcat/gox/pkg/analyzer"
	"github.com/kidandcat/gox/pkg/cache"
)

func TestKeyWithDeps_dependsOnDeps(t *testing.T) {
	f := filepath.Join(t.TempDir(), "a.go")
	if err := os.WriteFile(f, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	k1, err := cache.KeyWithDeps("m/a", []string{f}, []string{"m/b=/cache/x-d"}, "v")
	if err != nil {
		t.Fatal(err)
	}
	k2, err := cache.KeyWithDeps("m/a", []string{f}, []string{"m/b=/cache/y-d"}, "v")
	if err != nil {
		t.Fatal(err)
	}
	if k1 == k2 {
		t.Fatal("key must change when a dependency's export data changes")
	}
	k3, err := cache.KeyWithDeps("m/a", []string{f}, []string{"m/b=/cache/x-d"}, "v")
	if err != nil {
		t.Fatal(err)
	}
	if k1 != k3 {
		t.Fatal("key must be deterministic")
	}
	k4, err := cache.KeyWithDeps("m/a", []string{f}, []string{"m/b=/cache/x-d"}, "other")
	if err != nil {
		t.Fatal(err)
	}
	if k1 == k4 {
		t.Fatal("key must change with the analyzer set")
	}
}

func TestClean_removesEveryVersion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", home)

	dir, err := cache.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "entry"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(home, "gox", "v1")
	if err = os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(old, "stale"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err = cache.Clean(); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(home, "gox")); !os.IsNotExist(statErr) {
		t.Fatalf("cache root still present: %v", statErr)
	}
	if err = cache.Clean(); err != nil {
		t.Fatalf("cleaning a missing cache: %v", err)
	}
}

func TestKey_missingFile(t *testing.T) {
	if _, err := cache.Key("m/a", []string{filepath.Join(t.TempDir(), "nope.go")}, "v"); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func TestPutGet_roundTrip(t *testing.T) {
	dir := t.TempDir()
	want := []analyzer.Issue{{
		Analyzer: "errcheck",
		Pos:      token.Position{Filename: "/x/a.go", Line: 3, Column: 2, Offset: 17},
		Message:  "dropped error",
		Hint:     "handle it",
	}}
	if err := cache.Put(
		/* dir */ dir,
		/* key */ "k",
		want,
	); err != nil {
		t.Fatal(err)
	}
	got, ok := cache.Get(
		/* dir */ dir,
		/* key */ "k",
	)
	if !ok {
		t.Fatal("Get missed a key just Put")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip mismatch:\n got %#v\nwant %#v", got, want)
	}
	if _, ok := cache.Get(
		/* dir */ dir,
		/* key */ "absent",
	); ok {
		t.Fatal("Get hit an absent key")
	}
}

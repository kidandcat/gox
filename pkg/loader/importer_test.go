package loader_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/kidandcat/gox/pkg/loader"
)

// context.WithTimeout and time.Second must be the same time.Duration.
// A shared cache that hands back *types.Package values from a different
// importer makes go/types reject this file.
func TestLoadPackage_stdlibTypesAgree(t *testing.T) {
	const src = `package p

import (
	"context"
	"time"
)

func F() {
	_, cancel := context.WithTimeout(context.Background(), time.Second)
	cancel()
}
`
	files := map[string]string{}
	for i := 0; i < 8; i++ {
		files[fmt.Sprintf("p%d/p.go", i)] = src
	}
	writeModule(t, files)

	infos, pkgErrs, err := loader.ListWithErrors("./...")
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgErrs) != 0 {
		t.Fatalf("package errors: %v", pkgErrs)
	}
	if len(infos) != 8 {
		t.Fatalf("got %d packages, want 8", len(infos))
	}

	var wg sync.WaitGroup
	for _, info := range infos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pkg, loadErr := loader.LoadPackage(info)
			if loadErr != nil {
				t.Errorf("%s: %v", info.ImportPath, loadErr)
				return
			}
			if len(pkg.TypeErrors) != 0 {
				t.Errorf("%s: %v", info.ImportPath, pkg.TypeErrors)
			}
		}()
	}
	wg.Wait()
}

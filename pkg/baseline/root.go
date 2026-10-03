package baseline

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ModuleRoot returns the absolute path of the Go module root for the current
// working directory, by asking `go env GOMOD`.
func ModuleRoot() (string, error) {
	return ModuleRootIn("")
}

// ModuleRootIn is ModuleRoot with the `go` command's working directory set
// to dir. An empty dir uses the current working directory. The command is
// bounded so a stuck toolchain cannot hang `gox check`.
func ModuleRootIn(dir string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "env", "GOMOD")
	if dir != "" {
		cmd.Dir = dir
	}
	out, runErr := cmd.Output()
	if runErr != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("go env GOMOD: %w", ctx.Err())
		}
		return "", fmt.Errorf("go env GOMOD: %w", runErr)
	}
	gomod := strings.TrimSpace(string(out))
	if gomod == "" || gomod == "/dev/null" {
		return "", errors.New("not inside a Go module")
	}
	return filepath.Dir(gomod), nil
}

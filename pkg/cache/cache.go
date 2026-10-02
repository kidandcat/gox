// Package cache provides a per-package incremental cache for gox analyzer
// results.
//
// Key = SHA256( cache-format-version || gox-binary-fingerprint ||
// analyzer-set || import-path || sorted(file-name, file-size,
// file-mtime-nanos) || sorted(dependency export-data files) ). Stored under
// $XDG_CACHE_HOME/gox/v4 (or ~/.cache/gox/v4).
//
// The mtime+size key is a fast proxy for content equality. False negatives
// (stale cache after a content-preserving touch) only result in unnecessary
// re-analysis; false positives (cache hit on changed content) cannot occur
// because any edit updates mtime.
//
// Cross-package changes are tracked through the dependencies' export-data
// file names, which `go list -export` derives from a content hash: editing
// the exported API of any (transitive) dependency changes the key, so a
// dependent package is re-analyzed (e.g. a callee that starts returning an
// error). The binary fingerprint (module version, VCS revision, executable
// size+mtime) invalidates entries when gox itself is upgraded or rebuilt,
// since rule logic can change without the analyzer names changing.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"sync"

	"github.com/mentasystems/gox/pkg/analyzer"
)

// Version is bumped whenever cache-incompatible changes are made (new
// exemption sets, changed report filtering, etc.). Bumping invalidates all
// existing entries automatically.
const Version = "v4"

// Key computes a stable cache key for a package given its import path and
// file paths on disk. analyzersVersion should change whenever the set of
// registered analyzers changes (e.g. a new gox release).
//
// Key does not account for dependencies; prefer KeyWithDeps.
func Key(importPath string, files []string, analyzersVersion string) (string, error) {
	return KeyWithDeps(
		/* importPath */ importPath,
		/* files */ files,
		/* depExports */ nil,
		/* analyzersVersion */ analyzersVersion,
	)
}

// KeyWithDeps is Key plus depExports — typically loader.PackageInfo
// DepExports() — so the entry is invalidated when any dependency's export
// data changes.
func KeyWithDeps(importPath string, files, depExports []string, analyzersVersion string) (string, error) {
	type entry struct {
		Name  string
		Size  int64
		MTime int64
	}
	entries := make([]entry, 0, len(files))
	for _, path := range files {
		info, statErr := os.Stat(path)
		if statErr != nil {
			return "", fmt.Errorf("stat %s: %w", path, statErr)
		}
		entries = append(entries, entry{
			Name:  filepath.Base(path),
			Size:  info.Size(),
			MTime: info.ModTime().UnixNano(),
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	h := sha256.New()
	// hash.Hash.Write never returns an error, so fmt.Fprintf cannot fail here.
	fmt.Fprintf(h, "gox-cache-%s\n", Version)
	fmt.Fprintf(h, "binary=%s\n", binaryFingerprint())
	fmt.Fprintf(h, "analyzers=%s\n", analyzersVersion)
	fmt.Fprintf(h, "pkg=%s\n", importPath)
	for _, e := range entries {
		fmt.Fprintf(h, "%s|%d|%d\n", e.Name, e.Size, e.MTime)
	}
	for _, d := range depExports {
		fmt.Fprintf(h, "dep=%s\n", d)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// binaryFingerprint identifies the running gox build. Analyzer names alone
// do not change when a rule's logic does, so without this an upgraded gox
// would keep replaying results computed by the previous version.
//
// global-ok: computed once per process; read-only afterwards.
var binaryFingerprint = sync.OnceValue(func() string {
	fp := ""
	if bi, ok := debug.ReadBuildInfo(); ok {
		fp = bi.GoVersion + "|" + bi.Main.Path + "@" + bi.Main.Version
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" || s.Key == "vcs.modified" {
				fp += "|" + s.Key + "=" + s.Value
			}
		}
	}
	// Local rebuilds without a VCS stamp (or with uncommitted changes) keep
	// the same version string; the executable's size+mtime covers them.
	if exe, exeErr := os.Executable(); exeErr == nil {
		if st, statErr := os.Stat(exe); statErr == nil {
			fp += fmt.Sprintf("|exe=%d.%d", st.Size(), st.ModTime().UnixNano())
		}
	}
	return fp
})

// Dir returns the on-disk cache directory, creating it if necessary.
func Dir() (string, error) {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return "", homeErr
		}
		base = filepath.Join(home, ".cache")
	}
	dir := filepath.Join(base, "gox", Version)
	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
		return "", mkErr
	}
	return dir, nil
}

// storedIssue is the on-disk representation. token.Position contains an
// unexported offset field that would round-trip incorrectly through JSON;
// we flatten only the fields we need.
type storedIssue struct {
	Analyzer string `json:"analyzer"`
	Filename string `json:"filename"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Offset   int    `json:"offset"`
	Message  string `json:"message"`
	Hint     string `json:"hint,omitempty"`
}

type storedRecord struct {
	Issues []storedIssue `json:"issues"`
}

// Get returns the cached issues for the given key, or (nil, false) if there
// is no entry.
func Get(dir, key string) ([]analyzer.Issue, bool) {
	path := filepath.Join(dir, key+".json")
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		if !errors.Is(readErr, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "gox cache: read %s: %v\n", path, readErr)
		}
		return nil, false
	}
	var rec storedRecord
	if jsonErr := json.Unmarshal(data, &rec); jsonErr != nil {
		fmt.Fprintf(os.Stderr, "gox cache: decode %s: %v\n", path, jsonErr)
		return nil, false
	}
	out := make([]analyzer.Issue, len(rec.Issues))
	for i, s := range rec.Issues {
		out[i] = analyzer.Issue{
			Analyzer: s.Analyzer,
			Pos: token.Position{
				Filename: s.Filename,
				Line:     s.Line,
				Column:   s.Column,
				Offset:   s.Offset,
			},
			Message: s.Message,
			Hint:    s.Hint,
		}
	}
	return out, true
}

// Put stores the issues for the given key.
func Put(dir, key string, issues []analyzer.Issue) error {
	stored := make([]storedIssue, len(issues))
	for i, is := range issues {
		stored[i] = storedIssue{
			Analyzer: is.Analyzer,
			Filename: is.Pos.Filename,
			Line:     is.Pos.Line,
			Column:   is.Pos.Column,
			Offset:   is.Pos.Offset,
			Message:  is.Message,
			Hint:     is.Hint,
		}
	}
	data, jsonErr := json.Marshal(storedRecord{Issues: stored})
	if jsonErr != nil {
		return jsonErr
	}
	path := filepath.Join(dir, key+".json")
	tmp := path + ".tmp"
	if writeErr := os.WriteFile(tmp, data, 0o644); writeErr != nil {
		return writeErr
	}
	return os.Rename(tmp, path)
}

// AnalyzersVersion derives a stable identifier for the currently registered
// analyzer set. If the set changes (rules added, removed), cached entries
// from a previous build become invalid automatically because the key
// changes.
func AnalyzersVersion(analyzers []*analyzer.Analyzer) string {
	names := make([]string, len(analyzers))
	for i, a := range analyzers {
		names[i] = a.Name
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		fmt.Fprintln(h, n)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

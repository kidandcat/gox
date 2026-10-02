// Package baseline implements the "ignore pre-existing issues" workflow.
//
// `gox baseline` captures the current set of issues into a JSON file at the
// module root. `gox check` then auto-filters every issue that matches a
// baseline entry, so only NEW problems are reported.
//
// Issue identity is (file, analyzer, hash of the offending source line). This
// is robust against:
//
//   - unrelated code being inserted/deleted in the same file (the hash of
//     the buggy line stays the same even though its line number shifts)
//   - re-running gox with a different cache
//   - moving the project to a different machine (paths are stored relative
//     to the module root)
//
// It is NOT robust against:
//
//   - file renames (entries become stale; re-run `gox baseline`)
//   - editing the buggy line itself (the line hash changes; the issue is
//     reported as new, which is usually what you want)
//   - duplicate identical lines in the same file producing the same issue
//     (a small over-baselining; acceptable in practice)
package baseline

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mentasystems/gox/pkg/analyzer"
)

// Filename is the canonical name stored at the project root.
const Filename = ".gox-baseline.json"

// Version is the on-disk format version. Bump on incompatible changes.
const Version = 1

// Entry is a single baselined issue.
type Entry struct {
	File     string `json:"file"` // relative to ModuleRoot
	Analyzer string `json:"analyzer"`
	LineHash string `json:"line_hash"` // sha256(trimmed line content)[:16]
}

// File is the on-disk shape.
type File struct {
	Version          int       `json:"version"`
	CapturedAt       time.Time `json:"captured_at"`
	AnalyzersVersion string    `json:"analyzers_version"`
	IssueCount       int       `json:"issue_count"`
	Entries          []Entry   `json:"entries"`
}

// Build constructs a Baseline from a slice of issues. moduleRoot is the
// directory used to relativize file paths (every issue.Pos.Filename must be
// inside moduleRoot, otherwise it's skipped).
func Build(issues []analyzer.Issue, moduleRoot, analyzersVersion string) (*File, error) {
	entries := make([]Entry, 0, len(issues))
	for _, is := range issues {
		entry, err := makeEntry(is, moduleRoot)
		if err != nil {
			// Skip issues we can't fingerprint — leaving them out of the baseline
			// means they keep reporting until fixed, which is fine.
			fmt.Fprintf(os.Stderr, "gox baseline: skip %s:%d: %v\n", is.Pos.Filename, is.Pos.Line, err)
			continue
		}
		entries = append(entries, entry)
	}
	sortEntries(entries)
	return &File{
		Version:          Version,
		CapturedAt:       time.Now().UTC().Truncate(time.Second),
		AnalyzersVersion: analyzersVersion,
		IssueCount:       len(entries),
		Entries:          entries,
	}, nil
}

// Save writes the baseline to disk atomically.
func Save(path string, f *File) error {
	out, marshalErr := json.MarshalIndent(f, "", "  ")
	if marshalErr != nil {
		return marshalErr
	}
	out = append(out, '\n')
	tmp := path + ".tmp"
	if wErr := os.WriteFile(tmp, out, 0o644); wErr != nil {
		return wErr
	}
	return os.Rename(tmp, path)
}

// Load reads the baseline file. Returns (nil, nil) if the file does not exist.
func Load(path string) (*File, error) {
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return nil, nil
		}
		return nil, readErr
	}
	var f File
	if jsonErr := json.Unmarshal(data, &f); jsonErr != nil {
		return nil, fmt.Errorf("parse %s: %w", path, jsonErr)
	}
	if f.Version != Version {
		return nil, fmt.Errorf("baseline version %d is not supported by this gox (expected %d) — re-run `gox baseline`", f.Version, Version)
	}
	return &f, nil
}

// Filter returns the subset of `issues` that are NOT present in the baseline.
// Each baselined match is "consumed" once (so a real duplicate beyond what's
// in the baseline still gets reported).
func (f *File) Filter(issues []analyzer.Issue, moduleRoot string) []analyzer.Issue {
	if f == nil {
		return issues
	}
	// Build a count map of baseline entries.
	keyCount := map[string]int{}
	for _, e := range f.Entries {
		keyCount[entryKey(e)]++
	}
	out := make([]analyzer.Issue, 0, len(issues))
	for _, is := range issues {
		entry, mkErr := makeEntry(is, moduleRoot)
		if mkErr != nil {
			out = append(out, is)
			continue
		}
		k := entryKey(entry)
		if keyCount[k] > 0 {
			keyCount[k]--
			continue
		}
		out = append(out, is)
	}
	return out
}

// makeEntry computes the Entry fingerprint for a single issue.
func makeEntry(is analyzer.Issue, moduleRoot string) (Entry, error) {
	rel, relErr := relPath(
		/* absPath */ is.Pos.Filename,
		/* root */ moduleRoot,
	)
	if relErr != nil {
		return Entry{}, relErr
	}
	h, hashErr := lineHash(is.Pos.Filename, is.Pos.Line)
	if hashErr != nil {
		return Entry{}, hashErr
	}
	return Entry{File: rel, Analyzer: is.Analyzer, LineHash: h}, nil
}

func entryKey(e Entry) string {
	return e.File + "|" + e.Analyzer + "|" + e.LineHash
}

func relPath(absPath, root string) (string, error) {
	cleanRoot := filepath.Clean(root)
	cleanAbs := filepath.Clean(absPath)
	rel, err := filepath.Rel(cleanRoot, cleanAbs)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("file %s is outside module root %s", absPath, root)
	}
	return filepath.ToSlash(rel), nil
}

// lineHash returns a short hex digest of the trimmed content of the given
// 1-based line in `path`. Trimming makes the hash robust to indentation-only
// changes (gofmt etc.).
func lineHash(path string, line int) (string, error) {
	if line < 1 {
		return "", fmt.Errorf("invalid line number %d", line)
	}
	f, openErr := os.Open(path)
	if openErr != nil {
		return "", openErr
	}
	defer f.Close() // safe-ignore: read-only file
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for i := 1; scanner.Scan(); i++ {
		if i == line {
			sum := sha256.Sum256([]byte(strings.TrimSpace(scanner.Text())))
			return hex.EncodeToString(sum[:8]), nil
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return "", scanErr
	}
	return "", fmt.Errorf("file has fewer than %d lines", line)
}

func sortEntries(es []Entry) {
	sort.Slice(es, func(i, j int) bool {
		if es[i].File != es[j].File {
			return es[i].File < es[j].File
		}
		if es[i].Analyzer != es[j].Analyzer {
			return es[i].Analyzer < es[j].Analyzer
		}
		return es[i].LineHash < es[j].LineHash
	})
}

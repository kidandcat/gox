# Changelog

All notable changes to this project are documented here. The format is based
on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Releases before
this file existed are described on the
[GitHub releases page](https://github.com/kidandcat/gox/releases).

## [Unreleased]

## [0.6.0] - 2026-10-04

### Changed

- **Breaking — module path:** the module is now `github.com/kidandcat/gox`
  (the repository moved from `mentasystems/gox`). Install with
  `go install github.com/kidandcat/gox/cmd/gox@latest`. Versions up to
  v0.5.0 keep the old path `github.com/mentasystems/gox`; v0.6.0 and later
  cannot be installed under it.
- **Minimum Go is 1.26.8.** Go 1.25 is out of support, and `go 1.25.5` (the
  previous `go` line) ships a standard library affected by GO-2026-4602.
  `go install` now needs a toolchain that old.
- `gox check --json` prints one JSON object (`issues`, `issue_count`,
  `hidden`, `load_errors`) on stdout. Exit codes are unchanged. The same
  `--max-issues` cap applies.
- `gox cache clean` deletes the on-disk cache, including older version
  directories.
- `exhaustive` checks iota enums declared in **other packages of the same
  module** (the usual `model.Kind` switched in `service/`). Enums from the
  standard library and from third-party modules are still ignored. Constants
  that share a value (`const Default = Off`) count as one variant.
- `namedargs` no longer treats a dotless module (`module myapp`) as the
  standard library. Stdlib exemption uses `go list`'s `Standard` field.
- `httptimeout` flags `http.ListenAndServe`, `ListenAndServeTLS`, `Serve`,
  and `ServeTLS`.
- `errcheck` treats a result as an error only when it implements `error`
  (`Error() string`), including concrete types such as `*ValidationError`.
  An interface method that is merely named `Error` is no longer enough.
- `contextcheck` treats a function literal that receives its own
  `context.Context` as a root, so `errgroup.Go(func(ctx context.Context) error { context.Background() })`
  is reported.
- A package `go list` accepts but `go/types` does not is now a load error
  (exit 2) and its partial result is not cached.
- `gox check` / `gox baseline` resolve the module root from the first
  filesystem pattern, not only from the current directory.

- **Breaking — exit code:** `gox check` now **fails closed** when a package
  cannot be loaded (syntax error, type error, missing dependency). Such
  packages used to be printed to stderr and skipped, and the run exited `0`
  if nothing else was found. It now exits **`2`** and reports
  `gox: N package(s) failed to load or type-check; the report is incomplete`; any issues found
  are still printed. `gox baseline` refuses to write an incomplete snapshot
  in that case. Scripts or CI that relied on exit `0` for non-compiling code
  will now fail. ([#4](https://github.com/kidandcat/gox/pull/4))
- The cache key now also covers the export data of every dependency and a
  fingerprint of the gox binary. Results are no longer stale after a
  dependency's API changes or after gox is upgraded or rebuilt. The cache
  directory moved from `gox/v3` to `gox/v4`.
  ([#5](https://github.com/kidandcat/gox/pull/5))

### Fixed

- `bodyclose` matches `resp.Body.Close()` by `types.Object`. A shadowed
  `resp` in an inner block no longer counts as closing the outer response.
- `errorlint` understands explicit argument indexes (`%[1]v`) and `*` width
  or precision, which consumes an operand. String-literal escapes are
  unquoted before the format is read.
- The Stop hook sanitizes `session_id` before using it as a cache filename.
- Generated files are detected with `ast.IsGenerated` (the exact Go
  convention): markers after headers longer than 20 lines are recognized,
  and markers placed after the `package` clause no longer hide hand-written
  code. ([#6](https://github.com/kidandcat/gox/pull/6))
- `forcetypeassert`: `var x, ok = v.(T)` is accepted as comma-ok.
  ([#2](https://github.com/kidandcat/gox/pull/2))
- `bodyclose`: leaks inside closures are reported once instead of twice, and
  a response returned to the caller (`return resp, err`) is no longer
  flagged. ([#3](https://github.com/kidandcat/gox/pull/3))
- `gox install claude|grok` keeps the existing permissions of the settings
  file instead of rewriting it as `0644`. It also warns when `jq` (required
  by the hook) is missing, and the hook says so on stderr instead of doing
  nothing silently. ([#8](https://github.com/kidandcat/gox/pull/8))

### Internal

- Packages from one `go list` share a single export-data importer, so the
  standard library is decoded once and type identity stays intact. Internal
  test variants no longer re-walk production files, and baseline
  fingerprints read each source file once.
- Opt-out comments are indexed once per file. `goroutine`, `banany`,
  `noglobals`, and `exhaustive` use that index, so a comment group that
  ends on the flagged line suppresses the finding the same way
  `safe-ignore` already did.
- `pkg/analyzer`, `pkg/loader`, `pkg/cache`, and `pkg/baseline` are
  implementation packages. They are not a stable API.
- CI: `gofmt`, `go vet`, `go test -race`, `staticcheck` and `govulncheck`
  run on the minimum toolchain plus current stable. Also removed dead code
  (`entryHasGoxCommand`) and fixed gofmt drift.
  ([#7](https://github.com/kidandcat/gox/pull/7))
- An end-to-end golden test over `testdata/bad` exercises the full pipeline,
  and the stale `callsSwapOK` example was fixed.
  ([#9](https://github.com/kidandcat/gox/pull/9))
- Docs: `gox baseline` and the remaining `check` flags are documented, the
  `safe-ignore` coverage list is complete, and stale package comments were
  fixed. ([#10](https://github.com/kidandcat/gox/pull/10))

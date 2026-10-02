# Changelog

All notable changes to this project are documented here. The format is based
on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Releases before
this file existed are described on the
[GitHub releases page](https://github.com/mentasystems/gox/releases).

## [Unreleased]

### Changed

- **Breaking — exit code:** `gox check` now **fails closed** when a package
  cannot be loaded (syntax error, type error, missing dependency). Such
  packages used to be printed to stderr and skipped, and the run exited `0`
  if nothing else was found. It now exits **`2`** and reports
  `gox: N package(s) failed to load and were not analyzed`; any issues found
  are still printed. `gox baseline` refuses to write an incomplete snapshot
  in that case. Scripts or CI that relied on exit `0` for non-compiling code
  will now fail. ([#4](https://github.com/mentasystems/gox/pull/4))
- The cache key now also covers the export data of every dependency and a
  fingerprint of the gox binary. Results are no longer stale after a
  dependency's API changes or after gox is upgraded or rebuilt. The cache
  directory moved from `gox/v3` to `gox/v4`.
  ([#5](https://github.com/mentasystems/gox/pull/5))

### Fixed

- Generated files are detected with `ast.IsGenerated` (the exact Go
  convention): markers after headers longer than 20 lines are recognized,
  and markers placed after the `package` clause no longer hide hand-written
  code. ([#6](https://github.com/mentasystems/gox/pull/6))
- `forcetypeassert`: `var x, ok = v.(T)` is accepted as comma-ok.
  ([#2](https://github.com/mentasystems/gox/pull/2))
- `bodyclose`: leaks inside closures are reported once instead of twice, and
  a response returned to the caller (`return resp, err`) is no longer
  flagged. ([#3](https://github.com/mentasystems/gox/pull/3))
- `gox install claude|grok` keeps the existing permissions of the settings
  file instead of rewriting it as `0644`. It also warns when `jq` (required
  by the hook) is missing, and the hook says so on stderr instead of doing
  nothing silently. ([#8](https://github.com/mentasystems/gox/pull/8))

### Internal

- CI: `gofmt`, `go vet`, `go test -race`, `staticcheck` and `govulncheck`
  run on a Go 1.25 + stable matrix. Also removed dead code
  (`entryHasGoxCommand`) and fixed gofmt drift.
  ([#7](https://github.com/mentasystems/gox/pull/7))
- An end-to-end golden test over `testdata/bad` exercises the full pipeline,
  and the stale `callsSwapOK` example was fixed.
  ([#9](https://github.com/mentasystems/gox/pull/9))
- Docs: `gox baseline` and the remaining `check` flags are documented, the
  `safe-ignore` coverage list is complete, and stale package comments were
  fixed. ([#10](https://github.com/mentasystems/gox/pull/10))

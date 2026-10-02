# gox

[![ci](https://github.com/mentasystems/gox/actions/workflows/ci.yml/badge.svg)](https://github.com/mentasystems/gox/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/mentasystems/gox)](https://github.com/mentasystems/gox/releases/latest)
[![license](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)

Strict static analyzer for Go. Zero external dependencies — every rule is
implemented from scratch on top of `go/ast`, `go/types`, and the `go list`
command.

![demo](demo/demo.gif)

The bug above (`transfer("o-42", "u-7")` with parameters declared as
`userID, orderID string`) compiles, passes every test, and ships to
production. No major Go linter today catches this class. `gox` does, by
demanding inline `/* paramName */` comments at the call site whenever
two adjacent arguments share a type. (`namedargs` lives in the opt-in
style tier — see [Rules](#rules).)

The goal: catch the classes of bugs an LLM writing Go without supervision is
most likely to introduce. Be loud, be opinionated, fail closed.

> **Status: experimental.** Built as a tool for using Claude Code, Grok Build,
> and similar agents to write Go without leaving silent bugs behind. Used
> daily, but the rule set is still evolving — expect breaking changes to
> annotation syntax until v1.0.

## Install

```sh
go install github.com/mentasystems/gox/cmd/gox@latest
```

## Use

```sh
gox check ./...        # run the default (bug-tier) analyzers; exit 1 on any issue
gox check --all ./...  # one-off full run: also the opt-in style tier (env: GOX_ALL=1)
gox check --skip=errcheck ./...
                       # skip named analyzers for this run (env: GOX_SKIP)
gox list               # list registered analyzers (opt-in ones are marked)
gox explain <rule>     # print the rule's reference markdown
gox build [args]       # gox check && go build
gox test  [args]       # gox check && go test
gox baseline [pkgs]    # snapshot current issues into .gox-baseline.json
```

Other `check` flags: `--no-cache` (bypass the incremental cache),
`--no-baseline` (report baselined issues too), `--stats` (cache hits/misses
and elapsed time on stderr), `--max-issues=N` (see [Output cap](#output-cap)).

`gox build` / `gox test` always run the default `gox check` over `./...` of
the current module; `[args]` are passed only to `go build` / `go test`.

### Baseline

On an existing codebase, `gox baseline` runs every analyzer (bug and style
tier) and writes the current findings to `.gox-baseline.json` at the module
root. From then on `gox check` only reports *new* issues. Entries are keyed
by file, analyzer and a hash of the offending line, so they survive
unrelated edits that shift line numbers; editing the flagged line itself
makes the issue reappear. Commit the file so the team shares the same view.

## Rules

Rules come in two tiers. The **bug tier** runs by default. The **style tier**
is opt-in (`gox check --all`): a month of real agent transcripts showed that,
enforced in an agent loop, style rules overwhelmingly produce suppression
annotations rather than fixes — noise that drowns the bug tier.

Bug tier (default):

| Analyzer | What it catches |
|---|---|
| `errcheck` | `error` return values dropped silently |
| `forcetypeassert` | `x := v.(T)` without the comma-ok form |
| `exhaustive` | non-exhaustive switch on iota enums or sealed interfaces |
| `bodyclose` | `*http.Response.Body` left unclosed |
| `contextcheck` | `context.Background()`/`TODO()` inside a function that already receives a `context.Context` |
| `errorlint` | `==` / type-assert / `%s` on errors instead of `errors.Is` / `errors.As` / `%w` |
| `httptimeout` | HTTP shortcut calls, zero-Timeout `http.Client` (`var`/`new`/literal), or `http.Server` missing `ReadHeaderTimeout`/`WriteTimeout` |

Style tier (opt-in, `--all`):

| Analyzer | What it catches |
|---|---|
| `shadow` | `:=` re-declaring an outer variable (except `ok`) |
| `namedargs` | call sites passing 2+ args of the same basic type without `/* paramName */` comments (user code only — stdlib calls exempt) |
| `noglobals` | mutable package-level `var` declarations |
| `banany` | `any` / `interface{}` in declarations without justification |
| `goroutine` | `go f()` without a visible `*errgroup.Group`, `sync.WaitGroup`, or `context.CancelFunc` |

## Annotations

Every rule has a single opt-out marker. Annotations must include a reason
after the colon — empty reasons are ignored.

| Comment | Effect |
|---|---|
| `// safe-ignore: <why>` | Suppress `errcheck`, `forcetypeassert`, `bodyclose`, `contextcheck`, `errorlint`, `shadow`, `namedargs` on the same line |
| `// global-ok: <why>` | Allow a package-level `var` (`noglobals`) |
| `// any-ok: <why>` | Allow `any` / `interface{}` (`banany`) |
| `// goroutine-ok: <why>` | Allow a fire-and-forget `go` statement (`goroutine`) |
| `// exhaustive-ok: <why>` | Accept a `default:` case as covering missing variants (`exhaustive`) |
| `// timeout-ok: <why>` | Allow an HTTP call, zero-Timeout client, or `http.Server` literal without the required timeouts (`httptimeout`) |

## Rule reference

The compiled `gox` binary ships every rule's reference page embedded as
markdown. Print one to stdout:

```sh
gox explain bodyclose
```

The same content is available as a JSON envelope for agent consumption:

```sh
gox explain bodyclose --json
# { "rule": "bodyclose", "doc": "...", "explanation": "..." }
```

Each reference page covers what the rule catches, a bad / good example, the
opt-out syntax, and the analyzer's known limitations. The reference is
version-pinned to the binary you have installed — when a rule grows a new
edge case, `gox explain` reflects it without depending on an external docs
site.

## Claude Code / Grok Build integration

```sh
gox install claude
gox install grok
```

`gox install claude` writes `~/.claude/gox-hook.sh` and registers it as a
`Stop` hook in `~/.claude/settings.json` (timeout 30s). It is idempotent and
migrates legacy `PostToolUse` registrations automatically. Preserves every
other key in the file.

`gox install grok` writes `~/.grok/hooks/gox-hook.sh` and registers the same
Stop hook in `~/.grok/hooks/gox.json` (native Grok hook file). Multiple
`*.json` files under `~/.grok/hooks/` are merged; the command only touches
the Stop array for our entry and leaves other events or user content intact.

When the agent finishes a turn, the hook scans the current git repo for
changed `.go` files (unstaged, staged, and untracked), narrows them to the
files actually modified during that turn (mtime >= the last user message's
timestamp, read from the transcript; without a transcript — e.g. Grok — it
falls back to all dirty files), and runs `gox check` once per affected
package. If issues are found, the hook returns a
`{"decision":"block","reason":"..."}` payload. Claude surfaces it on the
next turn; Grok records it as a hook annotation in the scrollback (and, for
Stop hooks, the same shape is supported for feeding issues back).

The hook runs once per turn (on `Stop`) rather than on every edit. Earlier
versions used `PostToolUse` on each `Edit`/`Write`, which was too slow on
large packages.

Two guards keep the hook's signal-to-noise high:

- **Only the bug tier runs** — the hook calls plain `gox check`, which
  defaults to the bug-tier rules. To change what the hook enforces, pass
  flags through `GOX_HOOK_FLAGS` (e.g. `"--all"` or `"--skip=errcheck"`).
- **Identical reports are never repeated**: the hook remembers (per session,
  under `~/.cache/gox/hook/`) the last output it blocked with and stays
  silent if a later turn would produce the exact same report. This also
  prevents stop-loops.

- Claude Code re-reads `settings.json` when `/hooks` is opened or the app
  restarts.
- Grok re-reads `~/.grok/hooks/*.json` at session start and on `l` (reload)
  inside the hooks modal (Ctrl+L or `/hooks`).

The hook resolves `gox` via `$GOX_BIN` if set, otherwise `~/go/bin/gox`.
Run `go install github.com/mentasystems/gox/cmd/gox@latest` to ensure it is
present. Because Grok also reads `~/.claude/settings.json` for compatibility,
`gox install claude` works for Grok users too; the native `grok` target is
recommended when you only use Grok Build.

## Performance

A pure-Go implementation with no runtime overhead from external linters.
On a 442-package monorepo (~1800 .go files):

| | Time |
|---|---|
| Cold run (no cache) | ~9.4s |
| Warm run (full cache hit) | ~2.6s |

The cache is per-package, keyed by file mtime+size and the analyzer set
hash. It lives under `$XDG_CACHE_HOME/gox/v3` (or `~/.cache/gox/v3`).
Pass `--no-cache` to disable.

## Output cap

`gox check` prints at most 100 issues by default, then a one-line summary of
how many were hidden. This keeps the output bounded for context-limited
consumers — notably the Claude Code Stop hook, which pipes `gox check`
straight back into the model. Raise or lift the cap with `--max-issues=N`
(`0` = unlimited) or the `GOX_MAX_ISSUES` env var. Issues are sorted by
`file:line`, so the truncation is deterministic.

## Generated code

Files marked with the standard Go marker
`// Code generated <generator> DO NOT EDIT.` near the top are skipped
automatically. This covers `protoc-gen-go`, `yo`, `mockgen`, and most
common generators.

## How is this different from staticcheck / golangci-lint / revive?

Short answer: every existing Go linter is a **detector**. gox is a **gate**.

The existing tools surface warnings and let humans decide. That works when
the human is the one writing the code. When an LLM is writing the code at
the speed of a few tokens per second and no one is reviewing line-by-line,
"surface a warning" is the wrong default — the warning will be ignored
unless something downstream refuses to proceed.

Concrete differences:

| | gox | golangci-lint / staticcheck / revive |
|---|---|---|
| **Default severity** | every rule is an error | most rules are warnings |
| **Opt-out** | one annotation with a written reason on the same line | regex/path config files |
| **Dependencies** | none — only Go stdlib + `go list` | hundreds of transitive deps via `golang.org/x/tools` |
| **Audience** | code written by LLM agents (Claude Code, Cursor, etc.) | humans, optionally CI |
| **Coverage** | 12 rules, picked for high signal in unsupervised codegen | hundreds of rules; you pick the subset |
| **Ergonomics** | `gox install claude` / `gox install grok` wires it into the LLM's tool loop | manual config + `pre-commit` / CI plumbing |

The bug that motivated gox is the swap-prone call site:

```go
transfer(orderID, userID)   // vs transfer(userID, orderID)
```

Both compile. Both pass tests. The wrong one ships and corrupts the
ledger. No major linter catches this today; gox's `namedargs` rule forces
inline `/* paramName */` comments on adjacent same-type arguments at the
call site, which:
- catches the bug,
- documents the call site,
- and the LLM annotating its own code is essentially free.

Each labelled argument must be on its own line — single-line labels are not a
`gofmt` fixed point (gofmt relocates the comment onto the previous argument and
silently mis-names it), so the rule rejects them:

```go
transfer(
	/* from */ srcID,
	/* to */ dstID,
	amount,
)
```

That trade — "more typing at the call site, near-zero bugs of this class"
— only makes sense when typing is cheap, which is now.

If you already love golangci-lint, keep it. gox is meant to live alongside
it, not replace it: golangci-lint is the spell-checker, gox is the
production gate.

## Design notes

- **Zero external dependencies.** Everything uses Go stdlib + a shell-out to
  `go list -json -e -export -compiled -deps -test`. No `golang.org/x/tools`,
  no third-party linter packages.
  Minimal maintenance: when a new Go release ships, there's nothing to
  update.
- **Fail closed.** Every rule defaults to error. Opt-outs require an explicit
  annotation with a written reason — the reason is the documentation.
- **Targeted at LLM-written code.** Heuristics are tuned so each rule catches
  a high-frequency LLM bug class without flooding human-readable idioms. For
  example, `shadow` exempts `ok` (the universal comma-ok name) but still
  catches `err` re-declaration, which is exactly the bug we want.
- **`namedargs` is the killer rule.** Two adjacent string/int/bool parameters
  in user-defined code force the call site to label them with inline comments.
  Stdlib calls are exempt because their conventions are memorized. The bug it
  prevents — `transfer(orderID, userID)` vs `transfer(userID, orderID)` —
  produces no compile error and no test failure, and is the single most
  common silent-bug class in unsupervised AI-written Go.

## Contributing

Issues and pull requests welcome. Two ground rules:

1. **No external dependencies.** Every rule must be implementable with the
   Go standard library plus an out-of-process `go list -json` call. Adding
   `golang.org/x/tools/go/packages`, `staticcheck`, or any third-party
   linter is out of scope.
2. **Each new rule must pass its own check.** Run `gox check ./...` on a
   fresh clone before opening a PR — gox runs against its own source as
   part of the smoke test.

## License

BSD 3-Clause. See `LICENSE`.

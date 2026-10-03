#!/usr/bin/env bash
# Stop hook for Claude Code and Grok Build: when the turn ends, find the .go
# files that changed DURING THIS TURN and run `gox check` once per affected
# package. If issues are found, return them as a blocking JSON object so the
# model sees them in its next turn (or as a hook annotation in Grok).
#
# Design notes (learned from a month of transcripts):
#   - Turn scoping: only git-dirty .go files whose mtime is >= the timestamp
#     of the last real user message are checked. Without this, a file left
#     dirty in git re-blocks every later turn of every session that touches
#     the repo, even turns that never edited it (the same issue was observed
#     re-reported 73 times). If the transcript is unavailable (e.g. Grok),
#     fall back to all dirty files.
#   - Check selection: `gox check` itself defaults to the bug-tier analyzers
#     (style tier is opt-in via --all), so the hook adds no filtering of its
#     own. Pass extra flags with GOX_HOOK_FLAGS (e.g. "--all").
#   - Dedup: if the output is byte-identical to what this session was already
#     blocked with, do not block again (kills repeat-spam and stop-loops).
#
# Installed by: gox install claude  or  gox install grok
# Schema reference (Claude): https://docs.anthropic.com/en/docs/claude-code/hooks
# Grok reads the same Stop hook shape (and also ~/.claude/settings.json for compat).

set -u

PAYLOAD=$(cat)

# jq parses the payload and builds the block decision. Without it every
# check below would silently degrade to "no issues", so say so on stderr.
if ! command -v jq >/dev/null 2>&1; then
  echo "gox hook: jq not found in PATH; skipping gox check (install jq to enable the hook)" >&2
  exit 0
fi
CWD=$(printf '%s' "$PAYLOAD" | jq -r '.cwd // empty')
[ -n "$CWD" ] || CWD="$PWD"
[ -d "$CWD" ] || exit 0

GOX="${GOX_BIN:-${HOME}/go/bin/gox}"
[ -x "$GOX" ] || exit 0

# Extra flags for `gox check` (e.g. GOX_HOOK_FLAGS="--all" to include the
# opt-in style tier).
HOOK_FLAGS="${GOX_HOOK_FLAGS:-}"

# Resolve git root; bail quietly if not a repo.
ROOT=$(cd "$CWD" && git rev-parse --show-toplevel 2>/dev/null) || exit 0

# Collect changed/staged/untracked .go files.
FILES=$(
  cd "$ROOT" && {
    git diff --name-only -- '*.go'
    git diff --name-only --cached -- '*.go'
    git ls-files --others --exclude-standard -- '*.go'
  } 2>/dev/null | sort -u
)
[ -n "$FILES" ] || exit 0

# Turn start = timestamp of the last real user message in the transcript
# (string content, or an array containing a "text" block — tool_results and
# isMeta hook-feedback entries don't count, so a re-stop after a gox block
# keeps the same turn window).
TRANSCRIPT=$(printf '%s' "$PAYLOAD" | jq -r '.transcript_path // empty')
# session_id becomes a filename under ~/.cache. Keep it to a safe alphabet
# so a surprising payload cannot escape that directory.
SESSION=$(printf '%s' "$PAYLOAD" | jq -r '.session_id // empty' | tr -cd 'A-Za-z0-9_-')
TURN_EPOCH=""
if [ -n "$TRANSCRIPT" ] && [ -f "$TRANSCRIPT" ]; then
  TURN_EPOCH=$(
    jq -r '
      select(.type? == "user" and ((.isMeta? // false) | not))
      | select(
          ((.message.content? | type) == "string")
          or (((.message.content? | type) == "array")
              and ([.message.content[]? | select(type == "object" and .type? == "text")] | length > 0))
        )
      | .timestamp // empty
    ' "$TRANSCRIPT" 2>/dev/null \
      | tail -1 \
      | jq -rR 'select(length > 0) | sub("\\.[0-9]+Z$"; "Z") | try fromdateiso8601 catch empty' 2>/dev/null
  )
fi

case "$(uname -s)" in
  Darwin) mtime_of() { stat -f %m -- "$1" 2>/dev/null; } ;;
  *)      mtime_of() { stat -c %Y -- "$1" 2>/dev/null; } ;;
esac

# Unique directories of the files edited this turn (all dirty files when the
# turn start is unknown).
DIRS=$(
  while IFS= read -r f; do
    [ -n "$f" ] || continue
    [ -f "$ROOT/$f" ] || continue
    if [ -n "$TURN_EPOCH" ]; then
      M=$(mtime_of "$ROOT/$f")
      [ -n "$M" ] && [ "$M" -ge "$TURN_EPOCH" ] || continue
    fi
    dirname "$ROOT/$f"
  done <<< "$FILES" | sort -u
)
[ -n "$DIRS" ] || exit 0

OUT=""
while IFS= read -r d; do
  [ -d "$d" ] || continue
  # shellcheck disable=SC2086 — HOOK_FLAGS is intentionally word-split
  RES=$(cd "$d" && "$GOX" check $HOOK_FLAGS . 2>&1)
  if [ -n "$RES" ]; then
    OUT="${OUT}${OUT:+$'\n\n'}${RES}"
  fi
done <<< "$DIRS"

[ -n "$OUT" ] || exit 0

# Dedup: never block the same session twice with byte-identical output.
if [ -n "$SESSION" ]; then
  STATE_DIR="${HOME}/.cache/gox/hook"
  mkdir -p "$STATE_DIR" 2>/dev/null && {
    find "$STATE_DIR" -type f -mtime +7 -delete 2>/dev/null
    HASH=$(printf '%s' "$OUT" | cksum | tr -s ' ' '-')
    STATE_FILE="$STATE_DIR/$SESSION"
    if [ -f "$STATE_FILE" ] && [ "$(cat "$STATE_FILE" 2>/dev/null)" = "$HASH" ]; then
      exit 0
    fi
    printf '%s' "$HASH" > "$STATE_FILE" 2>/dev/null
  }
fi

jq -nR --arg msg "$OUT" '{
  decision: "block",
  reason: ("gox found issues in the Go files you edited this turn. Fix them, or annotate genuinely intentional cases (// safe-ignore: <why> for errcheck; `gox explain <rule>` shows each rule'"'"'s annotation):\n\n" + $msg + "\n\nAfter resolving these, repeat your full end-of-turn summary as your final message so it stays visible (otherwise the gox output buries it).")
}'

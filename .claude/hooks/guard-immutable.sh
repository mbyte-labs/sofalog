#!/usr/bin/env bash
# PreToolUse guard for sofalog.
#
# Blocks writes that CLAUDE.md forbids in prose, because a machine check beats a
# rule nobody re-reads:
#
#   1. Editing a FROZEN migration under server/migrations/ -> append-only
#   2. Anything under data/                        -> runtime volume (SQLite + assets)
#   3. Secrets (.env, *.pem, *credentials*)        -> never edited by tooling
#
# Rule 1 has a pre-deploy carve-out (D-016): while server/migrations/FROZEN does
# not exist, sofalog has never been deployed, so the single baseline schema
# 0001_init.sql is editable in place and dev databases are recreated. Creating
# FROZEN at the first deployed release turns append-only back on for good.
#
# Exit 2 + a message on stderr blocks the tool call and tells Claude why.
# Everything else exits 0.
#
# Input: Claude Code hook JSON on stdin.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

payload="$(cat)"

if command -v jq >/dev/null 2>&1; then
  file="$(printf '%s' "$payload" | jq -r '.tool_input.file_path // empty' 2>/dev/null)"
else
  file="$(printf '%s' "$payload" | sed -n 's/.*"file_path"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)"
fi

[ -n "$file" ] || exit 0

# Normalise to a repo-relative path when possible.
case "$file" in
  /*) rel="${file#"$repo_root/"}" ;;
  *)  rel="$file" ;;
esac

deny() {
  printf 'BLOCKED by .claude/hooks/guard-immutable.sh: %s\n' "$1" >&2
  exit 2
}

# The baseline schema. Editable in place only while the repo is un-frozen.
baseline="server/migrations/sql/0001_init.sql"
frozen="$repo_root/server/migrations/FROZEN"

case "$rel" in
  server/migrations/*|migrations/*)
    # New migrations are fine; rewriting a frozen one is not.
    if [ -f "$repo_root/$rel" ]; then
      if [ ! -e "$frozen" ] && [ "$rel" = "$baseline" ]; then
        : # pre-deploy baseline (D-016) — editing it is the intended workflow
      else
        deny "$rel already exists. Migrations are append-only (see CLAUDE.md). Add a new numbered migration instead of editing this one."
      fi
    fi
    ;;
esac

case "$rel" in
  data/*|*/data/*)
    deny "$rel is inside the runtime data volume (SQLite DB, downloaded assets, provider cache). The app writes there at runtime; tooling must not."
    ;;
esac

case "$rel" in
  .env|.env.*|*/.env|*/.env.*|*.pem|*.key|*credentials*|*Credentials*)
    deny "$rel looks like a secret or credential file. Ask the user to edit it themselves."
    ;;
esac

exit 0

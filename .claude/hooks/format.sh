#!/usr/bin/env bash
# PostToolUse formatter for sofalog.
#
# Formats the single file that was just edited. Every stage is guarded so this is
# completely inert until the corresponding toolchain exists, then activates on its
# own. It ALWAYS exits 0 — formatting must never block a turn.
#
# Input: Claude Code hook JSON on stdin.

# Hooks run in a non-login shell, so the usual toolchain locations may be missing.
export PATH="/usr/local/go/bin:$HOME/go/bin:/opt/homebrew/bin:/usr/local/bin:$PATH"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

payload="$(cat)"

# jq is the normal path; fall back to a crude grep so a missing jq is not a failure.
if command -v jq >/dev/null 2>&1; then
  file="$(printf '%s' "$payload" | jq -r '.tool_input.file_path // empty' 2>/dev/null)"
else
  file="$(printf '%s' "$payload" | sed -n 's/.*"file_path"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)"
fi

[ -n "$file" ] || exit 0
[ -f "$file" ] || exit 0

case "$file" in
  *.go)
    # No module yet -> nothing to format against. The Go module lives at server/.
    [ -f "$repo_root/server/go.mod" ] || exit 0

    if command -v goimports >/dev/null 2>&1; then
      goimports -w "$file" >/dev/null 2>&1
    elif command -v gofmt >/dev/null 2>&1; then
      gofmt -w "$file" >/dev/null 2>&1
    fi

    # Package-scoped vet only, and only once dependencies are resolved.
    # Never `go vet ./...` per edit — it is far too slow for an edit hook.
    if [ -f "$repo_root/server/go.sum" ] && command -v go >/dev/null 2>&1; then
      pkg_dir="$(dirname "$file")"
      case "$pkg_dir" in
        "$repo_root/server")   rel_pkg="." ;;
        "$repo_root/server"/*) rel_pkg="${pkg_dir#"$repo_root/server/"}" ;;
        *)                     exit 0 ;; # Go file outside the module — nothing to vet
      esac
      (cd "$repo_root/server" && go vet "./$rel_pkg" >/dev/null 2>&1)
    fi
    ;;

  *.svelte|*.ts|*.js|*.mjs|*.cjs|*.json|*.css|*.html)
    # Use the LOCAL prettier only. `npx prettier` would trigger a download on the
    # first edit, which is not something an edit hook should ever do.
    prettier="$repo_root/frontend/node_modules/.bin/prettier"
    [ -x "$prettier" ] || exit 0
    "$prettier" --write "$file" >/dev/null 2>&1
    ;;
esac

exit 0

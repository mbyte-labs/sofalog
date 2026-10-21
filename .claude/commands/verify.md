---
description: Run the full sofalog gate — format, vet, tests, frontend check, build, OpenAPI drift, and the invariant greps
---

Run the checks below **in order**. Skip any stage whose toolchain or directory does not exist yet
(this project is built incrementally — a skipped stage is a normal result, not a failure). Then print
one compact table of `pass` / `fail` / `skipped` per stage, followed by the details of any failure.

Do not fix anything while running this. Report first; fix only if the user asks.

### 1. Backend format, vet, tests
Skip entirely if there is no `server/go.mod`, or if `go` is not on `PATH` (Go may not be installed
yet).

```bash
cd server && gofmt -l .            # must print nothing
cd server && go vet ./...
cd server && go test ./...
```

### 2. Frontend check, lint, build
Skip if there is no `frontend/package.json`. If `frontend/node_modules` is missing, report the stage
as skipped and say `npm ci` is needed — do not run an install yourself.

```bash
cd frontend && npm run check && npm run lint && npm run build
```

### 3. Full build
Skip if there is no `server/go.mod`.

```bash
cd server && go build .
```

### 4. OpenAPI drift
Skip if `api/openapi.yaml` does not exist.

- Confirm the spec parses as YAML.
- List the routes registered in the feature packages' `handlers*.go` files
  (`grep -rn --include='handlers*.go' 'mux.HandleFunc' server/` — recursive, since features live
  at `server/features/<feature>/` and `server/auth/`), plus any route registered directly in
  `server/main.go`, and confirm each appears as a path in `api/openapi.yaml`. Report any route
  missing from the spec, and any spec path with no matching route.

This exists because `CLAUDE.md` requires the spec to land in the same commit as the endpoint, and
nothing else enforces it.

### 5. Invariant greps
Cheap checks for the rules that tests will not catch — including the layering rules, which are a
file convention inside each feature package (D-014), and the no-sibling-import rule between feature
packages (D-037). Each should find **nothing**; report
any hit with file and line.

```bash
# D-001: no shared items table, no media_type discriminator in the schema.
# (media_type IS legitimate in asset_jobs / pending_deletions — only flag CREATE TABLE
#  columns on catalog or per-user tables, and any `items` table.)
grep -rn "media_type" server/migrations/ 2>/dev/null
grep -rniE "create table (if not exists )?items" server/migrations/ 2>/dev/null

# No provider call on a render path: handlers must never talk to a provider directly.
# ytimg is in the list because video thumbnails proxy from YouTube (D-017) — a handler
# reaching it directly, or the SPA hot-linking it, is the failure that decision rules out.
grep -rnE "http\.(Get|Post|Client)|themoviedb|api\.steampowered|steamcdn|ytimg" \
  --include="handlers*.go" server/ 2>/dev/null

# Layering (D-014): service and store files never touch net/http.
grep -rnE "net/http|http\." --include="service*.go" --include="store*.go" server/ 2>/dev/null

# Layering (D-014): SQL lives only in store*.go files, video/common/status.go and migrations.
# videotest is test-only support (seed helpers write rows directly), so it is excluded like _test.go.
grep -rlEi "INSERT INTO|DELETE FROM|UPDATE [a-z_]+ SET|SELECT .+ FROM" \
  --include="*.go" server/ 2>/dev/null \
  | grep -vE "store[^/]*\.go$|/video/common/status\.go$|_test\.go$|/videotest/"

# D-037: a feature package imports only its component's common — never a sibling feature.
# Only the component root (video.go, the wiring) imports features; tests may import siblings.
grep -rn '"mbyte.ro/sofalog/features/video/' --include="*.go" server/features/video/*/ 2>/dev/null \
  | grep -v "_test\.go:" | grep -vE 'features/video/common"'

# D-037: videotest is test-only — no production file may import it.
grep -rln '"mbyte.ro/sofalog/features/video/videotest"' --include="*.go" server/ 2>/dev/null \
  | grep -vE "_test\.go$|/videotest/"

# SPA must not bake absolute asset URLs (breaks sub-path reverse-proxy deployments).
grep -rnE "[\"'(]/assets/|[\"']/api/v1" frontend/src/ 2>/dev/null

# No provider image URL may reach the browser — every image goes through a local proxy.
grep -rniE "image\.tmdb\.org|ytimg|img\.youtube" frontend/src/ 2>/dev/null

# D-010: watch status has exactly ONE writer. Anything outside server/features/video/common/status.go is a
# bug. Tests are excluded like in the SQL-location grep: they legitimately write states directly to
# exercise the real writer, and to assert what the CHECK refuses (a movie's status is only ever
# 'watched' — SPEC.md §6 — so a seeded 'dropped' movie row is a rejection test, not a fixture).
grep -rnE "UPDATE (user_show_states|user_movie_states)|INTO (user_show_states|user_movie_states)" \
  server/ 2>/dev/null | grep -vE "server/features/video/common/status\.go|server/migrations/|_test\.go|/videotest/"

# D-010: progress is never persisted — no counters, no percentages, no last-watched pointer.
grep -rniE "progress_|percent|last_watched|watched_count|episodes_watched" server/migrations/ 2>/dev/null

# user_watch_history fans out (many rows per target) — a LEFT JOIN silently inflates counts.
grep -rniE "left join +user_watch_history" server/ 2>/dev/null
```

### Report

```
stage                    result
1 backend fmt/vet/test   pass | fail | skipped (reason)
2 frontend check/build   ...
3 go build               ...
4 openapi drift          ...
5 invariant greps        ...
```

Both toolchains exist now (`server/go.mod`, `frontend/package.json`), so every stage should
actually run — a skip indicates a missing toolchain on this machine, not a normal result.

---
name: go-backend
description: Use when writing or changing Go code in sofalog — the feature packages under server/ (auth, video, games), their handlers*/service*/store* files, the shared config/httpx/db/provider/assets/web packages, server/main.go wiring, config and environment loading, sessions, local auth, API tokens, or the OpenAPI spec in api/openapi.yaml. Covers the thin-handler layering convention, error-to-HTTP-status mapping, propagating the authenticated user, trusted-proxy and BASE_URL handling, go:embed of the frontend build, and table-driven tests.
---

# sofalog Go backend

Go 1.23+. One binary, embedded SPA, SQLite primary. See `sofalog-schema` for the DDL and
`providers-and-assets` for anything touching TMDB / Steam.

## Package layout and contracts

The Go module lives at `server/` and is named `mbyte.ro/sofalog`. Media components live under
`server/features/`, each split into one package per feature (D-037), so imports read
`mbyte.ro/sofalog/features/video/upnext` while package names in code stay the bare feature names
(`upnext.Service`). `auth` is also a feature but stays top-level (it is cross-cutting). There is
no `internal/` and no `cmd/` (D-014); `package main` is `server/main.go`.

```
server/main.go          Config load → open DB → run migrations → wire features → start server + workers
server/config           Env parsing, fail-fast validation. No other package reads os.Getenv.
server/httpx            Middleware (request id, logging, real-client-IP, recovery), error
                        sentinels, the JSON error envelope, writeJSON/decodeJSON helpers.
server/db               Open SQLite/Postgres, pragmas, InTx, migration runner. The ONLY
                        package that may import a DB driver.
server/migrations       Append-only numbered SQL files, embedded and applied at startup.
server/provider         Shared TMDB / Steam clients (see providers-and-assets).
server/assets           Download queue, path builders, cache pruning, deletion sweep.
server/auth             FEATURE, kept top-level: users, argon2id, sessions, login handlers,
                        session middleware.
server/features/video   COMPONENT: movies + shows. video.go is wiring only (builds common.Core and
                        every feature, registers routes, runs the background loops).
  common/               What 2+ features share: Core (deps + shared catalog operations as
                        methods), catalog stores, status.go (the status single writer), DTOs.
  upnext/ upcoming/ history/ watches/ ratings/ lists/ copies/ jellyfin/ details/ people/
  discover/ media/ restore/ tmdbaccount/ refresh/
                        One package per feature; each imports common, never a sibling.
  videotest/            Test-only support (DB, seeds, provider stubs); imported from _test.go only.
server/features/games   COMPONENT (M4) — same shape.
server/web              Serves the embedded SPA + the SPA fallback route.
```

Each feature is **one package** (D-037); layering inside it is a **file convention**, direction
strictly one way — `handlers*.go` → `service*.go` → `store*.go`:

- `handlers*.go` — /api/v1 handlers + route registration. No SQL, no domain branching.
- `service*.go` — ALL domain logic. Never references `net/http`; an `*http.Request` in a service
  function is a layering bug. No SQL strings.
- `store*.go` — ALL SQL. Every personal-state query filters on `user_id`.

Features depend on the shared packages and their component's `common`, **never on each other** —
anything two features need moves into `common`. The compiler enforces the direction (an import
cycle is an error); `/verify` greps enforce the no-sibling rule and the file convention.

A feature's skeleton is two small files: `service.go` declares `type Service struct{ *common.Core }`
(plus any runner fields) and `NewService(c *common.Core)`; `handlers.go` declares `Handler`,
`NewHandler(svc)` and `RegisterRoutes(mux)` with the feature's own `mux.HandleFunc` lines. The
embedded `*common.Core` is why `s.DB`, `s.Now()` and shared operations like `s.RefreshMovie(…)` read
the same in every feature. Register a new feature in `video.go` (`NewService`, `RegisterRoutes`,
and `Run` if it owns a loop).

## The handler → service → store slice

Copy this shape. Three files, one job each — same package, so no interfaces or DTO copying between
the layers, just the naming discipline.

**`server/features/video/watches/handlers_movies.go`** (illustrative) — no domain logic, no SQL, no branching on business rules:

```go
func (h *Handler) markMovieWatched(w http.ResponseWriter, r *http.Request) {
	userID := httpx.UserID(r.Context()) // set by auth middleware; always present here

	movieID, err := pathInt(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "movie id must be an integer")
		return
	}

	var req struct {
		WatchedAt string `json:"watched_at"` // RFC3339 instant; "" = now
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}

	state, err := h.svc.MarkMovieWatched(r.Context(), userID, movieID, req.WatchedAt)
	if err != nil {
		writeServiceError(w, err) // the ONLY error path — see the mapping table
		return
	}
	writeJSON(w, http.StatusOK, toMovieStateDTO(state))
}
```

**`server/features/video/watches/service_movies.go`** — the rules live here. Note the whole thing is one transaction:
the watch row and the status it implies must never be written apart.

```go
// userID is an EXPLICIT parameter. Never read the authenticated user out of ctx
// inside service code: it makes the dependency invisible and untestable.
func (s *Service) MarkMovieWatched(ctx context.Context, userID, movieID int64, watchedAt string) (MovieState, error) {
	// A watch is an instant, not a day. Normalise whatever offset the client
	// sent to UTC: the column holds one representation of an instant, so a
	// string comparison in SQL is a chronological one.
	at := s.now()
	if watchedAt != "" {
		t, err := time.Parse(time.RFC3339, watchedAt)
		if err != nil {
			return MovieState{}, fmt.Errorf("%w: watched_at must be an RFC3339 timestamp", httpx.ErrValidation)
		}
		at = t.UTC().Format(time.RFC3339)
	}

	var out MovieState
	err = s.db.InTx(ctx, func(tx db.Tx) error {
		if _, err := getMovie(ctx, tx, movieID); err != nil {
			return err // store code returns httpx.ErrNotFound
		}
		// Idempotent: inserts only if this movie has never been watched. A
		// deliberate rewatch is LogRewatch, a separate operation (D-009).
		if err := markWatched(ctx, tx, userID, "movie", movieID, at, "watch"); err != nil {
			return err
		}
		// The ONLY writer of status lives in common/status.go (D-010).
		st, err := recomputeMovieStatus(ctx, tx, userID, movieID, s.now())
		if err != nil {
			return err
		}
		// Re-tracking cancels a pending sweep (D-008).
		if err := cancelPendingDeletion(ctx, tx, "movie", movieID); err != nil {
			return err
		}
		out = st
		return nil
	})
	return out, err
}
```

**`server/features/video/common/store_movies.go`** — SQL only, plain package-level functions taking the transaction,
every personal-state query scoped by `user_id`:

```go
// Idempotent insert: no UNIQUE constraint exists on user_watch_history, because
// one would forbid a legitimate repeat watch — and with watched_at an instant it
// would not catch a double-submit anyway. See the schema skill.
func markWatched(ctx context.Context, tx db.Tx, userID int64, targetType string, targetID int64, at, watchType string) error {
	const q = `
INSERT INTO user_watch_history (user_id, target_type, target_id, watched_at, watch_type)
SELECT ?, ?, ?, ?, ?
WHERE NOT EXISTS (
    SELECT 1 FROM user_watch_history
    WHERE user_id = ? AND target_type = ? AND target_id = ?)`
	// ...
}
```

## Errors → HTTP status

`server/httpx` exports the sentinel errors; service and store code wraps them, and handlers map them
to HTTP in exactly one place (`httpx.WriteServiceError`). Handlers never choose a status for a
domain condition themselves.

| Sentinel | Status | When |
|---|---|---|
| `ErrValidation` | 422 | Well-formed request, invalid values (rating 11, bad date) |
| `ErrNotFound` | 404 | No such row, or not visible to this user |
| `ErrConflict` | 409 | Uniqueness violation (email already registered, duplicate read entry) |
| `ErrForbidden` | 403 | Authenticated but not permitted (non-admin touching admin config) |
| `ErrUnauthorized` | 401 | No/invalid session or token — usually produced by middleware |
| `ErrProviderUnavailable` | 503 | TMDB/Steam unreachable or key missing. **Only reachable from search, add, the live details views** (an untracked `tmdb_id`, D-015; any person view, D-027) **and the trending / discover lists** (D-034) — a read path over existing local data returning 503 means something violated the no-provider-on-render-path rule |
| anything else | 500 | Log with the request id; never leak the message to the client |

Wrap with `%w` so `errors.Is` works. Use `ErrNotFound` rather than `ErrForbidden` when a row exists
but belongs to another user — do not confirm its existence.

One error envelope, everywhere:

```json
{ "error": { "code": "invalid_body", "message": "watched_at must be an RFC3339 timestamp" } }
```

`code` is a stable machine string the SPA can branch on; `message` is human text. Never return a
Go error string directly.

## Watch status is written in exactly one place

`server/features/video/common/status.go` owns `RecomputeShowStatus` / `RecomputeMovieStatus` **and** the only
`UPDATE`s against `user_show_states.status` / `user_movie_states.status` — recompute logic and its
SQL live together in that one file. Nothing else may write those columns — `/verify` greps for it.

Status is a maintained denormalisation (D-010), so the invariants matter:

- The watch insert, the state-row upsert, the recompute and the pending-deletion cancellation are
  **one transaction**. A bulk action runs N inserts and **one** recompute, never one per episode.
- `dropped` is never recomputed. The recompute SQL excludes it via `WHERE status = '…'` rather than a
  Go conditional, so it cannot be forgotten.
- The same SQL runs scoped to one row after a watch, and unscoped as a whole-table pass inside the
  TMDB refresh — which is what bounds drift if a service bug slips through.
- Ids are integers and unique only within their own table, so every polymorphic predicate carries its
  `target_type` (D-013). A store test should seed a movie and an episode with the same id.

## Store code

Service and store share the feature package, so there are **no store interfaces and no mocks** —
the naming convention is the boundary, and tests hit real SQLite (below).

- Store functions are package-level, take `db.Tx` (or the read pool) as a parameter, and are
  unexported — only the feature's own service code calls them.
- Queries are `const` strings next to the function. No query builder, no ORM.
- Placeholders are written `?`; the Postgres path rewrites to `$n` in **one** helper in `server/db`.
  Never write `$1` in a query.
- Transactions: service code calls `s.db.InTx(ctx, func(tx db.Tx) error {…})`. Handlers never see a
  transaction. Anything that must be atomic — deleting an item plus its polymorphic
  `user_video_ratings`, promoting cache files plus updating paths — goes in one.
- `sql.ErrNoRows` never escapes a store function: translate to `httpx.ErrNotFound`.
- SQLite connection setup (in `server/db`, the only package importing a driver):
  `PRAGMA journal_mode=WAL`, `PRAGMA foreign_keys=ON`, `PRAGMA busy_timeout=5000`, and
  `SetMaxOpenConns(1)` for the **write** pool to avoid `SQLITE_BUSY` under concurrent writes
  (reads can use a separate multi-conn pool).

## Config

`server/config` is the only package that reads the environment, and it fails fast at startup.

| Env | Required | Notes |
|---|---|---|
| `BASE_URL` | **yes** | Sole source of truth for OIDC redirects, absolute links, PWA manifest, cookie `Secure`. Never derived from a request header. |
| `DATA_DIR` | no (`./data`) | SQLite file + `assets/` + `cache/` |
| `DATABASE_URL` | no | Absent → SQLite at `$DATA_DIR/sofalog.db` |
| `LISTEN_ADDR` | no (`:8080`) | Plain HTTP only |
| `TRUSTED_PROXIES` | no | CIDRs. Empty → `X-Forwarded-*` is **ignored entirely** |
| `ENV_FILE` | no | Explicit env-file path. Set but missing → startup error |
| `TMDB_API_KEY_V3` | no | TMDB "API Key" → `api_key` query param. Either credential alone suffices |
| `TMDB_API_KEY_V4` | no | TMDB "API Read Access Token" (JWT) → `Bearer` header. **Wins if both set** |
| `STEAM_API_KEY` | no | Server-wide, admin config, never per user, never sent to the browser |
| `OIDC_ISSUER` | no | Enables single sign-on with the two below. All three or none — a partial set is a startup error |
| `OIDC_CLIENT_ID` | with issuer | |
| `OIDC_CLIENT_SECRET` | with issuer | Server-side only, never logged |
| `OIDC_ADMIN_CLAIM` / `OIDC_ADMIN_VALUE` | no | IdP group/role granting admin, re-read every sign-in. Set together |
| `OIDC_SCOPES` | no | Default `openid profile email` |
| `OIDC_PROVIDER_NAME` | no | Sign-in button label |

Enabling OIDC **disables local authentication entirely** (D-018): login, setup, user creation and
password changes return 404, and the boot probe never reports `setup_required`. The redirect URI is
`BASE_URL` + `/api/v1/auth/oidc/callback`, derived in `config`, never from request headers.

Derive `cookieSecure` from the **scheme of `BASE_URL`**, not from the listener and not from
`X-Forwarded-Proto`.

An optional `.env` is layered **under** the process environment — a real variable always wins, so a
container never loses to a stale local file. Search order: `$ENV_FILE`, `./.env`, `../.env` (the last
is what makes `cd server && go run .` find the repo-root file). Use `godotenv.Read`, never
`godotenv.Load`: returning a map instead of mutating the process environment keeps precedence
explicit and leaves the `load(getenv func(string) string)` test seam usable without `os.Setenv`.
A missing file is not an error; a malformed one joins the same error list as a bad `BASE_URL`.

`config` reads the two TMDB credentials verbatim — no trimming, no shape validation. `server/provider`
owns what a credential *is*: it trims, rejects a value in the wrong variable (a `eyJ` JWT in the v3
slot, a non-JWT in the v4 slot) and exposes the reason via `CredentialWarnings()` for `main` to log.
Neither absent nor misplaced credentials stop startup.

## Trusted proxy handling

`server/httpx` resolves the real client IP once, in middleware, and stashes it in the context.
Nothing downstream reads `X-Forwarded-For` again.

```go
// If the direct peer is not in TRUSTED_PROXIES, X-Forwarded-* is untrusted input:
// ignore it completely and use r.RemoteAddr. Otherwise take the right-most entry
// that is not itself a trusted proxy.
```

Getting this wrong means a client can spoof its own IP and defeat rate limiting and audit logs.

## Serving the embedded SPA

In `server/web` (the Vite build lands in `server/web/dist`):

```go
//go:embed all:dist
var distFS embed.FS
```

Two things that will bite:

1. **`go:embed` fails to compile if `dist/` is missing or empty.** The build output is gitignored, so
   a fresh clone must still compile: `server/web/dist/.gitkeep` and a stub `index.html` are
   committed (see `.gitignore`), and `vite.config.js` sets `emptyOutDir: false` so the build does
   not delete them.
2. **SPA fallback**: an unknown path that is not under `/api/` and has no file extension serves
   `index.html` so client-side routing survives a refresh. A missing `/api/v1/...` route must return
   a JSON 404, never `index.html` — otherwise API clients get HTML and a confusing parse error.

Serve `data/assets/` as a separate static route with long `Cache-Control` (paths are content-stable),
and `index.html` with `no-cache`.

## OpenAPI

`api/openapi.yaml` is versioned with releases and is the contract third-party clients build against.

Checklist for every `/api/v1` change, all in the **same commit**:

- [ ] Path, method, params, request and response schemas added/updated
- [ ] Every error status the handler can actually return is documented
- [ ] No existing response field removed, renamed or retyped — add, or add a version
- [ ] The auth scheme (session cookie or bearer token) is stated for the operation
- [ ] `/verify` reports no route/spec drift

## Tests

- Table-driven, `t.Run` per case, `t.Parallel()` where there is no shared state.
- **Store and service tests use a real SQLite temp file**, not mocks: `t.TempDir()`, run the real
  migrations, assert against real SQL. There are no store interfaces to fake (service and store
  share the package), and mocking the SQL would test nothing about it — which is where the bugs are.
- **Shared fixtures live in `video/videotest`** (`videotest.DB(t)`, `SeedUser`, `SeedShow`,
  `ProviderStub`, `FixedClock`, …) — a normal package imported only from `_test.go` files, because a
  `_test.go` helper is invisible to other packages. It may import `common` and nothing else of the
  component. A test that needs another feature calls it on the same core,
  `watches.NewService(svc.Core).MarkWatched(…)`; tests (only) may import siblings. `common`'s own
  tests are the external package `common_test`, since `videotest` imports `common`.
- **Handler tests** use `httptest` and assert status + JSON body shape, including the error envelope.
  A feature's `routes_test.go` provides `allHandlers(svc, cookie)`, registering every feature's
  routes around the package's own `Service` the way `video.go` does.
- **No network in tests, ever.** Provider clients take a `provider.Doer` interface; tests inject a
  stub (see `WithHTTPDoer`). A test that reaches TMDB is a broken test.
- **Assert that a provider key never reaches an error string.** `net/http` embeds the request URL in
  transport errors, so a client using the `api_key` query parameter leaks the key into any log line
  built from one. `server/provider` routes every error through `redact`, and a test asserts the key
  appears nowhere in `err.Error()`.
- Assert the multi-user invariant explicitly: create two users, write state as A, assert B's reads
  return nothing. Add such a test for every new per-user table.

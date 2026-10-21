# sofalog — Specification

The single source of truth for **sofalog**, a self-hosted, multi-user media tracker for watched
movies & TV episodes and games.

Everything here is settled. Where a rule is surprising or its alternative looks obviously better,
the reasoning is in [Appendix A](#appendix-a--why-these-decisions) under a stable `D-00x` id.
`CLAUDE.md` carries the subset of these rules that must be enforced on every change.

> **Maintaining this file** (about the document, not the product): it is deliberately one file.
> When any *single* component section — [§6](#6-movies--tv), [§7](#7-games) — grows
> past roughly 150 lines, split **that one** out to `docs/spec/<component>.md` and link it from the
> Contents. Do not split both preemptively; at ~60 lines each they do not earn separate files.
> The cross-cutting sections ([§4](#4-data-model), [§5](#5-local-first-data-ownership),
> [Appendix A](#appendix-a--why-these-decisions)) stay here regardless — they apply to both
> components identically and have no per-component split.

## Contents

1. [Overview](#1-overview)
2. [Stack](#2-stack)
3. [Architecture](#3-architecture)
4. [Data model](#4-data-model)
5. [Local-first data ownership](#5-local-first-data-ownership)
6. [Movies & TV](#6-movies--tv)
7. [Games](#7-games)
8. [Milestones](#8-milestones)
9. [Out of scope](#9-out-of-scope)
- [Appendix A — Why these decisions](#appendix-a--why-these-decisions)

---

## 1. Overview

### Goals & constraints

- **Simple structure** — minimal moving parts, easy to understand and maintain
- **Minimal resource usage** — must run comfortably on a home server / NAS / small VPS
- **Multi-user** — with proper authentication
- **Self-hosted via Docker** — single-command deployment
- **Open source** — clean, documented API so the community can build alternative UIs and clients
- **PWA** — installable on mobile devices
- **Proxy-friendly** — must work correctly behind a reverse proxy (Caddy, nginx, Traefik, NPM…),
  the typical self-hosted setup

### Two components, split by media domain

Each is deliberately kept separate — its own metadata provider, its own fields, its own progress
model, its own screens. Each should read as an independently shippable feature.

| # | Component | Provider | Core idea |
|---|---|---|---|
| 1 | Movies & TV | TMDB | Watch tracking, down to the individual episode |
| 2 | Games | Steam | Owned collection, hours played, achievements |

### Guiding principle: your data lives on your server

The app is **local-first**. Every piece of data a tracked item needs in order to be displayed — full
text metadata *and* every related image — is downloaded and stored on the server's own volume.
External providers are used to *discover and enrich*, never as a runtime dependency for showing what
you already track.

The reason is durability: if a provider changes its API, revokes a key, paywalls its images or
disappears entirely, **your library still works and still looks complete**. The full rules are in
[§5](#5-local-first-data-ownership) and apply to both components equally.

---

## 2. Stack

| Layer | Choice | Notes |
|---|---|---|
| Backend language | **Go** | Single static binary, ~20 MB idle RAM, tiny Docker image. Also a deliberate learning goal. |
| Database | **SQLite (primary)**, PostgreSQL (supported) | SQLite first-class: one file, zero extra containers, WAL mode. Postgres as an optional alternative. |
| API | **Versioned JSON API (`/api/v1/`) with OpenAPI specification** | The API is the first-class product. The spec is published and versioned so third parties can build clients against it. |
| Frontend | **Svelte** (plain Svelte + Vite, SPA mode — *not* SvelteKit) | Built to static files, embedded into the Go binary via `embed.FS`. No Node runtime in production. |
| PWA | Web app manifest + service worker | Network-first caching. Installable on Android/iOS. Requires HTTPS. |
| Authentication | **Local (email + password)** and **OpenID Connect** | Both create the same server-side session cookie. API tokens for third-party clients. |
| Reverse proxy / TLS | **Caddy** | Automatic HTTPS (required for the PWA service worker). Two-line config. |
| Deployment | **Docker / docker-compose** | Two containers: app + Caddy. One volume for the SQLite file and assets. |

Alternatives considered and rejected are in [Appendix A](#rejected-stack-alternatives).

---

## 3. Architecture

### Feature-first structure

Code is organised **per feature**, on both sides, mirroring the two components plus auth. The Go
module lives at `server/` and is named `mbyte.ro/sofalog`; the media components live under
`server/features/`, each split into one package per feature with a `common` package for what the
features share, so imports read `mbyte.ro/sofalog/features/video/upnext`. `auth` stays top-level
next to the shared infrastructure. There is no `internal/` and no `cmd/` — see
[D-014](#d-014--feature-first-packages-no-internal-no-cmd) and
[D-037](#d-037--one-package-per-feature-inside-a-component-one-common-no-sibling-imports).

```
server/                Go module: mbyte.ro/sofalog
  main.go              ← package main: config → open DB → migrate → wire features → serve
  migrations/          ← the baseline schema (append-only from release), embedded and applied at startup
  config/              ← env parsing, fail-fast; the ONLY reader of os.Getenv
  httpx/               ← middleware, error sentinels, the JSON error envelope
  db/                  ← open SQLite/Postgres, pragmas, transactions, migration runner
  provider/            ← shared TMDB / Steam clients
  assets/              ← download queue, path builders, cache pruning, deletion sweep
  auth/                ← FEATURE, kept top-level: users, sessions, login, OIDC; API tokens in M5
  features/
    video/             ← COMPONENT: movies + shows; video.go only wires the features together
      common/          ← what 2+ features share: Core (deps + shared catalog operations),
                         catalog stores, status.go (the status single writer), shared DTOs
      upnext/ upcoming/ history/ watches/ ratings/ lists/ copies/ jellyfin/
      details/ people/ discover/ media/ restore/ tmdbaccount/ refresh/
                       ← one package per feature; imports common, never a sibling
      videotest/       ← test-only fixtures, imported from _test.go files only
    games/             ← COMPONENT (M4), same shape
  web/                 ← serves the embedded Svelte SPA static build (go:embed of web/dist)
frontend/              Svelte SPA (Vite)
  src/features/        ← auth/ settings/ video/ games/ — routes, components, stores per feature;
                         video/ is split per feature like the Go side (upnext/, lists/, details/, …)
                         with video/common/ for the api module and shared components
  src/lib/             ← shared only: the one fetch client, image helper
```

### Layering inside a feature package

Each Go feature is **one package** (D-037) whose files follow a fixed naming convention. Between
packages the direction is compiler-enforced — a feature imports its component's `common`, never a
sibling. Inside a package the dependency direction handlers → service → store is enforced by
convention and `/verify` greps rather than by package boundaries (D-014):

| File pattern | Role | Rule |
|---|---|---|
| `handlers*.go` | /api/v1 JSON handlers | thin: decode → validate → one service call → encode; no SQL, no domain branching |
| `service*.go` | ALL domain logic | never touches `net/http` or SQL |
| `store*.go` | ALL SQL | the only files containing SQL; every personal-state query filters `user_id` |

- Handlers are **thin** wrappers over the same service functions everywhere.
- The bundled Svelte UI is just the **reference client** of the public API. If a screen needs
  something the API cannot express, the API is what gets fixed.
- The OpenAPI spec is maintained alongside the API and versioned with releases.

### Database

- SQLite with **WAL mode** enabled — handles self-hosted multi-user scale easily.
- SQL stays **portable** across SQLite and Postgres, so Postgres support stays cheap.
- Migrations are checked into the repo, numbered, and applied automatically on startup. Until the
  first deployed release there is exactly **one** — the baseline `0001_init.sql`, edited in place —
  and append-only begins at release ([D-016](#d-016--pre-release-the-schema-is-one-editable-baseline-not-a-migration-chain)).
- Schema shape is in [§4](#4-data-model).

### Authentication

- **Local auth**: email + password (argon2id), server-side sessions in a `sessions` table, token
  in an HTTP-only cookie. Instant revocation via `DELETE`. The **email is the identifier**; a
  separate optional `display_name` carries the human label
  ([D-019](#d-019--the-email-is-the-identity-the-display-name-is-only-a-label)).
- **OpenID Connect**: standard authorization-code flow with PKCE (`golang.org/x/oauth2` +
  `github.com/coreos/go-oidc/v3`). After the callback the *same* session cookie is created — OIDC is
  an alternate login method, not a separate auth architecture. **Configuring a provider disables
  local auth entirely** ([D-018](#d-018--configuring-oidc-disables-local-authentication-entirely)):
  login, first-run setup, user creation and password changes all refuse, and accounts attach to an
  identity by **verified email** (or are provisioned on first sign-in). Admin is mapped from an IdP
  group/role claim and re-evaluated on every sign-in.
- **API tokens** (bearer keys) for third-party clients consuming the JSON API. Only a hash is
  stored, never the token.
- Schema: `users.email` is `NOT NULL UNIQUE` (the identifier), `users.display_name` is nullable and
  **not** unique, `users.password_hash` is **nullable** (an OIDC-only user has no local password),
  `users.match_watchlist` is the [D-028](#d-028--matching-watchlists-is-opt-in-and-only-ever-shows-the-overlap-with-your-own-list) consent flag (`NOT NULL DEFAULT 0`, written only by its
  own account — never by an admin), plus an
  `oauth_identities` table (`provider`, `provider_user_id`, `user_id`) and an `api_tokens` table.
  All three exist from the **first migration** — see
  [D-007](#d-007--oidc-and-api-tokens-ship-in-m5-but-the-schema-makes-room-in-migration-1), which is
  what let OIDC land early with no migration. API tokens still ship in M5.
- No JWTs. Server-side sessions are simpler and revocable for a single-server app.

### Frontend & PWA

- Svelte SPA built with Vite; the production build is embedded in the Go binary (`//go:embed`).
- SPA fallback route in Go: unknown paths serve `index.html` so client-side routing survives a
  refresh. A missing `/api/v1/…` route must still return a JSON 404, never `index.html`.
- `manifest.json` (name, icons, `display: standalone`) + a minimal service worker: cache the app
  shell and `/assets/` images, provide an offline fallback page, and **never** cache `/api/v1`
  mutations.
- **No offline-capable client.** No IndexedDB mirror of the library, no client-side write queue, no
  sync or conflict-resolution layer. Mutations require the server. The "works with providers down"
  requirement in [§5](#the-app-works-with-providers-down) is about *provider* reachability, not
  client↔server connectivity — see
  [D-004](#d-004--works-with-providers-down-is-about-providers-not-about-being-offline-capable).
- Dev workflow: `vite dev` proxying `/api` to the Go server; embedding happens only in the
  production build.

### Reverse proxy friendliness

Self-hosted deployments almost always sit behind a reverse proxy — the bundled Caddy, or the user's
existing nginx / Traefik / Nginx Proxy Manager. The app must behave correctly there:

- **Respect `X-Forwarded-*`** (`X-Forwarded-For`, `-Proto`, `-Host`) so client IPs (rate limiting,
  audit logs) and scheme detection (redirects, OIDC callbacks, cookie `Secure`) are correct behind a
  TLS-terminating proxy. **Only trust these headers when the request comes from a configured trusted
  proxy** — otherwise a client can spoof its own IP and defeat rate limiting.
- **Configurable external base URL** (`BASE_URL=https://media.example.com`), used for OIDC redirect
  URIs, absolute links, the PWA manifest and the cookie `Secure` flag. Never guessed from the
  incoming request.
- **Configuration is environment variables.** As a convenience, an optional `.env` file — searched at
  `$ENV_FILE`, then the working directory, then `../` so `cd server && go run .` finds the repo-root
  file `docker compose` also reads — is layered *under* the real environment: a real variable always
  wins, so a container or unit file overrides a stale local file rather than the reverse. The file is
  never committed; a committed `env.example` documents the variables. A missing file is not an error;
  a malformed one is reported alongside every other configuration problem.
- **No assumptions about TLS at the app** — the Go server speaks plain HTTP; HTTPS is the proxy's
  job. Cookie `Secure` follows the `BASE_URL` scheme, not the local listener.
- **Relative asset paths in the SPA build**, so the UI doesn't break behind a proxy. Serving under a
  sub-path (`/media/`) is a nice-to-have; a dedicated (sub)domain is the documented default.
- **Ship example proxy configs** in the docs — Caddy by default, plus nginx and Traefik snippets,
  including WebSocket upgrade headers if live features are added later.

### Deployment

- Multi-stage Dockerfile:
  1. Node stage — build the Svelte `dist/`
  2. Go stage — compile the binary with the embedded frontend
  3. Final stage — minimal `alpine`/`distroless` image (~20–30 MB)
- `docker-compose.yml`: `app` (with a `./data` volume) + `caddy` (TLS termination).
- Provider API keys are server-side configuration only — never in an API response, never in the SPA
  bundle, never logged. The compose file *names* each key and takes its value from the environment or
  `.env`; a key is never written into a committed file or baked into an image layer. Error strings are
  part of "never logged": the provider clients redact `api_key` and `Bearer` values, because
  `net/http` puts the full request URL into its transport errors.
- The runtime image must carry `ca-certificates`, or every HTTPS call to a provider fails with an
  x509 error that reads like a permanently unreachable provider.

---

## 4. Data model

### Separate tables per media type

Each media type gets its own table with its own columns — `movies`, `shows`, `seasons`, `episodes`,
`games` — because the two domains barely share fields (episodes vs. achievements).

There is **no** shared `items` table and **no** `media_type` discriminator column. See
[D-001](#d-001--separate-tables-per-media-type).

### Multi-user: shared catalog, private per-user state

There is **one canonical row** per movie, show, episode and game — provider metadata is fetched
and cached once, for everyone. Everything personal is **per user**, and every per-user table carries a
**`user_`** name prefix so the two halves are distinguishable at a glance.

| Kind | Tables | Rule |
|---|---|---|
| **Catalog** — one row per real-world thing, shared by every user | `movies`, `shows`, `seasons`, `episodes`, `games`, `game_achievements` | **Never** contains `user_id`, a status, a rating, or a personal date |
| **Per-user** — private state | `user_watch_history`, `user_movie_states`, `user_show_states`, `user_video_watchlist`, `user_video_favorites`, `user_video_copies`, `user_tmdb_accounts`, `user_tmdb_sync`, `user_game_states`, `user_achievements`, and the two ratings tables | Always has `user_id`; every query filters on the authenticated user |

No duplicate provider fetches, and **no user can see another user's progress**. The one deliberate
exception is opt-in and narrow: the watchlist's matching filter ([D-028](#d-028--matching-watchlists-is-opt-in-and-only-ever-shows-the-overlap-with-your-own-list)) shows a user the
**overlap** between their own watchlist and a consenting user's — never the other list itself, and
never anyone's progress, ratings or history.

Note that a catalog table may hold a *provider* status — `shows.status` is TMDB's production status
(Returning Series / Ended). That is catalog data, unrelated to a user's watch status in
`user_show_states`.

### Identifiers

Every table uses ordinary integer primary keys. Ids are therefore **unique only within their own
table** — movie 5 and episode 5 both exist.

A few per-user tables reference more than one kind of thing (`user_watch_history` covers movies and
episodes; `user_video_ratings` covers movies, shows, seasons and episodes; `user_video_watchlist`
and `user_video_favorites` cover movies and shows; `user_video_copies` covers movies and seasons). These carry a
**`target_type` + `target_id` pair**, and that pair — never the
id alone — identifies the thing referred to. Looking up episode 5 means matching `target_id = 5`
**and** `target_type = 'episode'`. See
[D-013](#d-013--catalog-rows-keep-integer-primary-keys).

### Watch history

Watches are recorded in **`user_watch_history`**, a per-user log covering both movies and episodes.
Each row is one watch: a `watched_at` **instant** (RFC3339 UTC, like every other `_at` column — a
day would make a same-day rewatch unorderable and would hide the time the feed shows), and a
`watch_type` label recording how it was captured (`watch` | `checkin` | `scrobble`). **The same item
may be logged many times** — rewatches are new rows. See
[D-009](#d-009--rewatch-history-is-first-class-for-video).

**The instant is the user's to state.** A watch may be logged with an explicit past `watched_at` —
"I saw this in July" — and that is also what makes a rewatch reachable: naming an instant always
writes a row, where marking without one is idempotent. The two modes are one endpoint
(`POST /video/watches`), discriminated by the field's presence. A `watched_at` in the future is
refused; there is no lower bound, because a watch legitimately predates the library. Backdating
moves that column and nothing else: the status row's own timestamp and the aired-episode predicate
([D-012](#d-012--completion-counts-aired-non-special-episodes)) keep taking the real clock, or a
watch logged for last year would decide what had aired by then.

**A single row can be removed**, addressed by its own id — the correction path for a mis-marked or
duplicated watch. Unlike taking a target off the watchlist or clearing a rating, this is not
idempotent: it names one row, so removing it twice is a 404, and a row belonging to another user is
the same 404. The delete and the status recompute it implies are one transaction, which is what
makes removing the *last* watch safe: a movie loses its state row and reads unwatched again, a show
falls back to `watching` or loses its row too, and a user-set `dropped` survives because it is
intent rather than a derivation. Nothing else needs correcting, because progress is always derived.
The catalog row and its stored artwork stay even when this was the last reference —
[D-008](#d-008--untracking-never-hard-deletes) governs that, and nothing is queued here.

`watched_at` is stored as one UTC instant and never with an offset, so ordering is a plain string
comparison on both engines. Clients render it in the **user's time zone**
([D-038](#d-038--every-instant-is-stored-in-utc-every-day-is-measured-in-the-users-zone)), which
means the day or month an entry shows can differ from its UTC one near midnight — anything grouping
the feed by day or month groups on the zoned value, not on a prefix of the stored string. Derived values keep the same
precision: `last_watched_at` is `MAX(watched_at)`, not a truncated day.

### Progress and status — where the line falls

**Anything countable or positional** — episode counts, totals, percentages, "next unwatched episode",
"last watched" — is **computed at query time and never persisted**. No progress counters, no
percentage columns, no `last_watched_episode` field.

**`status` is the single stored label.** It is *system-maintained*: marking a watch recomputes it, and
the scheduled air-date refresh recomputes it again when new episodes arrive. It must be reproducible
from `episodes` + `user_watch_history` alone, and it is written by exactly one service function.
`dropped` is the one value that is user intent rather than a derivation. See
[D-010](#d-010--status-is-stored-but-system-maintained).

### Ratings

- **Scale: integers 1–10**, uniform across both components.
- **Both components have ratings**, stored in **separate tables per category**:
  - **`user_video_ratings`** — movies, shows and episodes, with a target-type discriminator;
    individual episodes are rateable. It carries a `season` target type as well, but **every write
    path refuses one** — see [D-020](#d-020--seasons-are-not-rateable)
  - **`user_game_ratings`**
- **One rating per user per item.** Re-rating **overwrites** the previous value — no rating history,
  no duplicate rows for the same item. Deliberately unlike watches: a watch is an *event*, a rating is
  a *current opinion*.
- **Video ratings can be pushed to the user's own TMDB account**, one way, opt-in per user
  ([D-029](#d-029--ratings-push-one-way-to-the-users-own-tmdb-account)). Two per-user tables carry it: `user_tmdb_accounts` (the link — its row IS the
  opt-in — holding TMDB's long-lived session id, which never leaves the server) and
  `user_tmdb_sync`, an **outbox** filled in the same transaction as every local rating write and
  drained by a background worker. Watchlist adds and removes ride the same outbox, while their
  switch is on. Linking is followed by a **one-time setup** — per kind, pull TMDB's list into the
  library once, push sofalog's once, or skip, plus a per-kind switch for the ongoing push
  ([D-036](#d-036--the-link-is-followed-by-a-one-time-setup-pull-push-or-skip-per-kind-and-per-kind-switches)).
  Local stays authoritative; after that setup nothing is read back.

The full DDL, the derived queries and the SQLite/Postgres portability rules live in the
`sofalog-schema` skill.

---

## 5. Local-first data ownership

This applies identically to **movies, shows, episodes and games**.

### Images are stored on disk, paths in the DB

All artwork is **downloaded to the data volume**, next to the SQLite file; the database stores
**relative paths**. This keeps SQLite small, backs assets up with the same volume, and lets images be
served with ordinary HTTP caching.

| Media type | Artwork |
|---|---|
| Movie | Poster, backdrop, media-section poster and backdrop galleries, video thumbnails |
| Show | Poster, backdrop, per-season posters |
| Episode | Episode still |
| Game | Cover, one icon per achievement |

**Two sizes per image — thumbnail + full.** Both are fetched as **provider size variants** (TMDB
`w185`/`w500` for posters and `w300`/`w1280` for backdrops, Steam capsule
vs. library art), so the app needs **no image-resizing code** of its own. Every card, list and page DTO carries
**both** (`poster` / `poster_full`, `backdrop` / `backdrop_full`, `still` / `still_full`; the Lists
hub's `backdrops` / `backdrops_full` as parallel arrays), the SPA offers both through `srcset` with
a `sizes` hint, and the **browser** picks by device pixel ratio × rendered width — so a phone's
grid still pulls `w185` where that is enough, a retina screen gets `w500`, and a page-wide banner
gets `w1280` instead of a stretched `w300`. Only the media section's lightbox pins the full size.
A grid that sends no `sizes` is the mistake to avoid: the browser then assumes the full viewport
width and over-fetches.

**Three exceptions** store a **single** size: achievement icons
([D-005](#d-005--achievement-icons-store-a-single-size)) and video thumbnails
([D-017](#d-017--video-thumbnails-are-proxied-from-youtube-not-hot-linked)) because the provider
publishes no second variant worth fetching, and an item's cast photos (`w185`) because no *item*
page renders a larger one — a lightbox for actor portraits does not exist. The **person** page does
render a portrait at TMDB's profile detail size (`h632`), but that page is served live and owns no
assets ([D-027](#d-027--the-person-page-is-always-live-and-never-a-catalog-row)): both of its portrait variants
go through the cache proxy only, so the item directories keep their one cast-photo size.

### On-disk layout

Assets are organised **one top-level folder per component**, then **one folder per item**. The video
folder is named `video/` rather than `tv/` because it holds movies as well as shows. Item folders are
keyed by **internal database ID**, not provider ID, so adding a second provider later strands nothing.
No sharding — item folders sit directly under their media-type folder.

```
data/
├── sofalog.db
├── assets/
│   ├── video/
│   │   ├── movies/<movie_id>/
│   │   │   ├── poster.jpg          poster.thumb.jpg
│   │   │   ├── backdrop.jpg        backdrop.thumb.jpg
│   │   │   ├── gallery/backdrops/<tmdb_file>.jpg  <tmdb_file>.thumb.jpg
│   │   │   ├── gallery/posters/<tmdb_file>.jpg    <tmdb_file>.thumb.jpg
│   │   │   ├── cast/<tmdb_file>.jpg               # single size, w185
│   │   │   └── videos/<youtube_key>.jpg           # single size (D-017)
│   │   └── shows/<show_id>/
│   │       ├── poster.jpg          poster.thumb.jpg
│   │       ├── backdrop.jpg        backdrop.thumb.jpg
│   │       ├── gallery/backdrops/<tmdb_file>.jpg  <tmdb_file>.thumb.jpg
│   │       ├── gallery/posters/<tmdb_file>.jpg    <tmdb_file>.thumb.jpg
│   │       ├── cast/<tmdb_file>.jpg               # single size, w185
│   │       ├── videos/<youtube_key>.jpg           # single size (D-017)
│   │       ├── seasons/<season_number>/poster.jpg     poster.thumb.jpg
│   │       └── episodes/<season>-<episode>/still.jpg  still.thumb.jpg
│   └── games/<game_id>/
│       ├── cover.jpg               cover.thumb.jpg
│       └── achievements/<achievement_key>/icon.jpg
└── cache/                          # temporary only — see below
    └── <provider>/<provider_id>/…
```

Note the levels that are *not* database ids: season and episode folders use the positional
`season_number` / `episode_number`, and achievement folders use Steam's `api_name`. So a season is
addressed by number on disk but by `seasons.id` everywhere in the database — do not mix the two. The
`<season>-<episode>` name is unambiguous without zero-padding, because both numbers are non-negative
and rendered plainly: `1-23` and `12-3` cannot collide.

A show's seasons and episodes live **inside the show's directory**, not in trees of their own. That
is what keeps "deleting an item is removing one directory" true for a show, and it is why the whole
subtree reconciles as **one** asset unit keyed `(show, shows.id)` — `asset_jobs`' `season` and
`episode` media types stay unused for video.

Two properties this layout is chosen for: **deleting an item is removing one directory**, and
**exporting is copying directories** — no filename globbing or path parsing in either case.

### Downloaded eagerly, but only for tracked items

- **Trigger: any tracked state at all.** As soon as an item is referenced by any user — watched,
  watching, dropped, watchlist, owned, wishlist, lent out, or **merely rated** — its full metadata
  and all its images become **permanent local data**, fetched eagerly rather than on first view. The
  library must be complete *before* a provider fails, not after.
- **Search results are never stored permanently.** Searching a provider for something not tracked
  writes nothing to the library. Result images may be held in the **separate temporary cache
  directory** `data/cache/`, with a **TTL and a maximum size**, pruned automatically. The cache is
  deliberately a different location from permanent assets, so cache pruning can never touch library
  data.
  The cache is keyed by **provider ID**, not internal ID — a search result is not in the database
  yet, so it has no internal ID. When such an item is actually added, its cached files are
  **promoted** into `assets/…/<internal_id>/` rather than re-downloaded.
- **Downloads are queued and throttled**, never fired in a burst. Adding one 200-episode show queues
  several hundred image fetches. The queue is **per-provider** rate-limited, runs in the background, and is **resumable** —
  an interrupted or partial download set is retried rather than left with gaps.

### Metadata is stored, not just referenced

A **full text metadata snapshot** is kept locally for every tracked item — descriptions, runtimes,
air dates, genres, and so on — not merely provider IDs and titles. **Rendering
any page must never require a provider call.**

### The app works with providers down

An explicit requirement: with **no provider access at all**, everything except *adding new items*
behaves normally — browsing the library, marking episodes watched, changing statuses, and rating.
Loss of provider access degrades discovery, never the existing library.

Provider calls are therefore allowed in exactly eight places:

1. **Search** — the user typed a query; results are never written to the library
2. **Add-item enrichment** — an item just became tracked; fetch metadata and enqueue images.
3. **User-triggered Steam sync** — manual only ([§7](#steam-sync-user-driven-never-surprising))
4. **The one scheduler** — the TMDB refresh: two jobs on one hourly loop, re-fetching watchlisted,
   favorited and in-production items weekly and finished ones quarterly by default, and feeding the
   upcoming-episodes calendar ([D-006](#d-006--exactly-one-scheduled-job-exists), [D-025](#d-025--two-refresh-jobs-split-by-current-interest-on-the-row-timestamp)).
   **TMDB only** — games are deliberately not on it, and adding anything to it takes a
   new decision ([D-006](#d-006--exactly-one-scheduled-job-exists))
5. **The movie, show, season and person details views** — serve an untracked `tmdb_id` **live from
   TMDB, storing nothing**; one with a catalog row is served from its snapshot, refreshed on view
   when older than 2 days, with the stale snapshot served when the refresh fails. Movie, show and
   season behave identically; the only write a view may cause is a **tracked** show's missing season
   being materialised, on either the show or the season page
   ([D-015](#d-015--the-details-views-may-call-tmdb-live-for-untracked-snapshot-with-bounded-staleness-for-tracked)).
   The **person** view is the same rule with no tracked half: people have no catalog table, so
   every person view is live-and-discarded and TMDB being down is always a 503 there. Its
   filmography's library marks are a local join by `tmdb_id`, not a second call
   ([D-027](#d-027--the-person-page-is-always-live-and-never-a-catalog-row)).
6. **The TMDB account link and its rating outbox** — the one **write** to a provider: a user's own
   Link (request token, session exchange, one account lookup for the username) and Unlink (session
   revoke), plus a background worker that pushes queued rating changes to that user's TMDB account.
   The queue fills inside the local rating's own transaction, so no request and no render ever
   waits on TMDB, and TMDB being down defers the push rather than failing the rating
   ([D-029](#d-029--ratings-push-one-way-to-the-users-own-tmdb-account)). Its one read into the
   library is the **setup pull** — once per link, only when the user asks for it right after
   linking, the account's rated, watchlist and favorite lists are read into the library as a
   background job; never afterwards
   ([D-036](#d-036--the-link-is-followed-by-a-one-time-setup-pull-push-or-skip-per-kind-and-per-kind-switches))
7. **The Jellyfin library scan** — a background read of the household's **own** media server, on
   a schedule and on a "scan now" from Settings, that answers one question — which catalog movies
   and seasons are on it — and writes the answer to one shared table the details views read as the
   `jellyfin` copy kind. It is not a metadata source: nothing it returns becomes catalog data, it
   never calls TMDB, never adds an item to the library, and reads presence, never playback
   ([D-033](#d-033--the-jellyfin-tile-mirrors-the-households-server-when-one-is-configured))
8. **The trending and discover lists** — TMDB's `/trending` list and its `/discover` catalog
   listing (the popular list when unfiltered) on the Trending page, plus the genre, language and
   watch-provider option lists the Discover filters offer, served **live on every view and never
   stored**: the person view's rule with no tracked half — there is no catalog row to be over, so
   TMDB being down is always a 503 there and never anywhere in the library. Each entry is marked
   against the library by the same local join by `tmdb_id`, never a second call. The streaming-
   service filter is the **one** request that sends `TMDB_WATCH_REGION` to TMDB (as
   `watch_region`). **TMDB's alone**, like site 5
   ([D-034](#d-034--the-trending-and-popular-lists-are-always-live-and-never-a-catalog-row))

The **episode details view** does not extend that list. An episode has no snapshot of its own — it
is one entry in its season's, projected out of `seasons.metadata_json`.
It resolves its season through the season view's own path, so whatever provider call it causes is
that call site, not a new one, and it stores nothing of its own.

The **image proxies** look like a ninth and are not. A poster, cast photo, gallery image or video
thumbnail is fetched once for a given filename and then served from `data/cache` — the miss rides
whichever of the sanctioned calls above named that filename in the first place. No page render and no
read API response ever waits on a provider. Video thumbnails come from YouTube rather than a
metadata provider, which is why they get their own decision
([D-017](#d-017--video-thumbnails-are-proxied-from-youtube-not-hot-linked)). Tracked artwork is
served from a second route straight off `data/assets` — pure disk, no provider path at all — with
the proxy as the fallback while the download queue is still draining.

That is the **only** scheduled job in the system; see
[D-006](#d-006--exactly-one-scheduled-job-exists). The asset download queue is a background *worker*,
not a scheduler: it drains work that user actions enqueued and never initiates provider traffic on
its own.

### Cleanup: unreferenced items are removed, but never abruptly

When the **last** user removes their reference to an item, that item is no longer tracked by anyone
and its row and image files should go. But an accidental untrack would discard a local copy the
provider may no longer serve — the exact failure this whole section exists to prevent. So deletion is
deferred and confirmed rather than immediate:

1. When the last reference drops, insert a `pending_deletions` row. Nothing is deleted yet.
2. The UI confirms when a user's action would orphan an item.
3. A deferred sweep, after a grace period, **re-checks** the reference count and then removes the
   row plus its asset directory — in one operation.
4. Re-tracking before the sweep runs simply drops the `pending_deletions` row. Nothing was lost and
   nothing is re-downloaded.

A **rating, a watchlist row, a favorite or an owned copy alone counts as a reference**. See
[D-008](#d-008--untracking-never-hard-deletes).

### Export

A user-triggered **export archive containing both metadata and the image files** (a zip of JSON per
media type plus the assets), so the export on its own can reconstruct the library. The data must be
portable *out* of the app, not merely present on the server.

Because asset folders are keyed by internal database ID, the archive must also include the
**ID → provider ID + title mapping**. Without it the exported tree is just numbered directories and
the "reconstructs the library on its own" requirement fails.

### Restore from a Ticket Box dump

The one import path ([D-023](#d-023--a-ticket-box-dump-is-the-one-import-path)): Settings → Restore
backup → Ticket Box dump takes the JSON backup file exported by **Ticket Box**, the author's own
Trakt client, and folds its watch history, ratings, watchlist and favorites into the signed-in
user's state. sofalog reads the **file**; it never calls Trakt. Only the export's `schemaVersion` 5
is accepted — version 1 named episodes by their own TMDB id, which nothing can resolve to a show — and
the decode is strict, so a field the version does not define is a 422 naming it rather than a silent
mis-read.

Per movie or show the file names: an item the file has **watches** for is **replaced** — this user's
existing watch rows for it (every season, for a show) are removed and the file's rows written, with
the file's provenance label as `watch_type` and its instant as `watched_at`. An item the file only
rates, watchlists or favorites keeps its watches: the file is the record for what it names, and a
rating names no viewing. Ratings upsert, watchlist and favorite rows insert if absent (D-030), and
anything sofalog cannot take — a season
rating (D-020), a season or episode on the watchlist, an entry with no TMDB id, an episode TMDB's
listing lacks, a show TMDB does not know — is **skipped and reported**, never a failure. Items not yet
in the catalog are materialised exactly as any tracking write materialises them (D-015), which makes a
whole-library import minutes long: it runs as an in-memory job, one per user, polled by the SPA, and a
restart forgets it — the groups it committed stay, and re-running the same file converges. The SPA's
Restore page also shows a **Sofalog dump** card, disabled: restoring sofalog's own export is deferred,
not decided against (see [§9](#9-out-of-scope)).

### Ratings pushed to the user's own TMDB account

The one outward flow ([D-029](#d-029--ratings-push-one-way-to-the-users-own-tmdb-account)): Settings → TMDB account lets a user link their **own** TMDB
account, and from then on every rating they set or clear on a movie, show or episode, and every
movie or show they add to or remove from their watchlist or their favorites, is sent there — for
the kinds whose switch is on. The link is made through TMDB's own approval
page: the browser is sent to TMDB with a request token, the user approves, TMDB sends them back to
`{BASE_URL}/api/v1/video/tmdb/link/callback`, and the token is exchanged for a long-lived session
stored per user. The link queues nothing by itself. It is followed by a **one-time setup**
([D-036](#d-036--the-link-is-followed-by-a-one-time-setup-pull-push-or-skip-per-kind-and-per-kind-switches)):
for each of ratings, watchlist and favorites the user picks **Pull** (TMDB's list is read into the
library once, as a background job — a rating there overwrites the local one, rounded half up to
1–10; a listing is added if absent and nothing local is removed; an unknown item is materialised
through the ordinary tracking gate; nothing pulled is queued back), **Push** (everything sofalog
already holds of that kind is queued once) or **Skip**, and ticks whether to keep that kind in sync
from then on. The three switches stay on the page afterwards and govern future writes only: turning
one on never resends what is already held, turning one off drops that kind's pending queue. Writes
go through an **outbox** table filled in the same transaction as the local rating write and drained
by a worker the video service owns, with backoff on outages and a durable queue, so TMDB being slow
or down never touches the local action. A session TMDB refuses marks the link **broken** rather
than silently unlinking: the page names the account and offers to link again, which keeps the setup
and the switches (linking a different account resets them). Unlinking removes the link and the
pending rows, cancels a pull still running, and revokes the session; **TMDB's ratings stay** — the
account is the user's, not sofalog's. After the setup nothing is read back from the account; local
is authoritative.

### Provider attribution

Caching TMDB images and metadata requires the attribution notice their terms mandate — *"This
product uses the TMDB API but is not endorsed or certified by TMDB"* — shown in the UI, somewhere
permanent. Its home is the **Settings → Data Sources** page, which also documents what each provider
supplies and how its data is handled. The sidebar's account chip links to Settings unconditionally,
so the notice is always reachable.

That page is the **only** home. Provider disclaimers are not repeated per page: a page that leans on
a provider — search, a details view, anything showing a provider's mark — explains itself in its own
words and links to Data Sources for the rest. One canonical place stays accurate; copies scattered
across pages drift and read as legal boilerplate. Adding a second instance is a deliberate per-case
decision, not a default.

---

## 6. Movies & TV

### Purpose

Track what has been watched, and what to watch next — episode by episode.

### Tracking unit: individual episodes

- A **movie** is a single entry.
- A **show** is tracked **per episode**. Season and show progress are **derived** from the episodes,
  never stored as a separate source of truth.

### Statuses

Three, for shows:

- **Watching** — in progress
- **Watched** — completed
- **Dropped** — started and abandoned (kept out of the completed count)

**A movie has exactly one status: *watched*.** Dropping means abandoning something partway with
content left that you have decided not to come back to — and that needs parts. A show has episodes,
so stopping after two of ten is a real, lasting state worth recording and acting on. A movie is a
single sitting with no episode granularity: it is watched or it is not, and "started it, gave up"
leaves nothing behind to model and nothing a later view could do with it. *Watching* is
inapplicable for the same reason — a movie in progress is a partial-watch position, which
[§9](#9-out-of-scope) rules out. So *watched* is the only status ever stored for a movie:
**there is no dropped movie, in any milestone.** "Tracked but not watched" is a watchlist row, not
a status.

**Watchlist is not a status.** "Want to watch" is a separate, independent axis — its own table,
covering movies and shows. An item can be
on the watchlist *and* in progress at the same time. See
[D-011](#d-011--the-watchlist-is-an-independent-axis).

**Owned copies are another independent axis.** `user_video_copies` records which formats or services
a user holds an item on — `jellyfin`, `dvd`, `bluray`, `uhd`, or the unbranded `physical` / `digital`
— never a status, and orthogonal to both watching and watchlisting. Copies are stored **per movie and
per season**: a show is never itself "owned on DVD", its discs are, so at show level `show` is only a
**bulk fan-out scope** (add or remove a kind across every non-special season in one call), never a
stored target. Specials (season 0) never ride a show-level action and are managed from their own
season page. An **episode** is never itself owned, so the episode page displays its **season's**
copies read-only, with the way to change them being that season's page. When a Jellyfin server is
configured, the `jellyfin` kind stops being hand-set: a **library scan** decides it for every user,
from what the server actually holds
([D-033](#d-033--the-jellyfin-tile-mirrors-the-households-server-when-one-is-configured)). Adding a copy is a
**tracking action** — it materialises an untracked item exactly as
watchlisting does
([D-015](#d-015--the-details-views-may-call-tmdb-live-for-untracked-snapshot-with-bounded-staleness-for-tracked))
— and a copy row alone is a [D-008](#d-008--untracking-never-hard-deletes) reference that keeps the
item and its artwork alive. See
[D-021](#d-021--owned-copies-are-per-user-and-per-season-for-shows).

**Favorites are a third independent axis.** `user_video_favorites` records the movies and shows a
user calls their favorites — never a status, and orthogonal to watching, watchlisting and owning: a
favorite can be unwatched, in progress, finished or dropped. It is the watchlist's shape byte for
byte (movies and shows only, one row per user and target), and it carries the watchlist's weight:
favoriting is a **tracking action** that materialises an untracked item and downloads its full media
set ([D-015](#d-015--the-details-views-may-call-tmdb-live-for-untracked-snapshot-with-bounded-staleness-for-tracked)),
a favorite row alone is a [D-008](#d-008--untracking-never-hard-deletes) reference, it puts the item
on the scheduled refresh's **active** cadence
([D-025](#d-025--two-refresh-jobs-split-by-current-interest-on-the-row-timestamp)), and it mirrors
to the user's linked TMDB account
([D-029](#d-029--ratings-push-one-way-to-the-users-own-tmdb-account)). It does **not** feed the
upcoming calendar. The movie and show pages carry a heart beside the bookmark, and a Favorites page
lists both kinds together. See [D-030](#d-030--favorites-are-a-third-independent-axis).

**The system maintains these statuses; the user does not set them by hand** (see
[D-010](#d-010--status-is-stored-but-system-maintained)):

- Marking a movie watched records the watch **and** sets its status.
- Marking an episode watched recomputes the show: once every aired episode is watched, it flips from
  *watching* to *watched*.
- When a new episode airs, a *watched* show flips back to *watching* — there is something to watch
  again. This happens during the periodic TMDB refresh, so the label can lag by up to one refresh
  interval; the next-episode and progress figures are computed live and are never stale.
- Completion counts **aired, non-special episodes only** — otherwise a currently-airing show could
  never be *watched*. Specials remain individually markable and rateable, they just don't gate
  completion. See [D-012](#d-012--completion-counts-aired-non-special-episodes-only).

**Dropped is the exception, it is sticky, and it applies to shows only.** Only the user sets or
clears it. A dropped show is excluded from the currently-watching view and the upcoming-episodes
calendar, and stays dropped when new episodes air, until the user explicitly un-drops it.
Un-dropping recomputes from the watch log, so a show dropped after finishing returns to *watched*,
not *watching*. It is also the one piece of show state that **reads downward**, the way copies do:
a season and an episode page each show a notice saying the show is dropped, read-only, because
"have I given up on this?" is a question you ask while looking at an episode, and the answer lives
two levels up. Only the notice travels — the show's status label and its progress stay on the show
page.

**Enforced in the schema, not in a comment.** `user_movie_states.status` permits `watched` and
nothing else; `watching` and `dropped` live in the show state table's `CHECK` and nowhere else. A
movie therefore cannot be given either value even by a bug — the same trick that keeps `watchlist`
out of every status `CHECK` ([D-011](#d-011--the-watchlist-is-an-independent-axis)).

**The path itself is `POST` / `DELETE /video/shows/{id}/drop`**, pulled forward into M1.5 on request
([§8](#8-milestones)). Only a show the user has **started** can be dropped: without an episode watch
there is no state row, and a drop that created one would invent a watch status for a show never
begun — the same reason watchlisting and rating leave the status absent. A state row exists if and
only if the log holds an episode watch or the user already dropped the show, which is exactly why a
non-null status is what the show page's button turns on. Neither direction is a tracking action: the
show is already local, so both address it by its own id, never a `tmdb_id`, and neither can call a
provider. Both are idempotent, and the removal answers with the **recomputed** status rather than a
bare 204, because which label the log implies is not the client's to derive
([D-010](#d-010--status-is-stored-but-system-maintained)).

### Data recorded per watch

A watch records **the instant it happened, plus how it was captured** — `watch` (marked in the app),
`checkin`, or `scrobble`. The instant is asked for rather than assumed: marking a movie or an episode
watched opens a small dialog for the date and time, with an optional rating stored in the same
transaction, so "I watched this last Tuesday and thought it was an 8" is one action. Confirming it
always adds an entry, which is how a rewatch is logged. The show page's whole-show mark opens the
same dialog in a bulk shape (see [Bulk actions](#bulk-actions)), and an Up Next card offers it
behind the chooser its tap opens. The season page's episode grid marks nothing: each still only
displays whether the episode is watched, and binge-marking is the season-level bulk action below it.
The undated, idempotent first-mark remains a mode of the API with no caller in the SPA. The type is a provenance label only; nothing in the app behaves differently
because of it, and the features that produce the other two values are deferred (see
[§9](#9-out-of-scope)).

**The same movie or episode can be watched many times.** Each watch is a new dated row, so rewatch
history is first-class. See
[D-009](#d-009--rewatch-history-is-first-class-for-video). Because rows accumulate, a single one can
also be removed by its own id, and the status recompute follows it in the same transaction — see
[§4](#watch-history).

Still not in scope here: notes and reviews. (Ratings do exist — see [§4](#ratings) — but a rating is
one current opinion that gets overwritten, not a history.)

### Getting items in: TMDB search only

Search TMDB inside the app, pick a result, and its metadata + poster are pulled in and stored. There
is **no manual entry**, no CSV / Letterboxd import, no Trakt *API* import, and no "paste an IMDb ID"
path. The one way records enter other than by search is the Ticket Box dump restore
([§5](#restore-from-a-ticket-box-dump)), which reads a file the user already holds.

**A TMDB API key is required for this component** — either the v3 *API Key* (`TMDB_API_KEY_V3`, sent
as an `api_key` query parameter) or the v4 *API Read Access Token* (`TMDB_API_KEY_V4`, sent as a
Bearer header). Either satisfies it: TMDB documents the v4 token as valid for v3 endpoints, and this
component calls nothing else. Without a configured key, **adding new items** is
impossible — setup documentation treats the key as a prerequisite. Items already tracked keep working
without it, because their metadata and images are stored locally.

### Where to watch

Every movie and show details page carries an **online providers** section beside the user's owned
copies: who streams, rents or sells the item, with locally proxied service logos and a link to
JustWatch. TMDB supplies the data (it sources it from JustWatch, which the section credits) and it
rides the movie and show metadata calls, so it is **not** a sixth provider call site and it renders
with TMDB unreachable, like every other snapshot field.

The listing is **per country**, chosen by one server-wide setting, `TMDB_WATCH_REGION` (ISO-3166-1
alpha-2, default `US`). TMDB answers with every country at once — 111 of them for one film, some
45% of the whole response — so the snapshot is **pruned to the configured region before it is
stored**, with a marker recording which. Changing the setting therefore costs each item **one**
refresh, taken automatically on its next view. A **season** and an **episode** have no listing of their
own — TMDB publishes providers per movie and per show — so both borrow the owning show's, and both
show nothing while the show is untracked.

"Nothing to show" is **two different answers**, and the section words them differently: when TMDB
has answered for the country and named nobody, the page says so ("This show isn't on any streaming
service in RO"); when no listing was available to read at all — an untracked show's season, or a
snapshot that has none yet — the section is simply absent, because claiming unavailability off a
lookup nobody performed would be a guess. See
[D-022](#d-022--online-providers-ride-the-item-snapshot-region-is-server-config).

### Similar titles

Every movie and show details page ends with a **"Similar movies" / "Similar shows"** rail: TMDB's
recommendations for the item, at most twenty, in TMDB's own order. Like the providers section it
**rides the item's snapshot** — the list is appended to the one details call the snapshot already
makes and stored with it, so a tracked item's rail renders with TMDB unreachable and is as current as
its `fetched_at`. Each card is a title the library has **no row for**: it links to that title's own
details page by TMDB id, exactly as a search result does, and its poster is served through the cache
proxy rather than filed under the item's artwork. A season and an episode page carry no rail — TMDB
publishes recommendations per movie and per show only. See
[D-024](#d-024--similar-titles-ride-the-item-snapshot).

### People

Every cast card — on the movie, show, season and episode pages, guest stars included — links to a
**person page**: portrait, biography, birth and death dates, outbound TMDB / IMDb / homepage links,
and a filmography of the person's acting credits, one card per title, movies and shows together
with a filter for either kind. It is fed by TMDB's `/person/{id}` with combined credits appended,
**fetched live on every view and never stored** — a person is not a library item, has no catalog
row and nothing to track, so the page is discovery and TMDB being unreachable is a 503 there. The
one thing of the user's it shows is the **library marks**: each credit the catalog holds carries its
local id (the card links by it and wears an "In library" chip) and whether this user has watched it
(a movie: any watch in the log; a show: completed — the watchlist page's ✓), from a local join by
TMDB id. Above the filmography, a **"Seen with"** grid narrows the same list to the titles in the
user's own history — a movie they watched, a show they watched **any** episode of, finished or not
(`in_history`, distinct from `watched`) — so "where do I know them from" is answered before the
whole list. The person's department ("Acting", "Directing") is named on the page; crew credits are
not listed. See [D-027](#d-027--the-person-page-is-always-live-and-never-a-catalog-row).

### Images stored locally

Per [§5](#5-local-first-data-ownership), for every tracked item:

- **Movies** — poster + backdrop, plus the media galleries, cast photos and video thumbnails
- **Shows** — the same set, plus **per-season posters**
- **Episodes** — **episode still** for each episode

Each in two sizes. A long-running show is therefore a few hundred image files; that is intended, and
is why downloads are queued and throttled rather than fired in a burst.

### Bulk actions

Episode-level truth stays, but marking must never be tedious:

- **Mark whole season watched**
- **Mark whole show watched** — for shows finished long ago that just need to be in the library

Each is a single API call, not a loop of per-episode requests.

Bulk marking is **idempotent by default** — episodes already watched are skipped, so running it
twice does not create duplicate watches. The whole-show and whole-season marks (one endpoint, a
`scope` field — the season is the target then, and Specials are refused because they never gate
completion) open the watch dialog in a bulk shape asking two independent questions. **Which
episodes**: the remaining unwatched ones (the default), or *every* aired episode again — a full
deliberate rewatch, the one non-idempotent bulk path, guarded by the dialog's awaited pending state
exactly like the single dated log. **Dated when**: now (the default), one fixed instant, each
episode's own release date, or each season's premiere — the last two derived server-side as
midnight **in the user's time zone**, stored as its UTC instant
([D-038](#d-038--every-instant-is-stored-in-utc-every-day-is-measured-in-the-users-zone)). Every
markable episode has an air date (that is what makes it markable —
[D-012](#d-012--completion-counts-aired-non-special-episodes)), but a season's premiere can be
missing or unaired, and those episodes take a user-supplied fallback date, required exactly when
such seasons exist — the season scope checks only its own season's premiere. Backdating in any of
these shapes moves `watched_at` and nothing else, as always; the status recompute (and the status
and progress a bulk response reports) stays the owning show's whatever the scope, because a season
has no status of its own.

### Views

- **Currently watching + next episode** — the home view: shows in progress, each showing the exact
  next unwatched episode, markable from the card itself. Excludes dropped shows.
- **Watchlist** — fed by the watchlist table, independent of status; movies and shows in one
  grid, ordered together, with a filter for either kind alone. A show sorts on its first air date
  where a movie sorts on its release date, and its ✓ badge means **finished**, not started. A
  **"Matching watchlist"** filter picks one or more other users who turned "Allow matching
  watchlist" on in Settings → Account and narrows the grid to what **all** of you have listed — an
  intersection, never a union, and never the other lists themselves ([D-028](#d-028--matching-watchlists-is-opt-in-and-only-ever-shows-the-overlap-with-your-own-list)).
- **Favorites** — fed by the favorites table, the watchlist page's twin without the matching
  filter: movies and shows in one grid, most recently favorited first by default, the same kind
  filter and sort keys, the same card ([D-030](#d-030--favorites-are-a-third-independent-axis)).
- **Dropped** — every show the user has dropped, most recently dropped first by default
  (`GET /video/dropped`). Not a list of its own but a view over the `dropped` state rows (D-010),
  so un-dropping a show takes it off; shows only, since a movie cannot be dropped. The favorites
  page's grid, sort keys and card without the kind filter, the card's date reading "Dropped …".
- **Lists** — the one nav entry the Watchlist, Favorites and Dropped pages sit under
  (`video/lists/watchlist`, `video/lists/favorites`, `video/lists/dropped`): a hub of banner cards,
  one per list, each with its movie and show counts and a collage of up to four backdrops from its
  most recently added items, all from one local read (`GET /video/lists`).
- **History by date** — reverse-chronological feed of what was watched when. Because rewatches are
  real entries, the same title can legitimately appear more than once. The same feed, narrowed,
  is what each details page's timeline renders: one target's watches on a movie or episode page,
  one season's episodes on a season page, one show's across every season on a show page.
- **Upcoming calendar** — one poster grid in date order, each card wearing its date, not a month
  grid: the air dates of future / unwatched episodes for followed shows **and** for shows on the
  watchlist, plus the release dates of unwatched movies on the watchlist, over a chosen horizon. Today's unwatched episode counts as
  upcoming until it is marked; dropped shows are excluded even when watchlisted, and undated
  episodes are left out (Up Next badges them instead). Fed by the periodic TMDB refresh
  ([D-026](#d-026--the-upcoming-calendar-is-a-day-agenda-over-followed-and-watchlisted-items)).
- **Trending** — TMDB's trending list (today / this week) and a Discover tab over TMDB's
  `/discover` listing — the popular list by default, with TMDB's own sort orders (popularity,
  rating, newest, votes) and filters (score range, minimum votes, genres, original language, year
  range, streaming service in the server's region) — movies and shows together or one kind alone,
  in one ranked poster grid that grows by "Load more". The one view that is **not** over the
  library: it is fetched live on every open and never stored, so TMDB being down empties it and
  nothing else. Each card is marked against the library the way the person page's filmography is —
  "In library" and a link by local id when the catalog holds the title, the ✓ when this user has
  watched it ([D-034](#d-034--the-trending-and-popular-lists-are-always-live-and-never-a-catalog-row)).
- **Stats** — the user's own numbers over one period, all time or one calendar year in their zone
  (`GET /video/stats`): hours and plays, the most watched show, the rating distribution and the
  rating trend, "you vs the crowd" against TMDB's averages out of the stored snapshots, the watched
  movies still waiting for a rating (rateable from the page), watch activity — plays per month, a
  daily heatmap, weekdays, time of day, streaks, the first play — release decades, the most
  rewatched titles, show completion and top genres. The server computes every section from the
  log, the ratings and the show states and the page only renders; a section with nothing to show
  is null and hidden. A pure local read — never a provider call
  ([D-039](#d-039--statistics-are-a-read-only-projection-of-your-own-log-computed-on-the-server-in-your-zone)).

Progress is shown as **watched / aired**, with the season's full episode count secondary. Since
completion counts only aired episodes, showing `8/10` next to a status of *watched* would otherwise
read as a bug.

---

## 7. Games

### Purpose

An owned-games collection with a rich detail page.

- **Library page** — a grid of owned games showing **cover art and name only**. Nothing else for now.
- **Game detail page** — locally stored game information, **hours played**, the user's **achievement
  list** (including achievement images), and a rating.

### Progress: hours played

**Hours played is the progress metric.** There are no explicit play statuses — played vs unplayed is
**derived from hours** (0 hours = unplayed). No backlog / finished / dropped states.

### Per-game data

- Platform
- Store / launcher
- Hours played
- Achievements (with images)
- Rating

### Metadata source: Steam only

Steam supplies both the metadata and the artwork — header/capsule and `library_600x900` cover images
from the Steam CDN, and achievement icons via `GetSchemaForGame` — so no second provider is needed.
See [D-002](#d-002--steam-is-the-only-games-provider).

**Hours played and achievements can also be entered manually**, for games Steam doesn't know about or
values the user wants to set themselves.

### Images stored locally

The same rules as the other two components, not a games-only arrangement: the **game cover** and
**every achievement icon** are downloaded and stored. The cover gets two sizes; achievement icons get
one ([D-005](#d-005--achievement-icons-store-a-single-size)).

### Steam sync: user-driven, never surprising

- One **shared server-side Steam Web API key** — admin configuration, held server-side and never
  exposed to the browser — plus a **per-user SteamID64**.
- Sync is **manually triggered** by the user. There is no scheduler and no background refresh for
  games.
- The sync presents its results for **selection**: the user picks which games are actually imported.
- For games already in the library, the user **decides per game whether to overwrite** existing data
  with the freshly fetched values. Manually entered hours and achievements are never silently
  clobbered.

Caveats to document for users:

- A user's Steam profile and game details must be **public** for `GetOwnedGames` to return anything.
  Surface that as a clear message, not an empty list.
- The `appdetails` endpoint used for store descriptions is **undocumented and rate-limited** — treat
  it as best-effort and never fail a sync because of it.

---

## 8. Milestones

Delivery order. Each milestone is shippable. `CLAUDE.md` names the currently active one, and work
does not start on a later milestone before the active one closes.

| Milestone | Contents |
|---|---|
| **M0** — skeleton | Repo layout, config loading (fail-fast on required env), migration runner applied at startup, health endpoint, embedded SPA shell, multi-stage Dockerfile, `docker-compose.yml`, Caddyfile |
| **M1** — auth + movies | Local auth (argon2id) + server-side sessions, **plus OIDC login pulled forward from M5** (D-018); movies: TMDB search, add, mark watched, the watchlist axis, ratings; `user_watch_history` with rewatches, **plus the history-by-date feed over it pulled forward from M2** — movies only, since the episode arm needs M2's catalog tables; poster + backdrop stored locally **through the throttled queue**; TMDB attribution in the UI. *watched* is the only status a movie ever has — **a movie cannot be dropped, in this or any milestone** (see [§6](#statuses)) |
| **M1.5** — show tracking | Pulled forward from M2 on request, because a show page whose "Mark As Watched" is permanently disabled is a dead end. Adds the `shows` and `episodes` catalog tables, `user_show_states`, and a `show_id` on `seasons` (see [D-015](#d-015--the-details-views-may-call-tmdb-live-for-untracked-snapshot-with-bounded-staleness-for-tracked) — a season VIEW must not create rows at all; only tracking writes a season, so the column is **NOT NULL**); `RecomputeShowStatus` as the second writer in `common/status.go`; episode marking and the **"mark whole show watched"** bulk action; show and episode ratings but **not** seasons ([D-020](#d-020--seasons-are-not-rateable)); derived show progress; the show asset tree with every season poster and episode still downloaded eagerly; the show details view gains its snapshot/refresh/stale-serve half; the history feed **gains its episode entries**; and **show watchlisting, also pulled forward on request** — a show is watchlistable from its details page and the watchlist page lists both kinds together; and **removing a single watch entry, also pulled forward on request** — a watch-history list you cannot correct is one where a mis-tap is permanent, so the details-page timeline deletes one row by its id and the status recomputes with it; and **dated watch logging with an optional rating, also pulled forward on request** — [D-009](#d-009--rewatch-history-is-first-class-for-video)'s deferred "log a rewatch" operation, which is what lets the movie and episode pages' watch button stay usable after the first press. The timeline itself runs on **all four** video details pages, scoped three ways — one target, one season's episodes, or one show's across every season. The season and show forms are **scopes, not targets** (neither is ever itself watched), so they are separate query parameters rather than `target_type` values, and at most one narrowing may be sent. The whole-show mark later gained the **bulk dialog** — remaining episodes vs a full rewatch, and per-row dating: now, one fixed instant, each episode's air date, or each season's premiere with a fallback for seasons that have none (see [Bulk actions](#bulk-actions)). **"Mark whole season watched" was then pulled forward too**, on request: the same endpoint's `scope: season` (the season is the target; Specials are refused), the same dialog on the season page, the premiere fallback narrowed to that one season. **The currently-watching home view ("Up Next") was then pulled forward too**, on request, replacing the M0 dummy: started shows only (≥1 episode watch, dropped excluded — a watchlisted-but-never-started show does not appear), and the next episode is the first non-special episode **strictly after the highest watched position** — after S1E2 the answer is S1E3, never S1E1, and a show with nothing after its highest watched episode is absent rather than wrapping around. An unaired or undated next episode stays listed, flagged `aired: false` and badged "Upcoming" on the card, unless the release-date filters remove it; `only_released` splices D-012's predicate verbatim. **Owned copies were then pulled forward too**, on request — they belonged to no milestone: `user_video_copies` records which formats/services a user holds an item on, per movie and **per season** ([D-021](#d-021--owned-copies-are-per-user-and-per-season-for-shows)); the detail pages' "Your copies" tiles stop being a demo, a show-level tap fans out across every non-special season, and the show page's tiles report per-kind coverage ("3 of 5 seasons"). **Online providers were then pulled forward too**, on request — they belonged to no milestone either: the "Watch now" section's streaming half stops being a demo and renders TMDB's watch-provider data for one server-configured country, on the movie, show, season and episode pages, with locally proxied service logos and the JustWatch credit ([D-022](#d-022--online-providers-ride-the-item-snapshot-region-is-server-config)). **The Up Next card's mark-watched action was then pulled forward too**, on request, because a card that could only take you elsewhere made the one thing the view is for a trip through another page — but as a **chooser rather than the one tap this milestone described**: the whole card opens a two-action dialog, mark it watched or go to the episode's details page (the latter still a real link), and marking opens the same dated watch dialog the episode page uses — one instant and an optional rating in one transaction. The list is refetched afterwards, because the response states the show's status and progress but not its next episode. The season grid's per-episode tick was later removed: its stills are read-only badges (watched, the user's rating, the TMDB score — the history card's), and the season-level bulk button is that page's only mark action, so no one-tap undated mark remains in the SPA. **The user-set drop / un-drop path was then pulled forward too**, on request, because a show page that could mark and watchlist but never say "I gave up on this" left `dropped` as a value the whole app could read and nothing could write: `POST` / `DELETE /video/shows/{id}/drop`, show-only, idempotent both ways, never a provider call, with the two statements that write the value living in `common/status.go` beside the recomputes — the single-writer rule covers the column, not just the recompute. Only a started show can be dropped (no episode watch, no state row, a 422), the un-drop clears to `watching` and recomputes in the same transaction so a show dropped after finishing returns to *watched*, and Up Next needed no change at all: its query already excluded dropped shows. **Restoring a Ticket Box backup was then pulled forward too**, on request — it belonged to no milestone: `POST` / `GET /video/restore/ticket-box` take the version-2 JSON export of the author's own Trakt client and fold its history, ratings and watchlist into the user's state, replacing the watches of every item the file has watches for and upserting the rest, skipping-and-reporting what sofalog cannot take, materialising untracked items through the ordinary tracking gate, as an in-memory job the SPA polls from Settings → Restore backup ([D-023](#d-023--a-ticket-box-dump-is-the-one-import-path)). **Per-season progress and the episode-rating average on the show page's season grid were then pulled forward too**, on request: two derived, additive per-user fields on each season entry — watched / aired / total for that one season (Specials counting their own episodes, since this is display rather than the completion rule), and the mean of the user's episode ratings in it, a season not being rateable itself ([D-020](#d-020--seasons-are-not-rateable)); both null for an untracked show. **Similar titles were then pulled forward too**, on request — they belonged to no milestone: the movie and show pages' placeholder "Similar" rail renders TMDB's **recommendations** out of the item's own snapshot — no new provider call, stored verbatim in `metadata_json`, every card a foreign title reached by `tmdb_id` with its poster through the cache proxy ([D-024](#d-024--similar-titles-ride-the-item-snapshot)). **The scheduled TMDB refresh was then pulled forward too**, on request — D-006's one scheduler, now carrying two separately configured jobs ([D-025](#d-025--two-refresh-jobs-split-by-current-interest-on-the-row-timestamp)): an **active** job (default weekly) for every movie and show on any watchlist and every followed show still in production, and an **archive** job (default quarterly) for watched movies and followed shows TMDB has ended or cancelled, neither on any watchlist. Both re-fetch a show's root and its **open** seasons only, both are driven by the row's own `fetched_at` rather than a last-run record, and every hourly tick ends with the local status sweep — D-010's whole-table pass — whatever the intervals say. **A person details page was then pulled forward too**, on request — it belonged to no milestone: every cast card with a TMDB person id links to `video/person-details?tmdb_id=`, served live from TMDB on every view and never a catalog row, its filmography marked against the library by TMDB id ([D-027](#d-027--the-person-page-is-always-live-and-never-a-catalog-row)). **Matching watchlists were then pulled forward too**, on request — they belonged to no milestone and carve one narrow, opt-in exception out of "no sharing": a `users.match_watchlist` consent flag only its own account may set, a `match_user` filter on `GET /video/watchlist` that intersects the caller's list with every named consenting user's, and a peer list carrying id and label only ([D-028](#d-028--matching-watchlists-is-opt-in-and-only-ever-shows-the-overlap-with-your-own-list)). **One-way rating sync to the user's own TMDB account was then pulled forward too**, on request — it belonged to no milestone and is the product's first write to a provider: Settings → TMDB account links the user's own account through TMDB's approval page (the link is the opt-in), every local rating write queues a row in an outbox inside its own transaction, a worker drains it in the background, linking backfills the ratings already held, watchlist adds and removes ride the same outbox, and unlinking stops the push without touching TMDB's ratings or list ([D-029](#d-029--ratings-push-one-way-to-the-users-own-tmdb-account)). **A setup step after linking was then added**, on request: per kind, pull TMDB's list into the library once, push sofalog's once, or skip, plus a per-kind switch for the ongoing push that lives on the page afterwards ([D-036](#d-036--the-link-is-followed-by-a-one-time-setup-pull-push-or-skip-per-kind-and-per-kind-switches)). **Favorites were then pulled forward too**, on request — they belonged to no milestone: a third independent axis for movies and shows, modelled on the watchlist byte for byte — `POST` / `DELETE /video/favorites`, a heart beside the bookmark on the movie and show pages, and a Favorites page over `GET /video/favorites`. A favorite is a tracking action and a D-008 reference, keeps the item on the active refresh cadence, mirrors to the linked TMDB account through the same outbox, and stays off the upcoming calendar ([D-030](#d-030--favorites-are-a-third-independent-axis)). **The Jellyfin library scan was then pulled forward too**, on request — it belonged to no milestone and reverses one line of D-021: when `JELLYFIN_URL` and `JELLYFIN_API_KEY` are set, a background scan reads what the household's Jellyfin server holds, matches movies and seasons by TMDB id, and the `jellyfin` copy tile mirrors the answer for every user — read-only, refused by the copy routes, refetched on a schedule and on "Scan now" under Settings → Jellyfin. It never calls TMDB, never adds an item, and reads presence, not playback ([D-033](#d-033--the-jellyfin-tile-mirrors-the-households-server-when-one-is-configured)). **Trending and popular lists were then pulled forward too**, on request — they belonged to no milestone: a Trending page over `GET /video/trending` (today / this week) and a Discover tab over `GET /video/discover` — TMDB's `/discover` listing with its sort orders and filters, the popular list when unfiltered, its option lists from `GET /video/discover/options` — every list served live on every view, paged by TMDB's own `page` behind a "Load more" button and never stored — the person page's rule, an eighth provider call site — each card marked against the library by a local join and linking by local id when the catalog holds it ([D-034](#d-034--the-trending-and-popular-lists-are-always-live-and-never-a-catalog-row)). **Statistics were then pulled forward too**, on request — they belonged to no milestone and reverse one word of §9: a Stats page over `GET /video/stats`, every section computed on the server from the user's own log, ratings and show states for all time or one calendar year in their zone, never stored, never shared, never a provider call ([D-039](#d-039--statistics-are-a-read-only-projection-of-your-own-log-computed-on-the-server-in-your-zone)) |
| **M2** — shows, the rest | What M1.5 left: the upcoming calendar — shipped as a poster grid in date order with the date on each card, widened on request to watchlisted shows and to unreleased watchlisted movies ([D-026](#d-026--the-upcoming-calendar-is-a-day-agenda-over-followed-and-watchlisted-items)). **Closed.** |
| **M3** — books | **Withdrawn** ([D-035](#d-035--books-are-withdrawn-from-scope)): the component was specified, its first slice shipped, and the whole of it was then removed when the product narrowed to video and games. The number is kept so M4 and M5 keep theirs |
| **M4** — games | Manual Steam sync with per-game import selection and per-game overwrite choice; achievements + icons; hours played; manual entry of hours and achievements |
| **M5** — the rest | Export archive, API tokens (OIDC login shipped early, in M1) |

**M1 is deliberately more than a movies list with a toggle.** The local-first asset pipeline and the
catalog/per-user split ship in the first slice, because both are architectural — retrofitting either
means rewriting every query and every image path that exists by then. Everything else about M1 stays
as small as possible. See [D-003](#d-003--delivery-is-milestoned-m0m5).

---

## 9. Out of scope

Decided against rather than forgotten:

- **IGDB integration** / metadata for non-Steam games ([D-002](#d-002--steam-is-the-only-games-provider))
- **Rating history** — only the latest rating is kept
- **Reviews and free-text notes** on any media type
- **Game replay history** — games track hours, not sessions. (Video rewatches *are*
  in scope — see [D-009](#d-009--rewatch-history-is-first-class-for-video).)
- **Live check-in progress** (partial-watch position) and **media-player scrobbling** (Plex, Jellyfin,
  Emby, Trakt). `watch_type` records these as provenance labels so a third-party API client can set
  them, but no integration, webhook endpoint or progress tracking is built. The Jellyfin library
  scan ([D-033](#d-033--the-jellyfin-tile-mirrors-the-households-server-when-one-is-configured))
  reads what the server *holds*, never what was *played*: presence, not scrobbling.
- **Trakt / Letterboxd / Goodreads imports through their APIs.** The one import path is the Ticket
  Box dump file ([D-023](#d-023--a-ticket-box-dump-is-the-one-import-path)): the user's own Trakt
  client's export, read from a file the user already holds; sofalog never calls Trakt.
- **Books** — the third component (Open Library, ownership and reading state) was specified and
  its first slice built, then withdrawn ([D-035](#d-035--books-are-withdrawn-from-scope)). No
  books table, route or provider client exists
- **Explicit game play statuses** (backlog / playing / finished / dropped)
- **A "mark all previous watched" bulk action** (everything up to and including a tapped
  episode). Once planned for M2, dropped by decision: whole-season and whole-show are the two bulk
  marks, and a single episode is logged through the dated watch dialog
- **Social features and sharing** — with exactly one opt-in carve-out: the watchlist's
  matching filter ([D-028](#d-028--matching-watchlists-is-opt-in-and-only-ever-shows-the-overlap-with-your-own-list)), which shows the intersection of your list with a consenting user's
  and nothing else. No feeds, no profiles, no seeing anyone's list, progress, ratings or history.
  Statistics over your **own** log are in ([D-039](#d-039--statistics-are-a-read-only-projection-of-your-own-log-computed-on-the-server-in-your-zone));
  nobody else's ever are, and no number is ever compared across accounts
- **An offline-capable client** — no library mirror, write queue, or sync layer
  ([D-004](#d-004--works-with-providers-down-is-about-providers-not-about-being-offline-capable)).
  The one-way rating push to your own TMDB account ([D-029](#d-029--ratings-push-one-way-to-the-users-own-tmdb-account)) is not a sync layer: it exports
  your opinions outward and, after the one-time setup import ([D-036](#d-036--the-link-is-followed-by-a-one-time-setup-pull-push-or-skip-per-kind-and-per-kind-switches)), reads nothing back
- **Manual entry for movies and shows** — TMDB search is the only path. Manual entry *is* first-class
  for game hours and achievements.

Deferred but anticipated: a native mobile client (e.g. Flutter) consuming the same `/api/v1/`, which
requires no server changes; and **importing sofalog's own export archive back in** — the Restore
page's disabled "Sofalog dump" card. Until it lands, restore is done by restoring the data volume.

---

## Appendix A — Why these decisions

Reasoning for the rules above that are not guessable from their own statement. Each has a stable id;
`CLAUDE.md` and the skills reference these.

### D-001 — Separate tables per media type

An earlier draft proposed a common `items` table with a `media_type` discriminator and shared
`title` / `cover` / `status` / `rating` / `dates` columns, plus an `episodes` table for TV progress.

**Rejected** because the domains barely overlap — episodes vs. achievements
— so the shared table would be mostly-NULL columns plus a discriminator every single query has to
remember to filter on.

Two traps that came with that draft and are worth naming explicitly:

- The "shared fields" `status`, `rating` and `dates` are **per-user state**, not catalog data. They
  must not live on a catalog table at all, not even temporarily.
- "An `episodes` table for TV progress" conflates two things. `episodes` is a **catalog** table (one
  canonical row per real episode, shared by all users); watch state belongs in a separate per-user
  table.

**The carve-out.** D-001 rejects a discriminator on *catalog* rows. It does not forbid one on
**per-user** tables that legitimately span media types — `user_watch_history` (movie|episode),
`user_video_ratings` (movie|show|season|episode) and `user_video_watchlist` (movie|show) all carry
`target_type` + `target_id`, as do `asset_jobs` and `pending_deletions`. Two prices come with that,
and both are paid by discipline rather than by the database:

- A polymorphic column **cannot have a foreign key**, so deleting a catalog row does not cascade.
  Every delete path removes those rows explicitly in the same transaction.
- Ids are unique only within their own table, so **every** join or predicate through a polymorphic id
  must carry its `target_type` alongside. Omit it and a watch on movie 5 makes episode 5 look
  watched — the query returns the wrong row and nothing about the result looks wrong (D-013).

### D-002 — Steam is the only games provider

An earlier draft listed IGDB alongside TMDB and Open Library.

**Rejected for now:** IGDB is a second API key, a second rate-limit budget, a second metadata mapping
and a second image-variant scheme, all to serve non-Steam games. Manual entry of hours and
achievements already covers that case, and Steam supplies both metadata and artwork.

What keeps this reversible is already required elsewhere: asset directories are keyed by **internal
database id**, not provider id, so adding IGDB later strands no files. Keep it that way.

### D-003 — Delivery is milestoned M0–M5

An earlier draft described v1 as "local auth, movies list, watched toggle", while the product
requirements described every component in full. These are not in conflict — one is a *delivery
order*, the other a *target scope*. [§8](#8-milestones) records the order.

The one deliberate departure: **M1 includes the asset pipeline and the catalog/per-user split**, both
of which that minimal v1 would have skipped. Both are architectural. A movies list that stores
provider URLs and a `status` column on the catalog row is not a smaller version of the real thing —
it is a different thing that has to be rewritten.

**Rejected:** building every component in parallel. Nothing forces the choice technically, but
three half-finished components share no working vertical slice, and the asset queue and auth model
are better validated against one domain before three depend on them.

### D-004 — "Works with providers down" is about providers, not about being offline-capable

Read carelessly together, "network-first PWA caching with an offline fallback page" and "with no
internet access at all, everything except adding items behaves normally" imply an offline-capable
client. They don't.

The requirement is about **provider** reachability. A self-hosted app on a LAN still reaches its own
server when the WAN is down, so the requirement is satisfied **server-side**, by storing metadata and
images locally — not by the client.

**Rejected:** the literal joint reading. It turns a CRUD app into a distributed system with a merge
problem, and directly contradicts the "simple structure / minimal moving parts" goal. Hence: no
IndexedDB mirror, no client write queue, no sync layer.

### D-005 — Achievement icons store a single size

Three rules collide for this one case: two sizes per image; both fetched as provider variants; no
image-resizing code. Steam's `GetSchemaForGame` returns `icon` and `icongray`, which are the same
dimensions in different **states** (earned / unearned), not two sizes. There is no second size to
fetch.

So the two-sizes rule bends — it is the only one of the three that isn't load-bearing, since
achievement icons are already thumbnail-sized (64×64). The thumbnail path resolves to the same file.

**Rejected:** adding a resize step just for achievement icons. It adds an image-processing dependency
and a code path to the whole asset pipeline in order to downscale an image that is already tiny.

Note `icongray` is a *state* variant, orthogonal to sizing: store both if the UI distinguishes earned
from unearned.

### D-006 — Exactly one scheduled job exists

"Sync is manually triggered, there is no scheduler and no background refresh" is scoped to **Steam**.
The upcoming-episodes calendar separately requires a periodic TMDB refresh. Both are true.

Recorded explicitly because "no scheduler" is the memorable phrase and over-generalises easily.
Anything that wants to become a second scheduled job needs a new decision here. The asset download
queue is a background *worker*, not a scheduler — it drains user-enqueued work and never initiates
provider traffic on its own.

The one scheduler is an hourly loop in the video feature. It carries **two refresh jobs** with their
own intervals — active and archive, [D-025](#d-025--two-refresh-jobs-split-by-current-interest-on-the-row-timestamp) — plus the status sweep
[D-010](#d-010--status-is-stored-but-system-maintained) promises. That is still one scheduled job in
this decision's sense: one loop, one clock, one place that initiates provider traffic on its own.
D-025 is the decision that widened what rides it; anything else that wants to ride it, or to tick
on its own, needs another.

### D-007 — OIDC and API tokens ship in M5, but the schema makes room in migration 1

Both features are deferred to M5. The **schema** accommodates them from the first migration:
`users.password_hash` nullable, plus `oauth_identities` and `api_tokens`.

**Rejected:** a strictly minimal first migration with `password_hash NOT NULL` and no
`oauth_identities`. It saves nine lines of DDL and buys a schema migration plus an auth refactor
later.

**Vindicated in M1.** OIDC was subsequently pulled forward into M1 on request, and landed with **no
migration and no auth refactor** — exactly the cost this decision was taken to avoid. API tokens
remain M5.

### D-008 — Untracking never hard-deletes

The cleanup rule (last reference dropped → row and assets deleted) is in direct tension with the
durability goal (a provider may no longer serve what we cached). Naming that as "a risk to handle at
implementation time" is not enough — without a mechanism decided up front, the first delete path
written will be an immediate `RemoveAll`.

Hence the queue → confirm → deferred sweep design in
[§5](#cleanup-unreferenced-items-are-removed-but-never-abruptly). The single-directory-removal
property is why the on-disk layout is one directory per item.

**Rejected:** immediate hard delete on last-untrack. One misclick discards local copies that may be
unobtainable — the exact failure the local-first design exists to prevent.

**The reference set is larger than it looks.** A show is referenced by its state row, a watchlist row,
a favorite row, **any** episode watch, a rating on the show, **a season**, or an episode, or an owned
copy on the show, **a season**, or an episode — ten ways, and missing one
deletes data somebody still tracks. Because the polymorphic tables have no foreign keys (D-001), the
sweep must also delete their rows explicitly, *before* deleting the show, since those statements read
`seasons` and `episodes` which the show delete cascades away.

### D-009 — Rewatch history is first-class for video

Earlier drafts recorded at most one watch per episode and stated "the watched date, and nothing else",
with rewatch history explicitly deferred. Both are now reversed: `user_watch_history` holds many
timestamped rows per movie or episode, plus a `watch_type` provenance label.

The reversal was made for consistency with the books component the spec then carried, whose read log
was always multi-entry (books have since been withdrawn — [D-035](#d-035--books-are-withdrawn-from-scope)
— but the reasoning stands on its own). There was never a principled reason for "I read this twice" to be recordable while "I watched this twice"
was not.

**The asymmetry that survives, and why it is not an oversight:** rewatches accumulate, re-ratings
overwrite. *A watch is an **event** — it happened, on a date, possibly repeatedly. A rating is a
**current opinion** — there is only one, now.* Do not "fix" this by adding rating history.

**Rejected:** a `UNIQUE(user_id, target, watched_at)` constraint to make inserts idempotent. It
forbids a legitimate repeat watch — and once `watched_at` is an instant rather than a day, it stops
catching the duplicate it was meant to catch anyway, since two submissions a second apart differ. Idempotency belongs in the service layer instead, which
is why marking watched and logging a rewatch are two distinct operations rather than one. They are
two *modes* of one endpoint, not two routes: both resolve the same target the same way, so
`POST /video/watches` switches on whether the request states a `watched_at`. Stating one is the
user's explicit "this viewing happened, then", and always inserts; omitting one is the idempotent
mark a grid tick or a retry means.

### D-010 — Status is stored, but system-maintained

Status could have been derived on read like progress is. It is stored instead, because it must survive
being read cheaply from a list endpoint, and because `dropped` is not derivable from anything.

`dropped` is a **show** value. A movie's only status is *watched*, which means a movie's status
genuinely *is* derivable — it is `EXISTS(a watch row)`. It stays stored anyway, for the same
cheap-list-read reason and so that one recompute function covers both media types; see
[§6](#statuses) for why a movie has no other status to hold.

**The boundary, stated so it can be enforced:** *anything countable or positional — episode counts,
totals, percentages, "next episode", "last watched" — is computed at query time and never persisted.
`status` is the single stored label; it is written only by the recompute function, must be reproducible
from `episodes` + `user_watch_history` alone, and `dropped` is the one value that is user intent rather
than a derivation.*

This is not in conflict with the derived-progress rule — that rule forbids stored *counters*, and a
status is not a counter. Enforcement is mechanical: a grep asserts a single writer, and a repair query
asserts that any stored status still matches what the log implies.

**Accepted cost.** Eligibility depends on today's date, so a status can go stale with **no write
anywhere** — midnight passes, an episode becomes aired, nothing in the database changed. The recompute
therefore runs as a whole-table pass inside the existing TMDB refresh, which bounds drift to one
refresh interval without adding a second scheduler (D-006). Only the *label* lags: next-episode and
progress take the date as a parameter and are always correct.

**Rejected:** recomputing lazily on read. It puts a write on a read path and makes `GET` non-idempotent.
**Also rejected:** a second cron at midnight — it needs a new decision under D-006 and buys a few hours.

### D-011 — The watchlist is an independent axis

"Want to watch" was a value in the status enum, which made it mutually exclusive with *watching* and
*watched*. It is now its own table for movies and shows.

The precedent was the books component's two axes — ownership and reading status, deliberately
separate because you can own an unread book (books have since been withdrawn,
[D-035](#d-035--books-are-withdrawn-from-scope); the reasoning stands). The same logic applies to video: watchlisting a show you are already rewatching, or
one you dropped and mean to revisit, is ordinary behaviour that an enum forbids for no reason.

**Rejected:** keeping it as a status value stored in a separate table — that would be a storage change
with no behaviour change, which is not worth a table.

### D-012 — Completion counts aired, non-special episodes only

A show is *watched* when every episode that has actually aired, outside season 0, has been watched.

- **Unaired episodes are excluded** because otherwise a currently-airing show could never reach
  *watched*, and the "new episode → back to watching" transition could never fire. The predicate is
  `air_date IS NOT NULL AND air_date <= today` — never `IS NULL OR …`, which would treat an unknown
  date as aired and permanently block completion for any show with an announced-but-undated episode.
  `today` is the **user's** day, in their time zone
  ([D-038](#d-038--every-instant-is-stored-in-utc-every-day-is-measured-in-the-users-zone)): an
  episode dated the 19th has aired at midnight on the 19th where they live.
- **Specials are excluded** because TMDB adds season-0 entries retroactively. If they counted, a
  third-party catalog edit would silently un-complete somebody's show — a status change with no user
  action and no new content. Specials stay individually markable, rateable and present in history.
- A show with **zero eligible episodes** is *watching*, not *watched*. Without that guard "all zero
  episodes are watched" is vacuously true and a freshly added show flashes *watched* before its
  episodes are fetched.

Known limitation, accepted: a specials-only TMDB entry can never reach *watched*. Rare; the escape
hatch is *dropped*.

### D-013 — Catalog rows keep integer primary keys

Every table uses `INTEGER PRIMARY KEY` — SQLite's rowid alias, mapping to `BIGSERIAL` /
`GENERATED AS IDENTITY` on Postgres. Ordinary, boring, and the right default.

**The consequence that makes this worth writing down:** integer ids are unique only *within a table*,
so movie 5 and episode 5 both exist. Everywhere a polymorphic `target_id` appears — in
`user_watch_history`, `user_video_ratings`, `user_video_watchlist`, `asset_jobs` and
`pending_deletions` — the id alone does not identify anything. **The `target_type` predicate is a
correctness requirement, not hygiene.** A query that matches `target_id = 5` without
`target_type = 'episode'` will happily return a watch on movie 5, and the result will look entirely
plausible. Store tests should seed a movie and an episode with the same id and assert they stay
separate.

**Rejected: UUID primary keys.** Making catalog ids globally unique would turn that class of mistake
from "silently wrong row" into "returns nothing" — a real safety gain. It was rejected because the
cost lands everywhere while the benefit is confined to five columns that a single well-stated rule and
one test already cover: 36-byte keys inflate every index and foreign key, SQLite loses the rowid-alias
optimisation (and can't use `WITHOUT ROWID` without breaking dialect portability), asset directories
become unreadable when browsing the data volume, and it adds a dependency and a generation step to
every insert. For a self-hosted app at this scale, the discipline is cheaper than the machinery.

### D-014 — Feature-first packages, no `internal/`, no `cmd/`

*(Amended by [D-037](#d-037--one-package-per-feature-inside-a-component-one-common-no-sibling-imports):
"one package per feature" now means per feature **inside** a media component — `video/upnext`,
`video/lists`, … — not one package per component. Everything below about `internal/`, `cmd/`,
`features/` and the file-convention layering still holds, now per feature package.)*

Code is grouped by **feature** (`auth`, `video`, `games`), not by layer. An earlier draft
used the conventional Go layer layout — `cmd/sofalog` plus `internal/{api,service,store,web}` with
per-feature *files* inside each layer package.

**Rejected** in favour of one package per feature, because the product is explicitly "three
components, each an independently shippable feature": everything about the video domain should sit
in one place, not be sliced across four layer directories. The same applies to the SPA, which groups
routes/components/stores under `src/features/<feature>/`.

The choices that came with it, and their accepted costs:

- **No `internal/`.** `internal/` is a Go toolchain keyword granting compiler-enforced privacy
  against external importers. sofalog is an application, not a library — its public contract is
  `/api/v1`, not its Go packages — so that enforcement buys almost nothing here and costs a path
  segment in every import. Accepted cost: packages are technically importable by anyone who vendors
  the repo.
- **No `cmd/`.** The `cmd/<name>` convention exists for repos building several binaries. sofalog
  builds one; `package main` lives at `server/main.go`.
- **`server/` + `frontend/`** as the two top-level halves, with `go.mod` at `server/` declaring
  module `mbyte.ro/sofalog`. The media features are grouped under a `features/` directory — so
  imports read `mbyte.ro/sofalog/features/video` and the package name in code is still the bare
  feature name (`video.Service`). Grouping them keeps the `server/` root scannable: everything
  under `features/` is a product domain, everything beside it is infrastructure.
- **`auth` stays top-level**, not under `features/`. It is still a feature package (same file
  layering, same rules), but it is cross-cutting — every other feature's requests pass through its
  session middleware — so it sits with the infrastructure it behaves like. `features/` is reserved
  for the media domains a user tracks.
- **Layering survives as a file convention.** One package per feature means the compiler no longer
  enforces handlers → service → store or "only store code touches SQL". The direction still holds,
  carried by the `handlers*.go` / `service*.go` / `store*.go` naming rule and checked by `/verify`
  greps. **Also rejected:** layer *subpackages* per feature (`video/api`, `video/service`,
  `video/store`), which keeps compiler enforcement but triples the package count and forces
  interface plumbing inside every feature — the wrong trade for a "minimal moving parts" app.
- **Shared infrastructure stays flat** next to the features: `config`, `httpx`, `db`, `provider`
  (the metadata clients together), `assets` (the download queue and cache), `web` (the
  embedded SPA server). Features depend on shared packages, never on each other.
- A consequence for tests: with service and store in the same package there are no store interfaces
  to fake — service tests run against a real temp-file SQLite with the real migrations, which is
  what store tests were required to do anyway.

### D-015 — The details views may call TMDB (live for untracked, snapshot with bounded staleness for tracked)

*(Revised: the original decision materialised the catalog row on first view. That reversal is
recorded below under Rejected.)*

The details page must work for movies that are **not tracked yet** — a search result the user is
still deciding on. Storing every search result is forbidden ([§5](#5-local-first-architecture)), so
the line is drawn at tracking:

- **An untracked `tmdb_id` view is served live and stores nothing.** The endpoint fetches
  `/movie/{id}` (credits, external ids, videos and images appended), builds the same DTO the
  snapshot path builds — never a raw provider response — and discards it. No catalog row, no
  eagerly cached images: artwork downloads through the `data/cache` proxy only as the browser
  actually requests it. The response carries `id: null` and `tracked: false`. TMDB being down fails
  exactly this case (503), like search.
- **Tracking creates the row.** Marking watched or watchlisting an untracked movie fetches and
  stores the full snapshot (that is the §5 *add-item enrichment* call site — with TMDB down the
  action fails and nothing is written), then enqueues the movie's **entire details-page media
  set** — poster and backdrop (both variants), both 30-image galleries (both variants), cast
  photos, video thumbnails — through the asset queue into `data/assets/video/movies/<id>/`,
  promoting files the cache already holds instead of re-downloading. Once the queue drains, the
  page renders identically with TMDB fully down, media section included. Until it drains, image
  URLs fall back to the cache proxy per file.
- **A movie with a catalog row is served from its snapshot.** One older than **2 days** is
  refreshed synchronously on view; **if the refresh fails, the stale snapshot is served and the
  failure is logged** — a movie with a local row never returns a 503. A successful refresh also
  reconciles the stored artwork: files the new snapshot no longer names are **deleted** and newly
  named ones enqueued, so replaced artwork never accumulates as orphans. A lookup by local `id`
  never falls through to TMDB — a miss is a plain 404.

- **A show details view behaves exactly like a movie's**, since [M1.5](#8-milestones) gave shows a
  catalog table. An **untracked** show is fetched live from `/tv/{id}` (aggregate credits, external
  ids, videos and images appended — the seasons array rides the show root, so the whole seasons grid
  costs no extra call; `aggregate_credits` rather than `credits`, because TMDB's TV `credits` is the
  *latest season's* regulars only, while the aggregate is everyone credited across every episode
  with a per-actor episode count — what TMDB's own "Series Cast" page shows) and **discarded**: nothing stored, nothing downloaded, `id` null and the
  tracking flags false, and TMDB being down is a 503 because there is no snapshot to fall back on.
  A **tracked** show is served from its snapshot, refreshed past 2 days, and **stale-served** when
  that refresh fails — so a tracked show never 503s. A local `id` miss is a plain 404, never a TMDB
  call. The response contract is the same in all three cases, which is what it was written for.

  One bounded extra: a refresh that finds a season the show did not have before materialises that
  season. Existing seasons and episodes are **not** re-fetched on a view — that would cost one call
  per season every two days per tracked show — so their freshness stays the season view's business
  and the scheduled air-date refresh's ([D-006](#d-006--exactly-one-scheduled-job-exists)).

- **A season details view behaves exactly like a show's.** The season is fetched as
  `/tv/{id}?append_to_response=season/{n},season/{n}/aggregate_credits,season/{n}/credits` — one
  call for the show title, the season document, the season's OWN cast (everyone credited in its
  episodes, with per-season episode counts) and its regular cast; the nested appends come back as
  sibling keys, so the provider folds them into the stored season document under
  `aggregate_credits` and `credits`, the second place after the watch-region prune where a stored
  body is not TMDB's verbatim. The episode page reads its cast out of that same document — the
  season's regulars plus the episode's own `guest_stars`, which TMDB puts on every episode entry —
  so an episode view is still not a provider call site. A season of an **untracked** show is
  fetched by `(show tmdb_id, season_number)` — `/tv/{id}?append_to_response=season/{n}`, one call,
  the root supplying the denormalised `show_title` because the season document does not name its
  show — and **discarded**: nothing stored, `id` null, its episodes' ids null, and TMDB being down
  is a 503. A season of a **tracked** show is served from its snapshot, refreshed past 2 days, and
  stale-served when that refresh fails, so it never 503s. A local-`id` miss is a plain 404, never a
  TMDB call.

  The one bounded write is the show view's, in season form: a tmdb-key view of a **tracked** show
  whose season has no row — TMDB gained a season since the show was written — materialises and links
  that season with its episodes and enqueues its artwork, rather than leaving a season the asset
  diff and the mark path can never see. The show refresh's new-season branch does the same thing
  from the other page; whichever the user opens first wins, and both converge on the same row.

  This is why `seasons.show_id` is **NOT NULL**: a season row exists if and only if its show is
  tracked, so the link is always known at insert time. Relaxing that column is how
  materialise-on-view would come back.

- **An episode details view is a projection of its season's, not a fourth case.** `GET
  /video/episodes/details` addresses a season exactly as the season view does — `season_id`, or the
  show's `tmdb_id` plus `season` — plus an `episode` number, resolves it through the same function,
  and returns one entry of the resulting snapshot with its season and show context denormalised in.
  It adds no table, no provider call of its own and no stored state: every rule above carries over
  unchanged, including that a `season_id` with no row is a plain 404 with no TMDB call, that an
  untracked show's season carries a null `season_id`, and that a season with a row never 503s. An `episode` the season does not list is a 404. The `episodes`
  catalog table that [M1.5](#8-milestones) added does **not** change this: an episode row is what a
  watch or a rating targets, while what the page *renders* still comes out of the season snapshot,
  so there is still exactly one projection and still no second provider call site.

This keeps the details views the **fifth allowed provider call site** in §5. Accepted costs:

- **The refresh GET still performs a write** — an idempotent catalog upsert
  (`ON CONFLICT (tmdb_id) DO UPDATE`), touching no per-user state; two concurrent refreshes
  collapse via the conflict clause.
- **Untracked views are not outage-proof and not cheap to revisit** — every view of an untracked
  movie, show or season is a fresh TMDB round-trip. That is the price of storing nothing; the page a
  user cares about surviving an outage is one they tracked.
- **A row that loses its last reference keeps serving from its snapshot** (strictly better than
  going live and failing on an outage); its rows and files await the
  [D-008](#d-008--untracking-never-hard-deletes) sweep.

**Rejected:** *materialising the row on first view* (the original form of this decision — it made a
GET grow the catalog from mere browsing, left invisible rows and orphaned cache files behind
readers, and forced the D-008 sweep to clean up after a page view rather than after a user choice).
This holds for movies, shows AND seasons; seasons were briefly the exception and no longer are —
browsing a show's seasons left permanent orphan rows that nothing collected, since seasons have no
delete path of their own.

Also rejected: never refreshing (snapshots of tracked movies would fossilise); refreshing in the background after
serving stale (a second worker and a two-request UX for a page that is already waiting on the
response); storing search results wholesale (§5 forbids it, and 20 rows per keystroke is write
amplification for nothing).

### D-016 — Pre-release the schema is one editable baseline, not a migration chain

sofalog is not deployed anywhere and has no users, so there is no database in existence whose
history has to be preserved. Numbering the schema's construction into `0002`, `0003`, … while it is
still being designed buys nothing and costs plenty: `users` would arrive in one file, the movie state
tables in another, and a reader wanting to know the shape of the schema would have to replay four
files and apply the deltas in their head. SQLite makes it worse — dropping a column or changing a
`CHECK` is create-new → copy → drop → rename, so an early wrong guess becomes a permanent scar in
the file that documents it.

So until release the schema is a **single baseline**, `server/migrations/sql/0001_init.sql`, and
changing it means **editing that file** and recreating the development database. It is the schema as
one readable document.

**Release flips this permanently.** Creating `server/migrations/FROZEN` freezes the baseline; from
then on every change is a new numbered file and the baseline is immutable, because from that moment
a database exists that cannot be recreated. Two mechanisms enforce it rather than prose:

- `.claude/hooks/guard-immutable.sh` blocks edits to any migration except the un-frozen baseline.
- `db.Migrate` records a checksum per applied file and **fails at startup** when an applied file no
  longer matches, telling the developer to recreate the database. Without this, editing the baseline
  is silently skipped on an existing database — the worst outcome available, a database missing the
  change while reporting itself fully migrated.

This does **not** loosen anything else. The baseline still carries the shapes that exist for the
sake of later milestones ([D-007](#d-007--oidc-and-api-tokens-ship-in-m5-but-the-schema-makes-room-in-migration-1),
the reserved `CHECK` values), because those exist to avoid *post-release* migrations, which is
exactly the constraint that survives the freeze.

**Rejected:** dropping the migration runner for a plain `schema.sql` applied at startup. It reads
the same pre-release and throws away the machinery — plus its tests — that release needs on day one.
The runner already handles a one-file set; only the immutability rule needed a pre-release carve-out.

### D-017 — Video thumbnails are proxied from YouTube, not hot-linked

The movie details page has a media section mirroring TMDB's own: tabbed videos, backdrops and
posters. Backdrops and posters are TMDB images and need nothing new. Videos are the awkward case —
**TMDB lists a video by its YouTube id and hosts no frame of it.** So a video card has three options,
and all three cost something:

- **Hot-link YouTube's thumbnail.** Free, but it puts a provider URL in an API response and in the
  DOM, which §5 forbids outright, and it makes every viewer's browser issue a request to Google from
  every details page — with whatever cookies that carries. A self-hosted tracker leaking a page view
  to a third party per movie is precisely the shape this project exists to avoid.
- **No thumbnail.** A gradient tile with a play glyph. Costs nothing and stays inside the three
  providers, but a media gallery whose largest tab shows no images is a poor version of the feature.
- **Proxy the frame**, the same way TMDB posters are already proxied.

Proxying wins. It keeps the two rules that actually matter — **no provider URL ever reaches the
client**, and **no render path waits on a provider** — while the section still looks like a media
section. `server/provider/youtube.go` is a credential-free image client with its own rate limiter;
`/api/v1/video/youtube-thumbs/{key}` serves from `data/cache` and fetches on the first miss, exactly
like the poster proxy. The browser talks only to sofalog.

**What it honestly costs:** a fourth external host, outside the three *metadata* providers the spec
otherwise enumerates. YouTube supplies no metadata here — TMDB already named the video — it is an
image source and nothing more, which is why this does not reopen
[D-002](#d-002--igdb-is-out-of-scope): the argument there was about a second source of *truth*.

**Single size variant**, unlike every other image (§5): `mqdefault` at 320×180. YouTube's larger
`maxresdefault` 404s for anything but major releases and `hqdefault` is 4:3 with black bars, so there
is no reliable second 16:9 variant to fetch — the same reasoning as
[D-005](#d-005--achievement-icons-store-a-single-size). Video cards link out to YouTube rather than
opening the lightbox, so nothing needs the larger frame.

**Rejected:** embedding the YouTube player (every objection to hot-linking, plus an iframe and a
third-party script); adding YouTube as a configured provider with a Data API key (a key and a quota
to obtain nothing — the thumbnail CDN needs neither); using the movie's own backdrop as each video
card's image (implies the frame is from that video when it is not).

### Rejected stack alternatives

| Option | Why rejected |
|---|---|
| Angular / heavy SPA + separate API containers | 3–5× complexity for a CRUD app; no real server-resource benefit at this scale |
| htmx server-rendered UI | Great fit technically, but a Svelte SPA guarantees the public API is complete (dogfooding) and keeps the frontend familiar territory |
| Flutter Web | Canvas rendering: heavy load, poor web-native behaviour. Flutter remains a good option **later** as a native mobile client of the API |
| Next.js | Coherent stack, but the heaviest option (Node runtime, 150–300+ MB RAM, framework churn) — conflicts with the minimal-resource goal |
| Quarkus / Spring Boot (native) | Genuinely viable (native images reach ~50–100 MB RAM), but Go chosen for learning value and fit with the self-hosted ecosystem |
| Stateless JWT auth | Unnecessary for a single-server app; server-side sessions are simpler and revocable |
| nginx in front | Go serves static files fine; Caddy chosen solely for automatic TLS |

### D-018 — configuring OIDC disables local authentication entirely

When `OIDC_ISSUER`, `OIDC_CLIENT_ID` and `OIDC_CLIENT_SECRET` are all set, the built-in
email/password paths stop working: `/auth/login`, `/auth/setup`, local user creation and password
changes all return 404 with a message naming the reason, and the boot probe never reports
`setup_required`. Account management that is *not* authentication — listing, deleting, setting an
email — stays available.

The point of single sign-on in a self-hosted deployment is that there is exactly **one** place where
credentials live and exactly one place to revoke them. A password that still works alongside the IdP
is a second, unaudited way in that survives an account being disabled centrally — which is the
failure SSO is adopted to prevent. One auth path is also one path to reason about: no question of
which takes precedence, no half-migrated accounts.

A **partial** OIDC configuration is a startup error rather than a fallback, for the same reason: a
typo in the issuer must not quietly leave password login open on an instance the operator believes
is SSO-only.

**Rejected:** a break-glass `OIDC_ALLOW_LOCAL_ADMIN` escape hatch for IdP outages. It reintroduces
precisely the standing password surface the decision removes, in the highest-privilege account.
Config is validated fail-fast at startup, so recovery from a bad provider setting is to fix the
environment and restart — not to keep a spare key under the mat.

**Consequence:** an account attaches to an identity by **verified email only** — which, since
[D-019](#d-019--the-email-is-the-identity-the-display-name-is-only-a-label) made the email the
identifier, is simply "the addresses match". An unverified address never links (it would let anyone
who can set their own email at the IdP seize an account), and a sign-in carrying no verified email
at all is **refused** rather than provisioning an account nobody can reach — without one there is
no identifier to key on. An address that differs from the one someone registered with is corrected
by an admin under Settings → Users, which is why that affordance exists.

### D-020 — seasons are not rateable

Movies, shows and episodes carry ratings. **A season does not**, even though `user_video_ratings`
admits a `season` target type that could hold one.

A season is a **packaging unit**, not a work. The show is the thing a person has an opinion about,
and the episode is where the opinion gets specific — "season 4 was the good one" is a statement
about its episodes, and the app can already show that as a run of episode ratings. A third control
on a third page buys a value nobody sorts, filters or averages by, and it splits the user's attention
between three scales where two are enough.

**The `CHECK` keeps the value.** Narrowing it to three types is the one version of this decision that
cannot be undone: SQLite cannot alter a `CHECK` after the fact, and once the baseline freezes
([D-016](#d-016--pre-release-the-schema-is-one-editable-baseline-not-a-migration-chain)) the column
is immutable. So the schema stays permissive and the **service** refuses — one entry in
`RateableTargets` in `server/features/video/common/service_tracking.go`, with the refusal surfacing as a
422. Reversing this decision is adding `"season"` to that slice and wiring the shared rating bar
into the season page's "Your rating" slot; nothing else changes, and `seasons.id` is the id it
would target.

The season page's "Your rating" slot therefore holds no control: it renders the **average of this
user's episode ratings**, with a rated-of-total count ("7 of 13 episodes rated") and a note that
seasons aren't rated directly. That is the "run of episode ratings" made legible at season level —
a derived display recomputed from `user_video_ratings` per request, never a stored season rating —
and it reads as a decision where a silently absent control would read as an oversight.

### D-019 — the email is the identity; the display name is only a label

`users` has no `username`. The **email** is `NOT NULL UNIQUE` and is what you log in with and what
an OIDC sign-in matches on. A separate nullable `display_name` carries the human label, and is
deliberately **not** unique.

One identifier, shared by both auth modes, removes a class of reconciliation. Previously a username
was the local credential while email was the OIDC linking key, so the two paths could disagree about
who an account belonged to — and provisioning had to *derive* a unique username from an IdP claim,
sanitising it into a restricted charset and appending `-2`, `-3` on collision. That machinery
existed only to manufacture uniqueness that the email already had. It is gone.

Non-uniqueness of the label is the point of the split: two people in a household may both be
"Alice" and nothing breaks, because nothing keys on it. It is presentation, so an empty one falls
back to the email's local part **in the client** rather than being stored pre-filled — which keeps
"never set a name" distinguishable from "chose one that happens to match the address".

**Rejected:** keeping `username` as an optional display field. It reads as an identifier, would
have kept its uniqueness constraint out of habit, and invites code to authenticate against it again
later. Dropping the column is what makes the rule enforceable rather than merely intended.

### D-021 — owned copies are per-user, and per-season for shows

`user_video_copies` records which formats or services a user holds an item on — one row per
`(user, target, kind)`, with six kinds: `jellyfin`, `dvd`, `bluray`, `uhd`, and the unbranded
`physical` / `digital`. Four rules define the axis:

- **It is an independent axis, exactly like the watchlist (D-011).** Owning a copy says nothing
  about having watched it and never appears in a status `CHECK`. The precedent is D-011's again: an
  ownership axis was already the model for the watchlist, and a video copy is literally ownership.
- **For a show, copies live on seasons — `show` is a scope, never a stored target.** A show is not
  itself "owned on DVD"; its discs are, and season 1 on DVD beside season 2 on Jellyfin is the
  normal case, not the edge case. So the writable targets are `movie` and `season`, and a
  show-scoped call fans out to one row per **non-special** season — the mirror image of the bulk
  watch, where the season/show scopes name groups of episode targets. Specials never ride a
  show-level action (they are box-set extras the way they are completion extras, D-012) but stay
  fully manageable from their own season page. The show page renders coverage per kind
  ("3 of 5 seasons") rather than a boolean, because a boolean would have to lie about partial sets.
  The **episode** page is the same rule read downward: an episode owns nothing, so it borrows its
  season's copies (`EpisodeDetails.season_copies`) and shows them read-only, sending the user to
  the season page to change them. Displaying nothing there would be worse — "do I have this?" is a
  question you ask at the episode, and its honest answer lives one level up.
- **A copy is a reference and a tracking action.** Adding one to an untracked item materialises it
  (D-015) and syncs its assets — "I own this" is the strongest possible claim of interest, so a
  copies-only item must render from its snapshot and survive the D-008 sweep. `MovieReferenced` and
  `ShowReferenced` each carry copy arms; the show check carries **defensive** arms for `show`- and
  `episode`-target copies too, which are unwritable through the API but admitted by the CHECK — the
  same posture as the season-rating arm.
- **The CHECK keeps `show` and `episode`, and the six kinds are the CHECK.** Target types follow
  the D-020 pattern: SQLite cannot alter a CHECK later, so the column admits all four and the
  service allow-list is where the refusal lives. The kind list is enforceable in the CHECK today
  because the baseline is editable (D-016); once `FROZEN` exists, adding a kind is a
  create-copy-drop-rename migration — accepted, because an open TEXT column would trade that one
  future migration for permanently unvalidated data.

**Rejected:** a show-level copy row ("the complete series on DVD") beside the per-season rows. Two
granularities of the same fact must be reconciled everywhere they are read — is a show "on DVD"
when the show row says yes but half its season rows are missing, or vice versa? The per-season
representation can express the complete-series case (every season carries the kind); the reverse is
not true, so only the per-season form is stored and the show level is derived.

**Rejected, then partly reversed:** scrobbling or library-syncing against the named services. As
written here, `jellyfin` was a shelf label the user sets by hand, not an integration.
[D-033](#d-033--the-jellyfin-tile-mirrors-the-households-server-when-one-is-configured) later
made the `jellyfin` kind the answer of a **library scan** when a Jellyfin server is configured —
presence on the server, read in the background, never playback. §9's exclusion of media-player
**scrobbling** (Plex, Jellyfin, Emby, Trakt) is unchanged: nothing the scan reads touches the
watch history.

### D-022 — online providers ride the item snapshot; region is server config

The details pages answer "where can I watch this" from TMDB's watch-provider data. Four rules keep
that from costing anything:

- **It rides the existing calls, so it is not a new provider call site.** `watch/providers` is
  appended to the `/movie/{id}` and `/tv/{id}` requests the details snapshot already makes, and is
  parsed back out of the stored body like `credits` and `images`. §5's "no provider call on a render
  path" is therefore untouched, and a tracked item lists its providers with TMDB down. The cost is
  that availability — which really does change weekly — is only as fresh as the 48-hour snapshot
  window (D-015). Accepted: a stale row that says "Netflix" is a far smaller failure than a details
  page that cannot render without the network.
- **The snapshot is pruned to the configured region at fetch, and says which.** TMDB returns every
  country in one document: for *The Matrix* that is 111 of them and 87 KB of a 196 KB response —
  about 45% of the snapshot, to render one country. So `PruneWatchProviders` keeps `region`'s
  listing and writes a `sofalog_region` marker beside it before the body is stored. This is the one
  place a stored body is **not** TMDB's verbatim, which is the price of the saving; only the top
  level is re-serialized, and the pruned section still keys by region, so one parser still serves
  both a fresh fetch and a stored row. The marker is what makes a changed `TMDB_WATCH_REGION`
  self-healing: an empty `results` alone cannot distinguish "nothing listed here" from "pruned to
  somewhere else", so without it a region change would silently render nothing until each item aged
  out of its 48-hour window. With it, the same probe that backfills a widened append asks for
  **one** refresh per item on its next view.
- **The region is server config, not per-user.** One household is one country in the overwhelming
  case, and a per-user region would mean a `users` column, an endpoint and a settings control for a
  value nobody in a shared install would set differently. Making it per-user later is additive.
- **"No answer" and "the answer is nothing" are distinct, and the DTO field is nullable to keep
  them apart.** Null means no listing was available to read; an empty provider list means TMDB
  answered for the country and named nobody. Collapsing the two would force a choice between
  staying silent when there is something to say, and asserting an unavailability nobody checked —
  and the second is the kind of confident wrongness a local-first app cannot afford.
- **Seasons and episodes borrow the show's; provider logos live in `data/cache`, never
  `data/assets`.** TMDB publishes providers per movie and per show only, so a season and an episode
  read the owning show's row — a local read, never a fetch, and null while the show is untracked.
  The logos go through the existing image proxy (one variant, like achievement icons in D-005 and
  video thumbnails in D-017) because they are provider *branding*, not the item's artwork: filing
  them under an item's asset directory would put a weekly-changing set in front of a reconciler
  that deletes everything its desired set does not name (D-008's tree).

**Rejected:** fetching providers live on the details render, to keep availability current. That is
precisely the call §5 forbids, and it would make a page that works offline today fail when TMDB
does.

**Rejected:** a `watch_providers` table. It is a snapshot of somebody else's data, read only in one
place, keyed by nothing sofalog owns — exactly the shape `metadata_json` exists for (D-001's
per-media-type tables hold what sofalog reasons about; this is not that).


### D-023 — a Ticket Box dump is the one import path

§9 rules out Trakt, Letterboxd and Goodreads imports, and this decision does not reverse that: it
admits one **file**, from one app, and nothing over the wire. Ticket Box is the author's own Trakt
client, and its backup export — watch history, ratings, watchlist and favorites, each entry
addressed by TMDB id — is the record a first-time sofalog user already holds. Refusing it would mean re-marking a
decade of viewings by hand, which is the failure "your data lives on your server" exists to avoid.

- **A file, never Trakt's API.** The dump is read from an upload; sofalog gains no Trakt client, no
  credential, no fourth provider. The only provider traffic a restore causes is the materialisation
  of items the file names that are not yet in the catalog — the same add-item enrichment call every
  tracking write makes (D-015), through the same `ResolveTrackedShow` / `RefreshMovie` gate. Nothing
  about "no provider call on a render path" changes.
- **One version at a time — now 5 — decoded strictly.** Ticket Box's version-1 export identified an
  episode by the
  episode's own TMDB id and nothing else — no show id, no season or episode number — and TMDB has no
  endpoint that resolves one of those. Rather than add a Trakt lookup to bridge the gap, the export
  was extended (version 2 carries the show's TMDB id and the position on every episode entry, adds
  the watchlist, and writes UTC), and sofalog refuses anything else with a 422 that says so. The
  decode disallows unknown fields because `schemaVersion` **is** the compatibility mechanism: a field
  the version does not define is a shape change and should bump the version, not slip through. The
  two sides move in lockstep and only the current version is read: version 3 dropped every Trakt
  id, 4 added the `favorites` section (movies and shows, each with a `favoritedAt`, imported
  insert-if-absent like the watchlist and mirrored to a linked TMDB account like every favorite,
  D-029/D-030), and 5 added an `imdbId` that is accepted and ignored.
- **Replace, scoped to what the file has watches for.** The user said the dump wins where the two
  disagree. "Wins" is defined as: for every movie or show the file has **watch rows** for, this
  user's existing watch rows for that item (every season, for a show) are deleted and the file's
  inserted, in one transaction with one status recompute. An item the file only rates, watchlists or
  favorites keeps its watches — a rating names no viewing, so it is no evidence about the log. Items the file
  does not name are untouched. This is what makes re-running the same file idempotent, and what
  makes "run it again" the whole recovery story below. Merge-and-dedupe heuristics (same target and
  timestamp?) were rejected: two sources that disagree about when a viewing happened have no
  reconciliation the app can defend, and the dump is the older, richer record.
- **Skip and report, never fail.** A ten-thousand-row import that aborted on its first season rating
  would be useless, so every entry sofalog cannot take is one line in the report with a stable
  reason: a season rating (D-020 — the refusal is the same `RateableTargets` policy), a season or
  episode on the watchlist (`watchlistTargets`), an entry with no TMDB id, an instant in the future
  or unparsable, an episode TMDB's listing lacks, a show TMDB does not know or could not be fetched.
  A report line must be actionable, so each names its movie or show by the catalog title (looked up
  locally when the job ends) and the SPA links it — and its season — by TMDB id; a missing episode
  also carries a hint deduced from the numbers TMDB lists for that season: specials (numbered
  differently on Trakt and TMDB), a season TMDB lacks, a number past the season's last, or a gap
  mid-season — the last two being how TMDB lists a two-part episode as one.
  After three consecutive provider outages the run stops asking and skips everything that would need
  a fetch, while items already local still import — an untracked show costs a full materialisation
  with its own timeouts, and a dead TMDB must not turn a restore into an hour of failing.
- **One transaction per movie or show, materialisation outside it.** The same shape as every other
  tracking write, for the same reason: under WAL the single writer must not be held across network
  calls. The clock is read once per group, not once per run — an import can cross UTC midnight, and
  D-012's aired predicate must not lag by the end. The file's instants move `watched_at` and nothing
  else; a rating's `updated_at` does take the file's `ratedAt`, because a restore reproduces the
  source's record where `MarkWatched` stamps a fresh opinion.
- **An in-memory job, one per user.** A durable job table would survive a restart, but resuming is
  the hard part of one — which groups committed, whether a half-materialised show is whole — and
  replace semantics already make the next run converge. So a restart loses the progress display and
  nothing else. The job outlives its request (the goroutine runs on a context detached from the
  request's cancellation) and is cancelled at process shutdown; a second start while one runs is a
  409. There is no cancel route: re-running is the correction.
- **No episode TMDB id column — but the restore resolves by the id first.** The schema's comment on
  `episodes` still holds (a second UNIQUE would turn a TMDB renumber into a hard failure), and the
  position within the show stays the handle every other path uses. The file's episode id is not
  ignored, though: an export's positions are whatever Ticket Box cached, and TMDB renumbers — a real
  backup named Top Gear's specials 0x1–0x15 where TMDB now lists them at 0x401 and up, so a
  position-only import skipped them. The id needs no column to be useful, because every stored
  season document keeps TMDB's episode ids verbatim: the restore reads the show's season documents,
  places the entry by its id within that show, and falls back to the position only when the file
  has no id or the show's listing does not know it. Restore-only; no other path changes.

**Rejected:** a Trakt API lookup to resolve version-1 files. It would have made sofalog a Trakt
client for one purpose, added a rate-limited provider (about fifty minutes of lookups for a real
library at Trakt's ceiling), and contradicted §9 for a problem the export's author could fix at the
source.

**Rejected:** a `restore_jobs` table, and a `DELETE` route to cancel. See the in-memory bullet.

### D-024 — similar titles ride the item snapshot

The movie and show details pages each end with a "Similar movies" / "Similar shows" rail. It renders
TMDB's **recommendations** for the item, and it arrives the way D-022's providers did: appended to
the one `/movie/{id}` or `/tv/{id}` call the snapshot already makes, stored, and parsed back out at
projection time. Nothing about §5's call-site list changes.

- **It rides the existing calls, so it is not a new provider call site.** `recommendations` is
  appended to `MovieDetails` and `TVDetails` beside `credits`, `images` and `watch/providers`. A
  tracked item's rail therefore renders with TMDB down, and an untracked view gets it from the same
  live body it gets everything else from. A dedicated `/similar` route answered on render would be
  the sixth provider call site §5 forbids.
- **TMDB's `recommendations`, not its `similar`.** TMDB publishes two lists. `similar` matches on
  keywords and genres and is noticeably noisy; `recommendations` is built from what its users watch
  together and is the list TMDB's own title pages show. The rail keeps the "Similar" label because
  that is what a reader expects the section to be called — the source is an implementation choice.
- **Stored verbatim.** Unlike the providers section, which spans every country and had to be pruned
  to one (D-022), a recommendations page is one list of at most twenty compact entries. It is stored
  byte for byte like `credits` and `images`; the fold and the region prune stay the only two places
  a stored body is not TMDB's. One page only — a second page is a second call, and twenty is already
  more than a rail scrolls.
- **Movies and shows only; a season and an episode carry no rail.** TMDB publishes recommendations
  per movie and per show, so `TVSeasonDetails` does not append the section and the season and
  episode DTOs do not borrow it downward the way they borrow `streaming` — a season page recommending
  whole shows would answer a question nobody asked there.
- **Every entry is a foreign title.** A recommended movie or show has no catalog row and gains none
  by being recommended (D-015). The card therefore links to its details page by `tmdb_id` — the
  same address a search result uses — and its poster goes through the poster proxy into
  `data/cache`, **never** `data/assets`: it is not this item's artwork, and filing it under the
  item's directory would put it in front of a reconciler that deletes whatever the item's desired
  asset set does not name. The DTO carries no `media_type`, because a movie's list holds movies and
  a show's holds shows — the page already knows the kind.
- **`similar` is never null.** D-022 distinguishes "no listing to read" from "TMDB named nobody"
  because the page words them differently. A rail does not: "no section yet" and "no
  recommendations" both render the same empty note, so the field is a plain array and the missing
  case is decided elsewhere — the two missing-sections probes gained a `recommendations` clause, so
  every existing row refetches once on its next view and then never again for this reason.

**Rejected:** a `GET …/similar` route fetching live on render. The whole point of §5 is that a page
of a tracked item never waits on TMDB.

**Rejected:** materialising recommended titles as catalog rows, or storing the list in a table of its
own. It is TMDB's opinion about other titles, read in exactly one place; that is what `metadata_json`
is for (D-022's argument).

### D-025 — two refresh jobs, split by current interest, on the row timestamp

The scheduled TMDB refresh ([D-006](#d-006--exactly-one-scheduled-job-exists)) was pulled forward from M2 and shipped as **two jobs on
one hourly loop**, each with its own interval. Eight rules hold it:

- **Two jobs, two intervals.** The **active** job (`TMDB_REFRESH_INTERVAL`, default 7 days) covers
  every movie and show on **anyone's** watchlist or among anyone's favorites (D-030), plus every
  followed show TMDB still lists as in production. The **archive** job (`TMDB_ARCHIVE_REFRESH_INTERVAL`, default 90 days) covers watched
  movies and followed shows TMDB marks `Ended` or `Canceled`. What someone is waiting for changes
  weekly — air dates, a release date, a new season; what they have finished with changes only in
  artwork and streaming listings, and that moves in months. `0` disables a job.
- **The watchlist and the favorites always win.** The archive arms all begin with *not on any
  watchlist and not among anyone's favorites*, so the two selections are disjoint and either row
  means the faster cadence whatever the production status. Those are the two places a user says
  "I am still interested".
- **Production status, not user status, splits shows.** A show the user has finished but TMDB still
  lists as returning gets new episodes — exactly what the calendar exists to surface — so it stays
  active. A `NULL` production status counts as in production until TMDB says otherwise. "Followed"
  is any non-dropped state row (which exists iff the log holds an episode watch, D-010), and it is
  read across all users: one user dropping a show must not starve another's calendar.
- **Ended shows are refreshed at all.** Revivals happen, specials land years later, and streaming
  availability moves; the cost is bounded by the slow cadence rather than avoided.
- **The row timestamp is the schedule.** There is no last-run table and no job state that survives
  the process. A pass selects the items of its class whose `fetched_at` predates now minus that
  job's interval — `fetched_at` is fixed-width RFC3339 UTC, written for exactly this range predicate
  — so a restart or a week of downtime self-heals on the first tick, and a page view that refreshed
  an item on its own simply postpones it. Movies use the details view's own pair (`RefreshMovie` +
  the asset reconcile); shows use `RefreshShow` (root + seasons TMDB has added since) and then their
  open seasons.
- **Open seasons only.** A season is re-fetched only if it is the show's highest-numbered one or a
  non-special season still holding an undated or future episode, and its own snapshot is due.
  Specials are left out of the undated arm because undated specials are common and permanent. A
  closed season is never re-fetched by the job; the season page's 48h rule still covers it on view.
  Open seasons are computed **after** the root refresh so a season the root just materialised is not
  fetched twice.
- **The status sweep is local and hourly.** Every tick ends by running `RecomputeShowStatus` for
  every non-dropped (user, show) pair holding a state row or an episode watch — D-010's whole-table
  pass, done as a loop over the scoped single writer so there is still exactly one recompute in the
  codebase. It touches no provider and runs whatever the intervals say, even with both jobs disabled
  or no TMDB credential; it is what flips `watched → watching` when an episode aired overnight with
  nothing fetched.
- **Failure keeps the snapshot.** A fetch that fails leaves the row and is retried next tick; three
  consecutive outages trip a breaker shared by both passes for the rest of the tick. A TMDB 404
  keeps the row — the log may reference it — and silences that item for one interval, in memory,
  so a removed title is not asked for every hour.

**Rejected:** a last-run table. Resuming is the hard part of one, and the row timestamps already
say what is due. **Rejected:** per-user selection — the catalog is shared, so an item is due once.
**Rejected:** re-fetching every season of a selected show; a twenty-season show would cost
twenty-one calls a week to learn nothing. **Rejected:** a recency window on watched movies (the
original sketch refreshed only those released in the last two years) — the archive cadence makes
the distinction unnecessary and the rule simpler.

### D-026 — the upcoming calendar is a day agenda over followed and watchlisted items

M2's calendar shipped as `GET /video/upcoming` and the SPA's **Upcoming** page, widened on request
beyond §6's original "followed shows" to two more sources. Seven rules hold it:

- **Three sources, one window.** Unwatched, dated, non-special episodes of every show the user
  **follows** (a non-dropped `user_show_states` row — which exists iff the log holds an episode
  watch, [D-010](#d-010--status-is-stored-but-system-maintained)) **or has on their watchlist**, and
  unwatched **movies on their watchlist** with a release date in the window. A watchlisted show the
  user has not started is exactly what a calendar is for — "when does this begin?" — and a movie's
  release date is the same question with one entry.
- **Dropped wins over the watchlist.** §6 already excluded dropped shows from the calendar; a
  watchlist row does not override the user's explicit "I gave up on this".
- **Today is in, and the window is `[from, from + days)`.** An episode airing today is the most
  useful entry on the page, and the `NOT EXISTS` on the watch log is what makes it leave once
  marked. That overlaps [D-012](#d-012--completion-counts-aired-non-special-episodes-only)'s aired
  predicate by one day deliberately: `listUpcoming`'s window is the **complement** of
  `EligibleEpisode`, not that constant, and the two are never to be unified — one decides
  completion, the other display. `days` is 1..366, default 90; the SPA offers two weeks, a month,
  three months and a year.
- **`from` is the user's day.** The server defaults to the user's today in their time zone, and the
  SPA sends that same day explicitly. A calendar headed "Today" must agree with the viewer's clock,
  and west of Greenwich in the evening UTC today is already local tomorrow. Completion, the status
  sweep and Up Next's `aired` measure "today" in the same zone
  ([D-038](#d-038--every-instant-is-stored-in-utc-every-day-is-measured-in-the-users-zone)), so the
  calendar and the aired predicate can no longer disagree about which day it is.
- **Undated episodes and specials are out.** A calendar entry needs a date; an announced-but-undated
  episode is surfaced by Up Next with its Upcoming badge, which is the honest place for "coming,
  when TMDB knows". Season 0 never gates completion and TMDB dates it loosely.
- **The API is flat; the client folds.** Entries are ordered by date, then case-folded title, then
  show id, season and episode — one `UNION ALL` over the two arms so ordering has one definition, in
  SQL, with the sort key emitted as a result column (a compound SELECT may only order by its result
  columns on both engines). The SPA collapses a same-show, same-season, same-day run — a whole
  season released in one day — into a single card and otherwise keeps the server's order. This is
  the history feed's convention: grouping is presentation.
- **One poster grid, the date on the card — not a month grid.** A month grid spends most of its
  cells on empty days in a personal library and collapses below 600px. The app's poster grid — the
  same card size as Up Next, the watchlist and history — in date order, with each poster carrying
  its date in a corner badge ("Today" and "Tomorrow" highlighted), reads the same at every width,
  needs no per-device fork, and needs no day headings that would only repeat what every card says.

**Rejected:** a month-grid calendar (above). **Rejected:** day sections with a heading bar over each
day's cards — tried first; a single-card day is a bar and one poster, and the badge on the poster
says the same thing in less room. **Rejected:** server-side grouping — a nested response shape for
what is one ordered list. **Rejected:** two per-kind queries merged in Go — the ordering
rule would then live in two languages, and `strings.ToLower` and SQL `LOWER` disagree on non-ASCII.
**Rejected:** a wide nullable `UNION` row carrying both catalog shapes — the watchlist store already
rejects scanning two shapes through one row; identity columns plus the feed's projection helpers
keep poster choice in one place. **Rejected:** including undated episodes in a "date TBA" tail — Up
Next already shows them, with a badge, where the action is. **Rejected:** starting the window
tomorrow — it hides the one day that matters most. **Rejected:** a UTC-only window — see the
`from` rule. (This decision first kept completion on the server's UTC clock while the calendar
took the client's day; D-038 replaced both with the user's zone.)

### D-027 — the person page is always live and never a catalog row

Clicking an actor on any cast rail opens a person page: portrait, biography and a filmography from
TMDB's `/person/{id}?append_to_response=combined_credits,external_ids`. It is
[§5](#5-local-first-data-ownership)'s call site 5 — a details view — in its purest form, and four
rules hold it:

- **Every view is live and stores nothing.** There is no `people` table and no per-user state on a
  person, so nothing is materialised, nothing refreshes and nothing goes stale. TMDB being down is a
  503 on every view, not only an untracked one — the page is discovery, and the library is
  untouched. This is what "no provider call on a render path" was always about: a page over
  **existing local data** never waits on a provider, and a person page has no local data to be
  over.
- **The filmography is marked against the library locally.** Each credit the catalog holds carries
  its local `id` and this user's `watched` — the watchlist page's meaning (movie: any watch in the
  log; show: completed, [D-012](#d-012--completion-counts-aired-non-special-episodes-only)) — from a
  join by `tmdb_id` over the catalog, never a second provider call. A second mark, `in_history`,
  is the looser "have I seen any of it" — the same fact for a movie, **any episode watch** for a
  show — and feeds the "Seen with" grid above the filmography; the two differ exactly for a show
  that is started and not finished. The join is two compile-time queries scanning the whole
  catalog (a household's library, each row one indexed `EXISTS`), mapped in Go, rather than a
  variable-length `IN` list spliced at runtime.
- **Cast credits only, deduplicated per title.** TMDB lists a title once per character; the page
  shows it once with the parts joined " / ", and a show's episode count is the largest part's — a
  sum would exceed the show. `known_for_department` names the person's craft, so a director who
  also acts is not misdescribed. Crew credits are deferred, not rejected: an additive `crew` array.
- **The portrait rides the cache, at two provider variants.** `w185` and `h632` — TMDB's profile
  ladder, not the poster one — through the poster proxy into `data/cache`, never `data/assets`: a
  person owns no asset directory, and there is no reconciler to put a file in front of. An item's
  own cast photos stay single-size `w185` (§5).

The cast DTO gained a nullable `tmdb_id` for this — the stored bodies always carried the person id,
it was simply never declared — and a card is a link exactly when it is present.

**Rejected:** a `people` catalog table or a stored snapshot — a row nothing tracks, that no action
references and no sweep would ever collect. **Rejected:** a crew section in this slice — the entry
point is a cast rail. **Rejected:** a variable `IN` list for the library join — a spliced fragment.
**Rejected:** a `w500` portrait — TMDB publishes no such profile variant; `h632` is its detail size.

### D-028 — matching watchlists is opt-in and only ever shows the overlap with your own list

A household picks a film together more often than alone, and two watchlists that both hold it are
the shortest answer to "what shall we watch". The watchlist page's **"Matching watchlist"** filter
takes one or more other users and narrows the grid to what **every** one of them and the viewer
have listed. It is the product's first cross-user read, [§4](#4-data-model) says no user sees
another's state, and [§9](#9-out-of-scope) says no sharing — so this is a deliberate exception,
and five rules keep it narrow:

- **Consent is a `users` column, and only its own account writes it.** `users.match_watchlist`
  (`NOT NULL DEFAULT 0`) is flipped by `PUT /auth/users/{id}/match-watchlist`, **self only** —
  unlike the display name and email, an admin cannot act on another account, because this is
  consent to be visible and consent is not given on someone's behalf. It rides the session probe's
  `User` so the Settings switch renders the stored state. Auth owns the write; the video feature
  only reads the column, as it reads `movies`.
- **Only the overlap travels, and it is the caller's own state.** `match_user` (repeated) on
  `GET /video/watchlist` adds one `EXISTS` on `user_video_watchlist` per named user; every entry
  returned is on the caller's list, with the caller's `added_at`, `user_rating` and `watched`.
  Nothing of the other user's list is ever listed, and nothing about their progress, ratings or
  history is read at all. What leaks is exactly "this consenting user also wants a thing you
  already want" — for items on **your** list only.
- **The consent check lives in the SQL, not only in the service.** Each `EXISTS` joins `users` on
  `match_watchlist = 1`, so an id whose owner opted out — or that names nobody — matches no row
  even if a caller bypassed the picker. The service additionally refuses a non-sharing, unknown,
  duplicate or self id with a 422, so a user who switched sharing off after being picked surfaces
  as an error the client acts on (drop them, re-read who is sharing) rather than as an empty list
  that reads as "nothing in common". The intersection is capped at 10 users.
- **The peer list is id and label, never an email.** `GET /video/watchlist/match-users` lists the
  OTHER accounts that opted in, each as an id and a label — the display name, else the email's
  local part, [D-019](#d-019--the-email-is-the-identity-the-display-name-is-only-a-label)'s
  fallback applied server-side so the address never leaves the server. It is not the admin user
  list, which stays admin-only.
- **All of them, not any of them.** Several picked users mean an intersection: three people
  choosing a film need what all three want, and a union is a different, larger question this filter
  does not ask.

The `EXISTS` fragment is a compile-time constant appended once per id with one bound argument —
the same idiom as the `type` narrowing, only repeated — not a spliced `IN` list.

**Rejected:** showing another user's whole watchlist — that is sharing, and the product does not
do it. **Rejected:** union semantics or an any-of toggle — a bigger list is not what "what shall we
watch together" asks for. **Rejected:** letting an admin set the flag — consent belongs to the
account. **Rejected:** exposing the email in the peer list — the label is enough, and the admin
list already exists for the one role that needs addresses. **Rejected:** a per-pair sharing table
("share with Bob only") — a household has a handful of accounts, and one switch is the setting
nobody would set differently per person.

### D-029 — ratings push one way to the user's own TMDB account

A user who rates in sofalog may also keep a TMDB account, and typing the same number twice is the
kind of chore that makes one of the two lists go stale. So a user can link their **own** TMDB
account, after which every rating they set or clear here, every watchlist add or remove, and every
favorite add or remove (D-030), is sent there. It is the product's first
**write** to a provider and [§5](#5-local-first-data-ownership)'s sixth call site, and six rules keep
it from bending the local-first thesis:

- **The link is the consent, and it is per user.** There is no server-wide TMDB account: a
  `user_tmdb_accounts` row means "this is my account", and deleting it means stop. Which kinds are
  sent is the setup's and the three per-kind switches' to say ([D-036](#d-036--the-link-is-followed-by-a-one-time-setup-pull-push-or-skip-per-kind-and-per-kind-switches)); the link alone
  queues nothing. Consent is given by the account owner on TMDB's own approval page, through the same
  browser round trip as single sign-on — a request token, a redirect to TMDB, a callback at
  `{BASE_URL}/api/v1/video/tmdb/link/callback` derived from `BASE_URL` alone, a flow cookie binding
  the token to the user and browser that started it, and an exchange for TMDB's long-lived session.
- **The outbox fills in the local transaction and is drained in the background.** `Rate`,
  `RatingRemove`, a watch logged with a rating, `WatchlistAdd`, `WatchlistRemove`, `FavoriteAdd`,
  `FavoriteRemove` and the Ticket Box restore's rating and watchlist writes each write a
  `user_tmdb_sync` row in the same transaction as
  the local write, guarded in the SQL by the link's existence and that kind's switch — so an
  unlinked user, or one who turned the kind off, writes nothing even if a caller forgot to ask. A worker
  the video service owns sends the rows with backoff and a per-batch breaker; the rows are durable,
  so a restart resumes. The push is the one non-render provider call that a user does not trigger
  directly, and no request and no render ever waits on it. TMDB down defers a rating; it never fails
  one.
- **One row per (kind, target), holding the current intent.** A newer rating replaces a pending row
  rather than queueing behind it, and every settle is guarded on the row's `updated_at`, so a rating
  rewritten while its row was in flight is sent again instead of being deleted as "sent".
- **Ratings, the watchlist and the favorites each mirror automatically while their switch is on.**
  Each is one thing both sides mean the same by — a number, "I want to watch this", or "this one
  matters to me" — so none needs a per-action question; the setup answered it per kind. The `kind`
  column keeps the three apart in the outbox, and the setup's Push backfills whichever the user
  chose. The watch history is **not** mirrored: TMDB has no watch log, and a watch is not a
  value the other side can hold.
- **Local is authoritative, and nothing is read back after the setup.** The account is read once at
  link time, for the username the Settings page shows, and once at setup if the user chose Pull
  (D-036) — a migration, not a channel. Ratings on TMDB are then a mirror of sofalog's, never a
  source; the two-masters problem is exactly what this rule refuses.
- **A refused session marks the link broken; it does not unlink.** TMDB answering 401 for the
  stored session sets `broken_at`, discards the user's pending rows and stops new ones queueing.
  The row stays so the page can name the account and offer "Link again", which heals it and keeps
  the setup and the switches. Deleting the row would make "why did it unlink itself" unanswerable.
- **Unlinking stops the push and leaves TMDB alone.** The link and the pending rows go, the session
  is revoked at TMDB, and every rating and listing already there stays — it is the user's account.
  The setup's Push, by contrast, **backfills**: every rating, watchlist entry or favorite the user
  already holds of a pushed kind is queued, so the account reflects the whole library and not only
  what comes after.

The session id is a secret exactly like the application key: stored as TMDB issued it (there is no
application secret to encrypt it with, and one would live in the same environment as the key it
protects), never in a DTO, never logged — `server/provider` redacts `session_id` and
`request_token` values from every error the way it redacts `api_key`. Seasons are never pushed:
TMDB has no season rating either, which is what makes [D-020](#d-020--seasons-are-not-rateable)
symmetric. The outbox's polymorphic target has no foreign key, so the D-008 sweep, when built, must
delete matching rows in its transaction.

**Rejected:** reading TMDB's ratings back into the library as an ongoing sync — two masters, and the
local-first thesis has one (the one-time import at setup is D-036, and it is a migration).
**Rejected:** pushing inline in the rating request — a rating would then wait up to a
provider timeout and fail when TMDB is down, which is the failure mode the whole design exists to
avoid. **Rejected:** a server-wide TMDB account every user writes through — ratings are personal,
and one account cannot hold several people's. **Rejected:** a single sync switch beside the link —
two concepts that can disagree where one suffices; the per-kind switches of D-036 are a different
thing, a choice the link cannot express. **Rejected:** asking per action whether a watchlist change
should reach TMDB — a question on every tap for a choice the link already answered; it was built and
removed within the same change. **Rejected:** mirroring the watch history — TMDB holds no watch log.

### D-030 — favorites are a third independent axis

A user asked to mark a movie or show as a **favorite**, have it cached like a watchlist or history
item so it works with TMDB down, and see all favorites on one page. `user_video_favorites` is the
answer: the watchlist's table shape byte for byte — movies and shows only, one row per (user,
target), an `added_at` — and the watchlist's weight in every rule that reads it. Five rules define
the axis:

- **It is a table, not a status value and not a flag on the watchlist row.** "Favorite" says nothing
  about having watched a thing, so it cannot live in a status `CHECK` (the D-011 argument, a third
  time). It is not a column on `user_video_watchlist` either: a favorite you have already seen has
  no watchlist row to hang it on, and a watchlist row you clear must not take the favorite with it.
  Two independent axes are two tables; the precedent is copies (D-021).
- **A favorite is a tracking action and a D-008 reference.** "This one matters to me" is at least
  as strong a claim of interest as "I want to watch this", so favoriting an untracked title
  materialises it through the ordinary tracking gate (D-015) and syncs its full asset set, and a
  favorite row alone keeps the row and its artwork alive. `MovieReferenced` and `ShowReferenced`
  each carry a favorites arm — a show is now referenced **ten** ways.
- **A favorite is the active refresh cadence (D-025).** Like a watchlist row, it overrides the
  production-status split: a favorited movie the user watched years ago, or a favorited show TMDB
  has ended, stays on the weekly job. Favorites are the titles a user returns to, and their
  streaming listings and artwork are what that page shows first.
- **It mirrors to the linked TMDB account (D-029).** TMDB has a per-account favorites list with the
  watchlist's exact call shape (`POST /account/{id}/favorite`), so the outbox gained a third `kind`,
  the link backfills favorites beside ratings and listings, and nothing is asked per action — the
  link already answered.
- **It does not feed the upcoming calendar (D-026).** Favorites are about keeping and finding
  titles, not waiting for them; a favorited show the user follows is on the calendar through the
  follow, and a favorited movie already seen is not something they are waiting for.

The favorites page is the watchlist page's twin without the matching filter (D-028 is a watchlist
feature), and the entry, the two card projections and the parameter parser are **shared** with the
watchlist rather than copied, so the two lists cannot drift on what "sorted by title" means. The
movie and show pages carry a heart beside the bookmark; both toggles are the same round control.

**Rejected:** a `favorite` value in the rating scale, or "rated 10 = favorite" — a rating is an
opinion of quality and a favorite is attachment; people love films they would rate a 6.
**Rejected:** per-season or per-episode favorites — a favorite names a title, the way a watchlist
entry does; a favorite episode is a use nobody asked for and a rating already covers it.
**Rejected:** putting favorites on the upcoming calendar — see the last rule.

### D-031 — retired (the books library was the shared catalog)

Withdrawn with the books component — see [D-035](#d-035--books-are-withdrawn-from-scope). The id
is kept so it is never reused; the entry described why `GET /books` listed every user's books with
only the viewer's own marks on them.

### D-032 — retired (a book was enriched once and never re-fetched)

Withdrawn with the books component — see [D-035](#d-035--books-are-withdrawn-from-scope). The id
is kept so it is never reused; the entry was the standing guarantee that nothing about a book was
ever re-fetched automatically, and the reason `/verify` grepped the books package for the video
refresh gate's vocabulary.

### D-033 — the Jellyfin tile mirrors the household's server when one is configured

A user asked for the obvious thing: the Jellyfin tile under "Watch now" should light itself,
from the Jellyfin server in the same house, instead of being a label everyone forgets to set.
D-021 had explicitly rejected "library-syncing against the named services"; this entry reverses
that one line and nothing else about D-021. Six rules hold it:

- **One server, configured by the operator, and the answer is everyone's.** `JELLYFIN_URL` and
  `JELLYFIN_API_KEY` (both or neither — a startup error otherwise, because a half-set pair looks
  exactly like "not configured" and would silently never scan) name the household's server. "It is
  on Jellyfin" is a fact about that server, identical for every account, so it is stored **once**,
  in a shared `jellyfin_library` table (`target_type` movie | season, `target_id`, the Jellyfin
  id, `seen_at`) with **no `user_` prefix** — the same posture as the streaming listing (D-022:
  catalog-side, same for everyone), not the per-user shelf. The details views read it into the
  `copies` list as the `jellyfin` kind, and carry a `jellyfin_synced` flag so the client knows the
  entry is the scan's — server config on the DTO exactly as `streaming.region` is.
- **The scan owns the kind while a server is configured.** It adds and removes — a movie deleted
  from the server goes dark on the next scan — the copy routes refuse `kind: jellyfin` with a 422,
  a hand-set `jellyfin` row is ignored by the projection, and the SPA renders the tile read-only
  (no `aria-pressed`, a "synced" caption, a tap opens an explanation with a link to the scan's
  settings page). Unconfigured, nothing here runs and the kind is the hand-set label D-021
  described. Two sources of truth for one tile, or a tile that lies until the next scan, were the
  alternatives; neither is honest.
- **It is a pure read plus a local join, and never a tracking action.** The scan pages every movie,
  series and episode file out of Jellyfin's `/Items` (with an API key, so no Jellyfin user is
  involved), matches movies on `movies.tmdb_id` and seasons on `(show_tmdb_id, season_number)` —
  the TMDB id Jellyfin's own metadata carries — and rewrites the table in one transaction, so a
  failed scan leaves the previous answer standing. It never calls TMDB, never materialises an item
  (an untracked movie on the server is simply not matched until somebody tracks it; "scan now"
  covers the gap), and a `jellyfin_library` row is **not** a D-008 reference: the server's contents
  are not a user's interest. Jellyfin's virtual "missing episode" placeholders are excluded, and a
  file Jellyfin never identified is invisible — there is nothing to match it on.
- **A season is "on Jellyfin" once any of its episodes is, and the tile says when that is not all
  of it.** The tile answers "can I watch some of this there", so a season being downloaded lights
  up rather than staying dark until complete — but the row also counts the distinct episode files
  found, and a lit tile turns **amber** instead of the accent when a season holds fewer files
  than its aired episodes, or when any non-special season of a show is absent or partial. "On
  Jellyfin" must never quietly mean "some of it". The comparison is the client's: only the page
  knows the aired denominator it already renders. Specials match like any other season: the row
  exists and its own page manages it (D-021).
- **Scheduled and on demand; presence, never playback.** `JELLYFIN_SCAN_INTERVAL` (default `6h`,
  `0` leaves only the button, under `1h` a startup error — the refresh jobs' parser) runs it on a
  loop started by `Service.Run`, and `POST /video/jellyfin/scan` wakes it from Settings → Jellyfin,
  a 409 while one runs. Any signed-in user may press it — the scan is server-wide, idempotent and
  cheap. The report (what the server holds, what matched, a stable error code) lives in memory like
  the restore job's. Nothing it reads is play state, so §9's exclusion of scrobbling stands.
- **It is §5's seventh call site, in `server/provider` like every other outbound client.** The API
  key is a secret like the TMDB key — header-borne, never in a URL, redacted from every error
  string, never in a DTO — and a refused key is `ErrJellyfinAuth`, deliberately not an outage.

**Rejected:** writing the scan's answer as per-user `user_video_copies` rows. One row per user per
matched item is the same fact stored N times, to be kept in step across account creation and
deletion, and it would make a scan-written row a D-008 reference — a claim of interest nobody made.
**Rejected:** per-user Jellyfin links (each account its own URL and key). Real, but a second secret
store and a settings page for a household that has one server; the shared table makes it additive
later.
**Rejected:** matching live on the details render, or at track time. The first is precisely the
call §5 forbids; the second would make tracking a movie wait on a second server for a tile that
the next scan fills anyway.
**Rejected:** matching by IMDb id as a fallback. It lives in `metadata_json` only, so every scan
would parse every snapshot; Jellyfin's default agents carry the TMDB id. Additive later if needed.

### D-034 — the trending and popular lists are always live and never a catalog row

The Trending page lists what TMDB ranks as trending today or this week (`/trending/{all|movie|tv}/{day|week}`)
and, on its Discover tab, TMDB's `/discover/{movie|tv}` catalog listing — the popular list when
unfiltered, otherwise TMDB's own sort orders and filters — the discovery view Trakt users know as
"trending". It is [§5](#5-local-first-data-ownership)'s call site 8, and it is
[D-027](#d-027--the-person-page-is-always-live-and-never-a-catalog-row)'s rule applied to a list
instead of a person. Eight rules hold it:

- **Every view is live and stores nothing.** There is no table, no snapshot, no scheduler and no
  per-user state behind the page, so nothing is materialised, nothing refreshes and nothing goes
  stale. TMDB being down is a 503 on every view — the page is discovery, and the library is
  untouched. This is what "no provider call on a render path" was always about: a page over
  **existing local data** never waits on a provider, and a trending list has no local data to be
  over. A request is one TMDB call (two for a combined Discover page), then two local reads.
- **Entries are marked against the library locally.** Each title the catalog holds carries its
  local `id` — the card then links by it, as a watchlist card does — and this user's `watched` and
  `in_history`, with the person page's meanings (movie: any watch; show: completed / any episode
  watch). The join is the person page's two compile-time whole-catalog reads mapped by `tmdb_id`,
  never a second provider call and never a spliced `IN` list. The DTO is the search result plus
  those three fields, which is what lets the card be the filmography card unchanged.
- **Paging is TMDB's own.** Both routes take `page` and answer TMDB's envelope — `entries`,
  `page`, `total_pages`, the spec's one paged object — and the SPA's "Load more" asks for the next
  page and appends, ranks continuing. TMDB serves no page past 500, and neither route asks.
- **Trending "all" is TMDB's own ranking, and Trending has no filters.** TMDB publishes a
  cross-type trending list, so the page reads it as one call and keeps its order — the people it
  ranks alongside are dropped, which is why a page can come up a few short of twenty. TMDB offers
  no filters on that list, so the route offers none either: filtering the twenty rows after the
  fetch would thin every page, and the Discover tab answers the same question properly.
- **Discover is `/discover`, filtered by TMDB.** The sort (`popularity` — TMDB's popular order —
  `rating`, `newest`, `votes`) and every filter (score range, minimum votes, genres, original
  language, year range, streaming service) travel to TMDB, so a page stays full. Genres combine
  with AND (a title must carry every selected genre); providers with OR. The provider translates
  the kind-neutral query into TMDB's per-kind parameter names, because movies and shows name their
  dates differently. Two defaults keep the orders honest: `rating` with no vote floor applies
  TMDB's own site's 200 votes, and `newest` caps the date at today so announced titles do not lead.
  TMDB has no cross-type discover list, so "all" is both kinds' page merged by the chosen sort's
  own key and **not** capped — a cap would drop titles no later page brings back; search's "all"
  keeps its cap, since a search is not paged.
- **The streaming filter is the one request that sends `TMDB_WATCH_REGION` to TMDB.**
  [D-022](#d-022--online-providers-ride-the-item-snapshot-region-is-server-config) keeps the
  region out of every snapshot fetch, because the stored body holds every country and the region
  only picks a key at render time; a discover filter has no stored body to pick from, and TMDB
  needs `watch_region` beside `with_watch_providers`. It is sent exactly when a provider is
  selected and nowhere else. The region stays a server setting.
- **The option lists are TMDB's, cached for the process.** Genres per kind, the language list
  and the watch providers of the configured region come from TMDB (`/genre/{movie|tv}/list`,
  `/configuration/languages`, `/watch/providers/{movie|tv}`), cached in the provider the way the
  image configuration is — only a success is cached — and served by `GET /video/discover/options`.
  They are effectively static; a restart is the refresh. Their outage is only the panel's: the
  grid and the typed filters still work.
- **Posters ride the cache, never `data/assets`; no disclaimer on the page.** Every entry is a
  foreign title, held or not, so its poster — and a provider's logo — is the cache-proxied variant
  the search card uses; nothing here files under an item's directory, where the reconciler would
  delete it. The page carries no TMDB notice: the attribution lives on Data Sources alone, which
  the page links to, and Data Sources names the lists and the one region-carrying request.

**Rejected:** a shared in-memory cache with stale-serve for the lists. It would be a new pattern
for a page a household opens a few times a day, and there is nothing local for it to be stale
against; the lists are TMDB's answer at the moment of asking. Additive later if the calls ever
matter.
**Rejected:** a rail on the Up Next page. That view is the home page over existing local data and
must never wait on a provider or show a 503.
**Rejected:** filtering the trending rows after the fetch, or fetching several trending pages to
refill a filtered one. Both thin or multiply calls for a list TMDB does not filter; Discover is
the filtered view.
**Rejected:** TMDB's `now_playing` / `on_the_air` lists. Region-dependent and not asked for;
Discover's `newest` sort covers the need.
**Rejected:** a `rank` field on the DTO. The position is presentation; the client numbers the grid.

### D-035 — books are withdrawn from scope

The product was specified with three components — movies & TV, books and games — and the books
component had shipped its first slice: the shared library over `GET /books`, manual entry, the
hand-edit route, and the `books`, `user_book_states`, `user_book_reads` and `user_book_ratings`
tables, under D-031 and D-032. The user then decided the application would track **video and games
only**, and the slice was removed outright rather than left dormant.

- **What went.** `server/features/books`, `frontend/src/features/books`, the four tables, the
  `book` value in `asset_jobs.media_type` and `pending_deletions.media_type`, the `openlibrary`
  value in `asset_jobs.provider`, the `assets/books/<id>/` layout and its path builders, the
  `/books` routes and schemas in the OpenAPI document, the Books navigation group, and the `/verify`
  greps that guarded D-032. The baseline is pre-release
  ([D-016](#d-016--pre-release-the-schema-is-one-editable-baseline-not-a-migration-chain)), so the
  tables are gone from `0001_init.sql` rather than dropped by a later migration.
- **What stays.** The reasoning books lent to video: [D-009](#d-009--rewatch-history-is-first-class-for-video),
  [D-011](#d-011--the-watchlist-is-an-independent-axis) and
  [D-021](#d-021--owned-copies-are-per-user-and-per-season-for-shows) cite the books component's
  two-axis model and multi-entry read log as their precedent. The precedent is historical now; the
  rules it justified stand on their own. The asset tree's three-or-four-element item directory stays
  too, because games use the shallower depth.
- **Numbering.** D-031 and D-032 are retired with their ids kept. Books was §7, so Games, Milestones
  and Out of scope each moved up one number; M3 keeps its slot in the milestone table as
  "withdrawn" so M4 and M5 keep theirs.

**Rejected:** keeping the code behind a flag or an unlinked route. Code that owns tables and API
schemas is maintained code whether or not a link points at it; a component is either in the product
or not.
**Rejected:** keeping the empty tables "for later". Pre-release the baseline is editable (D-016),
and a table with no writer is exactly what the freeze would make permanent.

If books ever return, they return as a fresh decision and a fresh slice. Nothing here forbids it,
and Open Library remains the obvious provider.

### D-036 — the link is followed by a one-time setup: pull, push or skip per kind, and per-kind switches

A user who links their TMDB account usually arrives with lists already on it — years of ratings, a
watchlist, favorites — and often with a library here too. [D-029](#d-029--ratings-push-one-way-to-the-users-own-tmdb-account)'s
unconditional push could neither bring TMDB's lists in nor leave one kind alone. So linking now
queues nothing, and a **setup step** follows it on the TMDB account page: for each of ratings,
watchlist and favorites the user picks **Pull**, **Push** or **Skip**, and ticks whether to keep
that kind in sync from then on. The three switches stay on the page afterwards. Nine rules hold it:

- **Pull is a one-time import, never an ongoing read-back.** It runs once, as a background job
  (`POST /video/tmdb/link/setup`, polled through `GET /video/tmdb/link`), reads the account's
  rated-movies, rated-shows, rated-episodes, watchlist and favorite lists page by page, and is
  never scheduled. D-029's "local is authoritative" holds from the moment it finishes: the pull is
  a migration, not a channel.
- **A pulled rating overwrites; a pulled listing adds.** The user chose Pull, so TMDB's number
  replaces the local one — rounded half up from TMDB's 0.5 steps to sofalog's 1–10 (`0.5 → 1`,
  `7.5 → 8`). A watchlist or favorite entry is inserted if absent and nothing local is ever
  removed, so re-running an interrupted pull converges.
- **Pulled items go through the ordinary tracking gate** (D-015): a movie or show the library has
  never seen is materialised exactly as the Ticket Box restore materialises one, with the same
  outage breaker, and an episode is resolved by show, season and episode number — the catalog's
  own address — so an episode the snapshot lacks is a skip, never a guess.
- **Pulled writes never queue a push.** They came from TMDB; sending them back would be a loop. The
  job writes the local rows directly and leaves the outbox alone, whatever the switches say.
- **Push is the old backfill, chosen per kind, and it ignores the switch.** "Send what I have, then
  stop" is a legitimate choice, so a pushed kind with its switch off still sends once.
- **The switches govern future writes only, and they live in the SQL.** `EnqueueTMDBSync`'s guard
  checks the link AND the kind's switch, so every write path is covered without touching it.
  Turning a switch on queues nothing already held — the setup's Push is the one backfill; turning
  one off deletes that kind's pending rows in the same transaction, so "off" means off at once.
- **Nothing queues before the setup.** Every switch is off at link time, so a rating given between
  linking and confirming the setup is not sent behind the user's back.
- **`setup_done` lands when the work finished.** At submit when nothing pulls, at the end of the
  pull job otherwise — so a pull that fails, or is cut by a restart (the job is in memory, like
  the restore's), leaves the setup panel showing and the user runs it again. A second setup for a
  finished one is a 409.
- **Re-linking the same account keeps the setup and switches; a different account resets them.**
  Healing a broken link must not re-ask what was already answered, while a new account's lists are
  a new question, and the old account's pending rows must not reach it.

**Rejected:** continuous two-way sync — two masters; D-029's refusal stands, the pull is the one
read and it happens once. **Rejected:** a per-item conflict prompt during the pull — choosing Pull
is the answer, and a library of thousands cannot be asked one by one. **Rejected:** a "pull again"
button — one-time by definition; unlink and link is the redo, and it asks the question afresh.
**Rejected:** writing `setup_done` at submit — a failed pull would then need an unlink to recover.

### D-037 — one package per feature inside a component, one `common`, no sibling imports

`features/video` had grown into one Go package of ~31k lines (half tests) and ~150 files, with a
single `Service` struct whose methods were spread across every file: finding "everything about Up
Next" meant scanning a flat list. The component is now split **by feature** — `upnext`,
`upcoming`, `history`, `watches`, `ratings`, `lists`, `copies`, `jellyfin`, `details`, `people`,
`discover`, `media`, `restore`, `tmdbaccount`, `refresh` — with one `common` package for what
they share. The SPA's `src/features/video/` mirrors it (`upnext/`, `lists/`, `details/`, …, plus
`common/` for the api module and shared components).

The choices that came with it:

- **A feature imports only `common`, never a sibling.** Anything two features need moves into
  `common`; that makes import cycles impossible by construction and is checked by one `/verify`
  grep. Only `video` itself (the wiring, `video.go`) imports the features. Test files are the one
  exception: a test may call another feature's service on the same core to set up state.
- **`common.Core` replaces the god-struct.** Its fields are the dependencies and server-wide
  settings (`DB`, `TMDB`, `Now`, `WatchRegion`, …); its methods are the shared catalog operations
  (the tracking gate, snapshot refresh, asset sync). Every feature's `Service` embeds
  `*common.Core`, so code reads the same in every package and runner state (the restore, refresh,
  outbox and scan loops) lives on the feature that owns it. The TMDB outbox kick is a channel on
  the Core, because writers in four features must wake a worker that lives in one.
- **Go's export rule is the cost.** A name that crosses a package boundary must be capitalised, so
  roughly two hundred shared identifiers became exported (`RecomputeShowStatus`,
  `ResolveTrackedTarget`, `GetMovieByID`, …). Accepted: the API contract is `/api/v1`, not the Go
  packages (D-014), so an exported name promises nothing outside the module.
- **The file convention and the `/verify` greps are unchanged** inside each package; file names
  were kept where a file moved whole.
- **Shared test fixtures live in `videotest`**, a normal package imported only from `_test.go`
  files — Go does not let one package see another's `_test.go` helpers. It imports only `common`,
  so any feature's tests can use it; `common`'s own tests are the external package `common_test`.

**Rejected:** folders without packages — Go has no such thing; a directory is a package.
**Rejected:** layer sub-packages (D-014's rejection stands). **Rejected:** letting features import
each other where convenient — the first cycle forces the same move into `common` anyway, later
and under pressure.

### D-038 — every instant is stored in UTC; every day is measured in the user's zone

Storing UTC was already the rule — every `_at` column is RFC3339 UTC written by Go, every air and
release date a zone-less `YYYY-MM-DD`. What the app lacked was a **zone**: the server's "today" was
the UTC date, and the SPA rendered in whatever zone the device happened to be in. East of UTC an
episode dated today counted as unaired until 02:00 or 03:00; west of it, "mark watched on air
dates" stamped midnight UTC, which renders as the evening *before*; and the season page (device
zone) could disagree with the server's aired counts near midnight.

So each user has one zone, and it decides both sides:

- **`users.timezone`** (nullable IANA name) is the user's choice, set on the Account page through
  `PUT /auth/users/{id}/timezone` — **self only**, like D-028's consent: it moves the account's own
  day. NULL follows **`DEFAULT_TIMEZONE`** (server config, default `UTC`; an unknown name is a
  startup error). The session probe's `User` carries both `timezone` and the resolved
  **`effective_timezone`**, so no client computes the fallback.
- **Server side, `Core.ClockFor` is the one definition of a user's day**: one clock reading, the
  instant still UTC for storage, the day formatted in the user's zone. It feeds D-012's aired
  predicate (every recompute, the bulk insert, Up Next's `aired` and `only_released`, show and
  season progress), the upcoming calendar's default `from`, Discover's `newest` cap and restore.
  The hourly status sweep reads each pair's zone in the same query and evaluates each user at their
  own midnight. Catalog-only work with no user (the refresh job's open-season check) uses the
  server default.
- **The bulk air-date modes left SQL.** "Midnight on the air date in this zone" has a UTC offset that
  depends on the date (DST), which no portable SQL can compute. `episode_air` / `season_air` now
  select the eligible rows under the same predicates and insert one row each inside the same
  transaction; `now` / `fixed` stay one `INSERT … SELECT`. The stored string stays fixed-width
  RFC3339, so the feed's plain-text ordering holds.
- **Client side, `src/lib/time.js`** renders every instant in `effective_timezone`, derives the
  user's day from it, and turns the watch dialog's date + time fields into an instant in that zone
  (a time a spring-forward skips lands after the jump; one a fall-back repeats takes its first
  occurrence). Plain dates are formatted anchored at UTC midnight with a UTC formatter, so no zone
  can move them.
- The zone database is embedded (`time/tzdata`, imported by the shared `tz` package), so a name
  never depends on the host shipping zoneinfo.

Backdating still moves `watched_at` and nothing else — the zone changes where the *real* clock's
day ends, never which instant is "now".

**Rejected:** the device's zone as the default for an unset user — the server needs a zone for its
own decisions anyway, and two answers is the bug this fixes. **Rejected:** `TZ` as the variable name
— Go reads it into `time.Local`, which nothing here may consult. **Rejected:** storing local times or
offsets — ordering is a plain string comparison only while every instant is UTC.

### D-039 — statistics are a read-only projection of your own log, computed on the server in your zone

§9 used to rule statistics out with social features and sharing, because the two usually arrive
together: a stats page is where products start comparing people. Ticket Box, the author's own
Trakt client, has a Stats page whose every number comes from the device's own history, and the
same numbers were wanted here — the log, the ratings and the show states already hold everything
they need, so nothing new is stored. The rule that stays is the one §9 was protecting: **a number
is only ever about your own state**, and no account's figures are visible to, or compared with,
another's.

- **One endpoint, one object.** `GET /video/stats?period=all|YYYY` answers every section at once.
  A section the page would hide is **null** (or an empty list where it lists items), and the page
  decides visibility from the data — no `shown` flags, no per-section routes to keep in step.
- **The server computes, the page renders.** Hours, distinct titles, streaks, quartile shades,
  verdicts and tie-breaks have one definition, in Go, with tests; the SPA formats labels. The
  numbers are recomputed on every request from the log — **no stats table, no cached counters**:
  D-010's "progress is always derived" holds here as it does on the show page, and a page view
  that costs a few queries is cheaper than a counter that can drift.
- **Every bucket is measured in the user's zone** ([D-038](#d-038--every-instant-is-stored-in-utc-every-day-is-measured-in-the-users-zone)):
  a play at 23:30 belongs to the day the user experienced. The period is the user's calendar year
  (`tz.Midnight` on 1 January, half-open), and month, day, weekday and hour come from the play's
  instant read in that zone — so the bucketing happens in Go, from **one bulk read** of the
  period's plays, exactly as the bulk air-date modes left SQL: no portable SQL knows a zone's
  offset on a given day. A play at exactly local midnight is left out of the time-of-day chart,
  because those modes stamp precisely that instant.
- **Never a provider call.** "You vs the crowd" reads TMDB's vote average and count out of the
  stored snapshots and compares only titles with at least 50 votes; a title with no stored
  score is simply not compared. Every title on the page is tracked (it was watched or rated), so
  its artwork is its own.
- **What differs from Ticket Box, and why.** No check-ins exist, so every history row is a play.
  Progress is derived, not a table, so "episodes" counts distinct episodes played in the period
  in both modes, and show completion is read live from the non-dropped show states — all time
  only, since a state row has no date. Seasons are not rateable ([D-020](#d-020--seasons-are-not-rateable)),
  so the per-kind averages are movie, show and episode. Genres count movies **and** shows,
  because the catalog stores both. The default period and the offered years are the server's
  answer to the first request, not a client guess.
- **The one write on the page is a rating**, through the existing `PUT /video/ratings` and its
  TMDB outbox ([D-029](#d-029--ratings-push-one-way-to-the-users-own-tmdb-account)); the page
  then re-reads itself, so which movie is "waiting" next stays the server's to say.

**Rejected:** a stats cache or materialised counters — D-010's drift argument, and the log is small
enough to fold on every view. **Rejected:** one endpoint per section — twelve round trips that
must agree on the period. **Rejected:** bucketing in SQL — no portable zone arithmetic, and
`strftime` is exactly the dialect trap the schema rules forbid. **Rejected:** bucketing in the
client — two definitions of "today", the bug D-038 removed. **Rejected:** any comparison across
accounts, leaderboards or shared stats — §9's rule stands; the carve-out is D-028's, and it is
not widened here.

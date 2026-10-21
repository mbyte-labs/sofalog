---
name: providers-and-assets
description: Use when touching sofalog's external metadata providers or its local artwork — TMDB or Steam Web API calls, the Jellyfin library scan, item search, add-item enrichment, the throttled resumable download queue, data/cache promotion into data/assets, provider image size variants, the on-disk asset directory layout, the periodic TMDB air-date refresh, user-triggered Steam sync, export archives, or deleting unreferenced items and their files.
---

# Providers and local assets

This is where sofalog's core promise lives: **the library keeps working when a provider does not.**
Discovery uses providers; rendering never does.

## The invariant, stated once

Provider calls are allowed in exactly eight places:

1. **Search** — user typed a query. Results are never written to the library.
2. **Add-item enrichment** — an item just became tracked; fetch metadata + enqueue images.
3. **User-triggered Steam sync** — manual only, never scheduled (D-006).
4. **The one scheduler** — the TMDB refresh: two jobs on one hourly loop (D-006, D-025), which is
   also what feeds the upcoming-episodes calendar.
5. **The movie, show, season and person details views** (D-015, D-027) — serve an untracked
   `tmdb_id` **live from TMDB, storing nothing** (viewing never creates catalog rows; tracking does, via call site 2),
   and refresh a catalog row's snapshot older than 2 days. A failed refresh serves the stale
   snapshot; a successful one reconciles the movie's stored artwork (replaced files deleted, new
   ones enqueued). Only an untracked view can 503. Seasons follow the same rule — an untracked
   show's season is one `/tv/{id}?append_to_response=season/{n}` call, served live and discarded.
   The one write a view may cause is a **tracked** show's missing season (TMDB added it since the
   show was written): the season page and the show refresh both materialise and link it with its
   episodes, and enqueue its artwork. The **person** view has no catalog half at all: always live,
   always discarded, always a 503 on an outage; its filmography's library marks are a local join by
   `tmdb_id` (D-027).
6. **The TMDB account link and its rating outbox** (D-029) — the one WRITE to a provider: a user's
   own Link/Unlink, and a background worker draining a durable queue filled inside the local
   rating's transaction.
7. **The Jellyfin library scan** (D-033) — a background read of the household's own Jellyfin
   server, on a schedule and on "Scan now", that writes the shared `jellyfin_library` table and
   nothing else. Not a metadata source: it never touches catalog data, never calls TMDB, never adds
   an item, reads presence and never playback.
8. **The trending and discover lists** (D-034) — `GET /video/trending`, `GET /video/discover` and
   `GET /video/discover/options`, discovery served live from TMDB on every view like the person
   page: no table, no snapshot, a 503 whenever TMDB is down. Each entry is marked against the
   library by a local join on `tmdb_id` (`LibraryMovieFlags` / `LibraryShowFlags`), never a second
   call. The Discover streaming filter is the ONE request that sends `TMDB_WATCH_REGION` to TMDB.

Anything else — a list view, a mark-watched call — must be answerable from SQLite and disk alone.
If a read path over existing local data can return 503 from a provider, the design is wrong.

## TMDB (movies & TV)

- Auth: **two interchangeable credentials**, `TMDB_API_KEY_V3` (the "API Key", sent as an `api_key`
  query parameter) and `TMDB_API_KEY_V4` (the "API Read Access Token", a JWT sent as
  `Authorization: Bearer`). TMDB documents the v4 token as valid for **v3** endpoints, and every
  endpoint below is a v3 endpoint — **we never call a `/4/` endpoint**, because v4 exists for
  user-authenticated TMDB lists and watchlists, which sofalog keeps local. So one credential is
  enough and neither unlocks more than the other; the v4 token wins when both are set. Do not add a
  v4 code path expecting new capability.
- **Required to add** movies/shows; not required to browse what is already tracked. No usable
  credential → `ErrProviderUnavailable` (503) from search, add, the live details views (an
  untracked item's view, or any person view) and the trending / popular lists only.
- Endpoints used: `/search/movie`, `/search/tv`, `/movie/{id}`, `/tv/{id}`,
  `/tv/{id}/season/{n}` (episode list + stills), `/person/{id}` (with `combined_credits` and
  `external_ids` appended — the person page, D-027), `/trending/{all|movie|tv}/{day|week}`,
  `/discover/{movie|tv}`, `/genre/{movie|tv}/list`, `/configuration/languages` and
  `/watch/providers/{movie|tv}` (the Trending page and its Discover filters, D-034), and
  `/configuration` for the image base URL.
- **Fetch `/configuration` once at startup and cache it.** Do not hardcode
  `https://image.tmdb.org/t/p/` — TMDB documents it as changeable.
- Image size variants: **`w185` = thumb, `w500` = full** for posters and stills; backdrops use `w300`
  / `w1280`; profiles (a person's portrait) use `w185` / `h632`. All are provider variants — no
  resizing (see CLAUDE.md). An item's cast photos store the single `w185` size in `data/assets`;
  the person page's two portrait variants live in `data/cache` only (D-027).
- Adding a show fetches every season, which is where the queue earns its keep: a 200-episode show
  enqueues several hundred image jobs.
- Attribution is mandatory and must be visible in the UI: "This product uses the TMDB API but is not
  endorsed or certified by TMDB" — on Settings → Data Sources and nowhere else. Provider disclaimers
  are never repeated on individual pages; those link to Data Sources instead.
- The scheduled refresh (the only scheduler; `server/features/video/refresh/service_refresh.go`). One
  hourly tick, three parts, in order:
  1. **Two selection passes**, active then archive, each over items whose `fetched_at` predates now
     minus that job's interval (`TMDB_REFRESH_INTERVAL` 7d / `TMDB_ARCHIVE_REFRESH_INTERVAL` 90d;
     `0` disables). Active = anything on *any* watchlist + followed shows still in production;
     archive = watched movies + followed `Ended`/`Canceled` shows, none watchlisted (queries in the
     `sofalog-schema` skill). Selection is across all users — one user dropping a show must not
     starve another's calendar. There is no last-run state: the row timestamp is the schedule.
  2. **Update the catalog** — a movie through `RefreshMovie` + the asset reconcile; a show through
     `RefreshShow` (root + seasons TMDB added since) and then its **open** seasons only (highest,
     or non-special with an undated/future episode) via `RefreshSeason`, which rewrites
     `episodes.air_date` and inserts newly announced episodes. `upsertEpisodes` never deletes an
     episode: `user_watch_history.target_id` has **no foreign key**, so that rule is the only thing
     standing between a catalog refresh and orphaned watch history. A fetch that fails keeps the
     snapshot; three consecutive outages trip a breaker for the rest of the tick; a 404 keeps the
     row and silences the item for one interval.
  3. **Recompute statuses** — the sweep runs `RecomputeShowStatus` for every non-dropped (user,
     show) pair, so shows that gained a newly aired episode flip `watched` → `watching`. Local, runs
     whatever the intervals say, and bounds status drift to one hour. Not a second scheduled job
     (D-006); it rides on this one.

## Steam (games)

- Auth: one **server-wide** `STEAM_API_KEY` (admin config, server-side only, never in a response or
  bundle) plus a **per-user** `users.steam_id64`.
- Endpoints: `GetOwnedGames` (needs `include_appinfo=1&include_played_free_games=1`),
  `GetPlayerAchievements`, `GetSchemaForGame` (achievement names + icon URLs), and `appdetails` for
  store descriptions.
- **`appdetails` is undocumented and rate-limited** — treat it as best-effort: on failure, keep the
  game with `GetOwnedGames` data and no description. Never fail a sync because of it.
- **The user's Steam profile and game details must be public** or `GetOwnedGames` returns nothing.
  Surface that as a clear message, not an empty list.
- Artwork (Steam CDN, `steamcdn-a.akamaihd.net/steam/apps/{appid}/…`):
  `capsule_231x87.jpg` = thumb, `library_600x900.jpg` = full.
- Achievement icons: **single size** (D-005). `icon` and `icongray` are earned/unearned **state**
  variants, not sizes; store both only if the UI distinguishes them.

## Jellyfin (the household's own server — a library to compare against, D-033)

- Auth: one server-wide `JELLYFIN_URL` + `JELLYFIN_API_KEY` (an API key from Jellyfin's dashboard;
  both or neither). The key rides `Authorization: MediaBrowser Token="…"` — a header, never a query
  parameter — and `redact` strips it from every error string. A 401/403 is
  `provider.ErrJellyfinAuth`, which deliberately does not wrap `ErrProviderUnavailable`.
- Endpoints: `GET /System/Info/Public` (the probe, no auth), and `GET /Items` paged with
  `startIndex`/`limit` for `includeItemTypes=Movie` and `Series` (`hasTmdbId=true`,
  `fields=ProviderIds` → `ProviderIds.Tmdb`) and `Episode` (`isMissing=false`; `LocationType`
  `Virtual` placeholders are dropped, as is an episode with no `ParentIndexNumber`). No `userId`:
  an API key needs none.
- **Nothing it returns is stored as catalog data.** The scan (`features/video/jellyfin/service_jellyfin.go`)
  matches against the catalog in Go and rewrites `jellyfin_library` in one transaction. No images,
  no metadata, no queue involvement — Jellyfin is not a provider in the D-015 sense, only an
  outbound client that lives here because `service*.go` may not import `net/http`.

### Sync is user-driven and never surprising

The three rules, all from the requirements:

1. **Manual trigger only.** No scheduler, no sync-on-login, no background refresh.
2. **Selection step.** The sync returns candidates; the user picks which games are imported.
3. **Per-game overwrite choice** for games already in the library. If `user_game_states.hours_manual = 1`
   or `user_achievements.is_manual = 1`, fetched values must **never** overwrite silently — the user
   decides per game.

## The download queue

Backed by `asset_jobs` (DDL in the `sofalog-schema` skill). Design properties, and why each one is
required:

- **Durable, not in-memory.** Rows in the table mean an interrupted 300-image show import resumes
  after a restart instead of leaving gaps. "Resumable" in the requirements means this.
- **Idempotent** via `dest_path UNIQUE` — one job per target file (galleries hold many files per
  kind+size, so the destination is the identity). Enqueueing twice is a no-op, so retry logic and
  re-adds cannot duplicate work; re-enqueueing a finished job resets it, because callers only
  enqueue files that are missing on disk or whose source moved.
- **Per-provider token bucket**, not one global limiter — TMDB's and Steam's budgets differ and must
  not throttle each other.
- **Bounded retries with exponential backoff** via `attempts` + `next_retry_at`; `failed` after the
  cap, with `last_error` kept for diagnosis. A permanently 404ing image must not spin forever.
- **Background worker**, started in `main`, draining `pending` jobs. It never initiates provider
  traffic on its own — it only drains what user actions enqueued (which is why it is a worker, not a
  scheduler).
- Write to a temp file then `os.Rename` into place, so a crash never leaves a truncated image that
  looks valid.
- **Never call a provider from a handler, and never in a bare loop.** Handlers enqueue and return.

## On-disk layout

Relative to `DATA_DIR`. The DB stores these paths **relative**, never absolute.

```
data/
├── sofalog.db
├── assets/                                    permanent, keyed by INTERNAL db id
│   ├── video/
│   │   ├── movies/<movie_id>/poster.jpg   poster.thumb.jpg
│   │   │   │                 backdrop.jpg backdrop.thumb.jpg
│   │   │   ├── gallery/backdrops/<tmdb_file>.jpg  <tmdb_file>.thumb.jpg
│   │   │   ├── gallery/posters/<tmdb_file>.jpg    <tmdb_file>.thumb.jpg
│   │   │   ├── cast/<tmdb_file>.jpg               (single size, w185)
│   │   │   └── videos/<youtube_key>.jpg           (single size, D-017)
│   │   └── shows/<show_id>/  poster.jpg   poster.thumb.jpg
│   │                         backdrop.jpg backdrop.thumb.jpg
│   │       ├── gallery/backdrops/<tmdb_file>.jpg  <tmdb_file>.thumb.jpg
│   │       ├── gallery/posters/<tmdb_file>.jpg    <tmdb_file>.thumb.jpg
│   │       ├── cast/<tmdb_file>.jpg               (single size, w185)
│   │       ├── videos/<youtube_key>.jpg           (single size, D-017)
│   │       ├── seasons/<season_number>/poster.jpg  poster.thumb.jpg
│   │       └── episodes/<season>-<episode>/still.jpg still.thumb.jpg
│   └── games/<game_id>/cover.jpg  cover.thumb.jpg
│       └── achievements/<api_name>/icon.jpg
└── cache/                                     temporary, keyed by PROVIDER id
    └── <provider>/<provider_id>/…
```

Two properties this layout exists for — preserve them:

- **Deleting an item is removing one directory** (one `os.RemoveAll`).
- **Exporting is copying directories** — no globbing, no path parsing.

`server/assets/paths.go` holds one builder per slot (`MoviePoster`, `ShowSeasonPoster`,
`ShowEpisodeStill`, …). **Nothing outside that file may concatenate an asset path.** The
id-vs-number split is enforced by the parameter types: an id is `int64` and a positional number is
`int`, so passing `seasons.id` where `season_number` belongs does not compile.

**A show's whole subtree is ONE asset unit.** Every job under `ShowDir(id)` — its own artwork, every
season poster, every episode still — is filed `media_type='show', item_id=<shows.id>`, and
`asset_jobs`' `season` and `episode` media types stay unused for video. This is not a shortcut: the
reconciler deletes anything on disk that its desired set does not name, so a per-season diff against
a whole-subtree listing would delete the other seasons, and a season poster filed under its own id
would be invisible to the diff and orphaned forever. It is also what makes the D-008 sweep one
`RemoveAll` plus one `DELETE`.

`Tree.ListItemFiles`/`RemoveItemFile` take a directory, validated by `itemDir` to be exactly
`assets/<component>/<type>/<numeric id>` — four plain elements. That guard is what keeps a caller
from walking or deleting across a whole media type, or into part of one item.

Note which levels are *not* database ids: seasons and episodes use their positional numbers,
achievements use Steam's `api_name`. A season is a numbered folder on disk but `seasons.id` as a
rating target — the two are not interchangeable.

Item folders are keyed by internal id, not provider id, so adding IGDB later strands nothing (D-002).

## Cache vs assets — the hard boundary

`data/cache/` and `data/assets/` are separate trees on purpose: cache pruning must be structurally
incapable of touching library data.

- Cache keys are **provider ids**, because a search result has no internal id yet.
- Cache entries have a **TTL and a maximum total size**; a prune loop enforces both.
- The prune function takes the cache root, resolves it with `filepath.Abs`, and **asserts every path
  it is about to delete is prefixed by that root**. No exceptions, no shared "delete these files"
  helper with `assets/`.
- **Promotion, not re-download**: when a tracked item's job is processed, the worker checks
  `data/cache` first and copies a hit into `assets/…/<internal_id>/` instead of calling the
  provider (copy, not move — a concurrent untracked view may still be served the cache entry, and
  the cache's own TTL prune collects it later). Only a cache miss actually downloads.

## Deletion (D-008)

Never a hard delete on untrack:

1. Last reference dropped → insert `pending_deletions` (see the reference-count queries in the
   `sofalog-schema` skill). **A rating alone counts as a reference, and so does a watchlist row.** A
   show is referenced six ways — state, watchlist, any episode watch, or a rating on the show, a
   season or an episode.
2. UI confirms when a user's action would orphan an item.
3. Deferred sweep, after a grace period, **re-checks** the reference count, then deletes and does one
   `os.RemoveAll` of the item directory. Because `user_watch_history`, `user_video_ratings` and
   `user_video_watchlist` are polymorphic and therefore have **no FK**, their rows must be deleted
   explicitly in the same transaction — and **before** the `shows` delete, since those statements read
   `seasons` and `episodes` which the show delete cascades away. The exact ordering is in the schema
   skill.
4. Re-tracking before the sweep just deletes the `pending_deletions` row. Nothing re-downloads.

The sweep also runs the **orphan check**: watch, rating and watchlist rows whose `target_id` no longer
resolves to a live catalog row. With no foreign keys on those columns this is the only thing that
collects them, and it belongs here rather than in a new scheduled job (D-006).

## Export archive

A zip containing JSON per media type **plus the asset directories**, so the archive reconstructs the
library on its own. Per-user collections must include `user_watch_history` (many rows per item — the
rewatch log) and `user_video_watchlist`, not just the states and ratings.

It must include the **internal id → provider id + title mapping**. Without it the exported tree is
just numbered directories and the "reconstructs on its own" requirement fails. Importing an archive
is explicitly out of scope — restore is done by restoring the data volume.

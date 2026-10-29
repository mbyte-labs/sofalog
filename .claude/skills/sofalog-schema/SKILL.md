---
name: sofalog-schema
description: Use when designing or changing the sofalog database schema, writing or reviewing a migration in server/migrations/, or writing any SQL query in a feature's store*.go files under server/ — covers the separate per-media-type catalog tables (movies, shows, seasons, episodes, games), the user_-prefixed private state tables, polymorphic target_type + target_id columns, user_watch_history and rewatches, the system-maintained show status state machine, the two ratings tables, the watchlist axis, derived show and season progress, reference counting for cleanup, asset job rows, and SQLite/Postgres-portable SQL.
---

# sofalog database schema

The authoritative schema. The decisions behind it are D-001, D-007 and D-009…D-013 in
[`SPEC.md` Appendix A](../../../SPEC.md#appendix-a--why-these-decisions).

## The two rules everything else follows from

1. **Catalog tables** hold one canonical row per real-world thing, shared by every user. They never
   contain `user_id`, a status, a rating, or a personal date. Bare names: `movies`, `shows`,
   `seasons`, `episodes`, `games`, `game_achievements`.
2. **Per-user tables** hold all private state. They always have `user_id`, and every query against
   them filters on the authenticated user. They are all prefixed **`user_`** — the prefix is how you
   tell the two halves apart at a glance.

There is **no** `items` table and **no** `media_type` discriminator column. If you find yourself
adding one, re-read D-001.

## Conventions

- Ids: `INTEGER PRIMARY KEY` (SQLite rowid alias; maps to `BIGSERIAL` / `GENERATED AS IDENTITY` in
  the Postgres dialect of the same migration). Never rely on `AUTOINCREMENT`. Get a generated key
  back with `RETURNING id`, which both engines support — never `last_insert_rowid()`.
  - Ids are **only unique within their own table**. Movie 5 and episode 5 both exist. That is what
    makes the `target_type` predicate below a correctness requirement rather than a nicety (D-013).
  - `tmdb_id`, `steam_appid`, `season_number`, `episode_number` are provider or positional data, not
    our keys — never use them as a foreign key or a rating target.
- Timestamps: `TEXT` holding ISO-8601 UTC (`2026-08-08T14:03:00Z`), and the column is named `_at`.
  Dates without a time (finish dates, air dates, release dates): `TEXT` `YYYY-MM-DD`, named `_on`
  or `*_date`. Generate them in Go, never with a SQL function — that is the main dialect trap.
  Fixed-width UTC RFC3339 sorts chronologically as plain text, which is why `ORDER BY watched_at`
  and `watched_at < ?` need no date function on either engine. A watch is an instant
  (`watched_at`), not a day: a same-day rewatch has to be orderable, and clients show the time.
- Booleans: `INTEGER` 0/1.
- Enumerations: `TEXT` + a `CHECK` constraint listing the exact allowed values. Readable in a raw
  `sqlite3` session, and portable.
- Asset paths: `TEXT`, **relative** to the data directory (e.g.
  `assets/video/movies/12/poster.jpg`). Never absolute, never a provider URL.
- Foreign keys always declare `ON DELETE CASCADE` toward the owning row — except polymorphic
  `target_id` columns, which cannot have one (see the warning below).

### The polymorphic columns, and the rule that keeps them safe

Four per-user tables plus three infrastructure tables carry a `target_type` + `target_id` pair:
`user_watch_history`, `user_video_ratings`, `user_video_watchlist`, `user_video_copies`,
`asset_jobs`, `pending_deletions`, `jellyfin_library`.

> **Every join or predicate through a polymorphic id MUST carry its `target_type` in the same
> `EXISTS` / `WHERE`.** Ids are unique only within their own table, so movie 5 and episode 5 both
> exist. A predicate that matches on `target_id` alone silently returns **the wrong row** — a watch
> on movie 5 makes episode 5 look watched, and nothing about the result looks wrong. Seed a movie and
> an episode with the same id in the store tests and assert they stay separate.

> **A polymorphic column cannot have a foreign key, whatever the id type.** Deleting a catalog row
> does **not** cascade to these tables. Every delete path must remove them explicitly in the same
> transaction, and the D-008 sweep runs an orphan check. This is the one integrity guarantee the
> database will not give you.

## Users, sessions, auth (migration 1 — includes the M5 shapes, per D-007)

```sql
CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    -- The login identifier, and what an OIDC sign-in matches on (D-019).
    email         TEXT    NOT NULL UNIQUE,
    -- Optional human label; deliberately NOT unique (D-019).
    display_name  TEXT,
    -- NULL for OIDC-only users. Nullable from migration 1 on purpose (D-007).
    password_hash TEXT,
    is_admin      INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT    NOT NULL,
    -- Per-user Steam identity; the API key itself is server-wide config, never per user.
    steam_id64    TEXT
);

CREATE TABLE sessions (
    token      TEXT    PRIMARY KEY,          -- random, opaque; the cookie value
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT    NOT NULL,
    expires_at TEXT    NOT NULL,
    user_agent TEXT,
    ip         TEXT                          -- resolved via trusted-proxy rules, see CLAUDE.md
);
CREATE INDEX idx_sessions_user ON sessions(user_id);

CREATE TABLE oauth_identities (
    id               INTEGER PRIMARY KEY,
    user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider         TEXT    NOT NULL,
    provider_user_id TEXT    NOT NULL,
    created_at       TEXT    NOT NULL,
    UNIQUE (provider, provider_user_id)
);

CREATE TABLE api_tokens (
    id           INTEGER PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT    NOT NULL,
    token_hash   TEXT    NOT NULL UNIQUE,    -- store a hash, never the token
    created_at   TEXT    NOT NULL,
    last_used_at TEXT,
    revoked_at   TEXT
);
```

## Catalog: video

```sql
CREATE TABLE movies (
    id             INTEGER PRIMARY KEY,
    tmdb_id        INTEGER NOT NULL UNIQUE,
    title          TEXT    NOT NULL,
    original_title TEXT,
    release_date   TEXT,                     -- YYYY-MM-DD
    runtime_min    INTEGER,
    overview       TEXT,
    tagline        TEXT,
    genres         TEXT,                     -- JSON array; catalog snapshot, not queried relationally
    poster_path    TEXT,
    backdrop_path  TEXT,
    metadata_json  TEXT    NOT NULL,         -- full provider snapshot (no provider call to render)
    fetched_at     TEXT    NOT NULL
);

CREATE TABLE shows (
    id             INTEGER PRIMARY KEY,
    tmdb_id        INTEGER NOT NULL UNIQUE,
    title          TEXT    NOT NULL,
    first_air_date TEXT,
    status         TEXT,                     -- PROVIDER status (Returning Series/Ended) — NOT user state
    overview       TEXT,
    genres         TEXT,
    poster_path    TEXT,
    backdrop_path  TEXT,
    metadata_json  TEXT    NOT NULL,
    fetched_at     TEXT    NOT NULL
);

-- Abridged: the live table also carries show_title, air_date, episode_count,
-- metadata_json and fetched_at, because a season row is a SNAPSHOT CACHE of the
-- season document. See 0001_init.sql for the full DDL.
--
-- show_id is NOT NULL; (show_tmdb_id, season_number) stays the natural key
-- because that is how the details views address a season. A season row exists
-- if and only if its show is tracked — only tracking writes one (D-015), so the
-- link is always known at insert time. Relaxing this to nullable brings
-- materialise-on-view back through the side door.
CREATE TABLE seasons (
    id            INTEGER PRIMARY KEY,
    show_id       INTEGER NOT NULL REFERENCES shows(id) ON DELETE CASCADE,
    show_tmdb_id  INTEGER NOT NULL,
    season_number INTEGER NOT NULL,          -- positional; also the asset-path component
    name          TEXT    NOT NULL,
    overview      TEXT,
    poster_path   TEXT,
    UNIQUE (show_tmdb_id, season_number)
);
CREATE INDEX idx_seasons_show ON seasons(show_id, season_number);

-- No tmdb_id: nothing addresses an episode by one, and a second UNIQUE would
-- turn a TMDB renumber from a silent re-point into a failing refresh upsert.
-- No metadata_json either: an episode's score, vote count and episode_type are
-- read out of its SEASON's snapshot.
CREATE TABLE episodes (
    id             INTEGER PRIMARY KEY,
    show_id        INTEGER NOT NULL REFERENCES shows(id) ON DELETE CASCADE,
    season_number  INTEGER NOT NULL,
    episode_number INTEGER NOT NULL,
    title          TEXT,
    air_date       TEXT,                     -- refreshed by the one scheduled TMDB job (D-006)
    runtime_min    INTEGER,
    overview       TEXT,
    still_path     TEXT,
    UNIQUE (show_id, season_number, episode_number)
);
-- Ordering within a show is served by the UNIQUE's own index on both engines,
-- so there is deliberately NO second index on those three columns.
CREATE INDEX idx_episodes_air_date ON episodes(air_date);
```

`shows.status` is the **provider's** production status. User watch status is
`user_show_states.status`. Two different things with the same word — do not merge them.

**`seasons.id` is what a rating targets. `season_number` is what an asset path uses.** They
are not interchangeable and this is the most likely place to mix them up.

## Catalog: games

```sql
CREATE TABLE games (
    id             INTEGER PRIMARY KEY,
    steam_appid    INTEGER UNIQUE,           -- NULL for a manually added game
    name           TEXT    NOT NULL,
    platform       TEXT,
    store          TEXT,                     -- store / launcher
    description    TEXT,
    cover_path     TEXT,
    is_manual      INTEGER NOT NULL DEFAULT 0,
    metadata_json  TEXT,
    fetched_at     TEXT
);

CREATE TABLE game_achievements (
    id             INTEGER PRIMARY KEY,
    game_id        INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    api_name       TEXT    NOT NULL,         -- Steam apiname; the stable provider key
    display_name   TEXT,
    description    TEXT,
    icon_path      TEXT,                     -- single size (D-005)
    icon_gray_path TEXT,                     -- STATE variant (unearned), not a size variant
    UNIQUE (game_id, api_name)
);
```

## Per-user state: video

```sql
-- The watch LOG. Many rows per (user, target): a rewatch is a new row.
-- This is what makes rewatch history first-class for video (D-009).
CREATE TABLE user_watch_history (
    id          INTEGER PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_type TEXT    NOT NULL CHECK (target_type IN ('movie','episode')),
    target_id   INTEGER NOT NULL,            -- no FK: polymorphic. See the warning above.
    watched_at  TEXT    NOT NULL,            -- RFC3339 UTC instant, generated in Go
    -- PROVENANCE ONLY. No query may branch on this value (D-010). 'checkin' and
    -- 'scrobble' are recorded now; the features that produce them are deferred.
    watch_type  TEXT    NOT NULL DEFAULT 'watch'
                        CHECK (watch_type IN ('watch','checkin','scrobble'))
    -- Deliberately NO created_at: watched_at is this table's only timestamp.
    -- Ordering ties break on id, and id (monotonic on insert) already marks a
    -- bulk write as a contiguous run.
);
-- "has this user watched target X?" — the hot path for progress and next-up
CREATE INDEX idx_user_watch_history_target ON user_watch_history(user_id, target_type, target_id);
-- history-by-date feed
CREATE INDEX idx_user_watch_history_feed   ON user_watch_history(user_id, watched_at, id);
-- "does ANY user reference X?" — not user-scoped, so it needs its own index
CREATE INDEX idx_user_watch_history_ref    ON user_watch_history(target_type, target_id);

CREATE TABLE user_movie_states (
    user_id    INTEGER NOT NULL REFERENCES users(id)  ON DELETE CASCADE,
    movie_id   INTEGER NOT NULL REFERENCES movies(id) ON DELETE CASCADE,
    -- SYSTEM-MAINTAINED (D-010). Written only by server/features/video/common/status.go.
    -- 'watching' is reserved for check-ins (deferred) — present in the CHECK from
    -- migration 1 because SQLite cannot alter one later; same reasoning as D-007.
    -- The API rejects it with ErrValidation until check-ins ship.
    -- NO watched_at column: watch times live in user_watch_history.
    status     TEXT    NOT NULL CHECK (status IN ('watching','watched','dropped')),
    updated_at TEXT    NOT NULL,
    PRIMARY KEY (user_id, movie_id)
);
CREATE INDEX idx_user_movie_states_status ON user_movie_states(user_id, status);
CREATE INDEX idx_user_movie_states_movie  ON user_movie_states(movie_id);   -- reference count

CREATE TABLE user_show_states (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    show_id    INTEGER NOT NULL REFERENCES shows(id) ON DELETE CASCADE,
    -- 'watching'/'watched' are derived from user_watch_history and rewritten by
    -- RecomputeShowStatus. 'dropped' is user intent and is NEVER recomputed.
    status     TEXT    NOT NULL CHECK (status IN ('watching','watched','dropped')),
    updated_at TEXT    NOT NULL,
    PRIMARY KEY (user_id, show_id)
    -- NO progress counter, NO percentage, NO last_watched_episode. Still derived.
);
CREATE INDEX idx_user_show_states_status ON user_show_states(user_id, status); -- home view, calendar
CREATE INDEX idx_user_show_states_show   ON user_show_states(show_id);         -- refresh + ref count

-- An INDEPENDENT axis (D-011), never a status value.
-- Movies and shows only: never seasons, never episodes.
-- 'watchlist' is therefore NOT a value in any status CHECK.
CREATE TABLE user_video_watchlist (
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_type TEXT    NOT NULL CHECK (target_type IN ('movie','show')),
    target_id   INTEGER NOT NULL,            -- no FK: polymorphic
    added_at    TEXT    NOT NULL,
    PRIMARY KEY (user_id, target_type, target_id)
);
CREATE INDEX idx_user_video_watchlist_recent ON user_video_watchlist(user_id, added_at);
CREATE INDEX idx_user_video_watchlist_ref    ON user_video_watchlist(target_type, target_id);
```

A watchlist row and a `user_show_states` row with `status='watching'` can coexist — that is the
feature, not a bug to normalise away.

```sql
-- Owned copies: ANOTHER independent axis (D-021), modelled on the watchlist.
-- Writable targets are movie and season only — a show's copies live PER-SEASON,
-- and 'show' is the API's bulk fan-out scope (one row per non-special season),
-- never a stored target. 'show'/'episode' stay in the CHECK (the D-020
-- pattern: SQLite cannot alter one later).
CREATE TABLE user_video_copies (
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_type TEXT    NOT NULL CHECK (target_type IN ('movie','show','season','episode')),
    target_id   INTEGER NOT NULL,            -- no FK: polymorphic
    kind        TEXT    NOT NULL CHECK (kind IN ('jellyfin','dvd','bluray','uhd','physical','digital')),
    added_at    TEXT    NOT NULL,
    PRIMARY KEY (user_id, target_type, target_id, kind)
);
CREATE INDEX idx_user_video_copies_ref ON user_video_copies(target_type, target_id);

-- What the household's Jellyfin server holds (D-033). SHARED — a fact about
-- the server, identical for every account — so no user_ prefix: the
-- streaming listing's posture, not the copies shelf. Rewritten whole by every
-- scan in one transaction; read into the copies list as the 'jellyfin' kind
-- while a server is configured (when the copy routes refuse that kind). NOT a
-- D-008 reference. A 'season' row means "at least one episode file is there";
-- episodes counts the distinct files, so a page can tell complete from partial.
CREATE TABLE jellyfin_library (
    target_type TEXT    NOT NULL CHECK (target_type IN ('movie','season')),
    target_id   INTEGER NOT NULL,            -- no FK: polymorphic
    jellyfin_id TEXT    NOT NULL,
    episodes    INTEGER NOT NULL DEFAULT 0,  -- 0 on movie rows
    seen_at     TEXT    NOT NULL,
    PRIMARY KEY (target_type, target_id)
);
```

## Per-user state: games

```sql
CREATE TABLE user_game_states (
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    game_id      INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    -- Hours IS the progress metric. 0 = unplayed. There are NO play statuses.
    hours_played REAL    NOT NULL DEFAULT 0 CHECK (hours_played >= 0),
    hours_manual INTEGER NOT NULL DEFAULT 0, -- 1 = user-entered; a Steam sync must not clobber it
    updated_at   TEXT    NOT NULL,
    PRIMARY KEY (user_id, game_id)
);

CREATE TABLE user_achievements (
    user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    achievement_id INTEGER NOT NULL REFERENCES game_achievements(id) ON DELETE CASCADE,
    unlocked_at    TEXT,                     -- NULL = known-but-not-unlocked
    is_manual      INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, achievement_id)
);
```

## Ratings — three tables, 1–10, one per user per item

```sql
CREATE TABLE user_video_ratings (
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- 'season' is present but UNWRITABLE: every write path refuses a season
    -- target (D-020). The CHECK value stays because SQLite cannot alter a CHECK
    -- later, so narrowing it here would make that decision irreversible once the
    -- baseline freezes. If it ever reverses, a season rating targets seasons.id
    -- (the surrogate PK), NOT season_number.
    target_type TEXT    NOT NULL CHECK (target_type IN ('movie','show','season','episode')),
    target_id   INTEGER NOT NULL,            -- no FK: polymorphic by design
    rating      INTEGER NOT NULL CHECK (rating BETWEEN 1 AND 10),
    updated_at  TEXT    NOT NULL,
    PRIMARY KEY (user_id, target_type, target_id)
);
CREATE INDEX idx_user_video_ratings_ref ON user_video_ratings(target_type, target_id);

CREATE TABLE user_game_ratings (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    game_id    INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    rating     INTEGER NOT NULL CHECK (rating BETWEEN 1 AND 10),
    updated_at TEXT    NOT NULL,
    PRIMARY KEY (user_id, game_id)
);
```

Re-rating is an upsert that **overwrites**. There is no rating history — deliberately unlike watches:
*a watch is an event that happened on a date, possibly many times; a rating is one current opinion*
(D-009).

## Asset pipeline and deletion

```sql
CREATE TABLE asset_jobs (
    id           INTEGER PRIMARY KEY,
    -- youtube is a file source only, never a metadata provider (D-017).
    provider     TEXT    NOT NULL CHECK (provider IN ('tmdb','steam','youtube')),
    media_type   TEXT    NOT NULL CHECK (media_type IN ('movie','show','season','episode','game','achievement')),
    item_id      INTEGER NOT NULL,           -- internal id of the owning row
    kind         TEXT    NOT NULL,           -- poster | backdrop | gallery_backdrop | gallery_poster | cast | video_thumb | still | cover | icon
    size         TEXT    NOT NULL,           -- thumb | full | single (cast photos, video thumbs)
    -- Provider-relative identifier ("w500/abc.jpg", a YouTube key), never an
    -- absolute URL — the image host stays runtime-resolved.
    source_url   TEXT    NOT NULL,
    -- Idempotency: one job per target file, so enqueueing the same image twice
    -- is a no-op — which is what makes the queue resumable after a crash
    -- mid-import. (Galleries hold many files per kind+size, so the identity is
    -- the destination, not the kind.)
    dest_path    TEXT    NOT NULL UNIQUE,    -- relative, under assets/
    status       TEXT    NOT NULL CHECK (status IN ('pending','running','done','failed')),
    attempts     INTEGER NOT NULL DEFAULT 0,
    last_error   TEXT,
    next_retry_at TEXT,
    created_at   TEXT    NOT NULL
);
CREATE INDEX idx_asset_jobs_claim ON asset_jobs(status, provider, next_retry_at);
CREATE INDEX idx_asset_jobs_item  ON asset_jobs(media_type, item_id); -- per-item diff/cancel

CREATE TABLE pending_deletions (
    id         INTEGER PRIMARY KEY,
    media_type TEXT    NOT NULL CHECK (media_type IN ('movie','show','game')),
    item_id    INTEGER NOT NULL,
    queued_at  TEXT    NOT NULL,
    UNIQUE (media_type, item_id)
);
```

Seasons and episodes cascade from their show and are never deleted independently, which is why
`pending_deletions.media_type` has only four values.

## The show status state machine (D-010, D-012)

### The eligibility predicate — define once, reuse verbatim

```sql
e.season_number > 0
AND e.air_date IS NOT NULL AND e.air_date <= :today
```

```
complete(user, show) := eligible_total > 0 AND eligible_unwatched = 0
```

Keep this as one `const` fragment in the store package. The next-unwatched query and the recompute
must use it **character for character**, because of:

> **INV-A** — for any `user_show_states` row with status ∈ {`watching`,`watched`}:
> `status = 'watched'` ⟺ the next-unwatched query returns no row for that (user, show), given
> `eligible_total > 0`.

If they drift you get the worst UI state available: a show in "Currently watching" with no next
episode, or a "watched" show that still offers one.

Three deliberate choices inside that predicate:

- **Unaired episodes do not count.** Otherwise a currently-airing show can never reach `watched`, and
  the `watched`→`watching` transition could never fire. Note it is `air_date IS NOT NULL AND <=
  :today`, **never** `IS NULL OR <= :today` — treating an unknown date as aired permanently blocks
  completion for any show with an announced-but-undated episode.
- **Specials (season 0) do not count** toward completion. TMDB adds season-0 entries retroactively, so
  if they counted, a third-party catalog edit would silently un-complete a user's show. Specials stay
  fully markable, rateable, and present in history — they just do not gate completion.
- **`eligible_total > 0`** guards the vacuous case: without it, a freshly added show whose episodes
  have not been fetched yet is "complete" and flashes `watched`.

### States and transitions

`watching | watched | dropped`, plus *absent* (not tracked).

| From | Trigger | To |
|---|---|---|
| absent | first watch, or explicit "track" | `watching` — row created in the **same tx** |
| absent | watchlisting, or rating | **absent** — neither fabricates a watch status |
| `watching` | all eligible episodes watched | `watched` |
| `watching` | unmark, new episode | `watching` |
| `watched` | a newly eligible episode appears (refresh job) | `watching` |
| `watched` | the last watch of an eligible episode is deleted | `watching` |
| `watched` | a rewatch is logged | `watched` — a rewatch never demotes |
| `dropped` | any watch, refresh, watchlist or rating | **`dropped`** — sticky |
| `dropped` | user un-drops | recompute → `watching` \| `watched` |
| any | user untracks | absent (+ delete this user's watch rows, then the D-008 check) |

`dropped` is sticky for movies too. When a mark lands on a dropped item the backend keeps the status
and the SPA offers an explicit "resume?" prompt — never a hidden transition.

**Deleting every watch leaves the row at `watching`; it must not auto-delete.** A `user_show_states`
row means "I follow this show", which is a separate act from having watched something — and
auto-deleting would silently queue a `pending_deletions` row behind what the user experienced as an
undo.

### Single writer

`server/features/video/common/status.go` owns `RecomputeShowStatus` / `RecomputeMovieStatus`; they are the
**only** writers of either status column, and that same file holds the only
`UPDATE`s. `/verify` greps for violations.

`MarkEpisodeWatched` and every bulk variant are **one** `store.InTx`: validate → insert row(s)
idempotently → upsert the state row if absent → recompute **once** (never per episode) → delete any
`pending_deletions` row for the show → return the fresh status *and* recomputed progress.

### Recompute SQL

Two idempotent statements. `dropped` is excluded **structurally** by the `WHERE status = '…'` filter,
not by a Go conditional someone can forget.

```sql
-- DEMOTE: any 'watched' row that now has an eligible unwatched episode
UPDATE user_show_states
SET status = 'watching', updated_at = ?
WHERE status = 'watched'
  AND EXISTS (
      SELECT 1 FROM episodes e
      WHERE e.show_id = user_show_states.show_id
        AND e.season_number > 0
        AND e.air_date IS NOT NULL AND e.air_date <= ?
        AND NOT EXISTS (
            SELECT 1 FROM user_watch_history wh
            WHERE wh.user_id     = user_show_states.user_id
              AND wh.target_type = 'episode'
              AND wh.target_id   = e.id));

-- PROMOTE: any 'watching' row with >=1 eligible episode and none unwatched
UPDATE user_show_states
SET status = 'watched', updated_at = ?
WHERE status = 'watching'
  AND EXISTS (
      SELECT 1 FROM episodes e
      WHERE e.show_id = user_show_states.show_id
        AND e.season_number > 0
        AND e.air_date IS NOT NULL AND e.air_date <= ?)
  AND NOT EXISTS (
      SELECT 1 FROM episodes e
      WHERE e.show_id = user_show_states.show_id
        AND e.season_number > 0
        AND e.air_date IS NOT NULL AND e.air_date <= ?
        AND NOT EXISTS (
            SELECT 1 FROM user_watch_history wh
            WHERE wh.user_id     = user_show_states.user_id
              AND wh.target_type = 'episode'
              AND wh.target_id   = e.id));
```

**Two call sites, identical SQL:**

- scoped — append `AND user_id = ? AND show_id = ?` after a watch insert/delete or an un-drop;
- **unscoped whole-table pass** inside the TMDB air-date refresh. At self-hosted scale this is a scan
  of a few thousand rows, and it means **drift has a bounded lifetime of one refresh interval even if
  a service bug slips through**. That is the real mitigation; the scoped call is just latency.

Un-drop is `UPDATE … SET status='watching' WHERE user_id=? AND show_id=? AND status='dropped'`
followed by the scoped promote.

Movie recompute:

```sql
-- promote when a watch exists
UPDATE user_movie_states SET status = 'watched', updated_at = ?
WHERE user_id = ? AND movie_id = ? AND status <> 'dropped'
  AND EXISTS (SELECT 1 FROM user_watch_history wh
              WHERE wh.user_id = ? AND wh.target_type = 'movie' AND wh.target_id = ?);

-- collapse: a non-dropped row with no watches left is not a state at all
DELETE FROM user_movie_states
WHERE user_id = ? AND movie_id = ? AND status <> 'dropped'
  AND NOT EXISTS (SELECT 1 FROM user_watch_history wh
                  WHERE wh.user_id = ? AND wh.target_type = 'movie' AND wh.target_id = ?);
```

Deliberately asymmetric with shows: "tracked but unwatched" for a movie is a watchlist row, not a
status, so the last watch going away removes the state row. A show is an ongoing subscription; a
movie is not.

### Repair

`RecomputeAllShowStatuses` runs the two unscoped statements, then asserts INV-A returns no rows, then
sweeps orphans — watch rows whose `target_id` no longer resolves, and episode watches with no owning
`user_show_states` row. Run the orphan sweep from the existing D-008 deferred sweep, **not** as a new
scheduled job (D-006).

## Idempotency: `markWatched` vs `logRewatch`

`user_watch_history` has **no `UNIQUE` constraint** — one would forbid a legitimate repeat watch, and
since `watched_at` is an instant it would not even catch the duplicate it was meant to catch (two
submissions a second apart differ). Idempotency therefore lives in the service layer, and that forces
two distinct operations:

```sql
-- markWatched: insert ONLY if this target has never been watched
INSERT INTO user_watch_history (user_id, target_type, target_id, watched_at, watch_type)
SELECT ?, ?, ?, ?, ?
WHERE NOT EXISTS (SELECT 1 FROM user_watch_history
                  WHERE user_id = ? AND target_type = ? AND target_id = ?);

-- logRewatch: always inserts
```

Without this split, a double-tapped "mark whole season watched" — or the SPA's optimistic retry —
silently doubles every row. **This is the most likely bug in the whole design.**

## Derived queries — write these, do not denormalise them

> **`user_watch_history` fans out.** The old `episode_watches` had at most one row per episode, so a
> `LEFT JOIN` was safe. It no longer is: a `LEFT JOIN` here multiplies rows, corrupting
> `ROW_NUMBER()` and inflating counts. **Use `EXISTS` / `NOT EXISTS` everywhere.**

**Next unwatched episode per show** (the home view):

```sql
SELECT show_id, id, season_number, episode_number, title, still_path
FROM (
    SELECT e.show_id, e.id, e.season_number, e.episode_number, e.title, e.still_path,
           ROW_NUMBER() OVER (
               PARTITION BY e.show_id
               ORDER BY e.season_number, e.episode_number
           ) AS rn
    FROM episodes e
    JOIN user_show_states ss ON ss.show_id = e.show_id AND ss.user_id = ?
    WHERE ss.status = 'watching'                       -- excludes 'dropped' and 'watched'
      AND e.season_number > 0
      AND e.air_date IS NOT NULL AND e.air_date <= ?   -- pass :today
      AND NOT EXISTS (
          SELECT 1 FROM user_watch_history wh
          WHERE wh.user_id     = ?
            AND wh.target_type = 'episode'
            AND wh.target_id   = e.id)
) t
WHERE rn = 1;
```

**Season progress** — three numbers, not two:

```sql
SELECT e.season_number,
       COUNT(*)                                             AS total,
       SUM(CASE WHEN e.air_date IS NOT NULL AND e.air_date <= ?
                THEN 1 ELSE 0 END)                          AS aired_total,
       SUM(CASE WHEN EXISTS (
                     SELECT 1 FROM user_watch_history wh
                     WHERE wh.user_id     = ?
                       AND wh.target_type = 'episode'
                       AND wh.target_id   = e.id)
                THEN 1 ELSE 0 END)                          AS watched
FROM episodes e
WHERE e.show_id = ?
GROUP BY e.season_number
ORDER BY e.season_number;
```

`SUM(CASE …)` counts **episodes**, not watch rows, so rewatches cannot push `watched` past `total`.
`aired_total` exists because completion is aired-only: without it a caught-up viewer of an airing
show sees `8/10` beside a status of `watched` and reads it as a bug. The UI renders
`watched / aired_total`, with `total` secondary. Progress and completion use different denominators
on purpose.

**Does any user still reference this item?** (the D-008 cleanup check.) Every polymorphic predicate
carries its `target_type`:

```sql
-- MOVIE: state OR watchlist OR any watch OR any rating OR any owned copy
SELECT EXISTS (SELECT 1 FROM user_movie_states     WHERE movie_id = ?)
    OR EXISTS (SELECT 1 FROM user_video_watchlist  WHERE target_type = 'movie' AND target_id = ?)
    OR EXISTS (SELECT 1 FROM user_watch_history    WHERE target_type = 'movie' AND target_id = ?)
    OR EXISTS (SELECT 1 FROM user_video_ratings    WHERE target_type = 'movie' AND target_id = ?)
    OR EXISTS (SELECT 1 FROM user_video_copies     WHERE target_type = 'movie' AND target_id = ?);

-- SHOW: NINE ways to be referenced. Missing any one deletes tracked data.
-- The show- and episode-target COPY arms are defensive, like the season-rating
-- arm: unwritable through the API, but admitted by the CHECK.
SELECT EXISTS (SELECT 1 FROM user_show_states      WHERE show_id = ?)
    OR EXISTS (SELECT 1 FROM user_video_watchlist  WHERE target_type = 'show' AND target_id = ?)
    OR EXISTS (SELECT 1 FROM user_watch_history wh
               JOIN episodes e ON e.id = wh.target_id
               WHERE wh.target_type = 'episode' AND e.show_id = ?)
    OR EXISTS (SELECT 1 FROM user_video_ratings    WHERE target_type = 'show' AND target_id = ?)
    OR EXISTS (SELECT 1 FROM user_video_ratings vr
               JOIN seasons s ON s.id = vr.target_id
               WHERE vr.target_type = 'season'  AND s.show_id = ?)
    OR EXISTS (SELECT 1 FROM user_video_ratings vr
               JOIN episodes e ON e.id = vr.target_id
               WHERE vr.target_type = 'episode' AND e.show_id = ?)
    OR EXISTS (SELECT 1 FROM user_video_copies     WHERE target_type = 'show' AND target_id = ?)
    OR EXISTS (SELECT 1 FROM user_video_copies vc
               JOIN seasons s ON s.id = vc.target_id
               WHERE vc.target_type = 'season'  AND s.show_id = ?)
    OR EXISTS (SELECT 1 FROM user_video_copies vc
               JOIN episodes e ON e.id = vc.target_id
               WHERE vc.target_type = 'episode' AND e.show_id = ?);

-- game:  user_game_states OR user_game_ratings OR user_achievements (via game_achievements)
```

"Any tracked state at all" includes **merely rated** and **merely watchlisted** — both are references.

**The mirror obligation.** Polymorphic rows have no FK, so deleting a show must remove them
explicitly, **before** the `shows` delete cascades `seasons`/`episodes` away (these subqueries read
those tables):

```sql
DELETE FROM user_watch_history
 WHERE target_type = 'episode'
   AND target_id IN (SELECT id FROM episodes WHERE show_id = ?);
DELETE FROM user_video_ratings
 WHERE (target_type = 'show'    AND target_id = ?)
    OR (target_type = 'season'  AND target_id IN (SELECT id FROM seasons  WHERE show_id = ?))
    OR (target_type = 'episode' AND target_id IN (SELECT id FROM episodes WHERE show_id = ?));
DELETE FROM user_video_copies
 WHERE (target_type = 'show'    AND target_id = ?)
    OR (target_type = 'season'  AND target_id IN (SELECT id FROM seasons  WHERE show_id = ?))
    OR (target_type = 'episode' AND target_id IN (SELECT id FROM episodes WHERE show_id = ?));
DELETE FROM user_video_watchlist WHERE target_type = 'show' AND target_id = ?;
-- only now: DELETE FROM shows WHERE id = ?   (seasons/episodes cascade)
```

**What the scheduled refresh selects** (D-025; `refresh/store_refresh.go`). Two disjoint classes, each with
its own interval, over rows whose `fetched_at` predates a Go-computed cutoff. Every arm reads what
ANY user holds — one user dropping a show never starves another's calendar — and a watchlist row
always puts an item in the ACTIVE class:

```sql
-- ACTIVE shows: on any watchlist, or followed and still in production (NULL = in production)
SELECT id, tmdb_id FROM shows
WHERE fetched_at < ?
  AND (EXISTS (SELECT 1 FROM user_video_watchlist wl
               WHERE wl.target_type = 'show' AND wl.target_id = shows.id)
       OR (EXISTS (SELECT 1 FROM user_show_states ss
                   WHERE ss.show_id = shows.id AND ss.status <> 'dropped')
           AND (shows.status IS NULL OR shows.status NOT IN ('Ended', 'Canceled'))));

-- ARCHIVE shows: followed, ended or cancelled, and on nobody's watchlist
SELECT id, tmdb_id FROM shows
WHERE fetched_at < ?
  AND NOT EXISTS (SELECT 1 FROM user_video_watchlist wl
                  WHERE wl.target_type = 'show' AND wl.target_id = shows.id)
  AND EXISTS (SELECT 1 FROM user_show_states ss
              WHERE ss.show_id = shows.id AND ss.status <> 'dropped')
  AND shows.status IN ('Ended', 'Canceled');
```

Movies follow the same shape: active = `EXISTS` watchlist (`target_type = 'movie'`); archive =
`NOT EXISTS` watchlist `AND EXISTS user_watch_history` (`target_type = 'movie'`). A show's **open
seasons** — the ones the job re-fetches — are its highest `season_number`, plus any non-special
season with an episode whose `air_date IS NULL OR air_date > :today`, each only if its own
`fetched_at` is due. The sweep's targets are every non-dropped `user_show_states` row `UNION` every
(user, show) with an episode watch, the second arm being a repair for a missing state row.

## Portability checklist

SQL must run unmodified on SQLite and Postgres. Before committing a query:

- [ ] No `LEFT JOIN` onto `user_watch_history` — it fans out. `EXISTS` /
      `NOT EXISTS` / `SUM(CASE …)`.
- [ ] Every polymorphic predicate carries its `target_type`.
- [ ] No `AUTOINCREMENT`, no `rowid`, no `last_insert_rowid()` — use `RETURNING id`, which both
      engines support.
- [ ] No `datetime('now')` / `strftime(…)` / `now()` / `CURRENT_DATE`. Timestamps and `:today` come
      from Go, computed **once per request or job** and passed to every query in it.
- [ ] `COALESCE` not `IFNULL`; no `GROUP_CONCAT` vs `STRING_AGG`.
- [ ] Booleans compared as `= 1` / `= 0`, never `IS TRUE`.
- [ ] Upsert with an explicit conflict target and `excluded.`:
      `… ON CONFLICT (user_id, game_id) DO UPDATE SET rating = excluded.rating`.
- [ ] No bare column in a `GROUP BY` projection (SQLite tolerates it, Postgres rejects it).
- [ ] No `UPDATE … FROM` (SQLite only gained it in 3.33). Correlated subqueries referencing the
      update target work identically on both.
- [ ] `LIMIT n OFFSET m`, never `TOP` or `FETCH FIRST`.
- [ ] Placeholders written `?`; the store layer rewrites to `$n` for Postgres in one place.

## Migrations

- **Pre-release there is exactly one migration** (D-016): the baseline
  `server/migrations/sql/0001_init.sql` holds the whole schema and you **edit it in place**, then
  delete `server/data/sofalog.db` so it rebuilds. Do not add `0002_*.sql`. `db.Migrate` checksums
  applied files and fails at startup if the delete is forgotten, so an edited baseline is never
  silently skipped.
- **After release, append-only.** `server/migrations/FROZEN` exists from the first deployed release;
  from then on the baseline is immutable (a `PreToolUse` hook blocks edits) and each change is a new
  `NNNN_snake_case.sql`, zero-padded, monotonic, applied in filename order — one logical change per
  file.
- Include `CREATE INDEX` statements with the table they serve.
- SQLite has limited `ALTER TABLE`; a column drop or a `CHECK` change is
  create-new → copy → drop → rename. That is why values known to be coming (`watching` for movies,
  the OIDC columns) are in the CHECK from the baseline.
- Enable `PRAGMA journal_mode=WAL`, `PRAGMA foreign_keys=ON` and `PRAGMA busy_timeout=5000` at
  connection setup, not in a migration.

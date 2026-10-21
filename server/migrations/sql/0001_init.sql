-- The baseline schema (D-016).
--
-- sofalog is not deployed anywhere yet, so there is exactly ONE migration and it
-- is EDITABLE IN PLACE: schema changes are made here and dev databases are
-- recreated (delete server/data/sofalog.db and restart). The append-only rule
-- switches on at the first deployed release, marked by creating
-- server/migrations/FROZEN — from then on this file is immutable and every
-- change is a new 0002_*.sql. See SPEC.md D-016.
--
-- Scope is M1 (auth + movies) plus M1.5, which pulled show tracking forward:
-- shows, seasons, episodes and the show status table are all here. The games
-- tables arrive with their milestone.
-- The polymorphic tables below already carry the later target types
-- in their CHECK lists, because SQLite cannot alter a CHECK afterwards and a
-- polymorphic target_id has no foreign key to a table that does not exist yet.
--
-- Portability: SQLite (primary, WAL) and Postgres run this unmodified. Ids are
-- INTEGER PRIMARY KEY, timestamps are TEXT generated in Go — never a SQL date
-- function.

-- ---------------------------------------------------------------------------
-- Auth. All four tables exist from the baseline even though OIDC and API tokens
-- ship in M5 (D-007): password_hash stays nullable for OIDC-only users, and
-- retrofitting these later would cost a migration plus an auth refactor.
-- ---------------------------------------------------------------------------

CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    -- The login identifier, stored lowercased. Also what an OIDC sign-in
    -- matches on to attach to an existing account (D-019).
    email         TEXT    NOT NULL UNIQUE,
    -- Optional human label. Deliberately NOT unique: it is a label, not an
    -- identity, so two people may both be "Mihai" (D-019). NULL until set;
    -- the UI falls back to the email's local part.
    display_name  TEXT,
    password_hash TEXT,                      -- argon2id; NULL for an OIDC-only user (D-007)
    is_admin      INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT    NOT NULL,          -- RFC3339 UTC, generated in Go
    steam_id64    TEXT,                      -- per-user Steam identity; the API key is server config
    -- Consent to appear in other users' "Matching watchlist" picker and to be
    -- intersected against (D-028). Default off, and only the user flips it:
    -- an admin cannot opt someone in. Read by the video feature, written by
    -- auth.
    match_watchlist INTEGER NOT NULL DEFAULT 0,
    -- The IANA zone this user's day is measured in (D-038): what has aired,
    -- where the calendar starts, how instants render. NULL means the server's
    -- DEFAULT_TIMEZONE, so changing that moves everyone who never chose. Only
    -- a zone name — every instant stays stored in UTC.
    timezone TEXT
);

CREATE TABLE sessions (
    token      TEXT    PRIMARY KEY,          -- random opaque value; the cookie contents
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT    NOT NULL,
    expires_at TEXT    NOT NULL,
    user_agent TEXT,
    ip         TEXT                          -- resolved through the trusted-proxy rules
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
    token_hash   TEXT    NOT NULL UNIQUE,    -- only ever a hash, never the token
    created_at   TEXT    NOT NULL,
    last_used_at TEXT,
    revoked_at   TEXT
);

-- ---------------------------------------------------------------------------
-- Catalog: movies. One canonical row per real-world movie, shared by all users
-- — never a user_id, a status, a rating or a personal date (SPEC §4). Rows are
-- materialised lazily by the details view and refreshed when stale (D-015).
--
-- fetched_at is TEXT holding an RFC3339 UTC timestamp, written by exactly one
-- Go code path. Fixed-width UTC RFC3339 sorts chronologically as plain text,
-- so range predicates like `fetched_at < ?` (with a Go-computed cutoff) work
-- unmodified on SQLite and Postgres — no SQL date functions.
-- ---------------------------------------------------------------------------

CREATE TABLE movies (
    id             INTEGER PRIMARY KEY,
    tmdb_id        INTEGER NOT NULL UNIQUE,
    title          TEXT    NOT NULL,
    original_title TEXT,
    release_date   TEXT,                     -- YYYY-MM-DD
    runtime_min    INTEGER,
    overview       TEXT,
    tagline        TEXT,
    genres         TEXT,                     -- JSON array of names; catalog snapshot, not queried relationally
    poster_path    TEXT,
    backdrop_path  TEXT,
    metadata_json  TEXT    NOT NULL,         -- full provider snapshot (no provider call to render)
    fetched_at     TEXT    NOT NULL          -- RFC3339 UTC, generated in Go
);

-- ---------------------------------------------------------------------------
-- Catalog: shows. One canonical row per real-world show, materialised by
-- TRACKING (D-015) — a show details view of an untracked show still stores
-- nothing. Deliberately few columns: metadata_json holds the verbatim /tv/{id}
-- body with credits, external_ids, videos and images appended, and everything
-- the details DTO renders beyond the columns below is parsed back out of it.
-- That is why there is no original_title, tagline, last_air_date or
-- in_production column even though ShowDetails carries all four.
-- ---------------------------------------------------------------------------

CREATE TABLE shows (
    id             INTEGER PRIMARY KEY,
    tmdb_id        INTEGER NOT NULL UNIQUE,
    title          TEXT    NOT NULL,         -- TMDB's "name"
    first_air_date TEXT,                     -- YYYY-MM-DD
    -- TMDB's PRODUCTION status ("Returning Series" | "Ended" | "Canceled").
    -- NOT a watch status: that lives in user_show_states and only shares the
    -- word (SPEC.md §6).
    status         TEXT,
    overview       TEXT,
    genres         TEXT,                     -- JSON array of names; catalog snapshot, not queried relationally
    poster_path    TEXT,
    backdrop_path  TEXT,
    metadata_json  TEXT    NOT NULL,         -- full provider snapshot (no provider call to render)
    fetched_at     TEXT    NOT NULL          -- RFC3339 UTC, generated in Go
);
-- No index beyond the two implicit ones (PK, tmdb_id UNIQUE): every lookup is
-- by one of them.

-- ---------------------------------------------------------------------------
-- Catalog: seasons. One canonical row per season of a real-world show — never a
-- user_id, a status, a rating or a personal date (SPEC §4). Like movies, shows
-- and episodes, a season row is materialised by TRACKING and never by a view
-- (D-015): a season of an untracked show is fetched live and discarded, so a
-- row here exists if and only if its show does.
--
-- Keyed by (show_tmdb_id, season_number), which stays the natural key: it is
-- what the season and episode details views address a season by, and what the
-- bulk mark's season_air join reaches it through. No delete path of its own:
-- seasons cascade from their show. fetched_at semantics are exactly the movies
-- table's.
-- ---------------------------------------------------------------------------

-- No tmdb_id column for the season itself: the appended season sub-document
-- this table is fed from carries no integer season id (only a string _id),
-- and nothing addresses a season by it — the key is the pair below.
CREATE TABLE seasons (
    id            INTEGER PRIMARY KEY,
    -- The show link, always known: nothing may write a season except beneath a
    -- tracked show (D-015), so every row cascades. Relaxing this to NULLABLE is
    -- how materialise-on-view would come back — it is the constraint that makes
    -- "a season row implies a tracked show" enforceable rather than a
    -- convention.
    show_id       INTEGER NOT NULL REFERENCES shows(id) ON DELETE CASCADE,
    show_tmdb_id  INTEGER NOT NULL,          -- TMDB show id; the addressing key, survives the reshape
    season_number INTEGER NOT NULL,          -- 0 is Specials
    name          TEXT    NOT NULL,          -- "Season 2"
    show_title    TEXT    NOT NULL,          -- denormalised from the show root, for the header and back link
    air_date      TEXT,                      -- YYYY-MM-DD of the premiere
    episode_count INTEGER NOT NULL DEFAULT 0,
    overview      TEXT,
    poster_path   TEXT,
    metadata_json TEXT    NOT NULL,          -- full season snapshot incl. the episodes array
    fetched_at    TEXT    NOT NULL,          -- RFC3339 UTC, generated in Go
    UNIQUE (show_tmdb_id, season_number)
);
-- The show-scoped reads: the asset diff's season posters, D-008's season-rating
-- reference check, and the show page's season links once tracked.
CREATE INDEX idx_seasons_show ON seasons(show_id, season_number);

-- ---------------------------------------------------------------------------
-- Catalog: episodes. One canonical row per real-world episode. Unlike seasons,
-- an episode row is materialised by TRACKING only — never by a view.
--
-- Both halves of D-012's "aired, non-special" predicate live here as plain
-- columns, so the completion test stays ONE WHERE with no join:
--     e.season_number > 0 AND e.air_date IS NOT NULL AND e.air_date <= :today
-- NEVER `air_date IS NULL OR …` — an unknown date must not read as aired, or a
-- show with one announced-but-undated episode could never reach 'watched'.
--
-- Keyed on show_id rather than season_id for exactly that reason, and so that
-- DELETE FROM shows cascades the episodes away in one step.
--
-- No tmdb_id: nothing addresses an episode by one (episodes ride the season
-- document, which is fetched by season), and a second UNIQUE would turn a TMDB
-- renumber — a two-parter split, numbers shifting — from a silent re-point of
-- an existing watch into a HARD FAILURE of the refresh upsert, which nothing
-- could then resolve. The natural key converges instead. The accepted cost is
-- that a renumber silently re-points a watch to different content.
--
-- No metadata_json either: an episode's score, vote count and episode_type are
-- read out of its SEASON's snapshot, which stays the single source for them.
-- ---------------------------------------------------------------------------

CREATE TABLE episodes (
    id             INTEGER PRIMARY KEY,
    show_id        INTEGER NOT NULL REFERENCES shows(id) ON DELETE CASCADE,
    season_number  INTEGER NOT NULL,         -- 0 is Specials; excluded from completion (D-012)
    episode_number INTEGER NOT NULL,
    title          TEXT,
    air_date       TEXT,                     -- YYYY-MM-DD; NULL when TMDB has none
    runtime_min    INTEGER,
    overview       TEXT,
    still_path     TEXT,
    UNIQUE (show_id, season_number, episode_number)
);
-- Ordering within a show is served by the UNIQUE's own index on both engines,
-- so there is deliberately NO second index on those three columns.
CREATE INDEX idx_episodes_air_date ON episodes(air_date);   -- the upcoming-episodes calendar

-- ---------------------------------------------------------------------------
-- Per-user video state. Every table here is prefixed user_ and every query
-- against it filters on the authenticated user's id.
--
-- target_type + target_id is a POLYMORPHIC pair: the pair identifies the
-- target, the id alone identifies nothing. Ids are unique only within their own
-- table (D-013), so movie 5 and episode 5 both exist — every join or predicate
-- through target_id must carry its target_type in the same WHERE / EXISTS.
-- These columns also cannot have a foreign key, so every delete path removes
-- them explicitly in the same transaction.
-- ---------------------------------------------------------------------------

-- The watch LOG, many rows per (user, target): a rewatch is a new timestamped
-- row (D-009). No UNIQUE constraint — one would forbid a legitimate
-- watched-twice-in-a-row rewatch — so markWatched's idempotency lives in the
-- service layer instead.
-- It fans out, so never LEFT JOIN it; use EXISTS / NOT EXISTS / SUM(CASE …).
--
-- watched_at is a full instant, not a day: the feed shows the time of day and a
-- same-day rewatch has to be orderable. It is RFC3339 UTC written by Go, like
-- every other _at column here — fixed-width UTC RFC3339 sorts chronologically
-- as plain text, so ORDER BY and range predicates need no SQL date function and
-- run unmodified on SQLite and Postgres. Clients render it in local time; the
-- server never stores an offset, so there is exactly one representation of an
-- instant in the log.
CREATE TABLE user_watch_history (
    id          INTEGER PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_type TEXT    NOT NULL CHECK (target_type IN ('movie','episode')),
    target_id   INTEGER NOT NULL,            -- no FK: polymorphic
    watched_at  TEXT    NOT NULL,            -- RFC3339 UTC, generated in Go
    -- PROVENANCE ONLY — no query may branch on this value (D-010). 'checkin' and
    -- 'scrobble' are in the CHECK now; the features producing them are out of scope.
    watch_type  TEXT    NOT NULL DEFAULT 'watch'
                        CHECK (watch_type IN ('watch','checkin','scrobble'))
    -- Deliberately NO created_at. watched_at is this table's only timestamp,
    -- like every other user_ table's single domain timestamp. Row write time
    -- was here as an ordering tie-break, which (watched_at, id) now covers;
    -- and id — INTEGER PRIMARY KEY, monotonic on insert — already identifies a
    -- bulk mark by its contiguous run, which is what a backfill question
    -- actually asks.
);
-- "has this user watched target X?" — the hot path for progress and next-up
CREATE INDEX idx_user_watch_history_target ON user_watch_history(user_id, target_type, target_id);
-- history-by-date feed
CREATE INDEX idx_user_watch_history_feed   ON user_watch_history(user_id, watched_at, id);
-- "does ANY user reference X?" — not user-scoped, so it needs its own index
CREATE INDEX idx_user_watch_history_ref    ON user_watch_history(target_type, target_id);

-- SYSTEM-MAINTAINED status (D-010): written only by features/video/status.go and
-- reproducible from user_watch_history alone. 'watched' is the ONLY value a
-- movie can hold, so this is a single-value constraint rather than an enum: a
-- movie is watched, or it has no row here at all. 'watching' and 'dropped' are
-- SHOW values and live only in user_show_states (M2) — dropping means
-- abandoning something partway with content left, which needs parts, and a
-- movie has none (SPEC.md §6). Narrow on purpose: SQLite cannot alter a CHECK
-- later, and this is what makes a dropped movie unrepresentable rather than
-- merely unwritten.
-- No watched_at column here — watch times live in user_watch_history.
CREATE TABLE user_movie_states (
    user_id    INTEGER NOT NULL REFERENCES users(id)  ON DELETE CASCADE,
    movie_id   INTEGER NOT NULL REFERENCES movies(id) ON DELETE CASCADE,
    status     TEXT    NOT NULL CHECK (status = 'watched'),
    updated_at TEXT    NOT NULL,
    PRIMARY KEY (user_id, movie_id)
);
-- No (user_id, status) index: status has one possible value, so it cannot
-- narrow anything, and the PRIMARY KEY already covers the user_id prefix.
CREATE INDEX idx_user_movie_states_movie  ON user_movie_states(movie_id);   -- reference count

-- The show half of the same idea, and the only status table with a real enum.
-- SYSTEM-MAINTAINED (D-010): written only by features/video/status.go.
-- 'watching' and 'watched' are derived from episodes + user_watch_history and
-- rewritten by recomputeShowStatus. 'dropped' is USER INTENT, is sticky, and is
-- never produced by a recompute — every statement in that function excludes it
-- in SQL (DO NOTHING / status = 'watched' / status = 'watching' /
-- status <> 'dropped'), never in a Go conditional, so a user's drop survives
-- any number of recomputes.
-- NO progress counter, NO percentage, NO last_watched_episode column: progress
-- is always derived from episodes + user_watch_history (D-010).
CREATE TABLE user_show_states (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    show_id    INTEGER NOT NULL REFERENCES shows(id) ON DELETE CASCADE,
    status     TEXT    NOT NULL CHECK (status IN ('watching','watched','dropped')),
    updated_at TEXT    NOT NULL,
    PRIMARY KEY (user_id, show_id)
);
CREATE INDEX idx_user_show_states_status ON user_show_states(user_id, status);  -- home view, calendar
CREATE INDEX idx_user_show_states_show   ON user_show_states(show_id);          -- refresh + D-008 reference count

-- An INDEPENDENT axis, not a status (D-011): an item can be watchlisted and in
-- progress at once, so 'watchlist' is deliberately absent from every status
-- CHECK. Movies and shows only — never seasons, never episodes.
CREATE TABLE user_video_watchlist (
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_type TEXT    NOT NULL CHECK (target_type IN ('movie','show')),
    target_id   INTEGER NOT NULL,            -- no FK: polymorphic
    added_at    TEXT    NOT NULL,
    PRIMARY KEY (user_id, target_type, target_id)
);
CREATE INDEX idx_user_video_watchlist_recent ON user_video_watchlist(user_id, added_at);
CREATE INDEX idx_user_video_watchlist_ref    ON user_video_watchlist(target_type, target_id);

-- Favorites: a THIRD independent axis beside the watchlist and owned copies
-- (D-030), modelled byte for byte on the watchlist. Never a status — a title
-- can be a favorite and unwatched, watching, or dropped at once — and a row
-- alone is a D-008 reference and puts the item on the ACTIVE refresh cadence
-- (D-025). Movies and shows only — never seasons, never episodes.
CREATE TABLE user_video_favorites (
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_type TEXT    NOT NULL CHECK (target_type IN ('movie','show')),
    target_id   INTEGER NOT NULL,            -- no FK: polymorphic
    added_at    TEXT    NOT NULL,
    PRIMARY KEY (user_id, target_type, target_id)
);
CREATE INDEX idx_user_video_favorites_recent ON user_video_favorites(user_id, added_at);
CREATE INDEX idx_user_video_favorites_ref    ON user_video_favorites(target_type, target_id);

-- Owned copies: which formats/services this user holds an item on — another
-- INDEPENDENT axis like the watchlist (D-011), never a status, and a D-008
-- reference on its own. Writable targets are movie and season: a show's copies
-- live PER-SEASON, and 'show' is the API's bulk fan-out scope, not a stored
-- target (D-021). 'show' and 'episode' stay in the CHECK because SQLite cannot
-- alter one later — the D-020 pattern.
CREATE TABLE user_video_copies (
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_type TEXT    NOT NULL CHECK (target_type IN ('movie','show','season','episode')),
    target_id   INTEGER NOT NULL,            -- no FK: polymorphic
    kind        TEXT    NOT NULL CHECK (kind IN ('jellyfin','dvd','bluray','uhd','physical','digital')),
    added_at    TEXT    NOT NULL,
    PRIMARY KEY (user_id, target_type, target_id, kind)
);
CREATE INDEX idx_user_video_copies_ref ON user_video_copies(target_type, target_id);

-- What the household's Jellyfin server holds (D-033). SHARED, not per-user: a
-- fact about the server, identical for every account, so it takes no user_
-- prefix — the same posture as the streaming listing, not the copies shelf.
-- Rewritten whole by every scan (one transaction, so a failed scan leaves the
-- previous answer in place) and read into the copies list as the 'jellyfin'
-- kind while a server is configured, when the copy routes refuse that kind.
-- Not a D-008 reference: the server's contents are not a user's interest.
-- 'season' rows mean "at least one episode of the season is there"; episodes
-- counts the distinct episode files found, so a page can tell a complete
-- season from a partial one (the tile turns amber when aired episodes are
-- missing). 0 on movie rows.
CREATE TABLE jellyfin_library (
    target_type TEXT    NOT NULL CHECK (target_type IN ('movie','season')),
    target_id   INTEGER NOT NULL,            -- no FK: polymorphic
    jellyfin_id TEXT    NOT NULL,            -- the Movie's or the Series' Jellyfin id
    episodes    INTEGER NOT NULL DEFAULT 0,  -- distinct episode files in the season
    seen_at     TEXT    NOT NULL,            -- RFC3339 UTC, the scan that wrote it
    PRIMARY KEY (target_type, target_id)
);

-- Ratings are 1-10 integers, one per user per target. Re-rating is an upsert
-- that overwrites — there is no rating history (D-009).
-- 'season' is present but UNWRITABLE: every write path refuses a season target
-- (D-020). The CHECK value stays because SQLite cannot alter a CHECK later, so
-- narrowing it here would make that decision irreversible once this baseline
-- freezes; seasons.id is the id it would target if it ever reverses — never
-- season_number.
CREATE TABLE user_video_ratings (
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_type TEXT    NOT NULL CHECK (target_type IN ('movie','show','season','episode')),
    target_id   INTEGER NOT NULL,            -- no FK: polymorphic
    rating      INTEGER NOT NULL CHECK (rating BETWEEN 1 AND 10),
    updated_at  TEXT    NOT NULL,
    PRIMARY KEY (user_id, target_type, target_id)
);
CREATE INDEX idx_user_video_ratings_ref ON user_video_ratings(target_type, target_id);

-- The user's own TMDB account (D-029). The row is the consent: a linked user
-- whose switch for a kind is on gets that kind's outbox rows, and unlinking
-- deletes it. tmdb_session_id is
-- TMDB's long-lived v3 session, stored as issued — there is no application
-- secret to encrypt it with — and it never leaves the server: no DTO carries
-- it and server/provider redacts it from every error. broken_at is set when
-- TMDB answered 401 for the session; the row stays so the Settings page can
-- say WHICH account stopped working, and no new outbox rows are written until
-- the user links again (which clears it).
--
-- D-036: the one-time SETUP after linking. setup_done is 1 once the setup's
-- work finished — at submit when nothing was pulled, at the end of the pull
-- job otherwise, so an interrupted pull leaves the setup to be re-run. The
-- three sync_* switches govern the ongoing push of FUTURE writes only:
-- enqueueTMDBSync checks the one for its kind in SQL, and the setup's one-time
-- Push deliberately ignores them. All four reset when a DIFFERENT TMDB account
-- is linked; re-linking the same account (healing broken_at) keeps them.
CREATE TABLE user_tmdb_accounts (
    user_id         INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    tmdb_session_id TEXT    NOT NULL,
    tmdb_account_id INTEGER NOT NULL,
    tmdb_username   TEXT    NOT NULL,
    linked_at       TEXT    NOT NULL,          -- RFC3339 UTC, generated in Go
    broken_at       TEXT,                      -- NULL while the session works
    setup_done      INTEGER NOT NULL DEFAULT 0,
    sync_ratings    INTEGER NOT NULL DEFAULT 0,
    sync_watchlist  INTEGER NOT NULL DEFAULT 0,
    sync_favorites  INTEGER NOT NULL DEFAULT 0
);

-- Outbox for the one-way push to the user's TMDB account (D-029): filled
-- inside the SAME transaction as the local write, drained by the video
-- Service's worker. kind says WHAT is mirrored — a rating, a watchlist entry
-- or a favorite (every local write of each, while that kind's switch is on;
-- D-030 added the third). One row per (user, kind, target): a newer write REPLACES a pending
-- one (ON CONFLICT DO UPDATE), so the queue holds the current intent, never a
-- history. updated_at is the replace marker — the worker's settle statements
-- are guarded on it, so a re-rating that lands while a row is in flight is
-- never deleted as "sent". Polymorphic target with no FK: a catalog delete
-- path must remove these rows explicitly (the D-008 sweep, when built).
CREATE TABLE user_tmdb_sync (
    id            INTEGER PRIMARY KEY,
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind          TEXT    NOT NULL CHECK (kind IN ('rating','watchlist','favorite')),
    target_type   TEXT    NOT NULL CHECK (target_type IN ('movie','show','episode')),
    target_id     INTEGER NOT NULL,            -- no FK: polymorphic
    op            TEXT    NOT NULL CHECK (op IN ('set','delete')),   -- set = rate / add; delete = clear / remove
    rating        INTEGER CHECK (rating BETWEEN 1 AND 10),   -- rating rows with op 'set' only
    attempts      INTEGER NOT NULL DEFAULT 0,
    next_retry_at TEXT,                        -- NULL = due now
    last_error    TEXT,                        -- redacted; diagnosis only
    created_at    TEXT    NOT NULL,
    updated_at    TEXT    NOT NULL,
    UNIQUE (user_id, kind, target_type, target_id)
);
CREATE INDEX idx_user_tmdb_sync_due ON user_tmdb_sync(next_retry_at, id);

-- ---------------------------------------------------------------------------
-- Asset pipeline and deferred deletion. Both are infrastructure, not per-user
-- state, which is why they carry a media_type discriminator — the one place it
-- is legitimate (D-001 forbids it on catalog and per-user tables).
-- ---------------------------------------------------------------------------

CREATE TABLE asset_jobs (
    id            INTEGER PRIMARY KEY,
    -- youtube is a file source only, never a metadata provider (D-017).
    provider      TEXT    NOT NULL CHECK (provider IN ('tmdb','steam','youtube')),
    media_type    TEXT    NOT NULL CHECK (media_type IN ('movie','show','season','episode','game','achievement')),
    item_id       INTEGER NOT NULL,          -- internal id of the owning row
    kind          TEXT    NOT NULL,          -- poster | backdrop | gallery_backdrop | gallery_poster | cast | video_thumb | still | cover | icon
    size          TEXT    NOT NULL,          -- thumb | full | single (cast photos, video thumbs)
    -- Provider-relative identifier ("w500/abc.jpg", a YouTube key), never an
    -- absolute URL — the image host stays runtime-resolved.
    source_url    TEXT    NOT NULL,
    -- Idempotency: one job per target file, so enqueueing the same image twice
    -- is a no-op — which is what makes the queue resumable after a crash
    -- mid-import. (Galleries hold many files per kind+size, so the identity is
    -- the destination, not the kind.)
    dest_path     TEXT    NOT NULL UNIQUE,   -- relative, under assets/
    status        TEXT    NOT NULL CHECK (status IN ('pending','running','done','failed')),
    attempts      INTEGER NOT NULL DEFAULT 0,
    last_error    TEXT,
    next_retry_at TEXT,
    created_at    TEXT    NOT NULL
);
CREATE INDEX idx_asset_jobs_claim ON asset_jobs(status, provider, next_retry_at);
CREATE INDEX idx_asset_jobs_item  ON asset_jobs(media_type, item_id); -- per-item diff/cancel

-- Untracking the last reference queues a row here instead of hard-deleting
-- (D-008); a deferred sweep removes the catalog row plus one RemoveAll of the
-- item directory. Seasons and episodes cascade from their show and are never
-- deleted independently, which is why there are only three values.
CREATE TABLE pending_deletions (
    id         INTEGER PRIMARY KEY,
    media_type TEXT    NOT NULL CHECK (media_type IN ('movie','show','game')),
    item_id    INTEGER NOT NULL,
    queued_at  TEXT    NOT NULL,
    UNIQUE (media_type, item_id)
);

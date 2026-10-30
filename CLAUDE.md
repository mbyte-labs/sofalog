# sofalog

Self-hosted, multi-user media tracker: **Go backend + embedded Svelte SPA + SQLite**, one binary,
behind a reverse proxy. Tracks movies & TV episodes (TMDB) and games (Steam).

## The spec

[`SPEC.md`](SPEC.md) is the single source of truth — stack, architecture, the two components,
data model, local-first rules, milestones and scope. This file is the enforceable subset of it.

Rules that are surprising on their own carry a `D-00x` id; the reasoning is in
[`SPEC.md` Appendix A](SPEC.md#appendix-a--why-these-decisions). A change that contradicts `SPEC.md`
updates `SPEC.md` in the same commit — never leave the two disagreeing.

## Current milestone: M4 (games)

Build only what the current milestone lists in [`SPEC.md` §8](SPEC.md#8-milestones). Do not start
API tokens or export before the active milestone closes. M1.5 and M2 are closed. **M3 (books) was
withdrawn** (D-035): the product tracks video and games only. There is no books package, no books
table, no Open Library client and no `/books` route, and none is to be added without a new decision.
Update this line when a milestone closes.

**Five things were pulled forward on request.** **OIDC login**, from M5 into M1: configuring a
provider **disables local authentication entirely** (D-018); API tokens and export remain M5.
**Show tracking**, from M2 into M1.5 — because a show page whose "Mark As Watched" is permanently
disabled is a dead end. **Show watchlisting**, from M2 into M1.5 for the same reason: a
permanently disabled button is a dead end. **Removing a single watch entry**, which belonged to
no milestone at all — a history list you cannot correct makes a mis-tap permanent. And **dated
watch logging**, D-009's deferred rewatch operation, which likewise belonged to no milestone — a
watch button that goes inert after one press is the same dead end.

M1.5 shipped: the `shows` and `episodes` catalog tables, `user_show_states`, and a **NOT NULL**
`show_id` on `seasons`; `RecomputeShowStatus` beside `RecomputeMovieStatus` in `common/status.go`; episode
marking, the **"mark whole show watched"** bulk action (`POST /video/watches/bulk`), derived show
progress, the show asset tree, and the show details view's snapshot/refresh/stale-serve half. The
history feed **gained its episode arm**. Show watchlisting shipped with it: `watchlistTargets`
admits `show`, `GET /video/watchlist` lists both kinds interleaved, and `WatchlistPage.svelte`'s
"Shows only" filter and the show page's watchlist button are both live. Watch deletion shipped with
it too: `DELETE /video/watches/{id}` plus the click-to-confirm card in `HistoryTimeline.svelte`, the
timeline shared by **all four** video details pages. It has three mutually exclusive scopes —
`target_type`+`target_id`, `season_id`, `show_id` — and the last two are **scopes, not targets**: a
season and a show are never themselves watched, which is why neither is a `target_type` value.
Combining two is a 422. The show page renders the timeline **and** its derived progress line; the
old "progress, not a history list" rule there is gone.

**Dated watch logging shipped too**, which is D-009's deferred "log a rewatch" operation. It is a
**mode of `POST /video/watches`, not a route**: `watched_at` present means "this viewing happened,
then" and **always inserts** (so `created` is always true, and an already-watched target gains an
entry); absent keeps the idempotent first-mark. An optional `rating` rides in the same transaction.
Three rules hold it:

- **A future `watched_at` is a 422; there is no lower bound.** A watch legitimately predates the
  library. The service re-formats the parsed instant rather than storing the client's string, so an
  offset or `toISOString()`'s milliseconds cannot break the feed's plain string ordering.
- **Backdating moves `watched_at` and nothing else.** The state row's timestamp and
  `RecomputeShowStatus`'s `today` — D-012's aired predicate — keep taking the real clock. Feeding
  either the backdate would decide what had aired *then*.
- **The movie, episode, show and season pages ask.** `WatchLogDialog.svelte` (the app's only form
  modal; its overlay mechanics are the `MediaGallery.svelte` lightbox's) is wired to those four
  buttons, which stay enabled when watched and read "Log another watch". The season grid has **no
  per-episode mark** any more: its stills only display watched state (the history card's ✓ / user
  rating / ♥ badges), so every mark the SPA writes goes through the dialog. The undated idempotent
  mode of `POST /video/watches` stays an API mode with no SPA caller.

**The bulk marks gained the dialog too**, in a bulk shape (`bulk` prop; the rating bar hides — the
show page has its own). It asks two independent axes, which is why `BulkWatchRequest` grew two
fields rather than one enum: `rewatch` (default false keeps the insert's `NOT EXISTS`; true drops it
— every aired episode gets a NEW row, the one non-idempotent bulk path, guarded by the dialog's
awaited state) and `watched_at_mode` (`now` | `fixed` | `episode_air` | `season_air`; the air-date
modes derive per-row instants server-side as **midnight in the user's zone** stored as its UTC
instant, computed in Go per row since no portable SQL knows a date's DST offset — D-038).
`watched_at` is required with `fixed`, is the fallback for `season_air` episodes whose season has no
usable premiere (missing, or itself unaired — required exactly when such seasons exist, pre-checked
in the same tx so nothing half-lands), and is refused with the other two modes. Eligibility already
guarantees every markable EPISODE an air date, so `episode_air` never needs a fallback. Backdating
still moves `watched_at` and nothing else — the recompute's `today`/`now` keep the real clock.

**"Mark whole season watched" shipped as the same endpoint's `scope: season`** — pulled forward
from M2 on request, exactly the additive widening the `scope` field was designed for. The scope
names the target's type (scope `show` takes a show target, scope `season` a season one; a mismatch
is a 422), so `season` became an addressable target type in `ResolveTrackedTarget`. Its
`target_id` path never calls TMDB — a season row implies a tracked show, so `show_id` resolves
locally — while the `tmdb_id` + `season` form is what the season page uses before the show is
tracked, materialising the show, its seasons and its episodes on the way through. Specials are refused with a 422 (season 0 never
gates completion, and eligibleEpisode would honestly mark nothing). The `season_air` premiere
fallback check narrows to that one season, and the response's status/progress stay the OWNING
SHOW's — a season has no status of its own (D-010). The season page refetches its details after
the mark rather than flipping ticks client-side: the flags are the server's to state.

**The currently-watching home view ("Up Next") shipped too** — pulled forward from M2 on request,
replacing the M0 dummy (`dummy.go` is gone; `listUpNext` in `upnext/store_upnext.go` is the real query).
**Started shows only**: ≥1 episode watch and not dropped — a watchlisted-but-never-started show does
not appear, and dropped is a `NOT EXISTS` on the state row because the log alone defines the show
set. The next episode is the first non-special episode **strictly after the highest watched
position** — after S1E2 the answer is S1E3, never S1E1, and a finished show is absent rather than
wrapping around (specials neither anchor the pick nor become it). **The pick deliberately ignores
airedness**: an unaired or undated next episode is served with the additive `episode.aired` flag
false (computed server-side against the user's today in their zone, D-038 — the client never
derives it) and the card stacks an
"Upcoming" badge under the marker badge; `only_released` is where `EligibleEpisode` is spliced, and
it lands after the pick, so an unaired pick filters to nothing rather than falling back to a later
aired episode. Ordering, nulls-last and the name/id tie-break follow `listWatchlistTargets`'s
compile-time-constant pattern. A card leads to the episode details page, which is why the entry
carries a nullable **`episode.season_id`**: an episode has no id-addressed endpoint, so that page is
reached by `season_id` + `number` — the same pair `HistoryEpisode` carries, resolved by the same
scalar subquery beside the season poster's.

**The card's mark-watched action was then pulled forward too**, on request — an Up Next card that
could only take you elsewhere made the one thing the view is for a three-tap trip through another
page. It is a **chooser, not the milestone's one tap**: the whole card is a `<button>` that opens
`ChoiceDialog.svelte` with two actions — "Mark as watched", and "Episode details" carrying the
`href` (omitted when `season_id` is null, where marking still works because it needs nothing but
the episode's own id). Choosing to mark swaps the chooser for `WatchLogDialog.svelte` in its
**single** mode, so the instant and an optional rating ride one transaction exactly as the episode
page's does; the two dialogs are rendered mutually exclusively, because two mounted focus traps
fight. It addresses the episode by its **local id**, so the write never touches TMDB, and
`initialRating` is deliberately not passed — the entry's `rating` is TMDB's community score, not
this user's. **The list is refetched afterwards, never patched**: `TrackResult` states the show's
status and progress but not its next episode, so which episode a card shows — and whether the show
is listed at all — stays the server's to state. That refetch shares `load`'s `seq` guard, skips the
skeleton, and on failure keeps the list and says so, because the stale card still offers the episode
just watched and a dated watch always inserts. No one-tap undated mark remains in the SPA: the
season grid's stills are read-only badges, and its only mark action is the season-level bulk button.

**Owned copies shipped too** — pulled forward on request; they belonged to no milestone (D-021).
`user_video_copies` records which formats/services a user holds an item on (`jellyfin`, `dvd`,
`bluray`, `uhd`, `physical`, `digital`) — another independent axis like the watchlist, never a
status. Copies live **per movie and per season**: a show is never itself "owned on DVD", so the
writable targets are `movie` and `season`, and `target_type: show` on `POST /video/copies` /
`DELETE /video/copies/{target_type}/{target_id}/{kind}` is a **bulk fan-out scope** — one row per
NON-SPECIAL season, Specials managed only from their own page — with `CopyResult.created` a COUNT,
not `TrackResult`'s bool. Adding a copy is a tracking action (materialises an untracked item,
D-015) and a copy row alone is a D-008 reference — `MovieReferenced`/`ShowReferenced` carry copy
arms, the show check defensively covering the CHECK-admitted-but-unwritable `show`/`episode`
targets like the season-rating arm. The details DTOs gained `copies` (`MovieDetails`,
`SeasonDetails`, per-season on `ShowSeason` — the first per-user field on both season shapes, `[]`
on live untracked views), and `EpisodeDetails` gained **`season_copies`**: an episode is never
itself owned, so its page BORROWS the owning season's, read-only. That fetch lives in
`seasonRenderState`, which both season-shaped views share, so the two can never disagree about it.
In the SPA, `WatchNowSection`'s "Your copies" tiles stopped being a demo: every tap opens
`ChoiceDialog.svelte` (WatchLogDialog's modal mechanics, parent owns the request), the season
page gained the section (streaming half hidden), and the show page's tiles show per-kind coverage
("3 of 5 seasons") with partial coverage offering both add-remaining and remove-all. The **episode
page** has the section too, in the component's `readonly` mode — sub-label "Your copies of this
season", no `aria-pressed` (the tiles toggle nothing), and a tap opens the same dialog in its INFO
shape: no confirm, just a link to the season page and a Close. That link is why an `actions` entry
may carry an `href`, rendered as an anchor with the router action — the SPA has no programmatic
navigation anywhere. `ChoiceDialog.svelte` is named for the general contract, not for copies: it
serves a confirmation, that info panel, and the up-next card's fork. An entry's `secondary` flag —
set by the **caller**, since only the caller knows which of its actions is the lesser one — outlines
it instead of filling it, so a fork reads as a choice rather than two equal commands.

**The Jellyfin library scan shipped too** — pulled forward on request; it belonged to no milestone
and reverses one line of D-021's "shelf label, not an integration" (D-033). When `JELLYFIN_URL` +
`JELLYFIN_API_KEY` are set (both or neither), the `jellyfin` copy kind stops being hand-set and
mirrors what the household's Jellyfin server holds. Six rules hold it:

- **The answer is stored ONCE, in the shared `jellyfin_library` table** (`target_type` movie |
  season, `target_id`, `jellyfin_id`, `seen_at`, no `user_` prefix) — a fact about the server,
  identical for every account, the `streaming` posture rather than the copies shelf. It is **not**
  a D-008 reference and **never** a `user_video_copies` row: `MovieReferenced` / `ShowReferenced`
  are unchanged.
- **The scan owns the kind while a server is configured.** `CopiesFor` / `SeasonCopiesFor` in
  `common/service_jellyfin.go` are what every details projection calls: the user's own kinds, a hand-set
  `jellyfin` row dropped, the scan's answer merged in (`MergeJellyfin` is the one rule).
  `assertAllowedKind` is a Service method now and refuses `jellyfin` with a 422 when synced, on
  add and remove, movie/season/show scopes alike. Unconfigured, nothing here runs and the label
  works exactly as before.
- **`jellyfin_synced` (bool) rides `MovieDetails`, `ShowDetails`, `SeasonDetails` and
  `EpisodeDetails`** — server config on the DTO like `streaming.region`; it is what makes the SPA
  render the tile read-only. `WatchNowSection`'s `managedKinds` prop: no `aria-pressed`, a "synced"
  caption, and a tap opens `ChoiceDialog` in its INFO shape with a link to `settings/jellyfin`.
- **A pure Jellyfin read plus a local join — never TMDB, never a tracking action.**
  `runJellyfinScan` pings, pages `/Items` for movies, series and episode FILES (`isMissing=false`,
  `Virtual` placeholders dropped), matches movies on `movies.tmdb_id` and seasons on
  `(show_tmdb_id, season_number)` through two compile-time whole-catalog maps in
  `jellyfin/store_jellyfin.go` (the `LibraryMovieFlags` pattern), and `replaceJellyfinLibrary` rewrites the
  table in ONE transaction — a failed scan leaves the previous answer standing. An untracked movie
  on the server is simply not matched until tracked. **A season is present once ANY of its
  episodes is**, Specials included, and the row's `episodes` column counts the DISTINCT episode
  files found, surfaced as `jellyfin_episodes` (nullable) on `ShowSeason` and `SeasonDetails`.
  The pages compare it with the season's **aired** episodes (the season page's `airedEps`, the
  show page's per-season `progress.aired_total`) and pass `incompleteKinds` to
  `WatchNowSection`: a lit tile is **amber** (`--warn`) when the season is partial, or when any
  non-special season of a show is absent or partial, and the accent only when everything is
  there. The server never decides "complete" — only the client knows the aired denominator it
  already renders.
- **Scheduled and on demand.** `jellyfinRunner` on `Service` (`jellyfinLoop`, the `tmdbSyncLoop`
  shape: timer + kick, started by `Run` only when configured, nil-safe on a hand-built test
  Service); `JELLYFIN_SCAN_INTERVAL` default `6h`, `0` = button only, parsed by the refresh jobs'
  parser. `GET /video/jellyfin` (status + last report, in memory like the restore job) and
  `POST /video/jellyfin/scan` (202 with the status; 409 while running; 404 unconfigured; any
  signed-in user). Presence, never playback — §9's scrobbling exclusion stands.
- **`provider.Jellyfin` in `server/provider/jellyfin.go`** is §5's seventh call site: the key rides
  the `Authorization: MediaBrowser Token="…"` header, never a URL; `redact` gained the
  `Token="…"` / `X-Emby-Token` arms; a 401/403 is `provider.ErrJellyfinAuth`, which deliberately
  does not wrap `ErrProviderUnavailable`.

SPA: `features/settings/JellyfinPage.svelte` (route `settings/jellyfin`, a section on
`SettingsPage.svelte`): not-configured hint naming the env vars, server name, schedule, the last
report, "Scan now" with 2 s polling while scanning. `DataSourcesPage.svelte` gained a Jellyfin
article (it is where every outside connection is disclosed, even to your own hardware).

**Online providers shipped too** — pulled forward on request; they belonged to no milestone
(D-022). `WatchNowSection`'s streaming half stopped being a demo: it renders TMDB's watch-provider
data (TMDB's own source is JustWatch, which the fine print credits) for **one** country. Four rules
hold it:

- **It rides the existing calls.** `watch/providers` is appended to `/movie/{id}` and `/tv/{id}`
  and parsed back out of the stored body, so it is **not** a sixth provider call site and a tracked
  item lists providers with TMDB down. `TVSeasonDetails` does NOT append `watch/providers` — a
  season has no listing of its own, it borrows the show's. It IS widened for the season's **own**
  `aggregate_credits` (the season page's cast) and `credits` (its regulars — the episode page's
  cast, beside each episode's own `guest_stars`, which the document carries anyway): TMDB returns a
  nested append as a sibling top-level key, which storing the season sub-document alone would
  discard, so the provider folds both into the stored season document (always written, `null` when
  TMDB sent none). That fold and the region prune are the two places a stored body is not TMDB's
  verbatim.
- **The snapshot is pruned to `TMDB_WATCH_REGION` at fetch.** TMDB sends every country — 111 of
  them, ~45% of a movie body — so `PruneWatchProviders` keeps one and writes a `sofalog_region`
  marker beside it. This is the ONE place a stored body is not TMDB's verbatim. The marker is
  load-bearing: an empty `results` cannot tell "nothing here" from "pruned elsewhere", so
  `watchProvidersStale` compares it to the configured region and a region change costs each item
  one automatic refresh on its next view. The region is a **server** setting, never per-user, and
  travels on the DTO (`streaming.region`) so an empty list can say *which* country is empty.
- **Seasons and episodes borrow the show's.** TMDB publishes providers per movie and per show only.
  `seasonRenderState` reads the owning show's row — a local read — and returns it as a **third**
  value beside `seasonUserState`, deliberately not inside it: it is catalog data, identical for
  every user. Both season-shaped views project that one call, so they can never disagree. An
  untracked show's season gets **nil**; fetching there would be a render-path provider call.
- **`StreamingFrom` returns a POINTER, and nil is not an empty list.** Nil = no listing to read
  (untracked show's season, a snapshot with no section, or one pruned to a different country whose
  refresh failed) → the SPA hides the section. Non-nil with `providers: []` = TMDB answered and
  named nobody → the SPA says "This show isn't on any streaming service in RO". Merging the two
  makes the UI either silent when it should speak or confidently wrong.
- **Logos go through the poster proxy into `data/cache`, never `data/assets`.** They are provider
  branding, not item artwork — filing a weekly-changing set under an item directory would put it in
  front of a reconciler that deletes everything its desired set does not name. One variant (`w185`,
  already whitelisted), like the achievement icons and video thumbnails.

`watchProvidersStale` is spliced into `movieSnapshotMissingSections` and — shows' first
missing-section probe, they had only time staleness — into the show details gate, so both a widened
append and a changed region reach existing rows **once**, on their next view, rather than waiting
out 48h.

**`ShowDetails`' two addressing forms fall through to ONE gate**, the shape `MovieDetails` already
had. They used to be separate branches with the staleness check on the `tmdb_id` side only, so a
show reached by its LOCAL id — how the watchlist links it, and how the show page re-reads itself
after every write — never refreshed at all: not on age, not on a widened append, not on a changed
region. Adding a second early return there re-breaks it; the regression tests name the id path
explicitly.

**Dropping a show shipped too** — the user-set **drop / un-drop** path, pulled forward from M2 on
request, because a show page that could mark and watchlist but never say "I gave up on this" left
`dropped` as a value the whole app could read and nothing could write. It is
`POST`/`DELETE /video/shows/{id}/drop`, and four rules hold it:

- **A state row exists if and only if the log holds an episode watch for the show, or the user
  already dropped it** — `common/status.go`'s `ensure` creates it on any episode watch (specials included)
  and its `collapse` removes it when the last one goes. So a **non-null `status` IS "at least one
  episode in the history"**, which is what gates the SPA's button and what the service's 422
  refuses. `progress.watched > 0` is the tempting wrong gate: it counts aired non-specials only
  (D-012), so a watched special or a marked unaired episode leaves it at 0 with a non-empty history.
- **The two writers live in `common/status.go`**, `SetShowDropped` and `ClearShowDropped` beside the
  recomputes — D-010's single-writer rule is about the **column**, not about the recompute, and
  `/verify` greps for it. `SetShowDropped` is an **UPDATE, never an upsert**: dropping must not
  fabricate a status for a show that was never started. `ClearShowDropped` lands on `watching`
  deliberately — a **landing value, not the answer** — because the un-drop then runs
  `RecomputeShowStatus` in the same transaction, and only `watching` is a status it acts on. That is
  what makes a show dropped after finishing come back as `watched`, and one whose watches were since
  deleted collapse to no row.
- **Neither direction is a tracking action** (contrast the watchlist and copies, D-015): you can
  only drop a show you already started, so the row is always local. Both routes are therefore
  **id-addressed with no `tmdb_id` form**, never call TMDB, and cannot 503.
- **The `DELETE` answers 200 with a `DropResult`**, not the 204 the watchlist, rating and copy
  removals use — the recomputed status is not derivable on the client, so it travels back. Both
  directions are idempotent and report `changed`. Up Next needed **no change**: `listUpNext` already
  ended with a `NOT EXISTS` on a `dropped` state row.
- **The state reads one rung down, as `show_dropped` on `SeasonDetails` and `EpisodeDetails`** —
  borrowed downward exactly as `streaming` and `season_copies` are, out of `seasonRenderState`'s
  one `ShowDroppedFor` read, so the two pages can never disagree. A **bool, not the status
  string**: those views deliberately report no show status and no show progress (that is the show
  page's job), and `dropped` is the one value that changes how they should read. Not to be confused
  with `ShowDetails.show_status`, TMDB's production status. `DroppedNotice.svelte` renders it
  read-only on both pages — drop/un-drop stays a show-page action, since both routes are
  id-addressed and neither page holds the show's local id — and it cannot go stale: `dropped` is
  sticky, so the one action reachable there (marking) leaves it set.

**Restoring a Ticket Box backup shipped too** — pulled forward on request; it belonged to no
milestone (D-023). Ticket Box is the user's own Trakt client; `POST /video/restore/ticket-box` takes
its **version-5** JSON export (history, ratings, watchlist, favorites, TMDB-addressed) and `GET`
the same path reports the job. sofalog reads the FILE — it never calls Trakt, and gains no provider. Seven rules
hold it:

- **Version 5 only, and the version probe comes BEFORE the strict decode.** `parseTicketBoxDump`
  reads `schemaVersion` leniently first so a v1 file gets "not supported, export again", then decodes
  with `DisallowUnknownFields` so an undefined field is a 422 naming it. Ticket Box and sofalog
  bump together (v3 dropped Trakt ids, v4 added `favorites`, v5 an ignored `imdbId`). Version 1 named episodes by
  the episode's own TMDB id, which nothing can resolve to a show; **no episode TMDB id column** was
  added — an episode entry is scoped by `showTmdbId`, then resolved by its own `tmdbId` FIRST through
  the show's stored season documents (`seasonDocumentsByShow` + `episodeResolver`, restore-only) and
  by `season` + `episode` only as the fallback: Ticket Box's positions are what it cached, and TMDB
  renumbers (Top Gear's specials moved 0x1 → 0x401). Every other path still addresses by position.
- **Replace = items the file has WATCHES for.** `deleteMovieWatches` / `deleteShowEpisodeWatches`
  (whole show, Specials included) run only for a group with history rows; a movie or show the file
  only rates, watchlists or favorites keeps its watches. Ratings upsert with the file's `ratedAt` as
  `updated_at` (a restore reproduces the source's record, unlike `MarkWatched`'s fresh-opinion
  `now`); watchlist and favorite rows insert-if-absent (favorites enqueue `SyncKindFavorite`). Re-running the same file converges.
- **Skip and report, never fail.** Season ratings (D-020), season/episode watchlist entries, missing
  ids, unparsable or future instants, unknown episodes, unknown or unreachable shows all become one
  `RestoreSkip` each with a stable reason; the list is capped at 200, the counts never are. Each
  skip carries the catalog `title` (movie's, else the show's — `enrichRestoreSkips`, local point
  reads once at finish, never a failure) and an `episode_not_found` a `hint` code +
  `tmdb_last_episode` from `episodeSkipHint` over the season's local numbering
  (`specials_numbering` | `season_missing` | `past_season_end` | `numbering_gap` — TMDB lists a
  two-parter as one episode). The SPA owns the sentences (`skipReasonExplanation`) and links each
  row by TMDB id to the movie / show / season page. A
  **breaker** trips after 3 consecutive provider outages: later groups needing a fetch skip as
  `provider_unavailable` with no call, groups already local still import.
- **Materialise outside the tx, one tx + at most one recompute per group, clock read per group.**
  `ResolveRestoreMovie`/`ResolveRestoreShow` go through `RefreshMovie` / `ResolveTrackedShow` (the
  ordinary tracking gate); the recompute runs iff the group had watches (a rating is not a watch,
  D-010); `today`/`now` come from `s.ClockFor` per group because a run can cross midnight. The
  file's instants move `watched_at` and nothing else; `NormaliseInstant` (shared with
  `resolveWatchedAt`) is the one rule for them.
- **The job is in memory, one per user, and outlives its request.** `restoreRunner` on `Service`;
  a second start while one runs is a 409; the goroutine runs on
  `context.WithoutCancel(r.Context())` (the request's cancel fires at the 202) under the runner's own
  cancel, which `Service.Run` pulls at shutdown (`failed` / `cancelled`). A restart forgets the job;
  committed groups stay; re-running is the recovery. No cancel route.
- **`httpx.DecodeJSONLimit` exists for this one body** (32 MiB, `restoreMaxBodyBytes` in the
  handler); every other route keeps `DecodeJSON`'s 1 MiB.
- **The SPA side is `features/settings`**: `RestorePage.svelte` (the chooser — Ticket Box, and a
  disabled "Sofalog dump" card) and `TicketBoxRestorePage.svelte` (file input, client-side
  `schemaVersion` pre-check, two-step confirm, 2 s polling, report). No provider disclaimer there;
  it links to Data Sources.

**Per-season progress and the episode-rating average on the show page's season grid shipped
too** — pulled forward on request. `ShowSeason` gained two derived, additive per-user fields:
`progress` (the show's three counts narrowed to one season, from `SeasonUserStatsByShow` in
`common/store_shows.go` — one `GROUP BY season_number` round trip beside `ShowProgress`, same
`SUM(CASE …)` + `EXISTS` shape; **Specials count their own episodes** here, because this is display,
not D-012's completion rule) and `user_rating` (the mean of the user's EPISODE ratings in the
season — a float, and a derived read, since a season is not itself rateable, D-020). Both are
**null for an untracked show**, which has no episode rows. The SPA hides the progress badge until
`watched > 0`; whether to show it is the client's call, the server always answers. `ShowDetails`
carries the show-wide pair too — `user_episode_rating` and `user_rated_episodes`, two scalar
subqueries in `UserShowFlags` — **server-computed** because a mean of the seasons' means would
weight seasons, not episodes; null / 0 when untracked. The display is `EpisodeAverage.svelte`,
shared by the season page's "Your rating" slot and the show page's card under its `RatingBar`.

**Similar titles shipped too** — pulled forward on request; they belonged to no milestone (D-024).
The movie and show pages' "Similar movies" / "Similar shows" rails render TMDB's **`recommendations`**
(not its keyword-matched `similar` list; the label stays "Similar"). Four rules hold it:

- **It rides the existing calls.** `recommendations` is appended to `/movie/{id}` and `/tv/{id}`
  beside `watch/providers` and parsed out of the stored body by `SimilarTitles` in
  `common/service_similar.go` — so it is **not** a sixth provider call site, and a tracked item's rail
  renders with TMDB down. `TVSeasonDetails` does NOT append it: seasons and episodes have no rail,
  and unlike `streaming` it is not borrowed downward.
- **Stored verbatim.** One page of at most 20 compact entries, kept byte for byte like `credits` —
  the season fold and the region prune stay the only two deviations from TMDB's body.
- **Every entry is a foreign title with no local row.** `SimilarTitle` carries `tmdb_id`, title,
  year, TMDB's rating and the two poster variants — no `media_type`, since a movie's list holds
  movies and a show's holds shows. Cards link by `tmdb_id` and posters go through the poster proxy
  into `data/cache`, **never** `data/assets`: they are not the item's artwork, and the reconciler
  would delete them.
- **`similar` is never null** (`[]` for "no section yet" and for "TMDB lists nobody" alike — the
  D-022 null distinction is deliberately not reproduced, since a rail reads the same either way).
  `movieSnapshotMissingSections` and `showSnapshotMissingSections` gained a `recommendations`
  clause instead, so every existing row refetches once on its next view. A test fixture that must
  count as complete now needs a `recommendations` key — `SeedHistoryMovie` has one.

The SPA side is `SimilarRow.svelte`, shared by both pages (props `items`, `kind`, `emptyNote`), the
cast rail's shape with the search card's link-and-badge contract; no "+N more" expander, since one
page is one rail.

**The scheduled TMDB refresh shipped too** — pulled forward from M2 on request. It is D-006's one
scheduler and SPEC §5's fourth call site, and it carries **two jobs** (D-025). There is **no last-run
state**: every hour (`refreshTick`, first tick a minute after boot) `runRefreshTick` in
`refresh/service_refresh.go` selects the items of each class whose `fetched_at` predates now minus that job's
interval — the row timestamp IS the schedule, so a restart self-heals and a page view that refreshed
an item merely postpones it. The **active** job (`TMDB_REFRESH_INTERVAL`, default `7d`) covers every
movie and show on **anyone's** watchlist plus followed shows TMDB still lists as in production
(`NULL` counts as in production); the **archive** job (`TMDB_ARCHIVE_REFRESH_INTERVAL`, default
`90d`) covers watched movies and followed shows marked `Ended`/`Canceled`, **neither on any
watchlist** — the two selections are disjoint, and a watchlist row always means the faster cadence.
`0` disables a job. Per movie it is the details view's own pair (`RefreshMovie` + `SyncMovieAssets`);
per show `RefreshShow` (root + NEW seasons) and then the show's **open seasons** only — the
highest-numbered one plus any non-special season with an undated or future episode, computed AFTER
the root refresh so a season it just materialised is not fetched twice; a closed season is never
re-fetched by the job. Selection SQL lives in `refresh/store_refresh.go`; a class picks a compile-time query,
never a spliced fragment. Every tick ends with the **status sweep** (`runStatusSweep`):
`RecomputeShowStatus` for every non-dropped (user, show) pair holding a state row or an episode watch
— D-010's whole-table pass, done as a loop over the scoped single writer so there is still exactly
one recompute. It is local, runs whatever the intervals say (both `0`, no TMDB credential), and is
what flips `watched → watching` when an episode aired overnight with nothing fetched. Guards: one
`ProviderBreaker` per tick shared by both passes (3 consecutive outages abandon fetching until the
next tick); a TMDB 404 keeps the row, never deletes, and silences that item for one interval in
memory; `Service.Run` starts the loop and waits for it at shutdown. No route, no table, no OpenAPI
change.

**The upcoming calendar shipped too, which closed M2** (D-026). `GET /video/upcoming` lists, in
calendar order, the dated things a user is waiting for: every unwatched, dated, non-special episode
of a show they **follow** (a non-dropped `user_show_states` row — which exists iff the log holds an
episode watch) **or have on their watchlist**, plus every unwatched movie on their watchlist whose
release date falls in the window. The two watchlist arms are the widening asked for on request;
SPEC §6 used to say "followed shows" alone. Six rules hold it:

- **The window is `[from, from + days)`**, `days` 1..366 (default 90), and **today is in**: today's
  unwatched episode is the most useful entry, and the `NOT EXISTS` on the log is what makes it leave
  once marked. That overlaps D-012's "aired" by one day on purpose — `listUpcoming`'s predicate is
  the **complement** of `EligibleEpisode`, not that constant, and must never be unified with it:
  one decides completion, the other display.
- **`from` is the user's day** (D-038). The server defaults to the user's today in their zone and
  the SPA sends `todayLocal()`, the same day. Completion, the status sweep and Up Next's `aired`
  measure "today" in that same zone, so the calendar's heading and the aired predicate agree.
- **Dropped wins over the watchlist**, as `listUpNext`'s `NOT EXISTS` on a `dropped` row does;
  **undated episodes and specials are out** — a calendar entry needs a date (Up Next surfaces an
  undated next episode with its Upcoming badge), and season 0 never gates completion.
- **One thin `UNION ALL` in `upcoming/store_upcoming.go`**, identity columns only, ordering owned by SQL:
  the case-folded title is emitted as a `sort_name` result column because a compound SELECT may only
  `ORDER BY` result columns on both engines; `type` picks one of three compile-time constants, never
  a spliced fragment. Projection reuses the history feed's `NewHistoryEpisode` (season-poster-then-
  show fallback, `season_id` handle) and the movie asset linker, so a card's poster has one
  definition.
- **The API is flat and the SPA folds** (`history/service_history.go`'s convention): `upcoming.js` keeps
  the server's date order and collapses a same-show, same-season, same-day run (a whole season
  dropped in one day) into one card linking the season page; single episodes link by
  `season_id` + `number`, movies by id. The card is `UpcomingCard.svelte` — the app's poster card
  in the `--poster-min` grid, kind and markers stacked top-left like Up Next's badges, and **the
  date bottom-right** where the History card keeps its score ("Today"/"Tomorrow" in the accent).
  There are **no day headings**: one grid, every poster wearing its date. The page is
  `UpcomingPage.svelte` (nav "Upcoming", route `video/upcoming`).
- **Never a provider call site**: a pure local read, so it cannot 503. `/verify`'s SQL-location
  grep matches prose too, so `upcoming/service_upcoming.go` and the handler carry no `SELECT … FROM` in
  comments.

`format.js` gained `localDayOf` / `todayLocal`, the one definition of the user's day that the show
page, the season page and `WatchLogDialog` share (each used to hand-roll it), plus
`formatCalendarDay` / `daysFromToday` for the day headings. Both now delegate to `src/lib/time.js`
(D-038, below).

**Per-user time zones shipped too** — on request; they belonged to no milestone (D-038). Every
instant was already stored as RFC3339 UTC; what was added is **one zone per user that decides both
display and the server's "today"**. Five rules hold it:

- **`users.timezone`** (nullable IANA name) is set by `PUT /auth/users/{id}/timezone` — **self
  only**, like the matching consent — and NULL follows **`DEFAULT_TIMEZONE`**. The session probe's
  `User` carries `timezone` and the resolved **`effective_timezone`** (`auth.Service.present`);
  the SPA never computes the fallback.
- **`Core.ClockFor(ctx, userID)` in `common/service_zone.go` is the one definition of a user's
  day**: one clock reading, `Now` UTC RFC3339 for storage, `Today` formatted in the zone. Nothing
  in the video feature formats a `DateOnly` from a bare UTC instant any more — every former
  `s.Now().UTC().Format(time.DateOnly)` (watches, drop, show details, Up Next, upcoming, discover,
  restore) goes through it. The status sweep reads each pair's zone in `showStatusTargets`' own
  query and calls `ClockIn`; the refresh job's open-season check has no user and uses
  `DefaultLocation()`. A hand-built test `Core` (nil `DefaultZone`) reads as UTC.
- **`BulkMarkShowWatched`'s air-date modes are a SELECT + per-row insert**, not
  `air_date || 'T00:00:00Z'`: `tz.Midnight` stamps midnight in the user's zone. `now`/`fixed` keep
  the one `INSERT … SELECT`. Same `EligibleEpisode` splice, same `NOT EXISTS`.
- **`server/tz`** is the one rule for a zone name (`tz.Load` refuses `""` and `Local`, caches, and
  embeds `time/tzdata`), shared by config, auth and video.
- **SPA: `src/lib/time.js`** renders every instant in `effective_timezone` (`formatInstant`,
  `zonedFormat`), derives the day (`dayOf`, `today`), and converts the watch dialog's fields
  (`wallTimeToInstant`). **Never** `toLocaleString`, `getHours`, `getDate` or
  `new Date(y, m, d, …)` on an instant — they read the device's zone. Plain `YYYY-MM-DD` dates are
  formatted anchored at UTC midnight with a UTC formatter (`formatDay`), so no zone moves them. The
  Account page's "Time zone" card saves on change like the matching switch.

**A person details page shipped too** — pulled forward on request; it belonged to no milestone
(D-027). Every cast card whose `CastMember.tmdb_id` is non-null — on all four detail pages, guest
stars included — is a link to `video/person-details?tmdb_id=`; a null id (a stored body without one;
TMDB always sends it) renders the same card as a plain block. `GET /video/people/details` is the
details views' rule with **no tracked half**: no table, no snapshot, no per-user state, so every view
is live from TMDB (`/person/{id}` + `combined_credits,external_ids`), stores nothing, and 503s on
any outage — the one read route that always can. The filmography is marked against the library by
`LibraryMovieFlags` / `LibraryShowFlags` in `common/store_library_flags.go`: two **compile-time** whole-catalog
queries mapped in Go by `tmdb_id`, never a spliced `IN` list; `watched` keeps the watchlist page's
per-kind meaning (movie: any watch; show: status `watched`), and `in_history` is the looser "any
watch in the log" (show: any episode watch, read from the log through `episodes`, not from the
state row) — what the page's **"Seen with"** grid above the filmography filters on, client-side,
same order. Credits dedupe on
(`media_type`, `tmdb_id`) with parts joined " / ", `episodes` is MAX not SUM, cast only, undated
first then newest first. The portrait is `w185` / `h632` (TMDB's profile ladder — `h632` joined
`proxySizes`) through the cache proxy only; a person owns no `data/assets` directory. SPA:
`PersonDetailsPage.svelte` on `DetailHero`, `CreditCard.svelte` for the grid (kind chip, "In
library" chip, ✓, link by local id when held else by `tmdb_id`), the Movies/Shows filter client-side.

**Matching watchlists shipped too** — pulled forward on request; they belonged to no milestone and
are the one opt-in carve-out from "no sharing" (D-028). `users.match_watchlist` (`NOT NULL DEFAULT
0`) is the consent flag: written by `PUT /auth/users/{id}/match-watchlist` in **auth**, **self
only** — no admin bypass, unlike display-name and email, because consent is not given on someone's
behalf — and carried on the session probe's `User` as `match_watchlist`. The video feature only
**reads** the column. `GET /video/watchlist` takes a **repeated** `match_user` (never comma-joined,
at most 10) and returns the **intersection**: `listWatchlistTargets` appends the compile-time
`matchOne` EXISTS fragment once per id with one bound arg — the `oneType` idiom repeated, not a
spliced `IN` — and that fragment joins `users` on `match_watchlist = 1`, so the privacy rule holds
in SQL even if the service check were removed. The service still 422s a self, duplicate, unknown or
non-sharing id, so a user who opted out after being picked surfaces as an error the SPA acts on
(drop the id, re-read the peers), never as a silently empty list. Entries stay the **caller's**:
their `added_at`, `user_rating`, `watched`; the DTO is unchanged. `GET /video/watchlist/match-users`
lists the OTHER opted-in accounts as `MatchUser{id, label}` — the label is `matchLabel`'s D-019
fallback (display name, else the email's local part) applied in the store so **no email leaves the
server**; the admin user list stays admin-only. SPA: `AccountPage.svelte`'s "Matching watchlist"
switch (saves on change, unbound checkbox, session refresh), `WatchlistPage.svelte`'s picker inside
the filters panel with a count badge on the collapsed toggle and a "Nothing you and Bob both have"
empty state.

**One-way rating and watchlist sync to the user's own TMDB account shipped too** — pulled forward
on request; it belonged to no milestone and is the product's first **write** to a provider, SPEC §5's
sixth call site (D-029). Seven rules hold it:

- **The link is the consent; the setup is the opt-in (D-036).** `user_tmdb_accounts` (one row per
  user: TMDB's long-lived `tmdb_session_id`, account id, username, `linked_at`, `broken_at`, plus
  `setup_done` and the three switches `sync_ratings` / `sync_watchlist` / `sync_favorites`) is the
  user's account; **linking queues nothing**. It is made through TMDB's approval page —
  `GET /video/tmdb/link/start` (302 to TMDB, sets the `sofalog_tmdb_link` flow cookie) and
  `GET /video/tmdb/link/callback` (TMDB returns `request_token` + `approved`/`denied`; always a 302
  to `settings/tmdb?tmdb_link=ok|denied|expired|provider_unavailable|failed`, never JSON at a
  browser). The redirect URI is `config.TMDBLinkRedirectURI()`, from `BASE_URL` alone. Both routes
  need the session cookie (`SameSite=Lax` is sent on TMDB's top-level GET back); they are **not** in
  auth's `publicAPI`. `GET /video/tmdb/link` (status, one shape linked or not) is a pure local read;
  `DELETE /video/tmdb/link` unlinks, idempotent.
- **The outbox fills in the writer's transaction; the kick comes AFTER commit.** `EnqueueTMDBSync`
  in `common/store_tmdb_sync.go` takes a `kind` (`SyncKindRating` | `SyncKindWatchlist`) and is called
  beside `UpsertVideoRating`/`deleteVideoRating` in `Rate`, `RatingRemove`, `MarkWatched` (when a
  rating rides along), beside `InsertWatchlist`/`deleteWatchlist` in `WatchlistAdd`/`WatchlistRemove`,
  beside `InsertFavorite`/`deleteFavorite` in `FavoriteAdd`/`FavoriteRemove` (`SyncKindFavorite`,
  D-030), and beside both in the two restore groups. **The link guard AND the kind's switch live in
  the SQL** (`WHERE EXISTS … user_tmdb_accounts … broken_at IS NULL AND (… sync_<kind> = 1)`), so an
  unlinked or broken user, or one who turned that kind off, writes nothing even if a caller forgot
  to ask. `UNIQUE(user_id, kind, target_type, target_id)` + `ON CONFLICT DO UPDATE`
  means one row per (kind, target) holding the current intent. `KickTMDBSync()` is nil-safe and is
  never called inside `InTx`.
- **The watchlist mirrors automatically while its switch is on, exactly like ratings.** A
  per-action "also on TMDB?" chooser was built and removed in the same change: the setup answers
  the question per kind, once. The
  worker sends a `watchlist` row through `provider.SetWatchlist` → `POST /account/{id}/watchlist`
  (`media_type` `tv` for a show), using the `tmdb_account_id` the link stored. The watch **history**
  is never mirrored. A `favorite` row goes through `provider.SetFavorite` → `POST
  /account/{id}/favorite`, the same shape with the flag renamed.
- **`updated_at` is the replace marker.** `settleTMDBSyncDone` and `deferTMDBSync` are guarded
  `WHERE id = ? AND updated_at = ?`, so a rating rewritten while its row was in flight survives to
  be sent again. Never settle by id alone.
- **The worker is `tmdbSyncRunner` on `Service`**, started by `Run` beside the refresh loop, nil on
  a hand-built test Service. `RunTMDBSyncBatch` is synchronous and report-returning (tests drive it
  without the loop): success deletes the row; an outage defers with backoff (30 s doubling to 1 h,
  no terminal state — an outage is not the row's fault) and counts against a per-batch
  `ProviderBreaker`; `provider.ErrSessionInvalid` sets `broken_at`, discards that user's rows and
  skips them for the rest of the batch; a TMDB 404 or 400 drops the row for good. **The setup's
  Push** queues the backfills of the chosen kinds (`enqueueRatingBackfill` — movie/show/episode
  only, seasons never, D-020 — `enqueueWatchlistBackfill` and `enqueueFavoriteBackfill`),
  deliberately **without** a switch guard: a Push with its switch off still sends once. Re-linking
  the same account clears `broken_at` and keeps the setup; a different account id resets the four
  columns and empties the outbox. Unlinking deletes the link and the user's rows, cancels a running
  pull, and revokes the session; **TMDB's ratings are never deleted**.
- **The setup is `SetupTMDBSync` (`POST /video/tmdb/link/setup`, 202) and the switches are
  `SetTMDBSyncKinds` (`PUT /video/tmdb/link/sync`)**, both answering `TMDBLinkStatus`, which
  gained `setup_done`, `sync` and `pull`. Per kind the body says `pull` | `push` | `skip` plus
  `sync`; switches and push rows land in ONE tx, the kick comes after. A switch turned **on never
  backfills**; one turned **off deletes that kind's pending rows** (`deleteTMDBSyncKind`) in the
  same tx. 404 unlinked, 409 broken / already set up / pull running, 422 bad action. **The pull is
  `tmdbaccount/service_tmdb_pull.go`'s job** — the restore runner's shape copied (`tmdbPullRunner`, one per
  user, in memory, cancelled by `Run` and by `UnlinkTMDB`): it pages the five account lists
  (`provider.AccountRated*` / `AccountWatchlist` / `AccountFavorites`, §5's one read of an
  account), resolves each item OUTSIDE any tx through `ResolveRestoreMovie` / `ResolveRestoreShow`
  (+ `GetEpisodeByShowKey`) with one `ProviderBreaker`, writes one tx per item — `UpsertVideoRating`
  with `roundTMDBRating` (half up, clamped 1..10) OVERWRITING, `InsertWatchlist` / `InsertFavorite`
  insert-if-absent — and **never calls `EnqueueTMDBSync`**. A refused session marks the link
  broken as the worker would; an unreadable list fails the job (`cancelled` | `session_invalid` |
  `provider_unavailable` | `internal`); `setup_done` is written at the job's END, so a failed pull
  leaves the setup panel to be re-run and a re-run converges. `TMDBLinkStatus.pull` hides a job
  older than the current link.
- **`provider.send` is the one request helper** (`get` is `send` with the method fixed). It maps a
  401 to the existing key error UNLESS the request carried `session_id` (or was the session
  exchange) and TMDB's `status_code` is not 7 — then it is `ErrSessionInvalid`, which deliberately
  does not wrap `ErrProviderUnavailable`. The authenticate URL is built in `provider`
  (`AuthenticateURL`), because `/verify` forbids `handlers*.go` from naming the TMDB host.
- **The session id is a secret like the key**: stored as issued (no app secret exists), never in a
  DTO, and `redact` has `session_id` / `request_token` arms (query and JSON-body forms). Errors name
  the path, never the session.

SPA: `features/settings/TMDBAccountPage.svelte` (route `settings/tmdb`; the Link button is a plain
anchor to `tmdbLinkStartUrl()` — a navigation, not a fetch; reads `?tmdb_link=` once for the
banner; inline two-step Unlink; "Link again" when broken). While `linked && !setup_done` it renders
the **setup panel**: per kind a Pull / Push / Skip segmented control (the Trending page's) with a
one-line consequence and a "keep in sync" checkbox (pre-ticked by pull/push, cleared by skip), one
Confirm; a running pull polls `getTMDBLink` every 2 s (the restore page's `$derived.by` pattern)
and shows progress, then the report; a failed pull shows its sentence above the panel again. Once
`setup_done`, a **"Keep in sync" card** holds the three switches — unbound native checkboxes
saving on change through `setTMDBSync`, reverted on failure (the account page's matching-watchlist
pattern). A "TMDB account" section on `SettingsPage.svelte`, and `getTMDBLink` / `unlinkTMDB` /
`setupTMDBSync` / `setTMDBSync` / `tmdbPullErrorMessage` / `tmdbLinkStartUrl` /
`tmdbLinkResultMessage` in `features/settings/api.js`. No provider disclaimer on that page — it
links to Data Sources.

**Favorites shipped too** — pulled forward on request; they belonged to no milestone (D-030).
`user_video_favorites` is a **third independent axis** for movies and shows, the watchlist's table
byte for byte, and it carries the watchlist's weight everywhere the watchlist is read. Five rules
hold it:

- **It is a tracking action and a D-008 reference.** `FavoriteAdd` goes through
  `ResolveTrackedTarget` with `FavoriteTargets` (`movie`, `show`), so an untracked `tmdb_id`
  materialises the movie or the whole show and syncs its asset set; `MovieReferenced` and
  `ShowReferenced` carry a favorites arm (a show is referenced **ten** ways). `FavoriteRemove` is
  idempotent, never a provider call, and keeps the row.
- **It is the ACTIVE refresh cadence (D-025).** `DueMovies`/`DueShows`' active arms are
  `EXISTS watchlist OR EXISTS favorites`; the archive arms carry `NOT EXISTS` on both, so the two
  classes stay disjoint. Favorites do **not** feed the upcoming calendar.
- **It mirrors to TMDB (D-029).** `SyncKindFavorite` is the outbox's third kind; `EnqueueTMDBSync`
  is called beside `InsertFavorite`/`deleteFavorite`, the worker sends it through
  `provider.SetFavorite` (`POST /account/{id}/favorite`), and the setup's Push backfills favorites
  beside ratings and listings (D-036). The `user_tmdb_sync.kind` CHECK admits `favorite`.
- **The list shares the watchlist's code, not a copy of its rules.** `GET /video/favorites` takes
  `order_by`/`order`/`type`/`limit`/`offset` (no `match_user`), parsed by `parseListPage` — the
  one parser both `Watchlist` and `Favorites` call — and projected by `projectListEntries` through
  the same two card helpers; `FavoriteEntry` is a type alias of `WatchlistEntry`. Only
  `listFavoriteTargets` in `lists/store_favorites.go` is the watchlist query's twin. Routes:
  `GET`/`POST /video/favorites`, `DELETE /video/favorites/{target_type}/{target_id}`.
- **`favorited` rides `MovieDetails` and `ShowDetails`** (additive bool, out of `UserMovieFlags` /
  `UserShowFlags`), which is what the pages' heart initialises from.

SPA: `FavoritesPage.svelte` (route `video/lists/favorites`) is `WatchlistPage.svelte`
without the matching picker, reusing `WatchlistCard`; the movie and show pages gained a heart
toggle beside the bookmark, both wearing `detail-page.css`'s `.icon-toggle` (the class the bookmark
used to own as `.watchlist-btn`). The movie toggle is optimistic, the show toggle awaited, exactly as
their watchlist toggles are and for the same reason.

**A Lists hub shipped too**, on request: the nav's Watchlist and Favorites entries folded into one
**"Lists"** entry (prefix-active, like Settings). `ListsPage.svelte` (route `video/lists`) renders
one banner card per list from `GET /video/lists` — `ListSummary{key, movies, shows, backdrops}`,
both lists always present in fixed order, counts from `countListTargets` in `lists/store_lists.go` (one
`UNION ALL` carrying the list queries' dangling-row guard, so a count agrees with its page), and up
to four backdrops at both sizes (`backdrops` / `backdrops_full`, parallel arrays) picked off the
newest dozen entries through the details views' linker. A pure local read. The two pages moved to `video/lists/watchlist` and `video/lists/favorites` and
carry a "← Lists" back link; the card's title, glyph and link are the SPA's, keyed by `key`.

**The hub gained a third card, "Dropped"**, on request. `GET /video/dropped` lists the user's
dropped shows and is **a view over `user_show_states` rows with `status = 'dropped'`, not a
table** — un-dropping takes a show off, and nothing new writes. Shows only (a movie cannot be
dropped). `listDroppedShows` in `lists/store_dropped.go` returns `watchlistTarget` rows with `AddedAt`
= the state row's `updated_at`, which IS the drop instant (`SetShowDropped` stamps it and no
recompute touches a dropped row), so the service reuses `parseListPage` (no `type` param) and
`projectListEntries`; `DroppedEntry` is another alias of `WatchlistEntry`, `watched` always
false. `countListTargets` gained a `dropped` arm and `ListSummary.key` the value. SPA:
`DroppedPage.svelte` (route `video/lists/dropped`) is `FavoritesPage.svelte` without the kind
filter; `WatchlistCard`'s optional `addedVerb` prop makes its sub-line read "Dropped …".

**Trending and Discover lists shipped too** — pulled forward on request; they belonged to no
milestone and are the **eighth provider call site** (D-034): `GET /video/trending`
(`window=day|week`, `type`, `page`), `GET /video/discover` (`type`, `sort`, `page` and the
filters `vote_min`, `vote_max`, `votes_min`, `language`, `year_from`, `year_to`, repeated `genre`
and `provider`) and `GET /video/discover/options`, behind the Trending page (`TrendingPage.svelte`,
route `video/trending`, nav "Trending", tabs Trending / Discover). Seven rules hold it:

- **Live on every view, stores nothing, 503 on any outage** — D-027's rule applied to a list. No
  table, no snapshot, no scheduler, no list cache; `discover/service_trending.go` is validate → fetch → two
  local reads → project, and nothing in it writes. A trending list has no local data to be over,
  which is why it is not a render-path violation. Up Next stays a pure local page; do not put a
  rail there.
- **`TrendingEntry` is `SearchResult` embedded plus `id`, `watched`, `in_history`** — the person
  page's library marks out of `LibraryMovieFlags` / `LibraryShowFlags` (`common/store_library_flags.go`), joined
  by `tmdb_id`, never a second call. `id` non-null means the catalog holds it and the card links by
  local id. **No new SQL** was added for this. Both routes answer **`TrendingPage`**
  (`entries`, `page`, `total_pages`) — TMDB's own paging envelope, the spec's one paged object;
  the SPA's "Load more" asks for `page + 1` and appends, ranks continuing by array index.
- **Trending `all` is ONE call to `/trending/all`** and keeps TMDB's cross-type order, dropping
  `person` rows (so a page can be short of 20); a single kind calls its own list, and the kind
  asked for is the fact, not the row's discriminator. **Trending takes no filters** — TMDB offers
  none there, and filtering after the fetch would thin every page; do not add them.
- **Discover is `/discover/{movie|tv}` with TMDB doing the filtering.** `provider.DiscoverQuery`
  is kind-neutral; `provider.Discover` owns TMDB's per-kind parameter names
  (`primary_release_date.*` vs `first_air_date.*`, the `newest` sort likewise). Genres are
  comma-joined (AND), providers pipe-joined (OR). `discoverQuery` in the service owns every 422 and
  two defaults: `sort=rating` without `votes_min` sends `vote_count.gte=200`, and `sort=newest`
  caps the date at the user's today (D-038). **`all` is both kinds' page merged by the sort's own key
  (`discoverKey`) and NOT capped** — up to 40 — through `mergeByKey`, the helper Search's `all`
  also uses before applying its own `searchResultCap`.
- **The streaming filter is the ONE request that sends `TMDB_WATCH_REGION` to TMDB**, as
  `watch_region`, exactly when a `provider` is given (and the option list's own
  `/watch/providers` read). D-022's rule is about item fetches and still holds there.
- **Option lists are cached in the provider like `/configuration`** (`Genres`, `Languages`,
  `WatchProviders`, success only, under `c.mu`) and projected by `DiscoverOptions`: languages
  sorted by name with `xx` dropped, providers movie ∪ tv by id in display order, logos through the
  poster proxy (cache, never `data/assets`). Their 503 leaves the grid working; the SPA says so.
- **No disclaimer on the page** — a footnote links to Data Sources, whose TMDB article names the
  lists and the region-carrying request. No `rank` field (the SPA numbers cards).

SPA: `getTrending` / `getDiscover` / `getDiscoverOptions` in `features/video/common/api.js` (repeated ids
via `qs.append`, the `match_user` idiom); the card is `CreditCard.svelte` widened to
`PersonCredit | TrendingEntry` with an optional `rank` badge bottom-left — the person page's card
was already the contract (local-id link, "In library", ✓), so it gained a second caller rather
than a twin. The filter panel is WatchlistPage's (`filters-toggle` with a count badge, full-width
checkbox pickers); the Load more row is HistoryPage's (`seq` capture, scroll pin, `overflow-anchor:
none`). Genre options follow the type filter, the union by id under `all`.

**Statistics shipped too** — pulled forward on request; they belonged to no milestone and reverse
one word of SPEC §9 (D-039). `GET /video/stats?period=all|YYYY` answers the whole Stats page
(`StatsPage.svelte`, route `video/stats`, nav "Stats") as ONE object; the package is
`features/video/stats`. Seven rules hold it:

- **Your own numbers only.** Every query filters on the user; nothing is compared across accounts
  (§9's rule stands, D-028's carve-out is not widened). A pure local read — never a provider call,
  never a 503. "You vs the crowd" reads `vote_average` / `vote_count` out of the stored
  `metadata_json` (≥ 50 votes to compare, ≥ 5 compared to show), never TMDB.
- **The server computes, the SPA renders.** Six queries in `stats/store_stats.go` — bounds
  (first play / first rating / plays this year), ONE bulk read of the period's plays with the
  movie / episode / show facts joined per row (the log is the LEFT side; the fan-out rule is about
  joining the log onto something), the show bodies for the episode-runtime fallback, the ratings,
  the rated titles' bodies, and the followed shows' derived progress (`EligibleEpisode` spliced
  verbatim, driven from `user_show_states` so a show with 0 aired episodes is still "tracked") —
  then everything is folded in Go. No table, no counters (D-010 holds).
- **Every bucket is in the user's zone (D-038).** The period is the user's calendar year,
  `tz.Midnight` on 1 January, half-open; months, heatmap days, weekdays, hours and streaks come
  from the instant read in `ClockFor`'s zone; day arithmetic runs on the named days in UTC so DST
  cannot stretch one. A play at EXACTLY local midnight is excluded from the time-of-day chart —
  the bulk `episode_air` / `season_air` modes stamp that instant.
- **No `period` = the server's default**: the current year if it holds ≥ 1 play (ratings never
  decide), else `all`; the applied period is echoed with `years` (first play or first rating
  through the current year, newest first, `[]` when nothing). A year outside the list is computed
  and empty, never a 422; a malformed value is one.
- **Null means hidden.** Every section is always present; the SPA hides a null section or an
  empty list and decides nothing else (`completion` is null in a year — the state table has no
  dates; `current_streak` null in a past year; `tracking_days` all time only; `active_days` a year
  only). Every title reference is one `StatsTitle` (`kind`, local `id`, title, year, poster and
  backdrop at both sizes through the asset linker's `Poster` / `Backdrop` pair methods); an episode
  refers to its show.
- **Ticket Box deviations are deliberate**: every history row is a play (no check-ins);
  "episodes" = distinct episodes played in the period in both modes; per-kind rating averages are
  movie / show / episode (D-020); genres count movies AND shows over the period's distinct played
  titles; the trend delta is rounded to one decimal BEFORE its verdict so the number and the
  sentence agree.
- **Chart.js is the SPA's one chart dependency** (`stats/chartjs.js` is its only importer,
  dynamically imported by `StatsBarChart.svelte`), themed purely through its options from the app's
  CSS variables read at mount; the heatmap, the TV/movies split and the genre and completion
  meters are plain CSS. The "Rate" action in "Waiting for your rating" is `RateDialog.svelte`
  over the existing `rateMovie`, then a silent refetch of the page.

A "mark all previous watched" bulk action was dropped by decision and is out of scope (SPEC §9); do
not build it.

Three rules M1.5 established that are easy to break:

- **`seasons.show_id` is `NOT NULL`, and a season VIEW of an untracked show writes nothing.** A
  season row exists **if and only if** its show is tracked — only tracking materialises one, exactly
  as for movies, shows and episodes (D-015). The single write a view may cause is a **tracked**
  show's missing season: `resolveSeasonRow` finds the show, then `RefreshSeason` links the season
  and writes its episodes in one transaction (the show refresh's new-season branch does the same
  from the other page). `UpsertSeasonByTMDBKey` takes `ShowID` as a parameter for exactly this
  reason — the link is a caller obligation, not a runtime constraint violation. Relaxing the column
  to nullable reintroduces materialise-on-view through the side door.
- **A show's whole asset subtree is one unit**, filed `media_type='show', item_id=<shows.id>` —
  season posters and episode stills included. The reconciler deletes anything on disk that its
  desired set does not name, so a per-season diff against a whole-subtree listing would delete the
  other seasons. `asset_jobs`' `season` and `episode` media types stay unused for video.
- **`EligibleEpisode` in `common/status.go` is D-012's predicate, spliced verbatim** into the recompute,
  the bulk insert and up-next's `only_released` filter (never its pick — see above). If they
  disagree by one row, a show reads *watched* while still offering a next released episode.

**Seasons are not rateable** (D-020) — movies, shows and episodes are. The refusal is one entry in
`RateableTargets`; the `CHECK` deliberately keeps the value, because narrowing it is irreversible
once the baseline freezes.

The **watchlist page** (`GET /video/watchlist`, `WatchlistPage.svelte`) lists **movies and shows
interleaved**, and three rules hold it together:

- **Ordering and paging have one definition, in SQL.** `listWatchlistTargets` LEFT JOINs both
  catalog tables — each join guarded by its own `target_type`, so the arms cannot cross — and
  `COALESCE`s the sort keys, `first_air_date` standing in for a show's release date. The
  `IS NOT NULL` pair drops a dangling `target_id` **before** `LIMIT`. Never merge two per-kind
  pages in Go: `LIMIT`/`OFFSET` cannot be redone after the fact.
- **The tie-break is the pair, never the id.** Both the `ORDER BY` tail and the SPA's `{#each}` key
  carry `target_type` alongside the id, because movie 5 and show 5 both exist (D-013) — a bare id
  is a duplicate key the moment both kinds are listed.
- **`watched` means something different per arm.** A movie: any watch in the log. A show:
  **completed** — the stored status reached `watched`, i.e. every aired non-special episode (D-012).
  A ✓ on a show card is "finished", not "started".

## Commands

```
Backend dev     cd server && go run .
Backend gate    cd server && gofmt -l . && go vet ./... && go test ./...
Frontend dev    cd frontend && npm run dev   # Vite :5173, proxies /api to :8080
Frontend gate   cd frontend && npm run check && npm run lint
Full build      cd frontend && npm run build && cd ../server && go build .
Everything      /verify
```

## Structure & layering

Code is organised **per feature** (D-014, D-037). The Go module is `server/` (module
`mbyte.ro/sofalog` — imports read `mbyte.ro/sofalog/features/video/upnext`); the SPA is
`frontend/` with `src/features/<component>/<feature>/`. There is **no `internal/` and no `cmd/`** —
`package main` is `server/main.go`.

- Media components live under `server/features/`: `features/video` (movies + shows together) and
  `features/games`. `server/auth` is also a feature (same layering) but stays **top-level** — it
  is cross-cutting. Shared infrastructure: `server/{config,httpx,db,provider,assets,web}`.
  Components depend on shared packages, **never on each other**. (Full tree and contracts: the
  `go-backend` skill.)
- **A component is split into one Go package per feature** (D-037): `features/video/upnext`,
  `…/upcoming`, `…/history`, `…/watches`, `…/ratings`, `…/lists`, `…/copies`, `…/jellyfin`,
  `…/details`, `…/people`, `…/discover`, `…/media`, `…/restore`, `…/tmdbaccount`, `…/refresh`.
  **A feature package imports only `video/common`** (plus shared infrastructure) — **never a
  sibling feature**; anything two features need moves into `common`. `/verify` greps for it.
  Only test files may import a sibling (to set up state through another feature's service).
- `video/common` holds what features share: the `Core` struct (the dependencies — `DB`, `TMDB`,
  `Now`, … — plus server-wide settings) whose methods are the shared catalog operations
  (tracking gate, snapshot refresh, asset sync), the catalog stores, `common/status.go`, the
  shared DTOs and card helpers. Every feature's `Service` embeds `*common.Core`, so `s.DB` and
  `s.RefreshMovie(…)` read the same in every package. `video` itself (`video.go`) is wiring only:
  it builds the `Core` and every feature, registers each feature's routes and runs the
  background loops. `video/videotest` is test-only support (DB, seeds, provider stubs) imported
  only from `_test.go` files.
- Inside each package, layering is a **file convention**, direction strictly one way:
  `handlers*.go` → `service*.go` → `store*.go`. Each feature declares its `Service` and
  `NewService` in `service.go`, and its `Handler` and `RegisterRoutes` in `handlers.go`.
- All domain logic lives in the feature's `service*.go` files. Handlers decode → validate → call
  **one** service function → encode. A conditional about domain rules inside a handler is a bug:
  move it to service. `service` and `store` code never touches `net/http`.
- SQL appears **only** in `store*.go` files (plus `server/features/video/common/status.go`, the
  status single-writer, and the `videotest` seed helpers); only `server/db` imports a database
  driver.
- SQL must run unmodified on **both** SQLite and Postgres. SQLite is primary, WAL mode on. No
  dialect-specific functions or types in queries.
- Every change to `/api/v1` lands with the `api/openapi.yaml` update **in the same commit**. Never
  break an existing `/api/v1` response shape — add a field, or add a version.
- Adding a Go module or an npm package requires asking first.

## Data model

- **Separate tables per media type**: `movies`, `shows`, `seasons`, `episodes`, `games`.
  There is **no** shared `items` table and **no** `media_type` discriminator column (D-001).
- **Catalog** tables hold one canonical row per real-world item, shared by all users, and **never**
  contain `user_id`, a status, a rating, or a personal date. They keep bare names.
- **Every** per-user table is prefixed `user_` (`user_watch_history`, `user_show_states`,
  `user_video_ratings`, …). The prefix is how you tell private state from catalog at a glance.
- Ids are `INTEGER PRIMARY KEY` throughout, and therefore **unique only within their own table** —
  movie 5 and episode 5 both exist (D-013).
- Every store query touching personal state filters on the authenticated user's id. No endpoint may
  return another user's state, ever.
- **Polymorphic columns** (`user_watch_history`, `user_video_ratings`, `user_video_watchlist`,
  `user_video_favorites`, `user_video_copies`, `user_tmdb_sync`, `asset_jobs`, `pending_deletions`) carry `target_type` + `target_id`. **The pair identifies the
  target; the id alone identifies nothing** — every join or predicate through one must carry its
  `target_type`, or a watch on movie 5 makes episode 5 look watched. They also **cannot have a foreign
  key**, so every delete path removes those rows explicitly in the same transaction.
- **`user_watch_history` is many-per-target** — a rewatch is a new dated row (D-009). It fans out, so **never `LEFT JOIN` it**; use `EXISTS`/`NOT EXISTS`. Marking
  watched is idempotent; logging a rewatch is a separate operation.
- **Deleting a watch recomputes status in the same transaction**, exactly as marking one does — that
  is what makes the recompute functions' collapse branches reachable, and why removing the last
  watch correctly un-watches a movie. The delete is scoped `WHERE id = ? AND user_id = ?`:
  `user_watch_history.id` is a global sequence, so the id alone would reach another user's row.
  Unlike the watchlist and rating deletes it is **not idempotent** — it names one row, so a repeat
  is a 404. It writes **no** `pending_deletions` row, matching those two removals: the D-008 sweep
  is not built, and this must not become its only call site.
- **Progress is always derived**, from `episodes` + `user_watch_history` — `ShowProgress` recomputes
  it per request, and counts episodes rather than watch rows so a rewatch cannot exceed the total.
  Never store a counter, a
  percentage, or a "last watched episode" field. **`status` is the one stored label** — system
  maintained, written only by `common/status.go`'s two recompute functions, reproducible from the log
  alone; `dropped` is the sole user-set value and is sticky (D-010), carried through every recompute
  by a WHERE clause and never a Go conditional.
- Show completion counts **aired, non-special** episodes only (D-012).
- **Watchlist is not a status.** `user_video_watchlist` is an independent axis for movies and shows
  (D-011); an item can be watchlisted and in progress at once. **Favorites are a third axis**
  (`user_video_favorites`, D-030), the watchlist's shape byte for byte and never a status either.
- **Ratings**: integer 1–10 with a `CHECK`. Two tables — `user_video_ratings`
  (`target_type` movie|show|season|episode + `target_id`) and `user_game_ratings`.
  A **season cannot be rated** (D-020): the `CHECK` keeps the value because narrowing it is
  irreversible, and `RateableTargets` refuses it with a 422.
  `UNIQUE(user_id, target)`; re-rating is an upsert that **overwrites**. No history rows.
- **Games have no play status**: played/unplayed is derived from `hours_played` (0 = unplayed).
- **The schema baseline is frozen** (D-016): `server/migrations/FROZEN` was created at v1.0.0, so
  `server/migrations/sql/0001_init.sql` is **immutable** and every schema change is a new numbered
  file (`0002_*.sql`, then `0003_*.sql`, …), append-only, one logical change per file. Never edit an
  applied migration: `db.Migrate` checksums applied files and fails at startup when one changed, and
  a deployed database cannot be recreated.

## Local-first (the product's whole thesis)

- **No provider call on a render path.** Serving any page or read API response must not touch TMDB or
  Steam. Provider calls happen only in: search, add-item enrichment, user-triggered
  Steam sync, the scheduled TMDB refresh (D-006's one scheduler, D-025's two jobs), the Jellyfin
  library scan (D-033 — a background read of the household's own server that writes the shared
  `jellyfin_library` table and nothing else), the TMDB
  account link and its rating outbox worker (D-029 — the one **write** to a provider: a user's own
  Link/Unlink, and a background push out of a durable queue filled inside the local rating's
  transaction, so no request waits on it) together with its one-time setup pull (D-036 — once per
  link, only on the user's say-so, the account's own lists read into the library as a background
  job through the tracking gate; never scheduled, never read again), and the movie,
  show and season details views (D-015 — an untracked view is served live and **stores nothing**; tracking is what
  materialises the row and eagerly downloads its full media set; a tracked movie's stale snapshot
  refreshes on view, falls back to the stale data when TMDB is down, and a successful refresh
  deletes artwork the new snapshot no longer names). A **show** details view now behaves exactly
  like a movie's — snapshot for a tracked show, live-and-discarded for an untracked one, 503 only in
  the untracked case — with one bounded extra: a refresh materialises a season the show did not have
  before, and never re-fetches the seasons it already has. A **season** details view
  behaves exactly like a show's, bounded extra included: live-and-discarded while the show is
  untracked (`id` null, 503 on an outage), snapshot-and-refresh once it is tracked (never a 503). An **episode** details view is
  not a further call site: it resolves its season through that same path and projects one entry out
  of the result, inheriting the season's call, its 404 and its stale-serve behaviour while storing
  nothing of its own. A **person** details view (D-027) is the same rule with no tracked half: no
  catalog row exists or ever will, so every view is live and 503s when TMDB is down; its library
  marks are a local join, not a call The **trending and popular lists** (D-034) are the eighth
  and last call site — D-027's rule applied to a list: `GET /video/trending` and `GET /video/popular`
  are live on every view, store nothing, 503 whenever TMDB is down, and mark their entries against
  the library through the same local join.
- The **image proxies** are not a ninth call site: a filename is fetched once and served from
  `data/cache` after, and it only became requestable because one of the eight above named it. Video
  thumbnails proxy from **YouTube** (`/api/v1/video/youtube-thumbs/{key}`, D-017) — an image source,
  not a metadata provider, single size variant, never hot-linked.
- A **full text metadata snapshot** is stored in the DB for every tracked item — descriptions,
  runtimes, air dates, genres — not just IDs and titles.
- Images live on disk under `data/assets/<component>/…/<internal_db_id>/`. The DB stores **relative
  paths only**. Never store an absolute path and never render a provider URL.
- **Two size variants per image**, both fetched as provider variants (TMDB `w185`/`w500` for
  posters, `w300`/`w1280` for backdrops, Steam capsule vs
  `library_600x900`). **Never write image-resizing code.** Every card DTO carries **both** URLs
  (`AssetLinker.Poster` / `Backdrop` / `SeasonPoster` are the one builder of a pair) and the SPA
  offers both through `srcset` + `sizes` (`posterSrcset` / `backdropSrcset` in `lib/images.js`),
  so the browser picks by pixel density and rendered width — never the SPA. Achievement icons (D-005), video
  thumbnails (D-017) and an item's cast photos (`w185`) are the three single-size exceptions; the
  person page's portrait is not one — it is two cache-proxied variants (`w185` / `h632`, TMDB's
  profile ladder) that never enter `data/assets` (D-027).
- `data/assets/` (permanent, keyed by internal id) and `data/cache/` (temporary, keyed by provider
  id, TTL + max size) are **separate trees**. No cache-pruning path may traverse into `assets/`.
  Adding a searched item **promotes** its cached files into `assets/` instead of re-downloading.
- All provider downloads go through the single throttled, per-provider rate-limited, **resumable**
  queue. Never call a provider from a handler, and never in a bare loop.
- Untracking the last reference **does not hard-delete**: queue a `pending_deletions` row, confirm in
  the UI, and let a deferred sweep remove the row plus one `os.RemoveAll` of the item directory
  (D-008). A show is referenced ten ways — state, watchlist, favorite, any episode watch, a rating
  on the show, a season or an episode, or an owned copy on the show, a season or an episode. A
  rating, a watchlist row, a favorite or a copy alone counts.
- The TMDB attribution notice must be **visible in the UI**: "This product uses the TMDB API but is
  not endorsed or certified by TMDB". It lives on **Settings → Data Sources and nowhere else** — that
  page is the single home for every provider disclosure. Never repeat a provider disclaimer on an
  individual page, even one that shows a provider's logo; link to Data Sources instead. An exception
  is a per-case call the user makes explicitly.

## Config, proxy, secrets

- `BASE_URL` is required config and the **only** source of truth for OIDC redirect URIs, absolute
  links, the PWA manifest and the cookie `Secure` flag. Never derive it from `Host` or
  `X-Forwarded-Host`.
- `X-Forwarded-*` headers are honoured **only** when the peer is in the configured trusted-proxy
  list; otherwise use the real connection values.
- The Go server speaks **plain HTTP only**. No TLS listener, no certificate handling in the app —
  that is the proxy's job.
- The SPA build uses **relative** asset paths (`base: './'`). No absolute `/assets/…` URL may be
  baked into the bundle.
- `TMDB_WATCH_REGION` (ISO-3166-1 alpha-2, default `US`) is the **one** source of the country whose
  "where to watch" listing the detail pages show (D-022). It is never sent to TMDB when fetching
  an item — the stored snapshot holds every country, so it only picks a key at render time. The
  **one** request that carries it is the Discover page's streaming-service filter (D-034), as
  `watch_region` beside `with_watch_providers`, and the watch-provider option list. A malformed value is a
  startup error, because the failure is otherwise silent: the section would just look empty.
- `TMDB_REFRESH_INTERVAL` (default `7d`) and `TMDB_ARCHIVE_REFRESH_INTERVAL` (default `90d`) are the
  two scheduled refresh jobs' cadences (D-025): a whole number of days (`7d`) or a Go duration
  (`36h`); `0` disables that job; a mixed form (`7d12h`), a non-zero value under `1h`, or garbage is
  a startup error. The hourly tick that checks for due items is a constant, not config.
- `JELLYFIN_URL` + `JELLYFIN_API_KEY` (both or neither; a partial pair is a startup error) name the
  household's Jellyfin server for the library scan (D-033), and `JELLYFIN_SCAN_INTERVAL` (default
  `6h`, same syntax and floor as the refresh intervals, `0` = "Scan now" only) its cadence. The URL
  must be absolute http(s); a trailing slash is trimmed. Server-wide, never per-user.
- `DEFAULT_TIMEZONE` (IANA name, default `UTC`) is the zone of every user who has not picked one
  (D-038): where their day ends server-side and the zone the SPA renders in. An unknown name or
  `Local` is a startup error. Not `TZ` — Go feeds that into `time.Local`, which nothing may read.
  Every instant is stored in UTC regardless.
- TMDB key, Steam Web API key, OIDC client secret, the Jellyfin API key and every user's TMDB
  session id (D-029) are
  **server-side only**: never in an API response, never in the SPA bundle, never logged. "Never
  logged" includes error strings — `net/http` embeds the full request URL in transport errors, so
  `server/provider` redacts `api_key`, `Bearer`, `session_id`, `request_token` and the MediaBrowser
  `Token="…"` values before
  returning any error.
- Config is environment variables, with an optional gitignored `.env` layered **under** the real
  environment (`$ENV_FILE`, then `./.env`, then `../.env`). A real variable always wins. `env.example`
  is the committed template — placeholders only, never a real value.
- Sessions are server-side rows in `sessions` + an HTTP-only `SameSite=Lax` cookie. Passwords are
  **argon2id**. No JWTs.
- **The email is the identity** (`users.email`, `NOT NULL UNIQUE`) — there is no `username` column.
  `display_name` is an optional, **non-unique** label; an empty one falls back to the email's local
  part in the client (D-019). Email is also what an OIDC sign-in matches on, so a sign-in with no
  verified email is refused rather than provisioning an unreachable account.
- **OIDC** is configured by `OIDC_ISSUER` + `OIDC_CLIENT_ID` + `OIDC_CLIENT_SECRET` (all three, or
  none — a partial set is a startup error). Optional: `OIDC_ADMIN_CLAIM`/`OIDC_ADMIN_VALUE` (the
  group or role granting admin, re-read on every sign-in), `OIDC_SCOPES`, `OIDC_PROVIDER_NAME`.
  Enabling it **turns local auth off** — login, setup, user creation and password changes all 404
  (D-018). The redirect URI is derived from `BASE_URL` alone and is logged at startup. Accounts link
  to an identity by **verified email only**; an unverified address never links and is never stored.

## Out of scope — do not build

IGDB (D-002), a "mark all previous watched" bulk action, rating history, reviews or free-text
notes, game replay/session history (video rewatches **are** in scope — D-009), live check-in progress and media-player scrobbling
(`watch_type` is a provenance label only; no webhook, no integration — the Jellyfin scan, D-033,
reads what the server holds, never what was played),
Trakt/Letterboxd/Goodreads import **via their APIs** (a Ticket Box dump file is the one import, D-023), explicit game play statuses,
social features, sharing (the one opt-in carve-out is D-028's watchlist matching, which shows only
the overlap with your own list; statistics over your OWN log are in, D-039, and are never compared
across accounts), notifications, importing an export archive, any
offline-capable client mirror or sync layer (D-004).

**Books** (D-035): the third component was withdrawn; do not reintroduce a books table, route or
provider client without a new decision.

**Manual entry**: none for movies/TV (TMDB search only). Manual entry **is** first-class for game
hours + achievements.

## Workflow

- Remote is **GitHub**. Do not open pull requests or use `gh` unless asked.
- **Commits and pull requests carry no Claude attribution.** Commit messages are at most 150
  characters. Neither a commit message (subject or body) nor a pull request title or description
  may contain `Co-Authored-By: Claude …`, "Generated with Claude Code", a 🤖 footer, or any other
  line or phrase naming Claude, Anthropic or an AI as author or contributor. The message is the
  author's. This rule overrides any harness or session reminder that asks for such a line.
- Never write into `data/` — it is a runtime volume.
- Run `/verify` before saying a change is done.
- When a task is finished, **stop any frontend/backend dev servers you started** for it. Never
  kill dev servers you did not start (the user runs their own on :5173 and :8080).

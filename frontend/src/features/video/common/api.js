// Video feature API calls, built on the shared /api/v1 client — this module
// never touches fetch. The backend computes everything server-side — next
// unwatched episode, progress, poster choice, sorting and filtering — the
// client never derives it (see the svelte-frontend skill).

import { api } from '../../../lib/api/client.js'

/**
 * @typedef {Object} UpNextEntry
 * @property {{ id: number, name: string }} show
 * @property {string | null} poster relative asset path; the server resolves the
 *   season poster and falls back to the show poster when the season has none
 * @property {string | null} poster_full the same poster at the full size (w500);
 *   null exactly when `poster` is — cards offer both through srcset
 * @property {{
 *   id: number,
 *   season_id: number | null,
 *   season: number,
 *   number: number,
 *   title: string,
 *   air_date: string | null,
 *   rating: number | null,
 *   marker: 'season_premiere' | 'mid_season_finale' | 'season_finale' | null,
 *   aired: boolean,
 * }} episode `season_id` is the local `seasons.id` and the only handle here
 *   that addresses anything — an episode has no id-addressed endpoint, so its
 *   details page is reached by `season_id` plus `number`. Null only if the
 *   season row is gone, which the catalog write order makes unreachable.
 *   `rating` is the TMDB community score from the stored metadata snapshot
 *   (0–10), not a user rating — up-next episodes are unwatched. `aired` is
 *   computed server-side against UTC today (false for a future or unknown air
 *   date); the card renders the "Upcoming" badge from it and never derives it
 * @property {string | null} last_watched_at when this user last watched any
 *   episode of the show, RFC3339 UTC — the maximum `watched_at` over the
 *   watch log, so it carries the same precision as its source. Only started
 *   shows are listed, so in practice it is never null
 * @property {{ watched: number, aired_total: number, total: number }} progress
 */

/**
 * @param {{
 *   orderBy: string,
 *   orderDir: string,
 *   onlyWithReleaseDate: boolean,
 *   onlyReleased: boolean,
 * }} params
 * @returns {Promise<UpNextEntry[]>}
 */
export function getUpNext({ orderBy, orderDir, onlyWithReleaseDate, onlyReleased }) {
  const qs = new URLSearchParams({ order_by: orderBy, order: orderDir })
  // Booleans only when set — the server defaults both to false.
  if (onlyWithReleaseDate) qs.set('only_with_release_date', 'true')
  if (onlyReleased) qs.set('only_released', 'true')
  return api.get(`/video/up-next?${qs}`)
}

/**
 * One dated row of the upcoming calendar, the episode arm: an unwatched,
 * dated, non-special episode of a show the user follows or has watchlisted.
 * `show` and `episode` are set, `movie` is null. `season_id` is the local
 * `seasons.id` the episode page is addressed by — null only if the season row
 * is gone. No `aired` and no `rating`: an upcoming episode is unaired-or-today
 * by construction and unscored.
 *
 * @typedef {Object} UpcomingEpisodeEntry
 * @property {string} date YYYY-MM-DD, the air date as TMDB states it — a day,
 *   not an instant, so it is never shifted into local time
 * @property {'episode'} kind
 * @property {string | null} poster relative asset path — the season's, falling
 *   back to the show's
 * @property {string | null} poster_full the same poster at the full size (w500);
 *   null exactly when `poster` is — cards offer both through srcset
 * @property {{ id: number, name: string }} show
 * @property {{
 *   id: number,
 *   season_id: number | null,
 *   season: number,
 *   number: number,
 *   title: string,
 *   marker: 'season_premiere' | 'mid_season_finale' | 'season_finale' | null,
 * }} episode
 * @property {null} movie
 */

/**
 * The movie arm: an unwatched movie on the watchlist whose release date is in
 * the window. `movie` is set, `show` and `episode` are null.
 *
 * @typedef {Object} UpcomingMovieEntry
 * @property {string} date YYYY-MM-DD release date
 * @property {'movie'} kind
 * @property {string | null} poster relative asset path
 * @property {string | null} poster_full the same poster at the full size (w500);
 *   null exactly when `poster` is — cards offer both through srcset
 * @property {null} show
 * @property {null} episode
 * @property {{ id: number, title: string }} movie
 */

/**
 * Discriminated on `kind`. The list is flat and ordered by date then title;
 * the page folds adjacent entries into day sections and collapses a same-show,
 * same-season run into one row — presentation, not derivation.
 * @typedef {UpcomingEpisodeEntry | UpcomingMovieEntry} UpcomingEntry
 */

/**
 * @param {{ type: string, days: number, from: string }} params `from` is the
 *   viewer's LOCAL day (YYYY-MM-DD), so a section headed "Today" agrees with
 *   their clock; `days` is the horizon, 1–366.
 * @returns {Promise<UpcomingEntry[]>}
 */
export function getUpcoming({ type, days, from }) {
  const qs = new URLSearchParams({ type, days: String(days), from })
  return api.get(`/video/upcoming?${qs}`)
}

/**
 * One TMDB search match, movie or show — pure provider data, no library or
 * user state. `tmdb_id` is only unique within its media type (movie 5 and
 * show 5 both exist), so the pair (`media_type`, `tmdb_id`) identifies a
 * result and is what a list key must be built from.
 *
 * @typedef {Object} SearchResult
 * @property {'movie' | 'show'} media_type
 * @property {number} tmdb_id
 * @property {string} title
 * @property {string | null} year release/first-air year ("1999"), or null when
 *   TMDB has no date
 * @property {string} overview
 * @property {number | null} rating TMDB community score (0–10), not a user
 *   rating; null when TMDB reports the item as unrated
 * @property {string | null} poster relative URL of the locally proxied thumb —
 *   never a provider URL — or null when TMDB has no poster
 * @property {string | null} poster_full the same poster at the full size
 *   (w500); null exactly when `poster` is — cards offer both through srcset
 */

/**
 * Live TMDB search; results are ephemeral and never stored. `type: 'all'`
 * merges movies and shows by popularity server-side.
 *
 * @param {{ q: string, type: string }} params
 * @returns {Promise<SearchResult[]>}
 */
export function searchVideo({ q, type }) {
  const qs = new URLSearchParams({ q, type })
  return api.get(`/video/search?${qs}`)
}

/**
 * One title of the trending or popular list (D-034): every `SearchResult`
 * field — a foreign title addressed by its TMDB id, because the list is live
 * on every view and never stored — plus the person page's library marks
 * (D-027), joined locally on the server. The pair (`media_type`, `tmdb_id`)
 * identifies an entry.
 *
 * @typedef {SearchResult & {
 *   id: number | null,
 *   watched: boolean,
 *   in_history: boolean,
 * }} TrendingEntry
 * `id`: local catalog id when the library holds the title (the card then
 * links by it), else null. `watched`: movie — any watch; show — completed
 * (the ✓). `in_history`: any watch of the title in this user's log; for a
 * show, any episode. Both false when `id` is null.
 */

/**
 * One page of a trending or discover list: TMDB's paging envelope around the
 * entries. There is more while `page < total_pages` (TMDB serves no page past
 * 500); "Load more" asks for `page + 1` and appends.
 *
 * @typedef {Object} TrendingPage
 * @property {TrendingEntry[]} entries
 * @property {number} page
 * @property {number} total_pages
 */

/**
 * What is trending on TMDB today or this week — live, never stored, and a
 * 503 whenever TMDB is unreachable (D-034). `type: 'all'` is TMDB's own
 * cross-type ranking with the people it lists dropped, so a page can come up
 * a few short of twenty. TMDB offers no filters here; those live on
 * getDiscover.
 *
 * @param {{ window?: 'day' | 'week', type?: 'all' | 'movie' | 'show', page?: number }} params
 * @returns {Promise<TrendingPage>}
 */
export function getTrending({ window, type, page } = {}) {
  const qs = new URLSearchParams()
  if (window) qs.set('window', window)
  if (type) qs.set('type', type)
  if (page != null && page > 1) qs.set('page', String(page))
  const q = qs.toString()
  return api.get(`/video/trending${q ? `?${q}` : ''}`)
}

/**
 * The filters of getDiscover. Every field is optional; null, '' or an empty
 * array means "no constraint". Genres must ALL match, providers ANY.
 *
 * @typedef {Object} DiscoverFilters
 * @property {number | string | null} [voteMin] TMDB score 0–10
 * @property {number | string | null} [voteMax]
 * @property {number | string | null} [votesMin] minimum vote count
 * @property {string | null} [language] ISO 639-1 original language
 * @property {number | string | null} [yearFrom]
 * @property {number | string | null} [yearTo]
 * @property {number[]} [genres] TMDB genre ids
 * @property {number[]} [providers] TMDB watch-provider ids, in the server's region
 */

/**
 * TMDB's discover lists — the popular list when unfiltered — filtered and
 * sorted by TMDB itself, live and never stored (D-034). `type: 'all'` merges
 * the movie and show pages by the sort's own key server-side, uncapped.
 * Repeated ids travel as `?genre=18&genre=80`, never comma-joined.
 *
 * @param {{ type?: 'all' | 'movie' | 'show', sort?: 'popularity' | 'rating' | 'newest' | 'votes',
 *   page?: number } & DiscoverFilters} params
 * @returns {Promise<TrendingPage>}
 */
export function getDiscover({
  type,
  sort,
  page,
  voteMin,
  voteMax,
  votesMin,
  language,
  yearFrom,
  yearTo,
  genres,
  providers,
} = {}) {
  const qs = new URLSearchParams()
  if (type) qs.set('type', type)
  if (sort) qs.set('sort', sort)
  if (page != null && page > 1) qs.set('page', String(page))
  const scalar = {
    vote_min: voteMin,
    vote_max: voteMax,
    votes_min: votesMin,
    language,
    year_from: yearFrom,
    year_to: yearTo,
  }
  for (const [key, value] of Object.entries(scalar)) {
    if (value != null && value !== '') qs.set(key, String(value))
  }
  for (const id of genres ?? []) qs.append('genre', String(id))
  for (const id of providers ?? []) qs.append('provider', String(id))
  const q = qs.toString()
  return api.get(`/video/discover${q ? `?${q}` : ''}`)
}

/**
 * The Discover filter panel's option lists, from TMDB and cached server-side
 * for the process. Genre ids are per kind — the movie and TV lists overlap
 * only partly — so a mixed list offers the union.
 *
 * @typedef {Object} DiscoverOptions
 * @property {{ movie: { id: number, name: string }[], show: { id: number, name: string }[] }} genres
 * @property {{ code: string, name: string }[]} languages sorted by name
 * @property {{ id: number, name: string, logo: string | null }[]} providers streaming
 *   services in `region`, in TMDB's display order; `logo` is a relative
 *   proxied URL or null
 * @property {string} region the configured watch region
 */

/** @returns {Promise<DiscoverOptions>} */
export function getDiscoverOptions() {
  return api.get('/video/discover/options')
}

/**
 * One credited actor on a details page — movie or show; the shape is the same.
 *
 * @typedef {Object} CastMember
 * @property {number | null} tmdb_id TMDB's person id — what the person page is
 *   addressed by (D-027). Null when the stored snapshot carries none, in which
 *   case the card is not a link
 * @property {string} name
 * @property {string | null} character the part played; on a show, several
 *   parts across the run are joined with " / ", most episodes first
 * @property {string | null} photo relative URL of the locally proxied profile
 *   photo, or null when TMDB has none
 * @property {number | null} episodes how many episodes the actor appears in —
 *   a show figure, from TMDB's aggregate credits; null for a movie
 */

/**
 * One video TMDB lists for a movie or a show — the media section's Videos
 * tab. YouTube only: TMDB stores no frames, so `thumb` is the app's own
 * proxied still and `url` is the one link in the section that leaves the app.
 *
 * @typedef {Object} MovieVideo
 * @property {string} name TMDB's title, e.g. "Official Trailer"
 * @property {string} type Trailer | Teaser | Clip | Featurette | Behind the
 *   Scenes | Bloopers. A label to render, never something to branch on
 * @property {boolean} official whether TMDB marks it as studio-published
 * @property {string | null} published_at RFC3339
 * @property {string} url YouTube watch URL — an outbound link, like `homepage`
 * @property {string | null} thumb relative URL of the locally proxied still
 *   frame, or null when the video's id is unusable; never a provider URL
 */

/**
 * One gallery image at both locally proxied variants — never a provider URL.
 * `width`/`height` are the full variant's provider dimensions, so a tile can be
 * laid out at the right shape before the image loads. Backdrops are 16:9 and
 * posters 2:3, and the same shape carries both.
 *
 * @typedef {Object} MovieImage
 * @property {string} thumb relative URL of the small variant, for the rail
 * @property {string} full relative URL of the large variant, for the lightbox
 * @property {number} width
 * @property {number} height
 */

/**
 * One movie, from the local catalog snapshot or served live from TMDB.
 * `id` is the local catalog id, `tmdb_id` TMDB's — two different id
 * namespaces, never interchangeable.
 *
 * @typedef {Object} MovieDetails
 * @property {number | null} id local catalog id, or null when the movie has no
 *   catalog row — an untracked view served live from TMDB. Tracking it (mark
 *   watched, watchlist) creates the row; adopt the returned `target_id`
 * @property {number} tmdb_id
 * @property {string} title
 * @property {string | null} original_title
 * @property {string | null} release_date YYYY-MM-DD
 * @property {string | null} year
 * @property {number | null} runtime_min
 * @property {string | null} overview
 * @property {string | null} tagline
 * @property {string[]} genres
 * @property {number | null} rating TMDB community score (0–10), not a user
 *   rating; null when unrated
 * @property {number | null} votes
 * @property {string | null} director
 * @property {CastMember[]} cast full credited cast, ordered by billing
 * @property {MovieVideo[]} videos the media section's Videos tab, ordered so
 *   the first entry is the video `trailer_url` points at; at most 30
 * @property {MovieImage[]} backdrops the Backdrops tab (16:9), best-rated
 *   first; at most 30
 * @property {MovieImage[]} posters the Posters tab (2:3), best-rated first; at
 *   most 30. Alternative artwork — the movie's own poster is `poster`/
 *   `poster_full`
 * @property {string | null} poster relative URL of the locally proxied thumb
 * @property {string | null} poster_full the same poster at the detail size
 * @property {string | null} backdrop relative URL of the hero backdrop thumb
 * @property {string | null} backdrop_full the same backdrop at the detail size
 * @property {string | null} homepage the movie's official site
 * @property {string | null} imdb_id IMDb title id ("tt0133093"); the client
 *   builds the IMDb URL from it
 * @property {string | null} trailer_url YouTube watch URL of the best trailer
 *   TMDB lists (trailer beats teaser, official beats unofficial, then newest)
 * @property {string | null} original_language ISO 639-1 code ("en")
 * @property {number | null} budget USD; null when TMDB reports 0 (unknown)
 * @property {number | null} revenue USD; null when TMDB reports 0 (unknown)
 * @property {number | null} popularity TMDB's relative trending metric, only
 *   as current as the snapshot — `fetched_at` is its refresh date
 * @property {string} fetched_at RFC3339 UTC snapshot time
 * @property {boolean} tracked whether ANY user references this movie (state,
 *   watchlist, favorite, watch, rating or copy); false on a live untracked view
 * @property {boolean} watched whether THIS user has watched it
 * @property {boolean} watchlisted whether THIS user has it watchlisted
 * @property {boolean} favorited whether THIS user has it among their favorites
 *   — a third independent axis (D-030), orthogonal to the two above
 * @property {number | null} user_rating THIS user's own rating (1–10), or null
 *   when they have not rated it — not a provider score, unlike `rating`. Always
 *   null on a live untracked view, which carries no user state
 * @property {string[]} copies the kinds THIS user holds a copy of the movie on
 *   (D-021), kind-ordered; [] on a live untracked view, which carries no user
 *   state
 * @property {boolean} jellyfin_synced a Jellyfin server is configured (D-033),
 *   so the `jellyfin` entry in `copies` is the library scan's answer, not this
 *   user's label: the tile is read-only and the copy routes refuse the kind
 * @property {Streaming | null} streaming where the movie streams, rents or
 *   sells in the configured region (D-022) — catalog data from the same
 *   snapshot as the cast, so it renders with TMDB down. Null when the snapshot
 *   holds no listing at all
 * @property {SimilarTitle[]} similar TMDB's recommendations for the movie, in
 *   TMDB's relevance order, at most 20 — the "Similar movies" rail (D-024).
 *   Never null; [] when the snapshot has no section yet or TMDB lists nobody
 */

/**
 * One service an item can be watched on, in the configured region — a tile of
 * the "Watch now" section (D-022). TMDB's watch-provider data, which TMDB in
 * turn sources from JustWatch.
 *
 * @typedef {Object} StreamingProvider
 * @property {number} provider_id TMDB's stable id for the service. It
 *   addresses nothing — it is the render key, which the name is not: two
 *   services can share a display name across regions
 * @property {string} name
 * @property {'stream'|'free'|'ads'|'rent'|'buy'} offer how it is available, at
 *   its STRONGEST offer only — a service listed under both rent and buy shows
 *   up once, as `rent`
 * @property {string | null} logo relative URL of the locally proxied mark, or
 *   null when TMDB has none — the tile falls back to a name monogram. Never a
 *   provider URL
 */

/**
 * Where an item can be watched online, in ONE country (D-022). It rides the
 * movie and show metadata snapshots, so it renders with TMDB down and is only
 * as current as `fetched_at`.
 *
 * Seasons and episodes have no listing of their own — TMDB publishes providers
 * per movie and per show — so those two views carry the OWNING SHOW's, and it
 * is empty while the show is untracked.
 *
 * The field itself is nullable, and the two "empty" cases are different
 * answers: a null `streaming` means nobody looked (a season or episode of an
 * untracked show, or a snapshot with no listing yet) and the section stays
 * hidden; a `providers: []` means TMDB answered and named nobody, which the
 * section says out loud.
 *
 * @typedef {Object} Streaming
 * @property {string} region the ISO-3166-1 alpha-2 country the listing is for,
 *   from the server's TMDB_WATCH_REGION. Always set, including when `providers`
 *   is empty — which is what lets the empty state name the country
 * @property {string | null} link JustWatch's page for the item in `region`, or
 *   null when the snapshot carries none
 * @property {StreamingProvider[]} providers strongest offer first, then TMDB's
 *   display priority, then name; [] when TMDB lists nobody in the region
 */

/**
 * One card of the details pages' "Similar movies" / "Similar shows" rail —
 * TMDB's recommendations for the item (D-024). Always the same kind as the
 * page it hangs off, so there is no `media_type`: a movie's list holds movies
 * and a show's holds shows. The title named has no catalog row — reach it by
 * `?tmdb_id=`, and treat its poster as the cache proxy's, like a search
 * result's.
 *
 * @typedef {Object} SimilarTitle
 * @property {number} tmdb_id
 * @property {string} title
 * @property {string | null} year release or premiere year
 * @property {number | null} rating TMDB community score (0–10), null when
 *   unrated
 * @property {string | null} poster relative URL of the locally proxied thumb
 * @property {string | null} poster_full the same poster at the detail size
 */

/**
 * Movie details. A tmdb_id with no catalog row is served live from TMDB and
 * stores nothing — only tracking creates rows (D-015). A tracked movie is
 * answered from its snapshot, refreshed when stale, with the stale snapshot
 * served when TMDB is down.
 *
 * @param {{ tmdbId?: number | string, id?: number | string }} params exactly one
 * @returns {Promise<MovieDetails>}
 */
export function getMovieDetails({ tmdbId, id }) {
  const qs = new URLSearchParams()
  if (tmdbId != null) qs.set('tmdb_id', String(tmdbId))
  if (id != null) qs.set('id', String(id))
  return api.get(`/video/movies/details?${qs}`)
}

/**
 * One season on a show details page's seasons grid. Three fields are this
 * user's: `copies` (owned copies live per-season, D-021) and the two derived
 * reads the poster badges, `progress` and `user_rating`. Everything else is the
 * snapshot's; there is no user status, because a season has none (D-010).
 *
 * @typedef {Object} ShowSeason
 * @property {number | null} id local seasons.id once the show is TRACKED, or
 *   null before then — only tracking writes season rows (D-015). The grid links
 *   by it when present, else by (show `tmdb_id`, `season_number`)
 * @property {number} season_number TMDB's season number; 0 is Specials
 * @property {string} name
 * @property {number} episode_count episodes TMDB lists, aired or not
 * @property {string | null} air_date YYYY-MM-DD of the premiere
 * @property {string | null} year premiere year ("2008"), from `air_date`
 * @property {string | null} overview
 * @property {number | null} rating TMDB community score for the season (0–10),
 *   not a user rating; null when unrated
 * @property {string | null} poster relative URL of the locally proxied thumb,
 *   or null when the season has no poster of its own
 * @property {string | null} poster_full the same poster at the detail size;
 *   null exactly when `poster` is
 * @property {string[]} copies the kinds THIS user holds a copy of this season
 *   on (D-021), kind-ordered; always [] for an untracked show, which has no
 *   season rows to hold copies
 * @property {number | null} jellyfin_episodes how many of this season's
 *   episode files the Jellyfin scan found (D-033); null when unsynced or not
 *   on the server. Compared with the aired episodes to colour the tile
 * @property {ShowProgress | null} progress this season's derived progress —
 *   the show's three counts narrowed to one season, Specials counting their
 *   own episodes; null for an untracked show, which has no episode rows
 * @property {number | null} user_rating the average of this user's EPISODE
 *   ratings in the season (1–10, fractional), or null when none is rated or the
 *   show is untracked. A season is not itself rateable (D-020) — this is the
 *   one "your rating" a season can show, and a derived read, never stored
 */

/**
 * One show, always served live from TMDB. Mirrors `MovieDetails` wherever the
 * concepts match — so both detail pages share their components — and diverges
 * where TV does: seasons and episode counts instead of one runtime,
 * `created_by` instead of a director, `networks` instead of budget and revenue.
 * `CastMember`, `MovieVideo` and `MovieImage` are reused as-is.
 *
 * @typedef {Object} ShowDetails
 * @property {number | null} id local `shows.id` once the show is tracked, null
 *   while it is only being looked at — a view of an untracked show still
 *   creates no catalog row. Adopt it from a tracking response and address the
 *   show by it afterwards
 * @property {number} tmdb_id
 * @property {string} title TMDB's `name`
 * @property {string | null} original_title TMDB's `original_name`
 * @property {string | null} first_air_date YYYY-MM-DD
 * @property {string | null} last_air_date YYYY-MM-DD of the most recently
 *   aired episode — a returning series has one; it is not an end date
 * @property {string | null} year premiere year, from `first_air_date`
 * @property {string | null} end_year final year, set only for a show that is
 *   no longer in production; null for a returning series
 * @property {number | null} episode_runtime_min typical episode length
 * @property {number} seasons_count
 * @property {number} episodes_count
 * @property {string | null} show_status TMDB's production status ("Returning
 *   Series", "Ended", "Canceled"). NOT a watch status — the user's
 *   watching/watched/dropped axis arrives in M2 and only shares the word
 * @property {boolean} in_production
 * @property {string | null} overview
 * @property {string | null} tagline
 * @property {string[]} genres
 * @property {string[]} created_by creator names — the show's `director`
 * @property {string[]} networks network names
 * @property {{ season: number, number: number, title: string | null,
 *   air_date: string | null } | null} next_episode TMDB's next episode to air
 * @property {number | null} rating TMDB community score (0–10)
 * @property {number | null} votes
 * @property {CastMember[]} cast full credited cast across every episode, ordered
 *   by billing, each with its `episodes` count — TMDB's aggregate credits
 * @property {ShowSeason[]} seasons every season, ascending with Specials
 *   (season 0) last. Uncapped — how many to show at once is the client's call
 * @property {MovieVideo[]} videos the media section's Videos tab; at most 30
 * @property {MovieImage[]} backdrops the Backdrops tab (16:9); at most 30
 * @property {MovieImage[]} posters the Posters tab (2:3); at most 30
 * @property {string | null} poster relative URL of the locally proxied thumb
 * @property {string | null} poster_full the same poster at the detail size
 * @property {string | null} backdrop relative URL of the hero backdrop thumb
 * @property {string | null} backdrop_full the same backdrop at the detail size
 * @property {string | null} homepage
 * @property {string | null} imdb_id IMDb title id ("tt0903747")
 * @property {string | null} trailer_url YouTube watch URL of the best trailer
 * @property {string | null} original_language ISO 639-1 code ("en")
 * @property {number | null} popularity as of `fetched_at`
 * @property {string} fetched_at RFC3339 UTC — the snapshot's time for a tracked
 *   show, the current instant for a live one
 * @property {boolean} tracked whether the show has a catalog row
 * @property {boolean} watched whether the stored status has reached `watched`
 * @property {boolean} watchlisted whether THIS user has it watchlisted — an
 *   independent axis, never a status, so it is orthogonal to `watched`
 * @property {boolean} favorited whether THIS user has it among their favorites
 *   — the third independent axis (D-030), orthogonal to `status`, `watched`
 *   and `watchlisted` alike
 * @property {boolean} jellyfin_synced a Jellyfin server is configured (D-033),
 *   so every season's `jellyfin` copy is the library scan's answer: the show
 *   page's tile is read-only
 * @property {'watching'|'watched'|'dropped'|null} status the system-maintained
 *   watch status, null when untracked. Distinct from `show_status`, which is
 *   TMDB's production status
 * @property {number | null} user_rating this user's 1–10 rating
 * @property {number | null} user_episode_rating the mean of this user's EPISODE
 *   ratings across the whole show (1–10, fractional), null when none is rated
 *   or the show is untracked. Server-computed so it is weighted per episode —
 *   a mean of the seasons' `user_rating` means would weight seasons instead
 * @property {number} user_rated_episodes how many ratings went into it; 0
 *   whenever `user_episode_rating` is null
 * @property {ShowProgress | null} progress derived, null when untracked
 * @property {Streaming | null} streaming where the show streams, rents or
 *   sells in the configured region (D-022) — and the listing its seasons and
 *   episodes borrow, since TMDB publishes providers per show, never per season.
 *   Null when the snapshot holds no listing at all
 * @property {SimilarTitle[]} similar TMDB's recommendations for the show, in
 *   TMDB's relevance order, at most 20 — the "Similar shows" rail (D-024).
 *   Never null; [] when the snapshot has no section yet or TMDB lists nobody.
 *   Shows only: seasons and episodes carry no such list
 */

/**
 * Show details, addressed by local `id` or by `tmdbId` — exactly one. Three
 * behaviours, exactly the movie view's:
 *
 * - `id` with no row: a plain 404, never a provider call.
 * - `tmdbId` for a TRACKED show: the stored snapshot, refreshed when stale and
 *   stale-served when that refresh fails — so a tracked show never 503s.
 * - `tmdbId` for an untracked one: live from TMDB, storing nothing, with a null
 *   `id`. TMDB down here IS a 503: there is no snapshot to fall back on.
 *
 * @param {{ tmdbId?: number | string, id?: number | string }} params exactly one
 * @returns {Promise<ShowDetails>}
 */
export function getShowDetails({ tmdbId, id }) {
  const qs = new URLSearchParams(id != null ? { id: String(id) } : { tmdb_id: String(tmdbId) })
  return api.get(`/video/shows/details?${qs}`)
}

/**
 * One filmography card on the person page: a title the person acted in,
 * addressed by `tmdb_id` like a search result, plus two library fields from a
 * local join — `id` when the catalog holds the title (link by local id, like a
 * watchlist card) and this user's `watched` (movie: any watch; show: completed,
 * the watchlist page's meaning). Its poster is the cache proxy's, even for a
 * title the library holds.
 *
 * @typedef {Object} PersonCredit
 * @property {'movie' | 'show'} media_type
 * @property {number} tmdb_id
 * @property {string} title
 * @property {string | null} year release or premiere year
 * @property {string | null} character the part, or several parts joined " / "
 * @property {number | null} rating TMDB community score (0–10), null when
 *   unrated
 * @property {string | null} poster relative URL of the locally proxied thumb
 * @property {string | null} poster_full the same poster at the detail size
 * @property {number | null} episodes how many episodes the person appears in
 *   — a show figure; null for a movie
 * @property {number | null} id local catalog id when the library holds the
 *   title ("in your library"), else null
 * @property {boolean} watched movie: any watch; show: completed (the ✓)
 * @property {boolean} in_history any watch of the title in this user's log —
 *   for a show, any episode, finished or not. What the "Seen with" grid
 *   filters on; always false when `id` is null
 */

/**
 * The person page (D-027). Always live from TMDB — a person has no catalog row,
 * no snapshot and no per-user state, so there is no `id` and no `fetched_at`,
 * and TMDB being down is a 503 on every view. The filmography is marked
 * against the library locally.
 *
 * @typedef {Object} PersonDetails
 * @property {number} tmdb_id
 * @property {string} name
 * @property {string | null} biography
 * @property {string | null} known_for_department "Acting", "Directing", …
 * @property {string | null} birthday YYYY-MM-DD
 * @property {string | null} deathday YYYY-MM-DD
 * @property {string | null} place_of_birth
 * @property {string[]} also_known_as never null
 * @property {string | null} homepage an outbound link
 * @property {string | null} imdb_id IMDb's name id, e.g. "nm0000093"
 * @property {string | null} photo relative URL of the locally proxied portrait
 *   thumb (w185)
 * @property {string | null} photo_full the same portrait at TMDB's profile
 *   detail size (h632)
 * @property {PersonCredit[]} credits acting credits, one per title, undated
 *   first then newest first; never null
 */

/**
 * Person details, live from TMDB on every call (D-027). `tmdb_id` only — a
 * person never has a local id.
 *
 * @param {{ tmdbId: number | string }} params
 * @returns {Promise<PersonDetails>}
 */
export function getPersonDetails({ tmdbId }) {
  const qs = new URLSearchParams({ tmdb_id: String(tmdbId) })
  return api.get(`/video/people/details?${qs}`)
}

/**
 * One row of the season page's episode list.
 *
 * @typedef {Object} SeasonEpisode
 * @property {number | null} id local `episodes.id`, or null when the show has
 *   no catalog row yet — an episode row is created by tracking, never by a
 *   view. A null id is not an error: mark the episode by the show's tmdb_id
 *   plus season and episode number instead, and tracking mints the id
 * @property {boolean} watched whether THIS user has ever watched it; false on
 *   an untracked show, which has no watch rows to find
 * @property {number | null} user_rating this user's 1–10 rating. An episode is
 *   rateable; its season is not
 * @property {number} episode_number
 * @property {string | null} title
 * @property {string | null} overview
 * @property {string | null} air_date YYYY-MM-DD
 * @property {number | null} runtime_min
 * @property {number | null} rating TMDB community score for the episode
 *   (0–10), not a user rating; null when unrated
 * @property {number | null} votes
 * @property {string | null} still relative URL of the locally proxied still at
 *   grid size (w300), or null when TMDB has none
 * @property {string | null} still_full the same still at the episode page's
 *   hero size (w1280); null exactly when `still` is
 * @property {('season_premiere' | 'mid_season_finale' | 'season_finale') | null} marker
 *   labels the episodes worth badging, in the up-next cards' vocabulary;
 *   null for a plain episode. Derived server-side from TMDB's episode_type
 *   plus the premiere rule (episode 1)
 */

/**
 * One season with its full episode list. Like the other detail DTOs `id` is
 * null until the item has a catalog row, and only tracking the show creates one
 * (D-015) — a season of an untracked show is fetched live and discarded. There
 * are no tracked/watched/watchlisted flags — season progress is derived state
 * arriving with M2 as additive fields.
 *
 * @typedef {Object} SeasonDetails
 * @property {number | null} id local seasons.id once the show is tracked, else
 *   null. There is no tmdb_id for the season itself; the addressable key is
 *   (show tmdb_id, season_number)
 * @property {number} show_tmdb_id TMDB id of the owning show, for the back link
 * @property {string} show_title the show's name, denormalised at fetch time so
 *   a direct ?id= visit renders its header without a second call
 * @property {number} season_number TMDB's season number; 0 is Specials
 * @property {string} name
 * @property {string | null} overview
 * @property {string | null} air_date YYYY-MM-DD of the premiere
 * @property {string | null} year premiere year ("2008"), from `air_date`
 * @property {number} episode_count episodes TMDB lists, aired or not
 * @property {number | null} rating TMDB community score for the season (0–10)
 * @property {string | null} poster relative URL of the locally proxied thumb
 * @property {string | null} poster_full the same poster at the detail size
 * @property {SeasonEpisode[]} episodes every episode, in episode order
 * @property {CastMember[]} cast this season's aggregate credits, ordered by
 *   billing, each `episodes` count taken within the season; [] when none
 * @property {string} fetched_at RFC3339 UTC snapshot time
 * @property {string[]} copies the kinds THIS user holds a copy of this season
 *   on (D-021), kind-ordered; [] on a live untracked view, which carries no
 *   user state
 * @property {boolean} jellyfin_synced a Jellyfin server is configured (D-033),
 *   so the `jellyfin` entry in `copies` is the library scan's answer: the tile
 *   is read-only
 * @property {number | null} jellyfin_episodes how many of this season's
 *   episode files the scan found; null when unsynced or not on the server.
 *   Compared with the aired episodes to colour the tile
 * @property {Streaming | null} streaming where to watch, borrowed from the
 *   OWNING SHOW (D-022) — a season has no listing of its own. Null while the
 *   show is untracked, which has no show row to read it out of
 * @property {boolean} show_dropped whether THIS user has dropped the OWNING
 *   SHOW (D-010) — borrowed from the show like `streaming`, per-user like
 *   `copies`. A bool, not the show's status string: a season-shaped view
 *   reports no show status and no show progress. False on a live untracked
 *   view, and sticky, so no write from the page can stale it
 */

/**
 * Season details, addressed by local `id` or by the SHOW's tmdb_id plus the
 * season number — exactly one of the two forms. An untracked show's season is
 * fetched live and stored nowhere, so `id` comes back null and TMDB being down
 * is a 503; a stored one is served from its snapshot, refreshed past 48h, and
 * stale-served when TMDB is down — a season with a row never 503s. An `id` that
 * has no row is a plain 404, never a TMDB call.
 *
 * @param {{ id?: number | string, tmdbId?: number | string,
 *   season?: number | string }} params
 * @returns {Promise<SeasonDetails>}
 */
export function getSeasonDetails({ id, tmdbId, season }) {
  const qs = new URLSearchParams()
  if (id != null) qs.set('id', String(id))
  if (tmdbId != null) qs.set('tmdb_id', String(tmdbId))
  if (season != null) qs.set('season', String(season))
  return api.get(`/video/seasons/details?${qs}`)
}

/**
 * One episode, projected out of its season's stored snapshot. There is no id
 * and no tmdb_id for the episode itself in this shape — an episode rides its
 * season's snapshot, so it is addressed by its season plus its number, and its
 * own id, watched flag and rating travel inside `episode`.
 *
 * @typedef {Object} EpisodeDetails
 * @property {number | null} season_id local seasons.id of the owning season,
 *   null until the show is tracked — the same rule as `SeasonDetails.id`
 * @property {number} season_number 0 is Specials
 * @property {string} season_name
 * @property {number} show_tmdb_id TMDB id of the owning show, for the back link
 * @property {string} show_title
 * @property {SeasonEpisode} episode
 * @property {CastMember[]} cast the SEASON's regular cast — the same people on
 *   every episode of it, TMDB's episode-page "cast"; `episodes` null
 * @property {CastMember[]} guest_stars credited for THIS episode beyond the
 *   regulars; `episodes` null; [] when none
 * @property {string} fetched_at RFC3339 UTC time of the SEASON snapshot — an
 *   episode has no snapshot of its own
 * @property {string[]} season_copies the kinds THIS user holds a copy of the
 *   OWNING SEASON on (D-021), kind-ordered — copies are recorded per season,
 *   never per episode, so the page shows these read-only; [] on a live
 *   untracked view
 * @property {boolean} jellyfin_synced the season's flag (D-033), borrowed with
 *   `season_copies`: the jellyfin entry is the library scan's answer
 * @property {Streaming | null} streaming where to watch, borrowed from the
 *   show two rungs up (D-022) — the same listing the season page shows, from
 *   the same call, so the two can never disagree. Null while the show is
 *   untracked
 * @property {boolean} show_dropped whether THIS user has dropped the show two
 *   rungs up (D-010), borrowed the way `streaming` is and out of the same
 *   call, so the season and episode pages can never disagree. See
 *   `SeasonDetails.show_dropped`
 */

/**
 * One episode's details, addressed by the local season `id` or by the SHOW's
 * tmdb_id plus the season number — exactly one of the two forms — plus the
 * episode number, which both forms need. The season resolves exactly as
 * `getSeasonDetails` resolves it, so the same rules apply: only an untracked
 * show's season can 503, and an episode the season does not list is a 404.
 *
 * @param {{ seasonId?: number | string, tmdbId?: number | string,
 *   season?: number | string, episode: number | string }} params
 * @returns {Promise<EpisodeDetails>}
 */
export function getEpisodeDetails({ seasonId, tmdbId, season, episode }) {
  const qs = new URLSearchParams()
  if (seasonId != null) qs.set('season_id', String(seasonId))
  if (tmdbId != null) qs.set('tmdb_id', String(tmdbId))
  if (season != null) qs.set('season', String(season))
  if (episode != null) qs.set('episode', String(episode))
  return api.get(`/video/episodes/details?${qs}`)
}

/**
 * What a tracking call did. `target_id` is the local catalog id — a page that
 * navigated by tmdb_id (null `id`) adopts it from here.
 *
 * @typedef {Object} TrackResult
 * @property {boolean} created false when the call was an idempotent no-op
 * @property {'movie'|'show'|'episode'} target_type
 * @property {number} target_id local catalog id in that type's own table
 * @property {TrackShow|null} show the owning show's recomputed state, for an
 *   episode target; always null for a movie
 */

/**
 * A show's DERIVED progress. Never stored — recomputed from the episodes and
 * the watch log on every read. All three figures count non-special episodes
 * only, and they count episodes rather than watch rows, so a rewatch can never
 * push `watched` past `total`.
 *
 * @typedef {Object} ShowProgress
 * @property {number} watched aired, non-special episodes this user has watched
 * @property {number} aired_total aired, non-special episodes — the denominator
 *   the status turns on
 * @property {number} total non-special episodes, aired or not; shown secondary,
 *   because "8/10" next to a status of watched would otherwise read as a bug
 */

/**
 * @typedef {Object} TrackShow
 * @property {number} id local shows.id
 * @property {'watching'|'watched'|'dropped'|null} status system-maintained;
 *   `dropped` is the only user-set value and is sticky
 * @property {ShowProgress} progress
 */

/**
 * Record a watch of a movie, in one of the two modes `watchedAt` chooses.
 *
 * - **Without `watchedAt`** — the first-watch mark. Idempotent: a double tap or
 *   retry changes nothing (`created: false`).
 * - **With `watchedAt`** — the deliberate log of one viewing at one instant. It
 *   always inserts, so the same movie accumulates entries; this is how a
 *   rewatch is recorded. The instant may be in the past; a future one is a 422.
 *
 * Pass `tmdbId` from a live view (the server materialises the catalog row; 503
 * if TMDB is down at that moment) or `id` for a movie already in the catalog.
 *
 * @param {{ tmdbId?: number, id?: number, watchedAt?: string, rating?: number | null }} params
 *   exactly one of tmdbId and id; `rating` is stored with the watch in one
 *   transaction, and omitting it leaves any existing rating alone
 * @returns {Promise<TrackResult>}
 */
export function markWatched({ tmdbId, id, watchedAt, rating }) {
  return api.post('/video/watches', watchBody(trackBody({ tmdbId, id }), { watchedAt, rating }))
}

/**
 * Record a watch of one episode. Two addressing forms:
 *
 * - `id` alone is the local `episodes.id`, which the season and episode DTOs
 *   carry once the show is tracked. Never a provider call.
 * - `tmdbId` + `season` + `episode`, where `tmdbId` is the SHOW's. This is the
 *   form an untracked show needs, and resolving it materialises the WHOLE show
 *   — every season and episode row, and every season poster and episode still.
 *   That is one TMDB call per season, so it can take a few seconds the first
 *   time and can fail with 503: do not mark optimistically on this path.
 *
 * Same two modes as `markWatched`: idempotent without `watchedAt`, an
 * always-inserting dated log with it. The result's `show` carries the owning
 * show's recomputed status and progress, so the page can settle without a
 * second call.
 *
 * @param {{ id?: number, tmdbId?: number, season?: number, episode?: number,
 *   watchedAt?: string, rating?: number | null }} params
 * @returns {Promise<TrackResult>}
 */
export function markEpisodeWatched({ id, tmdbId, season, episode, watchedAt, rating }) {
  return api.post(
    '/video/watches',
    watchBody(
      id != null
        ? trackBody({ targetType: 'episode', id })
        : trackBody({ targetType: 'episode', tmdbId, season, episode }),
      { watchedAt, rating },
    ),
  )
}

/**
 * Remove one watch from the log — the correction path for a mis-marked or
 * duplicated watch.
 *
 * NOT idempotent, unlike the watchlist and rating deletes: this names a single
 * row, so a second call is a 404 rather than a quiet success. The catalog row
 * and its artwork stay (untracking never hard-deletes), but the target's status
 * IS recomputed server-side — deleting a movie's only watch makes it unwatched
 * again, so a caller showing a watched flag must refresh it.
 *
 * @param {number} id `HistoryEntry.id`, the watch-row id. Never a target id: a
 *   rewatch repeats the target, so an id from the wrong field is ambiguous
 * @returns {Promise<null>}
 */
export function deleteWatch(id) {
  return api.del(`/video/watches/${id}`)
}

/**
 * Result of a bulk mark. `created` is a COUNT, not a boolean — which is why
 * this is a separate endpoint from `markWatched` rather than an overload.
 * `status` and `progress` are always the OWNING SHOW's, whatever the scope.
 *
 * @typedef {Object} BulkWatchResult
 * @property {'show'|'season'} target_type
 * @property {number} target_id local `shows.id` or `seasons.id`
 * @property {number} created watch rows actually written; 0 on a repeat of the
 *   default (non-rewatch) mode
 * @property {'watching'|'watched'|'dropped'|null} status
 * @property {ShowProgress} progress
 */

/**
 * Mark a whole show watched: every aired, non-special episode, in one call.
 * By default it skips episodes this user has already watched — idempotent
 * set-wise, so running it twice returns `created: 0` rather than a few
 * hundred duplicate rewatches. `rewatch: true` drops that guard on purpose (a
 * full deliberate rewatch, like the single dated log it is NOT idempotent —
 * the caller's pending state is the double-tap guard).
 *
 * `mode` picks each row's `watched_at`: 'now' (the default) or 'fixed' share
 * one instant, 'episode_air' takes each episode's own air date, 'season_air'
 * its season's premiere. `watchedAt` is required with 'fixed', and with
 * 'season_air' it dates the episodes whose season has no usable premiere —
 * required exactly when such seasons exist. It is rejected with the other two
 * modes, and defaults are simply omitted from the body.
 *
 * Like `markEpisodeWatched`, a `tmdbId` for an untracked show materialises the
 * whole show first, so this can be slow and can fail. Show a pending state.
 *
 * @param {{ tmdbId?: number, id?: number, rewatch?: boolean,
 *   mode?: 'now'|'fixed'|'episode_air'|'season_air',
 *   watchedAt?: string | null }} params tmdbId/id: exactly one
 * @returns {Promise<BulkWatchResult>}
 */
export function markShowWatched({ tmdbId, id, rewatch, mode, watchedAt }) {
  /** @type {Record<string, unknown>} */
  const body = {
    scope: 'show',
    ...trackBody({ targetType: 'show', tmdbId, id }),
  }
  if (rewatch) body.rewatch = true
  if (mode != null && mode !== 'now') body.watched_at_mode = mode
  if (watchedAt != null) body.watched_at = watchedAt
  return api.post('/video/watches/bulk', body)
}

/**
 * Mark one whole season watched — `markShowWatched`'s season scope, with the
 * same `rewatch`/`mode`/`watchedAt` semantics narrowed to that season (the
 * premiere-fallback rule then concerns only this season's premiere). The
 * response's status and progress are the SHOW's.
 *
 * Both addressing forms, because the season page holds a local `id` only when
 * the show is already tracked: `id` when it has one, else the SHOW's `tmdbId`
 * plus the season number. The tmdb form materialises the whole show first — so
 * this can take seconds and can 503. Await it either way.
 *
 * @param {{ id?: number | null, tmdbId?: number, season?: number,
 *   rewatch?: boolean,
 *   mode?: 'now'|'fixed'|'episode_air'|'season_air',
 *   watchedAt?: string | null }} params id, or tmdbId+season
 * @returns {Promise<BulkWatchResult>}
 */
export function markSeasonWatched({ id, tmdbId, season, rewatch, mode, watchedAt }) {
  /** @type {Record<string, unknown>} */
  const body = {
    scope: 'season',
    ...(id != null
      ? trackBody({ targetType: 'season', id })
      : trackBody({ targetType: 'season', tmdbId, season })),
  }
  if (rewatch) body.rewatch = true
  if (mode != null && mode !== 'now') body.watched_at_mode = mode
  if (watchedAt != null) body.watched_at = watchedAt
  return api.post('/video/watches/bulk', body)
}

/**
 * Put a movie or a show on the watchlist — an independent axis, not a status;
 * an item can be watchlisted and watched at once. Same idempotency and id rules
 * as `markWatched`.
 *
 * A show addressed by `tmdbId` is materialised whole on the way in — every
 * season and episode, plus its asset downloads — exactly like
 * `markEpisodeWatched`. That takes seconds and can fail, so await it and show a
 * pending state rather than flipping optimistically alone.
 *
 * @param {{ targetType?: 'movie' | 'show', tmdbId?: number, id?: number }} params
 *   exactly one of tmdbId and id
 * @returns {Promise<TrackResult>}
 */
export function addToWatchlist({ targetType = 'movie', tmdbId, id }) {
  return api.post('/video/watchlist', trackBody({ targetType, tmdbId, id }))
}

/**
 * Take a target off the watchlist. Idempotent; the catalog row, its artwork and
 * any watch history stay (untracking never hard-deletes).
 *
 * @param {number} id local catalog id — `movies.id` or `shows.id`
 * @param {'movie' | 'show'} [targetType] which table that id belongs to; the
 *   pair identifies the target, the id alone identifies nothing
 * @returns {Promise<null>}
 */
export function removeFromWatchlist(id, targetType = 'movie') {
  return api.del(`/video/watchlist/${targetType}/${id}`)
}

/**
 * Mark a movie or a show as a favorite — the third independent axis (D-030),
 * beside the watchlist and owned copies, never a status. A favorite is a
 * tracking action with the watchlist's exact rules: a show addressed by
 * `tmdbId` is materialised whole on the way in, so await it and show a pending
 * state rather than flipping optimistically alone. A linked TMDB account
 * mirrors the change automatically; the server decides that.
 *
 * @param {{ targetType?: 'movie' | 'show', tmdbId?: number, id?: number }} params
 *   exactly one of tmdbId and id
 * @returns {Promise<TrackResult>}
 */
export function addFavorite({ targetType = 'movie', tmdbId, id }) {
  return api.post('/video/favorites', trackBody({ targetType, tmdbId, id }))
}

/**
 * Take a target out of the favorites. Idempotent; the catalog row, its
 * artwork and any watch history stay (untracking never hard-deletes).
 *
 * @param {number} id local catalog id — `movies.id` or `shows.id`
 * @param {'movie' | 'show'} [targetType] which table that id belongs to; the
 *   pair identifies the target, the id alone identifies nothing
 * @returns {Promise<null>}
 */
export function removeFavorite(id, targetType = 'movie') {
  return api.del(`/video/favorites/${targetType}/${id}`)
}

/**
 * What a drop or un-drop did. `changed` is false when the stored status was
 * already what the call asked for, so a repeat is honest about having done
 * nothing.
 *
 * @typedef {Object} DropResult
 * @property {boolean} changed whether the stored status actually moved
 * @property {TrackShow} show the show's status and derived progress AFTER the
 *   write — it travels back because an un-drop recomputes, and whether that
 *   lands on watched, watching or no row at all is the server's to state
 */

/**
 * Drop a show — the one watch status the user sets by hand, and shows only (a
 * movie can never be dropped). Sticky: no later watch and no newly aired
 * episode clears it, only `undropShow` does. A dropped show disappears from
 * Up Next.
 *
 * Only a show with at least one episode watch can be dropped; without one
 * there is no state row and the call is a 422. That is exactly the condition
 * `ShowDetails.status != null` expresses, which is what gates the button.
 *
 * Idempotent, and never a provider call: the show is already local, so this
 * addresses it by its own id and cannot fail on TMDB.
 *
 * @param {number} id local `shows.id`
 * @returns {Promise<DropResult>}
 */
export function dropShow(id) {
  return api.post(`/video/shows/${id}/drop`)
}

/**
 * Release a drop. The status is recomputed from the watch log in the same
 * transaction, so a show dropped after finishing comes back as *watched*
 * rather than *watching* — which is why this answers with a body where the
 * watchlist and rating removals answer 204.
 *
 * Idempotent: un-dropping a show that is not dropped changes nothing.
 *
 * @param {number} id local `shows.id`
 * @returns {Promise<DropResult>}
 */
export function undropShow(id) {
  return api.del(`/video/shows/${id}/drop`)
}

/**
 * What a copies write did. `created` is a COUNT, not `TrackResult`'s boolean,
 * because the show scope fans out: 0 or 1 for a movie or season target, 0..N
 * seasons for a show. 0 means every addressed target already carried the kind.
 *
 * @typedef {Object} CopyResult
 * @property {'movie'|'season'|'show'} target_type
 * @property {number} target_id local catalog id — a page that navigated by
 *   tmdb_id (null `id`) adopts it from here, exactly as for a tracking call
 * @property {string} kind
 * @property {number} created copy rows actually written
 */

/**
 * Record an owned copy of a movie or a season (D-021) — an independent axis
 * like the watchlist, never a status. A copy counts as tracking: an untracked
 * target addressed by tmdbId is materialised whole (for a season or show, the
 * WHOLE show, seconds and 503-able), so await it and show a pending state
 * rather than flipping optimistically alone. Idempotent — re-adding a kind the
 * target already carries comes back with `created: 0`.
 *
 * `targetType: 'show'` is the bulk fan-out scope, not a stored target: it adds
 * the kind to every NON-SPECIAL season the show has. A Specials copy is added
 * by addressing season 0 itself. A season is addressed by its local `id`, or
 * by the SHOW's `tmdbId` plus `season` — a season has no TMDB id of its own.
 *
 * @param {{ targetType?: 'movie' | 'season' | 'show', tmdbId?: number,
 *   id?: number, season?: number, kind: string }} params exactly one of tmdbId
 *   and id
 * @returns {Promise<CopyResult>}
 */
export function addCopy({ targetType = 'movie', tmdbId, id, season, kind }) {
  return api.post('/video/copies', { ...trackBody({ targetType, tmdbId, id, season }), kind })
}

/**
 * Remove an owned copy. Idempotent and purely local — never a provider call;
 * the catalog row and its artwork stay (untracking never hard-deletes, D-008).
 * A 'show' scope removes the kind from every NON-SPECIAL season, mirroring the
 * fan-out add.
 *
 * @param {'movie' | 'season' | 'show'} targetType which table `id` belongs to;
 *   the pair identifies the target, the id alone identifies nothing
 * @param {number} id local catalog id — `movies.id`, `seasons.id` or `shows.id`
 * @param {string} kind the copy kind to remove
 * @returns {Promise<null>}
 */
export function removeCopy(targetType, id, kind) {
  return api.del(`/video/copies/${targetType}/${id}/${kind}`)
}

/**
 * The rating now stored. There is no `created` flag on purpose: a re-rating
 * overwrites, so whether one existed before is not something to act on.
 *
 * @typedef {Object} RatingResult
 * @property {'movie'|'show'|'episode'} target_type
 * @property {number} target_id local catalog id — a page that navigated by
 *   tmdb_id (null `id`) adopts it from here, exactly as for a tracking call
 * @property {number} rating
 */

/**
 * Set this user's rating of a movie, 1–10. PUT because a rating is one current
 * opinion that gets replaced, not an event that accumulates — re-rating
 * overwrites and keeps no history. Rating counts as tracking: the same
 * materialisation and 503 rules as `markWatched` apply. It never sets a watch
 * status, though — an opinion is not a claim to have seen it.
 *
 * @param {{ tmdbId?: number, id?: number, rating: number }} params exactly one id
 * @returns {Promise<RatingResult>}
 */
export function rateMovie({ tmdbId, id, rating }) {
  return rateTarget({ targetType: 'movie', tmdbId, id, rating })
}

/**
 * Set this user's rating of a show or an episode. Same PUT-overwrites
 * semantics as `rateMovie`, and rating still counts as tracking — rating one
 * episode of an untracked show materialises the whole show.
 *
 * A SEASON cannot be rated: the server refuses one with a 422.
 *
 * @param {{ targetType: 'movie'|'show'|'episode', tmdbId?: number, id?: number,
 *   season?: number, episode?: number, rating: number }} params
 * @returns {Promise<RatingResult>}
 */
export function rateTarget({ targetType, tmdbId, id, season, episode, rating }) {
  return api.put('/video/ratings', {
    ...trackBody({ targetType, tmdbId, id, season, episode }),
    rating,
  })
}

/**
 * Take this user's rating back. Idempotent; the catalog row, its artwork and
 * any watch history stay (untracking never hard-deletes). Only a movie with a
 * catalog row can carry a rating, so the local id always exists here.
 *
 * @param {number} id local catalog id
 * @returns {Promise<null>}
 */
export function clearMovieRating(id) {
  return clearRating('movie', id)
}

/**
 * Take this user's rating of any rateable target back. Idempotent; the catalog
 * row, its artwork and any watch history stay (untracking never hard-deletes).
 * Only a target with a catalog row can carry a rating, so the local id always
 * exists here.
 *
 * @param {'movie'|'show'|'episode'} targetType
 * @param {number} id local catalog id in that type's own table
 * @returns {Promise<null>}
 */
export function clearRating(targetType, id) {
  return api.del(`/video/ratings/${targetType}/${id}`)
}

/**
 * The polymorphic tracking body every write shares. Exactly one of `tmdbId` and
 * `id` — the pair (target_type, id) identifies the target, and the ids are
 * different namespaces per type, so an id alone identifies nothing.
 *
 * For a show or an episode `tmdbId` is the SHOW's; an episode addressed that
 * way also carries `season` and `episode`, which are the only handles the
 * episode page holds before its show is tracked.
 *
 * @param {{ targetType?: string, tmdbId?: number, id?: number, season?: number, episode?: number }} params
 */
function trackBody({ targetType = 'movie', tmdbId, id, season, episode }) {
  /** @type {Record<string, unknown>} */
  const body =
    tmdbId != null
      ? { target_type: targetType, tmdb_id: Number(tmdbId) }
      : { target_type: targetType, target_id: Number(id) }
  if (season != null) body.season = Number(season)
  if (episode != null) body.episode = Number(episode)
  return body
}

/**
 * The two fields only a watch carries, added to a `trackBody` result. Kept out
 * of `trackBody` itself because that body is shared with the watchlist and
 * rating writes, which accept neither.
 *
 * Both are omitted when absent rather than sent as null: `watched_at`'s absence
 * is what asks for the idempotent first-watch mark, and `rating`'s absence is
 * what leaves an existing rating untouched.
 *
 * @param {Record<string, unknown>} body
 * @param {{ watchedAt?: string, rating?: number | null }} extra
 */
function watchBody(body, { watchedAt, rating }) {
  if (watchedAt != null) body.watched_at = watchedAt
  if (rating != null) body.rating = Number(rating)
  return body
}

/**
 * One entry in the watch-history feed: exactly one recorded watch.
 *
 * `movie` and `episode` are a discriminated union keyed by `target_type` —
 * exactly one is non-null. There is no top-level target id on purpose: the pair
 * (`target_type`, that object's `id`) identifies the target, and an id alone
 * identifies nothing, because movie 5 and episode 5 both exist.
 *
 * @typedef {Object} HistoryEntry
 * @property {number} id the watch-row id — the only stable identity to key a
 *   list on, since a rewatch repeats the target id
 * @property {string} watched_at the instant the watch happened, RFC3339 UTC —
 *   not when the row was written, which a backdated entry sets differently.
 *   Render it in local time; the local day can differ from the UTC day near
 *   midnight
 * @property {'watch' | 'checkin' | 'scrobble'} watch_type provenance only — how
 *   the watch was captured. A client may render it as a label but never branch
 *   on it
 * @property {'movie' | 'episode'} target_type
 * @property {string | null} poster relative asset path; the server resolves the
 *   movie poster, or the season poster falling back to the show poster
 * @property {string | null} poster_full the same poster at the full size (w500);
 *   null exactly when `poster` is — cards offer both through srcset
 * @property {number | null} user_rating this user's rating (1–10), or null when
 *   unrated. Belongs to the target, not the watch, so both entries of a rewatch
 *   carry the same value
 * @property {number | null} rating the TMDB community score (0–10) of the
 *   watched target from the stored metadata snapshot — not a user rating; null
 *   when the snapshot has none
 * @property {{ id: number, title: string, release_date: string | null } | null} movie
 * @property {{
 *   id: number,
 *   show: { id: number, name: string },
 *   season_id: number | null,
 *   season: number,
 *   number: number,
 *   title: string,
 * } | null} episode `season_id` is the local `seasons.id` and the only handle
 *   here that addresses anything — an episode has no id-addressed endpoint, so
 *   its details page is reached by `season_id` plus `number`. Null only if the
 *   season row is gone, which the catalog write order makes unreachable.
 */

/**
 * Entries come back ordered by `watched_at` then `id`, so those sharing a month
 * are contiguous and the page can group them in one pass. The `id` tie-break
 * carries weight: a bulk mark writes one `watched_at` to many rows. Ordering is
 * by UTC instant, so group on the local-time month, never on a prefix of the
 * raw string.
 *
 * The feed is served from the watch log and carries both arms: `movie` and
 * `episode` are a discriminated union keyed by `target_type`, exactly one
 * non-null. There is deliberately no bare target id in the payload — movie 5
 * and episode 5 both exist, so an id alone identifies nothing.
 *
 * Three mutually exclusive narrowings, at most one at a time; sending two is a
 * 422. `targetType` + `targetId` (always together — an id alone identifies
 * nothing) restrict the feed to one item's watches. `seasonId` and `showId`
 * restrict it to one season's episodes, or one whole show's across every
 * season, interleaved. Both are SCOPES rather than targets — neither a season
 * nor a show is ever watched, so neither is a `targetType` value — and an
 * unknown `seasonId`/`showId` is a 404 rather than an empty array.
 *
 * `limit`/`offset` page the ordered feed; the response stays a bare array, so
 * a caller that asked for `limit` N knows more may exist exactly when it
 * received N entries back.
 *
 * @param {{
 *   order?: string,
 *   type?: string,
 *   targetType?: 'movie' | 'episode',
 *   targetId?: number,
 *   seasonId?: number,
 *   showId?: number,
 *   limit?: number,
 *   offset?: number,
 * }} params
 * @returns {Promise<HistoryEntry[]>}
 */
export function getHistory({ order, type, targetType, targetId, seasonId, showId, limit, offset }) {
  const qs = new URLSearchParams()
  if (order != null) qs.set('order', order)
  if (type != null) qs.set('type', type)
  if (targetType != null) qs.set('target_type', targetType)
  if (targetId != null) qs.set('target_id', String(targetId))
  if (seasonId != null) qs.set('season_id', String(seasonId))
  if (showId != null) qs.set('show_id', String(showId))
  if (limit != null) qs.set('limit', String(limit))
  if (offset != null) qs.set('offset', String(offset))
  return api.get(`/video/history?${qs}`)
}

/**
 * One entry on the watchlist. `movie` and `show` form a discriminated union
 * keyed by `target_type`: exactly one is non-null, the other is present as
 * null. There is no bare target id — an id alone identifies nothing, since
 * movie 5 and episode 5 both exist.
 *
 * @typedef {Object} WatchlistEntry
 * @property {string} added_at RFC3339 UTC instant the item was listed
 * @property {'movie' | 'show'} target_type
 * @property {string | null} poster relative asset path, grid size
 * @property {string | null} poster_full the same poster at the full size (w500);
 *   null exactly when `poster` is — cards offer both through srcset
 * @property {number | null} rating TMDB community score, or null
 * @property {number | null} user_rating this user's 1-10, or null when unrated
 * @property {boolean} watched for a movie, any watch in the log; for a show,
 *   COMPLETED — every aired, non-special episode. True alongside a watchlist
 *   entry is expected, not contradictory: the watchlist is an independent axis
 *   and never a status (D-011)
 * @property {{ id: number, title: string, release_date: string | null } | null} movie
 * @property {{ id: number, name: string, first_air_date: string | null } | null} show
 *   a show carries a `name` where a movie carries a `title`, and its release
 *   date is its first air date
 */

/**
 * The watchlist — movies and shows interleaved — ordered by `orderBy` then the
 * target's (`target_type`, id) pair, all following `order`. That tie-break
 * carries weight: adding several items in one sitting writes one `added_at` to
 * many rows, two items can share a title or a date, and a movie and a show can
 * share an id. Titles sort case-insensitively (a show sorts on its `name`), a
 * show sorts on its first air date under `release_date`, and an item with no
 * date sorts LAST in BOTH directions.
 *
 * The list holds at most one row per target, so the filled arm's id is a stable
 * list key — unlike the history feed, nothing repeats.
 *
 * `limit`/`offset` page the ordered list; the response stays a bare array, so a
 * caller that asked for `limit` N knows more may exist exactly when it received
 * N entries back.
 *
 * @param {{
 *   orderBy?: 'added' | 'title' | 'release_date',
 *   order?: string,
 *   type?: string,
 *   limit?: number,
 *   offset?: number,
 *   matchUsers?: number[],
 * }} params `matchUsers` narrows the list to what the caller and EVERY named
 *   user have watchlisted (D-028); the server takes it as a repeated
 *   `match_user`, never comma-joined
 * @returns {Promise<WatchlistEntry[]>}
 */
export function getWatchlist({ orderBy, order, type, limit, offset, matchUsers }) {
  const qs = new URLSearchParams()
  if (orderBy != null) qs.set('order_by', orderBy)
  if (order != null) qs.set('order', order)
  if (type != null) qs.set('type', type)
  if (limit != null) qs.set('limit', String(limit))
  if (offset != null) qs.set('offset', String(offset))
  for (const id of matchUsers ?? []) qs.append('match_user', String(id))
  return api.get(`/video/watchlist?${qs}`)
}

/**
 * One account the caller may intersect their watchlist with (D-028). Id and
 * label only — the label already applied the display-name-or-email fallback
 * server-side, and no email travels.
 *
 * @typedef {Object} MatchUser
 * @property {number} id the value `match_user` takes
 * @property {string} label
 */

/**
 * Every OTHER account that has allowed matching, by label. `[]` when nobody
 * is sharing.
 * @returns {Promise<MatchUser[]>}
 */
export function getWatchlistMatchUsers() {
  return api.get('/video/watchlist/match-users')
}

/**
 * One card on the Lists hub: which list, how many movies and shows it holds,
 * and a few backdrops from its newest items for the card's background. The
 * title, icon and link are the client's, keyed by `key`.
 *
 * @typedef {Object} ListSummary
 * @property {'watchlist' | 'favorites' | 'dropped'} key
 * @property {number} movies
 * @property {number} shows
 * @property {string[]} backdrops relative grid-sized (w300) backdrop URLs,
 *   newest first, at most 4; `[]` — never null — when the list is empty or
 *   none of its items has a backdrop
 * @property {string[]} backdrops_full the same backdrops at the full size (w1280),
 *   same order and length as `backdrops`
 */

/**
 * Every list this user has, in display order: the watchlist, the favorites,
 * then the dropped shows. All three are always present. A pure local read.
 * @returns {Promise<ListSummary[]>}
 */
export function getLists() {
  return api.get('/video/lists')
}

/**
 * One entry on the favorites page (D-030): the watchlist card's shape exactly,
 * with `added_at` the instant the title was favorited.
 * @typedef {WatchlistEntry} FavoriteEntry
 */

/**
 * The favorites — movies and shows interleaved — with `getWatchlist`'s
 * ordering, tie-break, paging and key rules to the letter, minus the
 * matching-users intersection, which is the watchlist's feature.
 *
 * @param {{
 *   orderBy?: 'added' | 'title' | 'release_date',
 *   order?: string,
 *   type?: string,
 *   limit?: number,
 *   offset?: number,
 * }} params
 * @returns {Promise<FavoriteEntry[]>}
 */
export function getFavorites({ orderBy, order, type, limit, offset }) {
  const qs = new URLSearchParams()
  if (orderBy != null) qs.set('order_by', orderBy)
  if (order != null) qs.set('order', order)
  if (type != null) qs.set('type', type)
  if (limit != null) qs.set('limit', String(limit))
  if (offset != null) qs.set('offset', String(offset))
  return api.get(`/video/favorites?${qs}`)
}

/**
 * One entry on the dropped page: the watchlist card's shape exactly, with
 * `target_type` always `'show'` and `added_at` the instant it was dropped.
 * @typedef {WatchlistEntry} DroppedEntry
 */

/**
 * The shows this user has dropped, with `getFavorites`' ordering and paging
 * rules minus the kind filter — every entry is a show. A pure local read.
 *
 * @param {{
 *   orderBy?: 'added' | 'title' | 'release_date',
 *   order?: string,
 *   limit?: number,
 *   offset?: number,
 * }} params
 * @returns {Promise<DroppedEntry[]>}
 */
export function getDropped({ orderBy, order, limit, offset }) {
  const qs = new URLSearchParams()
  if (orderBy != null) qs.set('order_by', orderBy)
  if (order != null) qs.set('order', order)
  if (limit != null) qs.set('limit', String(limit))
  if (offset != null) qs.set('offset', String(offset))
  return api.get(`/video/dropped?${qs}`)
}

// ── Stats (D-039) ────────────────────────────────────────────────────────────

/**
 * A movie or a show a stats section refers to. An episode is never a title —
 * it refers to its show. Every stats title is tracked, so the artwork is the
 * item's own.
 * @typedef {Object} StatsTitle
 * @property {'movie' | 'show'} kind
 * @property {number} id local catalog id
 * @property {string} title
 * @property {number | null} year
 * @property {string | null} poster grid-sized (w185) relative URL
 * @property {string | null} poster_full the same poster at the full size (w500);
 *   null exactly when `poster` is — cards offer both through srcset
 * @property {string | null} backdrop grid-sized (w300) relative URL
 * @property {string | null} backdrop_full the same backdrop at the full size (w1280);
 *   null exactly when `backdrop` is — banners offer both through srcset
 */

/**
 * @typedef {Object} StatsHeadline
 * @property {number} hours
 * @property {number} plays
 * @property {number} movies
 * @property {number} episodes
 * @property {number} movie_minutes
 * @property {number} episode_minutes
 */

/**
 * @typedef {Object} StatsMostWatchedShow
 * @property {StatsTitle} show
 * @property {number} minutes
 * @property {number} plays
 * @property {string} last_played_at
 */

/**
 * @typedef {Object} StatsRatings
 * @property {number[]} distribution ten counts, index 0 = rating 1
 * @property {number} count
 * @property {number} average
 * @property {number | null} movies_average
 * @property {number | null} shows_average
 * @property {number | null} episodes_average
 */

/**
 * @typedef {Object} StatsTopRated
 * @property {StatsTitle} title
 * @property {number} rating
 * @property {string} rated_at
 */

/**
 * @typedef {Object} StatsRatingMonth
 * @property {string} month YYYY-MM
 * @property {number} count
 * @property {number | null} average
 */

/**
 * @typedef {Object} StatsRatingSample
 * @property {number} count
 * @property {number | null} average
 */

/**
 * @typedef {Object} StatsRatingTrend
 * @property {StatsRatingMonth[]} months exactly 12, oldest first
 * @property {StatsRatingSample} recent
 * @property {StatsRatingSample} earlier
 * @property {number | null} delta
 * @property {'harsher' | 'kinder' | 'steady' | null} verdict
 * @property {string} reference_at
 */

/**
 * @typedef {Object} StatsCrowdEntry
 * @property {StatsTitle} title
 * @property {number} user_rating
 * @property {number} tmdb_average
 * @property {number} tmdb_votes
 * @property {number} gap
 */

/**
 * @typedef {Object} StatsCrowd
 * @property {number} user_average
 * @property {number} crowd_average
 * @property {number} delta
 * @property {'harsher' | 'kinder' | 'in_sync'} verdict
 * @property {number} compared
 * @property {number} total_rated
 * @property {StatsCrowdEntry[]} loved_more
 * @property {StatsCrowdEntry[]} loved_less
 */

/**
 * @typedef {Object} StatsUnratedMovie
 * @property {StatsTitle} movie
 * @property {string} last_played_at
 */

/**
 * @typedef {Object} StatsUnratedMovies
 * @property {number} total
 * @property {StatsUnratedMovie[]} recent at most 5, most recent play first
 */

/**
 * @typedef {Object} StatsHeatmap
 * @property {string} start YYYY-MM-DD of the first cell
 * @property {number[]} plays one per day
 * @property {number[]} levels 0..4, parallel to plays
 */

/**
 * @typedef {Object} StatsTimeOfDay
 * @property {number} morning
 * @property {number} afternoon
 * @property {number} evening
 * @property {number} night
 * @property {'morning' | 'afternoon' | 'evening' | 'night'} dominant
 */

/**
 * @typedef {Object} StatsFirstPlay
 * @property {StatsTitle} title the movie, or the episode's show
 * @property {{ season: number, number: number } | null} episode
 * @property {string} watched_at
 */

/**
 * @typedef {Object} StatsActivity
 * @property {{ month: string, plays: number }[]} months exactly 12
 * @property {StatsHeatmap} heatmap
 * @property {number[]} weekday seven counts, Monday first
 * @property {StatsTimeOfDay | null} time_of_day
 * @property {number} longest_streak
 * @property {number | null} current_streak null in a past year
 * @property {number | null} tracking_days all time only
 * @property {number | null} active_days a year only
 * @property {StatsFirstPlay | null} first_play
 */

/**
 * @typedef {Object} StatsEraTitle
 * @property {StatsTitle} title
 * @property {number} year
 */

/**
 * @typedef {Object} StatsDecades
 * @property {{ decade: number, titles: number }[]} buckets contiguous, oldest first
 * @property {number} favorite the decade's first year
 * @property {StatsEraTitle | null} oldest
 * @property {StatsEraTitle | null} newest
 * @property {number} covered
 * @property {number} total
 */

/**
 * @typedef {Object} StatsRewatched
 * @property {StatsTitle} title
 * @property {number} plays
 * @property {string} last_played_at
 */

/**
 * @typedef {Object} StatsAlmostThere
 * @property {StatsTitle} show
 * @property {number} watched
 * @property {number} aired
 * @property {number} left
 */

/**
 * @typedef {Object} StatsCompletion
 * @property {number} tracked
 * @property {number} completed
 * @property {number} in_progress
 * @property {number} average_completion 0..1
 * @property {StatsAlmostThere[]} almost_there
 */

/**
 * @typedef {Object} StatsGenres
 * @property {{ name: string, titles: number }[]} entries
 * @property {number} covered
 * @property {number} total
 */

/**
 * The whole Stats page for one period. Every section is always present; one
 * the page hides is null (or an empty list). Lists are never null.
 * @typedef {Object} Stats
 * @property {string} period the APPLIED period: 'all' or a four-digit year
 * @property {number[]} years the years the filter offers, newest first
 * @property {string} today the user's day, YYYY-MM-DD
 * @property {StatsHeadline} headline
 * @property {StatsMostWatchedShow | null} most_watched_show
 * @property {StatsRatings | null} ratings
 * @property {StatsTopRated[]} top_rated
 * @property {StatsRatingTrend | null} rating_trend
 * @property {StatsCrowd | null} crowd
 * @property {StatsUnratedMovies | null} unrated_movies
 * @property {StatsActivity} activity
 * @property {StatsDecades | null} decades
 * @property {StatsRewatched[]} most_rewatched
 * @property {StatsCompletion | null} completion
 * @property {StatsGenres | null} genres
 */

/**
 * The Stats page for one period. Without `period` the server picks the
 * default (the current year when it holds a play, else all time) and echoes
 * what it applied. A pure local read over this user's own state — never a
 * provider call.
 *
 * @param {{ period?: string | null }} [params]
 * @returns {Promise<Stats>}
 */
export function getStats({ period } = {}) {
  const qs = new URLSearchParams()
  if (period) qs.set('period', period)
  const q = qs.toString()
  return api.get(`/video/stats${q ? `?${q}` : ''}`)
}

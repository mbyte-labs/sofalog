// Settings feature API calls, built on the shared /api/v1 client — this module
// never touches fetch. Two things live here: the Ticket Box restore (one POST
// that starts a background import, one GET that reads its progress; the job
// is per user and lives in server memory only, so a restart forgets it — and
// re-running the import is safe, because it replaces per item) and the TMDB
// account link (D-029): its status, its removal, and the navigation URL that
// starts it.

import { api } from '../../lib/api/client.js'

/**
 * Where the import currently is. `current` is null before the first group is
 * picked up; `title` is null while the item is being fetched from TMDB and
 * its name is not yet known.
 *
 * @typedef {Object} RestoreProgress
 * @property {number} groups_total one group per movie or show in the file
 * @property {number} groups_done
 * @property {{ type: 'movie' | 'show', tmdb_id: number, title: string | null } | null} current
 */

/**
 * One entry the import could not apply. `kind` says which of the file's
 * four arrays it came from; the position fields are filled as far as the
 * entry had them, so a season rating carries `show_tmdb_id` + `season` and a
 * movie watch carries only `tmdb_id`. `title` is the movie's or the show's
 * when the catalog holds it; `hint` narrows an `episode_not_found` to its
 * likely cause — see {@link skipReasonExplanation}.
 *
 * @typedef {Object} RestoreSkip
 * @property {'history' | 'rating' | 'watchlist' | 'favorite'} kind
 * @property {string} type movie | show | season | episode
 * @property {number | null} tmdb_id
 * @property {number | null} show_tmdb_id
 * @property {number | null} season
 * @property {number | null} episode
 * @property {string} reason a stable code — see {@link skipReasonLabel}
 * @property {string | null} title
 * @property {'specials_numbering' | 'season_missing' | 'past_season_end' | 'numbering_gap' | null} hint
 * @property {number | null} tmdb_last_episode TMDB's highest episode number in that season
 */

/**
 * Counters accumulate while the job runs and are final once it is done.
 * `skipped` holds the FIRST 200 skips only; `skipped_total` is the real
 * count, so the two disagree once a file has more than that.
 *
 * @typedef {Object} RestoreReport
 * @property {number} movies_total
 * @property {number} shows_total
 * @property {number} movies_materialised
 * @property {number} shows_materialised
 * @property {number} history_inserted
 * @property {number} history_replaced
 * @property {number} ratings_upserted
 * @property {number} watchlist_inserted
 * @property {number} favorites_inserted
 * @property {number} skipped_total
 * @property {Record<string, number>} skipped_by_reason
 * @property {RestoreSkip[]} skipped
 */

/**
 * @typedef {Object} RestoreJob
 * @property {'ticket_box'} kind
 * @property {'running' | 'done' | 'failed'} state
 * @property {string} started_at RFC3339
 * @property {string | null} finished_at RFC3339, null while running
 * @property {RestoreProgress} progress
 * @property {RestoreReport} report
 * @property {{ code: string, message: string } | null} error set only when `state` is `failed`
 */

/**
 * Start importing a Ticket Box dump. The body is the dump object verbatim —
 * the server validates the schema, so the client only pre-checks what it can
 * explain better (schemaVersion, file size).
 *
 * Errors: `conflict` (409) when a restore is already running for this user;
 * `validation` (422) with a human message when the file is not a version-2
 * dump; `invalid_body` (400) when it is not a JSON object at all.
 *
 * @param {unknown} dump the parsed JSON object
 * @returns {Promise<RestoreJob>} 202, the job as just started
 */
export function startTicketBoxRestore(dump) {
  return api.post('/video/restore/ticket-box', dump)
}

/**
 * The caller's current or most recent restore job. A finished job stays
 * readable until the user starts another; `not_found` (404) means none since
 * the server started.
 *
 * @returns {Promise<RestoreJob>}
 */
export function getTicketBoxRestore() {
  return api.get('/video/restore/ticket-box')
}

/**
 * The backend builds validation messages by wrapping its ErrValidation
 * sentinel, so they reach the client as "invalid input: <the useful part>".
 * The prefix is server-side vocabulary — strip it and keep the actionable
 * half. (The auth feature has the same helper; features never import each
 * other, so it is repeated here rather than hoisted for two lines.)
 *
 * @param {string} message
 */
export function readableMessage(message) {
  return message.replace(/^invalid input:\s*/, '')
}

/** @type {Record<string, string>} */
const SKIP_REASON_LABELS = {
  missing_tmdb_id: 'No TMDB id in the file',
  unknown_type: 'Unknown item type',
  missing_position: 'Season or episode number missing',
  invalid_timestamp: 'Unreadable date',
  future_timestamp: 'Date is in the future',
  invalid_watch_type: 'Unknown watch type',
  rating_out_of_range: 'Rating outside 1–10',
  season_rating_unsupported: "Seasons can't be rated in sofalog",
  watchlist_target_unsupported: 'Only movies and shows can be watchlisted',
  movie_not_found: 'Movie not found on TMDB',
  movie_unavailable: 'Movie could not be fetched',
  show_not_found: 'Show not found on TMDB',
  show_unavailable: 'Show could not be fetched',
  provider_unavailable: 'TMDB was unreachable',
  episode_not_found: "Episode not in TMDB's listing",
}

/**
 * A short human label for a skip reason code. Unknown codes (a newer server)
 * fall back to the code itself, spaced out, rather than to nothing.
 *
 * @param {string} reason
 */
export function skipReasonLabel(reason) {
  return SKIP_REASON_LABELS[reason] ?? reason.replaceAll('_', ' ')
}

const RERUN = "Run the import again later — it replaces rather than duplicates, so that's safe."

/** @type {Record<string, string>} */
const SKIP_REASON_EXPLANATIONS = {
  missing_tmdb_id:
    "Ticket Box didn't know this item's TMDB id when it exported, so there is nothing to look it up by.",
  unknown_type: "The file names a kind of item sofalog doesn't track.",
  missing_position: 'Episodes are matched by season and episode number, and the file left one out.',
  invalid_timestamp: "The date isn't in a format sofalog can read.",
  future_timestamp:
    'A viewing cannot be in the future — check the clock of the device Ticket Box ran on.',
  invalid_watch_type: 'Only watch, check-in and scrobble are known.',
  rating_out_of_range: 'Ratings are whole numbers from 1 to 10.',
  season_rating_unsupported: "Rate the season's episodes, or the show, instead.",
  watchlist_target_unsupported: 'Add the show to your watchlist instead.',
  movie_not_found:
    'TMDB has no movie with this id — it may have been deleted or merged into another entry.',
  show_not_found:
    'TMDB has no show with this id — it may have been deleted or merged into another entry.',
  movie_unavailable: `TMDB didn't answer when asked for it. ${RERUN}`,
  show_unavailable: `TMDB didn't answer when asked for it. ${RERUN}`,
  provider_unavailable: `TMDB failed three times in a row, so the import stopped asking. ${RERUN}`,
}

/**
 * The likely cause of a skip, as a sentence, or null when there is nothing
 * to add to the label. An `episode_not_found` is explained by the server's
 * hint, which it deduced from the numbers TMDB lists for that season.
 *
 * @param {RestoreSkip} s
 */
export function skipReasonExplanation(s) {
  if (s.reason !== 'episode_not_found') return SKIP_REASON_EXPLANATIONS[s.reason] ?? null
  const season = s.season ?? '?'
  const episode = s.episode ?? 0
  switch (s.hint) {
    case 'specials_numbering':
      return "Trakt and TMDB number specials differently, and the file's episode id didn't match any of TMDB's specials for this show."
    case 'season_missing':
      return `TMDB lists no season ${season} for this show.`
    case 'past_season_end':
      return `TMDB's season ${season} ends at episode ${s.tmdb_last_episode ?? '?'}, so this is most likely the second half of a two-part episode that TMDB lists as one.`
    case 'numbering_gap':
      return episode > 1
        ? `TMDB skips episode ${episode} in season ${season}, so episode ${episode - 1} is most likely a two-part episode that TMDB lists as one.`
        : `TMDB lists no episode ${episode} in season ${season}, though it lists the episodes after it.`
    default:
      return null
  }
}

/**
 * The three ongoing-sync switches (D-036): whether a local write of that kind
 * is sent to the linked account from now on. All false until the setup.
 *
 * @typedef {Object} TMDBSyncKinds
 * @property {boolean} ratings
 * @property {boolean} watchlist
 * @property {boolean} favorites
 */

/** @typedef {'pull' | 'push' | 'skip'} TMDBSetupAction */

/**
 * One kind's setup choice: the one-time action and the ongoing switch.
 *
 * @typedef {Object} TMDBSetupSection
 * @property {TMDBSetupAction} action
 * @property {boolean} sync
 */

/**
 * @typedef {Object} TMDBSetupRequest
 * @property {TMDBSetupSection} ratings
 * @property {TMDBSetupSection} watchlist
 * @property {TMDBSetupSection} favorites
 */

/**
 * @typedef {Object} TMDBPullProgress
 * @property {string} section ratings | watchlist | favorites; '' once finished
 * @property {string} list movies | shows | episodes
 * @property {number} page
 * @property {number} total_pages 0 until the first page of the current list answers
 * @property {number} items_done
 */

/**
 * @typedef {Object} TMDBPullReport
 * @property {number} ratings_imported
 * @property {number} episode_ratings_imported
 * @property {number} watchlist_added
 * @property {number} favorites_added
 * @property {number} movies_materialised
 * @property {number} shows_materialised
 * @property {number} skipped_total
 * @property {Record<string, number>} skipped_by_reason the restore's reason codes
 */

/**
 * @typedef {Object} TMDBPullError
 * @property {'cancelled' | 'session_invalid' | 'provider_unavailable' | 'internal'} code
 * @property {string} message
 */

/**
 * The setup's pull job (D-036): TMDB's lists read once into the library.
 *
 * @typedef {Object} TMDBPullJob
 * @property {'running' | 'done' | 'failed'} state
 * @property {string} started_at
 * @property {string | null} finished_at
 * @property {TMDBPullProgress} progress
 * @property {TMDBPullReport} report
 * @property {TMDBPullError | null} error
 */

/**
 * The state of this user's TMDB account link (D-029). Nullable fields are
 * null, not absent, when unlinked.
 *
 * @typedef {Object} TMDBLinkStatus
 * @property {boolean} configured the server holds a TMDB credential at all
 * @property {boolean} linked
 * @property {boolean} broken TMDB refused the stored session; link again to resume
 * @property {string | null} username the TMDB account, read once at link time
 * @property {string | null} linked_at RFC3339
 * @property {number} pending rating changes still waiting to be sent
 * @property {boolean} setup_done the one-time setup after linking has finished (D-036)
 * @property {TMDBSyncKinds} sync
 * @property {TMDBPullJob | null} pull the setup's pull job for this link, or null
 */

/**
 * Whether this user has linked their own TMDB account for the one-way rating
 * push, and how much is still queued. A pure local read.
 *
 * @returns {Promise<TMDBLinkStatus>}
 */
export function getTMDBLink() {
  return api.get('/video/tmdb/link')
}

/**
 * Remove the link, drop what was still queued and revoke the session at TMDB.
 * Ratings already on TMDB stay. Idempotent.
 *
 * @returns {Promise<void>}
 */
export function unlinkTMDB() {
  return api.del('/video/tmdb/link')
}

/**
 * The one-time setup after linking (D-036): per kind, pull TMDB's list once,
 * push sofalog's once, or skip, plus the ongoing switch. Answers 202 with the
 * status; a pull runs on as a job polled through getTMDBLink. A 409
 * `conflict` means the link is broken, the setup is already done, or a pull
 * is still running.
 *
 * @param {TMDBSetupRequest} body
 * @returns {Promise<TMDBLinkStatus>}
 */
export function setupTMDBSync(body) {
  return api.post('/video/tmdb/link/setup', body)
}

/**
 * Write the three ongoing-sync switches (D-036). Off drops that kind's
 * pending queue; on never resends what is already held.
 *
 * @param {TMDBSyncKinds} kinds
 * @returns {Promise<TMDBLinkStatus>}
 */
export function setTMDBSync(kinds) {
  return api.put('/video/tmdb/link/sync', kinds)
}

/**
 * The sentence for a failed pull's error code.
 *
 * @param {string} code
 */
export function tmdbPullErrorMessage(code) {
  switch (code) {
    case 'cancelled':
      return 'The server stopped before the import finished. Run the setup again — that is safe.'
    case 'session_invalid':
      return 'TMDB no longer accepts the link. Link again, then run the setup.'
    case 'provider_unavailable':
      return 'Could not reach TMDB. Run the setup again.'
    default:
      return 'The import failed. Run the setup again.'
  }
}

/**
 * The link's start endpoint. A browser NAVIGATION, not a fetch: it redirects
 * to TMDB's approval page and TMDB redirects back into the app. Built against
 * the document base so a sub-path deployment resolves — the same rule as the
 * single sign-on start in the auth feature.
 */
export function tmdbLinkStartUrl() {
  return new URL('api/v1/video/tmdb/link/start', document.baseURI).toString()
}

/**
 * The sentence for a `?tmdb_link=` result code the callback redirected here
 * with. `ok` has no message — the linked state speaks for itself.
 *
 * @param {string} code
 * @returns {{ tone: 'ok' | 'error', text: string } | null}
 */
export function tmdbLinkResultMessage(code) {
  switch (code) {
    case 'ok':
      return { tone: 'ok', text: 'Your TMDB account is linked.' }
    case 'denied':
      return { tone: 'error', text: 'TMDB did not approve the link. Nothing was changed.' }
    case 'expired':
      return { tone: 'error', text: 'That link attempt expired. Please try again.' }
    case 'provider_unavailable':
      return { tone: 'error', text: 'Could not reach TMDB. Try again shortly.' }
    case 'failed':
      return { tone: 'error', text: 'Linking failed. Please try again.' }
    default:
      return null
  }
}

/**
 * One Jellyfin library scan's outcome (D-033). The `jellyfin_*` counts are
 * what the server holds; the `matched_*` counts are what found a catalog row.
 * On an error nothing was written and the previous scan's answer stands.
 *
 * @typedef {Object} JellyfinScanReport
 * @property {string} started_at RFC3339
 * @property {string} finished_at RFC3339
 * @property {number} jellyfin_movies
 * @property {number} jellyfin_series
 * @property {number} jellyfin_episodes
 * @property {number} matched_movies
 * @property {number} matched_seasons
 * @property {'auth_failed' | 'provider_unavailable' | 'internal' | null} error
 */

/**
 * The Jellyfin library scan's state. In server memory only — a restart
 * forgets the last report.
 *
 * @typedef {Object} JellyfinStatus
 * @property {boolean} configured a server is set; false means the jellyfin
 *   copy tile is a hand-set label and the scan route is a 404
 * @property {string | null} server_name what the server calls itself, once pinged
 * @property {boolean} scanning
 * @property {number} interval_seconds the scheduled cadence; 0 means only "scan now"
 * @property {JellyfinScanReport | null} last
 */

/**
 * Whether a Jellyfin server is configured, and the last scan's report. A
 * pure local read.
 *
 * @returns {Promise<JellyfinStatus>}
 */
export function getJellyfinStatus() {
  return api.get('/video/jellyfin')
}

/**
 * Run the library scan now. 202 with the state as of now when started;
 * `conflict` (409) while one is already running; `not_found` (404) when no
 * server is configured. Poll {@link getJellyfinStatus} for the report.
 *
 * @returns {Promise<JellyfinStatus>}
 */
export function startJellyfinScan() {
  return api.post('/video/jellyfin/scan', {})
}

/**
 * A short human sentence for a scan report's error code.
 *
 * @param {string} code
 */
export function jellyfinErrorLabel(code) {
  switch (code) {
    case 'auth_failed':
      return 'The Jellyfin server refused the API key. Check JELLYFIN_API_KEY.'
    case 'provider_unavailable':
      return 'The Jellyfin server could not be reached.'
    default:
      return 'The scan failed on the server. Check its log.'
  }
}

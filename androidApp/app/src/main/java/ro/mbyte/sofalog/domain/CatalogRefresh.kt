package ro.mbyte.sofalog.domain

import java.time.Duration
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId

/*
 * Which stored snapshots the catalog refresh re-fetches (SPEC D-025, on the device). The row's
 * `fetched_at` IS the schedule: there is no last-run state, so a page view that refreshed a title
 * merely postpones it. The server's selection is `refresh/store_refresh.go`; this is the same
 * rule over one user's library.
 */

/** Listed titles and followed shows still in production. */
val ACTIVE_REFRESH_INTERVAL: Duration = Duration.ofDays(7)

/** Watched movies and followed shows that ended, on no list. */
val ARCHIVE_REFRESH_INTERVAL: Duration = Duration.ofDays(90)

/**
 * One tracked title, as the refresh sees it.
 *
 * [listed] is "on the watchlist or a favorite". [held] is the weaker claim that keeps a title
 * worth refreshing at all: a movie with a watch in the log, a show with a state row that is not
 * dropped. [ended] is TMDB saying nothing more is coming — its production status `Ended` or
 * `Canceled` for a show, and always true for a movie.
 */
data class RefreshCandidate(
    val tmdbId: Long,
    val fetchedAt: String,
    val listed: Boolean,
    val held: Boolean,
    val ended: Boolean,
    /**
     * A followed show holding an episode that aired since its season was last fetched
     * ([airedSinceFetch]). Never a movie.
     */
    val airedSinceFetch: Boolean = false,
)

/**
 * Whether an episode dated [lastAired] (YYYY-MM-DD) aired after the snapshot taken at
 * [fetchedAt]: the snapshot's day in the user's [zone] is strictly before the air date. A
 * snapshot taken ON the air day counts as current — which is also what ends the rule, since one
 * refetch moves `fetched_at` to today. An unreadable instant counts as stale.
 */
fun airedSinceFetch(fetchedAt: String, lastAired: String, zone: ZoneId): Boolean {
    val fetched = runCatching { Instant.parse(fetchedAt) }.getOrNull() ?: return true
    return LocalDate.ofInstant(fetched, zone).toString() < lastAired
}

/** TMDB's production statuses after which a show is on the slow cadence. An unknown one is not. */
fun showEnded(showStatus: String?): Boolean = showStatus == "Ended" || showStatus == "Canceled"

/** The cadence a title is on, or null for one nothing refers to: a list always means the faster. */
fun refreshInterval(candidate: RefreshCandidate): Duration? = when {
    candidate.listed -> ACTIVE_REFRESH_INTERVAL
    !candidate.held -> null
    candidate.ended -> ARCHIVE_REFRESH_INTERVAL
    else -> ACTIVE_REFRESH_INTERVAL
}

/**
 * Whether the snapshot predates [now] minus the title's interval — or, whatever its age, belongs
 * to a followed show with an episode that aired since. An unreadable instant is due.
 */
fun refreshDue(candidate: RefreshCandidate, now: Instant): Boolean {
    val interval = refreshInterval(candidate) ?: return false
    // Air day is when a stale snapshot shows: a moved date, or the episode after it still unknown.
    if (candidate.held && candidate.airedSinceFetch) return true
    val fetched = runCatching { Instant.parse(candidate.fetchedAt) }.getOrNull() ?: return true
    return fetched.isBefore(now.minus(interval))
}

/** The TMDB ids to re-fetch, in the order given. */
fun dueForRefresh(candidates: List<RefreshCandidate>, now: Instant): List<Long> =
    candidates.filter { refreshDue(it, now) }.map { it.tmdbId }

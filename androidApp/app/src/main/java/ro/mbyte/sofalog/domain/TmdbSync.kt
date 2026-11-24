package ro.mbyte.sofalog.domain

import java.net.URLEncoder
import java.time.Duration
import java.time.Instant

/*
 * The rules of the TMDB account link (D-029, D-036) that need no device: how a pulled rating is
 * rounded, how a failed send backs off, when fetching is given up, what the browser's return
 * means. The server's `tmdbaccount` package holds the same rules; `fixtures/video-rules/tmdb-sync.json`
 * keeps the two in step.
 */

/** TMDB rates in halves; sofalog in whole numbers 1..10. Half rounds up. */
fun roundTmdbRating(value: Double): Int = Math.floor(value + 0.5).toInt().coerceIn(1, 10)

/** 30 s, doubling, capped at an hour. There is no terminal state: an outage is not the row's fault. */
fun syncBackoff(attempts: Int): Duration {
    val steps = (attempts - 1).coerceIn(0, 10)
    return minOf(Duration.ofSeconds(30L shl steps), Duration.ofHours(1))
}

/** Opens after [trips] consecutive outages; a success closes it again. */
class ProviderBreaker(private val trips: Int = 3) {
    private var run = 0
    val open: Boolean get() = run >= trips

    fun observe(outage: Boolean) {
        run = if (outage) run + 1 else 0
    }
}

/** What a failed TMDB call means, by HTTP status. [statusCode] is TMDB's own, read from the body. */
enum class TmdbFailure { BAD_KEY, SESSION_INVALID, NOT_FOUND, REFUSED, UNAVAILABLE }

/**
 * A 401 on a call that carried the user's session is the session's fault — unless TMDB says the
 * API key itself is invalid (its status 7).
 */
fun tmdbFailure(http: Int, userSession: Boolean, statusCode: Int?): TmdbFailure = when (http) {
    401 -> if (userSession && statusCode != 7) TmdbFailure.SESSION_INVALID else TmdbFailure.BAD_KEY
    404 -> TmdbFailure.NOT_FOUND
    400, 422 -> TmdbFailure.REFUSED
    else -> TmdbFailure.UNAVAILABLE
}

/** What the worker does with one outbox row after trying to send it. */
enum class SendOutcome { SENT, BROKEN, DROP, DEFER }

/** [failure] null is a success. A title TMDB no longer knows, or a refused body, is dropped for good. */
fun classifySend(failure: TmdbFailure?): SendOutcome = when (failure) {
    null -> SendOutcome.SENT
    TmdbFailure.SESSION_INVALID -> SendOutcome.BROKEN
    TmdbFailure.NOT_FOUND, TmdbFailure.REFUSED -> SendOutcome.DROP
    TmdbFailure.UNAVAILABLE, TmdbFailure.BAD_KEY -> SendOutcome.DEFER
}

/** The path of a rating on TMDB. A season is never rated (D-020), so there is no season form. */
fun ratingPath(targetType: String, tmdbId: Long, season: Int, episode: Int): String = when (targetType) {
    "movie" -> "movie/$tmdbId/rating"
    "show" -> "tv/$tmdbId/rating"
    else -> "tv/$tmdbId/season/$season/episode/$episode/rating"
}

/** TMDB calls a show `tv`. */
fun accountMediaType(targetType: String): String = if (targetType == "movie") "movie" else "tv"

const val TMDB_LINK_REDIRECT = "sofalog://tmdb-link"

/** How long a link attempt may wait for the approval. TMDB's own token lives 60 minutes. */
val TMDB_LINK_TTL: Duration = Duration.ofMinutes(45)

fun authenticateUrl(requestToken: String): String =
    "https://www.themoviedb.org/authenticate/" + URLEncoder.encode(requestToken, "UTF-8") +
        "?redirect_to=" + URLEncoder.encode(TMDB_LINK_REDIRECT, "UTF-8")

/** What TMDB's page sent back through the app's link. */
data class LinkReturn(val requestToken: String?, val denied: Boolean)

/** Null when [uri] is not the app's link return at all. */
fun parseLinkReturn(uri: String?): LinkReturn? {
    if (uri == null || !uri.startsWith(TMDB_LINK_REDIRECT)) return null
    val query = uri.substringAfter('?', "").substringBefore('#')
    val params = query.split('&').filter { it.isNotEmpty() }.associate {
        it.substringBefore('=') to java.net.URLDecoder.decode(it.substringAfter('=', ""), "UTF-8")
    }
    return LinkReturn(params["request_token"]?.takeIf { it.isNotEmpty() }, params["denied"] == "true")
}

enum class LinkDecision { IGNORE, EXPIRED, DENIED, EXCHANGE }

/**
 * What to do with a return. Any page can fire the app's link, so a return whose token is not the
 * pending one is ignored — and "approved" is never believed: the exchange is the truth.
 */
fun linkReturnDecision(pendingToken: String?, startedAt: String?, now: Instant, returned: LinkReturn?): LinkDecision {
    if (pendingToken == null) return LinkDecision.IGNORE
    if (returned != null && returned.requestToken != pendingToken) return LinkDecision.IGNORE
    val started = startedAt?.let { runCatching { Instant.parse(it) }.getOrNull() }
    if (started == null || !now.isBefore(started.plus(TMDB_LINK_TTL))) return LinkDecision.EXPIRED
    if (returned?.denied == true) return LinkDecision.DENIED
    return LinkDecision.EXCHANGE
}

// ---- the setup (D-036)

enum class SyncAction { PULL, PUSH, SKIP }

/** One kind's answer: what to do once, and whether to keep it in sync from then on. */
data class KindSetup(val action: SyncAction = SyncAction.SKIP, val sync: Boolean = false) {
    /** Picking an action presets the switch: importing or sending implies wanting it kept. */
    fun pick(next: SyncAction) = KindSetup(next, sync = next != SyncAction.SKIP)
}

data class SetupDraft(
    val ratings: KindSetup = KindSetup(),
    val watchlist: KindSetup = KindSetup(),
    val favorites: KindSetup = KindSetup(),
) {
    val pulls: Boolean get() = listOf(ratings, watchlist, favorites).any { it.action == SyncAction.PULL }
    val pushes: Boolean get() = listOf(ratings, watchlist, favorites).any { it.action == SyncAction.PUSH }
}

data class SyncKinds(val ratings: Boolean, val watchlist: Boolean, val favorites: Boolean)

// ---- the pull job

const val PULL_RUNNING = "running"
const val PULL_DONE = "done"
const val PULL_FAILED = "failed"

data class TmdbPullReport(
    val ratings: Int = 0,
    val episodeRatings: Int = 0,
    val watchlist: Int = 0,
    val favorites: Int = 0,
    val moviesAdded: Int = 0,
    val showsAdded: Int = 0,
    val skipped: Int = 0,
)

/** The setup's import as the page shows it. [error] is a code, turned into a sentence by [pullErrorMessage]. */
data class TmdbPullJob(
    val state: String = PULL_RUNNING,
    val section: String? = null,
    val list: String? = null,
    val page: Int = 0,
    val totalPages: Int = 0,
    val entries: Int = 0,
    val report: TmdbPullReport = TmdbPullReport(),
    val error: String? = null,
)

fun pullProgressLine(job: TmdbPullJob): String = when (job.section) {
    null -> "Preparing…"
    else -> "Importing ${job.section} — ${job.list}, page ${job.page} of ${maxOf(job.totalPages, job.page)}. " +
        "${job.entries} ${if (job.entries == 1) "entry" else "entries"} so far."
}

fun pullErrorMessage(code: String): String = when (code) {
    "cancelled" -> "The app was closed before the import finished. Run the setup again — that is safe."
    "session_invalid" -> "TMDB no longer accepts the link. Link again, then run the setup."
    "provider_unavailable" -> "Could not reach TMDB. Run the setup again."
    else -> "The import failed. Run the setup again."
}

fun pendingLine(pending: Int): String = when (pending) {
    0 -> "Everything has been sent."
    1 -> "1 change is waiting to be sent."
    else -> "$pending changes are waiting to be sent."
}

/** Blanks every secret a string might carry — a key, a session, a request token. */
fun redactSecrets(text: String): String = text
    .replace(Regex("(?i)(api_key|session_id|request_token)=[^&\\s\"]+"), "$1=REDACTED")
    .replace(Regex("(?i)\"(api_key|session_id|request_token)\"\\s*:\\s*\"[^\"]*\""), "\"$1\":\"REDACTED\"")
    .replace(Regex("(?i)Bearer\\s+[A-Za-z0-9._~+/=-]+"), "Bearer REDACTED")

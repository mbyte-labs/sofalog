package ro.mbyte.sofalog.domain

/*
 * A restore as the page shows it: progress while it runs, the report when it ends. All immutable —
 * the runner rebuilds the job through the functions below. The sentences are the web app's
 * (frontend/src/features/settings/api.js), kept word for word.
 */

const val RESTORE_SKIP_CAP = 200

const val RESTORE_RUNNING = "running"
const val RESTORE_DONE = "done"
const val RESTORE_FAILED = "failed"

/** What one group's transaction wrote. */
data class RestoreCounts(
    val historyInserted: Int = 0,
    val historyReplaced: Int = 0,
    val ratingsUpserted: Int = 0,
    val watchlistInserted: Int = 0,
    val favoritesInserted: Int = 0,
    val copiesInserted: Int = 0,
    val showsDropped: Int = 0,
)

data class RestoreReport(
    val moviesTotal: Int = 0,
    val showsTotal: Int = 0,
    val moviesMaterialised: Int = 0,
    val showsMaterialised: Int = 0,
    val historyInserted: Int = 0,
    val historyReplaced: Int = 0,
    val ratingsUpserted: Int = 0,
    val watchlistInserted: Int = 0,
    val favoritesInserted: Int = 0,
    val copiesInserted: Int = 0,
    val showsDropped: Int = 0,
    val skippedTotal: Int = 0,
    /** At most [RESTORE_SKIP_CAP] entries; [skippedTotal] always counts them all. */
    val skipped: List<RestoreSkip> = emptyList(),
)

fun RestoreReport.withSkips(more: List<RestoreSkip>): RestoreReport = copy(
    skippedTotal = skippedTotal + more.size,
    skipped = if (skipped.size >= RESTORE_SKIP_CAP) skipped else (skipped + more).take(RESTORE_SKIP_CAP),
)

operator fun RestoreReport.plus(c: RestoreCounts): RestoreReport = copy(
    historyInserted = historyInserted + c.historyInserted,
    historyReplaced = historyReplaced + c.historyReplaced,
    ratingsUpserted = ratingsUpserted + c.ratingsUpserted,
    watchlistInserted = watchlistInserted + c.watchlistInserted,
    favoritesInserted = favoritesInserted + c.favoritesInserted,
    copiesInserted = copiesInserted + c.copiesInserted,
    showsDropped = showsDropped + c.showsDropped,
)

data class RestoreCurrent(val type: String, val tmdbId: Long, val title: String?)

data class RestoreProgress(val groupsTotal: Int, val groupsDone: Int = 0, val current: RestoreCurrent? = null)

data class RestoreJob(
    val state: String,
    val startedAt: String,
    val finishedAt: String? = null,
    val progress: RestoreProgress,
    val report: RestoreReport,
    /** Why a failed job stopped, as a sentence; null while running and when done. */
    val error: String? = null,
)

const val RESTORE_CANCELLED_MESSAGE =
    "The import was stopped before it finished. Run it again to complete it — that is safe, the " +
        "import replaces what it already wrote."
const val RESTORE_INTERNAL_MESSAGE =
    "The import stopped on an internal error. Run it again — that is safe, the import replaces " +
        "what it already wrote."

fun restoreProgressLine(p: RestoreProgress): String {
    val current = p.current ?: return "Preparing…"
    val n = minOf(p.groupsDone + 1, p.groupsTotal)
    return "Importing ${current.type} $n of ${p.groupsTotal} — ${current.title ?: "Fetching…"}"
}

fun restorePercent(p: RestoreProgress): Int = if (p.groupsTotal <= 0) 0 else p.groupsDone * 100 / p.groupsTotal

private val SKIP_LABELS = mapOf(
    SKIP_MISSING_TMDB_ID to "No TMDB id in the file",
    SKIP_UNKNOWN_TYPE to "Unknown item type",
    SKIP_MISSING_POSITION to "Season or episode number missing",
    SKIP_INVALID_TIMESTAMP to "Unreadable date",
    SKIP_FUTURE_TIMESTAMP to "Date is in the future",
    SKIP_INVALID_WATCH_TYPE to "Unknown watch type",
    SKIP_RATING_OUT_OF_RANGE to "Rating outside 1–10",
    SKIP_SEASON_RATING to "Seasons can't be rated in sofalog",
    SKIP_WATCHLIST_TARGET to "Only movies and shows can be watchlisted",
    SKIP_MOVIE_NOT_FOUND to "Movie not found on TMDB",
    SKIP_MOVIE_UNAVAILABLE to "Movie could not be fetched",
    SKIP_SHOW_NOT_FOUND to "Show not found on TMDB",
    SKIP_SHOW_UNAVAILABLE to "Show could not be fetched",
    SKIP_PROVIDER_UNAVAILABLE to "TMDB was unreachable",
    SKIP_EPISODE_NOT_FOUND to "Episode not in TMDB's listing",
    SKIP_UNKNOWN_COPY_KIND to "Unknown kind of copy",
    SKIP_SEASON_NOT_FOUND to "Season not in TMDB's listing",
)

fun skipReasonLabel(reason: String): String = SKIP_LABELS[reason] ?: reason.replace('_', ' ')

private const val RERUN = "Run the import again later — it replaces rather than duplicates, so that's safe."

private val SKIP_EXPLANATIONS = mapOf(
    SKIP_MISSING_TMDB_ID to
        "Ticket Box didn't know this item's TMDB id when it exported, so there is nothing to look it up by.",
    SKIP_UNKNOWN_TYPE to "The file names a kind of item sofalog doesn't track.",
    SKIP_MISSING_POSITION to "Episodes are matched by season and episode number, and the file left one out.",
    SKIP_INVALID_TIMESTAMP to "The date isn't in a format sofalog can read.",
    SKIP_FUTURE_TIMESTAMP to
        "A viewing cannot be in the future — check the clock of the device Ticket Box ran on.",
    SKIP_INVALID_WATCH_TYPE to "Only watch, check-in and scrobble are known.",
    SKIP_RATING_OUT_OF_RANGE to "Ratings are whole numbers from 1 to 10.",
    SKIP_SEASON_RATING to "Rate the season's episodes, or the show, instead.",
    SKIP_WATCHLIST_TARGET to "Add the show to your watchlist instead.",
    SKIP_MOVIE_NOT_FOUND to
        "TMDB has no movie with this id — it may have been deleted or merged into another entry.",
    SKIP_SHOW_NOT_FOUND to
        "TMDB has no show with this id — it may have been deleted or merged into another entry.",
    SKIP_MOVIE_UNAVAILABLE to "TMDB didn't answer when asked for it. $RERUN",
    SKIP_SHOW_UNAVAILABLE to "TMDB didn't answer when asked for it. $RERUN",
    SKIP_PROVIDER_UNAVAILABLE to "TMDB failed three times in a row, so the import stopped asking. $RERUN",
    SKIP_UNKNOWN_COPY_KIND to "The file names a format this version of sofalog doesn't know.",
    SKIP_SEASON_NOT_FOUND to "TMDB no longer lists this season for the show, so the copy has nothing to hang on.",
)

/** The likely cause of a skip as a sentence, or null when the label says it all. */
fun skipReasonExplanation(s: RestoreSkip): String? {
    if (s.reason != SKIP_EPISODE_NOT_FOUND) return SKIP_EXPLANATIONS[s.reason]
    val season = s.season?.toString() ?: "?"
    val episode = s.episode ?: 0
    return when (s.hint) {
        HINT_SPECIALS_NUMBERING ->
            "Trakt and TMDB number specials differently, and the file's episode id didn't match any of " +
                "TMDB's specials for this show."
        HINT_SEASON_MISSING -> "TMDB lists no season $season for this show."
        HINT_PAST_SEASON_END ->
            "TMDB's season $season ends at episode ${s.tmdbLastEpisode ?: "?"}, so this is most likely the " +
                "second half of a two-part episode that TMDB lists as one."
        HINT_NUMBERING_GAP ->
            if (episode > 1) {
                "TMDB skips episode $episode in season $season, so episode ${episode - 1} is most likely a " +
                    "two-part episode that TMDB lists as one."
            } else {
                "TMDB lists no episode $episode in season $season, though it lists the episodes after it."
            }
        else -> null
    }
}

fun skipKindLabel(kind: String): String = when (kind) {
    RESTORE_HISTORY -> "Watch"
    RESTORE_RATING -> "Rating"
    RESTORE_WATCHLIST -> "Watchlist"
    RESTORE_FAVORITE -> "Favorite"
    RESTORE_COPY -> "Copy"
    RESTORE_DROPPED -> "Dropped"
    RESTORE_TITLE -> "Title"
    else -> kind
}

/** What a skip is about: the title when known, else its TMDB id, then its place in the show. */
fun skipTarget(s: RestoreSkip): String {
    val episodeish = s.showTmdbId != null && s.tmdbId == null || s.type == "episode" || s.type == "season"
    val base = s.title ?: when {
        episodeish -> s.showTmdbId?.let { "Show $it" }
        s.type == "movie" -> s.tmdbId?.let { "Movie $it" }
        s.type == "show" -> s.tmdbId?.let { "Show $it" }
        else -> (s.tmdbId ?: s.showTmdbId)?.let { "Item $it" }
    } ?: "Unknown item"
    val season = s.season?.let { if (it == 0) " · Specials" else " · Season $it" }.orEmpty()
    val episode = s.episode?.let { " · Episode $it" }.orEmpty()
    return base + season + episode
}

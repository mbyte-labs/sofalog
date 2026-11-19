package ro.mbyte.sofalog.domain

import java.time.Instant

/*
 * A Ticket Box file regrouped by the catalog item each entry belongs to, every value already
 * normalised, so the run needs no further judgement — the server's `ValidateTicketBoxDump`, rule
 * for rule. Per entry the FIRST failing check names the skip reason.
 */

const val SKIP_MISSING_TMDB_ID = "missing_tmdb_id"
const val SKIP_UNKNOWN_TYPE = "unknown_type"
const val SKIP_MISSING_POSITION = "missing_position"
const val SKIP_INVALID_TIMESTAMP = "invalid_timestamp"
const val SKIP_FUTURE_TIMESTAMP = "future_timestamp"
const val SKIP_INVALID_WATCH_TYPE = "invalid_watch_type"
const val SKIP_RATING_OUT_OF_RANGE = "rating_out_of_range"
const val SKIP_SEASON_RATING = "season_rating_unsupported"
const val SKIP_WATCHLIST_TARGET = "watchlist_target_unsupported"
const val SKIP_MOVIE_NOT_FOUND = "movie_not_found"
const val SKIP_MOVIE_UNAVAILABLE = "movie_unavailable"
const val SKIP_SHOW_NOT_FOUND = "show_not_found"
const val SKIP_SHOW_UNAVAILABLE = "show_unavailable"
const val SKIP_PROVIDER_UNAVAILABLE = "provider_unavailable"
const val SKIP_EPISODE_NOT_FOUND = "episode_not_found"

const val RESTORE_HISTORY = "history"
const val RESTORE_RATING = "rating"
const val RESTORE_WATCHLIST = "watchlist"
const val RESTORE_FAVORITE = "favorite"

private val WATCH_TYPES = setOf("watch", "checkin", "scrobble")

/** One entry of the file that was not imported, and why. */
data class RestoreSkip(
    val kind: String,
    val type: String?,
    val tmdbId: Long?,
    val showTmdbId: Long?,
    val season: Int?,
    val episode: Int?,
    val reason: String,
    val title: String? = null,
    val hint: String? = null,
    val tmdbLastEpisode: Int? = null,
)

data class PlannedRating(val value: Int, val at: String)

/** An episode entry keeps the file's episode TMDB id beside its position: the id is resolved first. */
data class PlannedEpisodeWatch(val season: Int, val episode: Int, val tmdbId: Long?, val at: String)

data class PlannedEpisodeRating(val season: Int, val episode: Int, val tmdbId: Long?, val rating: PlannedRating)

data class MovieGroup(
    val tmdbId: Long,
    val watches: List<String>,
    val rating: PlannedRating?,
    val watchlistAt: String?,
    val favoriteAt: String?,
) {
    /** A whole-group refusal as one skip per entry, so the counts stay exact. */
    fun skips(reason: String): List<RestoreSkip> = buildList {
        repeat(watches.size) { add(RestoreSkip(RESTORE_HISTORY, "movie", tmdbId, null, null, null, reason)) }
        if (rating != null) add(RestoreSkip(RESTORE_RATING, "movie", tmdbId, null, null, null, reason))
        if (watchlistAt != null) add(RestoreSkip(RESTORE_WATCHLIST, "movie", tmdbId, null, null, null, reason))
        if (favoriteAt != null) add(RestoreSkip(RESTORE_FAVORITE, "movie", tmdbId, null, null, null, reason))
    }
}

data class ShowGroup(
    val tmdbId: Long,
    val watches: List<PlannedEpisodeWatch>,
    val showRating: PlannedRating?,
    val episodeRatings: List<PlannedEpisodeRating>,
    val watchlistAt: String?,
    val favoriteAt: String?,
) {
    fun skips(reason: String): List<RestoreSkip> = buildList {
        watches.forEach { add(RestoreSkip(RESTORE_HISTORY, "episode", null, tmdbId, it.season, it.episode, reason)) }
        if (showRating != null) add(RestoreSkip(RESTORE_RATING, "show", tmdbId, null, null, null, reason))
        episodeRatings.forEach { add(RestoreSkip(RESTORE_RATING, "episode", null, tmdbId, it.season, it.episode, reason)) }
        if (watchlistAt != null) add(RestoreSkip(RESTORE_WATCHLIST, "show", tmdbId, null, null, null, reason))
        if (favoriteAt != null) add(RestoreSkip(RESTORE_FAVORITE, "show", tmdbId, null, null, null, reason))
    }
}

data class RestorePlan(val movies: List<MovieGroup>, val shows: List<ShowGroup>) {
    val groups: Int get() = movies.size + shows.size
}

private class MovieDraft(
    val watches: MutableList<String> = mutableListOf(),
    var rating: PlannedRating? = null,
    var watchlistAt: String? = null,
    var favoriteAt: String? = null,
)

private class ShowDraft(
    val watches: MutableList<PlannedEpisodeWatch> = mutableListOf(),
    var showRating: PlannedRating? = null,
    val episodeRatings: MutableMap<Pair<Int, Int>, PlannedEpisodeRating> = mutableMapOf(),
    var watchlistAt: String? = null,
    var favoriteAt: String? = null,
)

fun validateTicketBoxDump(dump: TicketBoxDump, now: Instant): Pair<RestorePlan, List<RestoreSkip>> {
    val movies = sortedMapOf<Long, MovieDraft>()
    val shows = sortedMapOf<Long, ShowDraft>()
    val skips = mutableListOf<RestoreSkip>()
    fun movie(id: Long) = movies.getOrPut(id) { MovieDraft() }
    fun show(id: Long) = shows.getOrPut(id) { ShowDraft() }
    fun skip(kind: String, a: TicketBoxAddress, reason: String) {
        skips += RestoreSkip(kind, a.type, a.tmdbId, a.showTmdbId, a.season, a.episode, reason)
    }
    // An instant, or the skip reason it earns.
    fun instant(raw: String?): Pair<String?, String?> = when (val r = normaliseInstant(raw, now)) {
        is InstantResult.Ok -> r.at to null
        InstantResult.Future -> null to SKIP_FUTURE_TIMESTAMP
        InstantResult.Unparsable -> null to SKIP_INVALID_TIMESTAMP
    }
    fun itemId(a: TicketBoxAddress): Long? = a.tmdbId?.takeIf { it >= 1 }
    // Season 0 is Specials and a pilot may be episode 0, so only negatives are refused.
    fun episodeReason(a: TicketBoxAddress): String? {
        val showId = a.showTmdbId
        val season = a.season
        val episode = a.episode
        return when {
            showId == null || showId < 1 -> SKIP_MISSING_TMDB_ID
            season == null || episode == null || season < 0 || episode < 0 -> SKIP_MISSING_POSITION
            else -> null
        }
    }

    for (e in dump.history.orEmpty()) {
        when (e.type) {
            "movie" -> {
                val id = itemId(e)
                if (id == null) { skip(RESTORE_HISTORY, e, SKIP_MISSING_TMDB_ID); continue }
                val (at, reason) = instant(e.addedAt)
                if (at == null) { skip(RESTORE_HISTORY, e, reason!!); continue }
                if (e.historyType !in WATCH_TYPES) { skip(RESTORE_HISTORY, e, SKIP_INVALID_WATCH_TYPE); continue }
                movie(id).watches += at
            }
            "episode" -> {
                val bad = episodeReason(e)
                if (bad != null) { skip(RESTORE_HISTORY, e, bad); continue }
                val (at, reason) = instant(e.addedAt)
                if (at == null) { skip(RESTORE_HISTORY, e, reason!!); continue }
                if (e.historyType !in WATCH_TYPES) { skip(RESTORE_HISTORY, e, SKIP_INVALID_WATCH_TYPE); continue }
                show(e.showTmdbId!!).watches += PlannedEpisodeWatch(e.season!!, e.episode!!, e.tmdbId, at)
            }
            else -> skip(RESTORE_HISTORY, e, SKIP_UNKNOWN_TYPE)
        }
    }

    // Ratings: a later entry for the same target wins, as the upsert would decide anyway.
    for (e in dump.ratings.orEmpty()) {
        when (e.type) {
            "season" -> { skip(RESTORE_RATING, e, SKIP_SEASON_RATING); continue }
            "movie", "show", "episode" -> Unit
            else -> { skip(RESTORE_RATING, e, SKIP_UNKNOWN_TYPE); continue }
        }
        val value = e.rating
        if (value == null || value !in 1..10) { skip(RESTORE_RATING, e, SKIP_RATING_OUT_OF_RANGE); continue }
        if (e.type == "episode") {
            val bad = episodeReason(e)
            if (bad != null) { skip(RESTORE_RATING, e, bad); continue }
            val (at, reason) = instant(e.ratedAt)
            if (at == null) { skip(RESTORE_RATING, e, reason!!); continue }
            show(e.showTmdbId!!).episodeRatings[e.season!! to e.episode!!] =
                PlannedEpisodeRating(e.season, e.episode, e.tmdbId, PlannedRating(value, at))
        } else {
            val id = itemId(e)
            if (id == null) { skip(RESTORE_RATING, e, SKIP_MISSING_TMDB_ID); continue }
            val (at, reason) = instant(e.ratedAt)
            if (at == null) { skip(RESTORE_RATING, e, reason!!); continue }
            if (e.type == "movie") movie(id).rating = PlannedRating(value, at) else show(id).showRating = PlannedRating(value, at)
        }
    }

    for (e in dump.watchlist.orEmpty()) {
        when (e.type) {
            "season", "episode" -> { skip(RESTORE_WATCHLIST, e, SKIP_WATCHLIST_TARGET); continue }
            "movie", "show" -> Unit
            else -> { skip(RESTORE_WATCHLIST, e, SKIP_UNKNOWN_TYPE); continue }
        }
        val id = itemId(e)
        if (id == null) { skip(RESTORE_WATCHLIST, e, SKIP_MISSING_TMDB_ID); continue }
        val (at, reason) = instant(e.listedAt)
        if (at == null) { skip(RESTORE_WATCHLIST, e, reason!!); continue }
        if (e.type == "movie") movie(id).watchlistAt = at else show(id).watchlistAt = at
    }

    // Favorites are movies and shows only (D-030); anything else is simply an unknown type.
    for (e in dump.favorites.orEmpty()) {
        if (e.type != "movie" && e.type != "show") { skip(RESTORE_FAVORITE, e, SKIP_UNKNOWN_TYPE); continue }
        val id = itemId(e)
        if (id == null) { skip(RESTORE_FAVORITE, e, SKIP_MISSING_TMDB_ID); continue }
        val (at, reason) = instant(e.favoritedAt)
        if (at == null) { skip(RESTORE_FAVORITE, e, reason!!); continue }
        if (e.type == "movie") movie(id).favoriteAt = at else show(id).favoriteAt = at
    }

    val plan = RestorePlan(
        movies = movies.map { (id, d) -> MovieGroup(id, d.watches.toList(), d.rating, d.watchlistAt, d.favoriteAt) },
        shows = shows.map { (id, d) ->
            ShowGroup(
                id, d.watches.toList(), d.showRating,
                d.episodeRatings.values.sortedWith(compareBy({ it.season }, { it.episode })),
                d.watchlistAt, d.favoriteAt,
            )
        },
    )
    return plan to skips
}

// ---- episodes

/** One held episode by both of its keys. */
data class EpisodeKey(val id: Long, val season: Int, val number: Int, val tmdbId: Long?)

const val HINT_SPECIALS_NUMBERING = "specials_numbering"
const val HINT_SEASON_MISSING = "season_missing"
const val HINT_PAST_SEASON_END = "past_season_end"
const val HINT_NUMBERING_GAP = "numbering_gap"

/**
 * Finds the local episode a file entry means. The episode's own TMDB id is tried FIRST and its
 * position only as the fallback: Ticket Box's positions are what it cached, and TMDB renumbers.
 * When two held rows share a TMDB id (a renumbered episode whose old row was kept), the one at the
 * file's own position wins, else the newest.
 */
class EpisodeResolver(rows: List<EpisodeKey>) {
    private val byTmdb = rows.filter { it.tmdbId != null }.groupBy { it.tmdbId }
    private val byPosition = rows.associateBy { it.season to it.number }
    private val seasons = rows.groupBy({ it.season }, { it.number })

    fun resolve(tmdbId: Long?, season: Int, episode: Int): Long? {
        val same = tmdbId?.let { byTmdb[it] }.orEmpty()
        val byId = same.firstOrNull { it.season == season && it.number == episode } ?: same.maxByOrNull { it.id }
        return byId?.id ?: byPosition[season to episode]?.id
    }

    /** The episode numbers held for [season], ascending; empty when the season is not held. */
    fun numbers(season: Int): List<Int> = seasons[season].orEmpty().sorted()
}

/** Why an episode was probably not found, from the numbers held for its season. */
fun episodeSkipHint(numbers: List<Int>, season: Int, episode: Int): Pair<String, Int?> = when {
    season == 0 -> HINT_SPECIALS_NUMBERING to null
    numbers.isEmpty() -> HINT_SEASON_MISSING to null
    episode > numbers.last() -> HINT_PAST_SEASON_END to numbers.last()
    else -> HINT_NUMBERING_GAP to numbers.last()
}

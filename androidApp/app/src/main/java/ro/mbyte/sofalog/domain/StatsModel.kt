package ro.mbyte.sofalog.domain

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.SerializationException

/*
 * The Stats page's answer (SPEC D-039), shaped like the server's `Stats` DTO field for field so the
 * shared cases in `fixtures/video-rules/stats.json` compare the two as JSON. Every section is
 * always present; one the page hides is null (or an empty list), and the screen decides nothing
 * else. Lists are never null.
 */

const val STATS_MOVIE = "movie"
const val STATS_SHOW = "show"
const val STATS_PERIOD_ALL = "all"

/** A movie or a show: the pair, never the id alone — movie 5 and show 5 both exist. */
data class StatsTitleKey(val kind: String, val id: Long)

/**
 * The one way a section refers to a movie or a show; an episode refers to its show. [id] is local
 * and never leaves the device — the page opens a title by [tmdbId], as every route does. The
 * artwork is TMDB's path, turned into a URL where it is drawn.
 */
@Serializable
data class StatsTitle(
    val kind: String,
    val id: Long,
    @SerialName("tmdb_id") val tmdbId: Long,
    val title: String,
    val year: Int?,
    @SerialName("poster_path") val posterPath: String?,
    @SerialName("backdrop_path") val backdropPath: String?,
)

@Serializable
data class Stats(
    /** The APPLIED period: "all" or a four-digit year in the user's zone. */
    val period: String,
    /** Every year the picker offers, newest first; empty with nothing recorded. */
    val years: List<Int>,
    /** The user's calendar day (D-038). */
    val today: String,
    val headline: StatsHeadline,
    @SerialName("most_watched_show") val mostWatchedShow: StatsMostWatchedShow?,
    val ratings: StatsRatings?,
    @SerialName("top_rated") val topRated: List<StatsTopRated>,
    @SerialName("rating_trend") val ratingTrend: StatsRatingTrend?,
    val crowd: StatsCrowd?,
    @SerialName("unrated_movies") val unratedMovies: StatsUnratedMovies?,
    val activity: StatsActivity,
    val decades: StatsDecades?,
    @SerialName("most_rewatched") val mostRewatched: List<StatsRewatched>,
    val completion: StatsCompletion?,
    val genres: StatsGenres?,
)

@Serializable
data class StatsHeadline(
    val hours: Int,
    val plays: Int,
    val movies: Int,
    val episodes: Int,
    @SerialName("movie_minutes") val movieMinutes: Int,
    @SerialName("episode_minutes") val episodeMinutes: Int,
)

@Serializable
data class StatsMostWatchedShow(
    val show: StatsTitle,
    val minutes: Int,
    val plays: Int,
    @SerialName("last_played_at") val lastPlayedAt: String,
)

@Serializable
data class StatsRatings(
    /** Index 0 = rating 1. */
    val distribution: List<Int>,
    val count: Int,
    val average: Double,
    @SerialName("movies_average") val moviesAverage: Double?,
    @SerialName("shows_average") val showsAverage: Double?,
    @SerialName("episodes_average") val episodesAverage: Double?,
)

@Serializable
data class StatsTopRated(
    val title: StatsTitle,
    val rating: Int,
    @SerialName("rated_at") val ratedAt: String,
)

@Serializable
data class StatsRatingTrend(
    val months: List<StatsRatingMonth>,
    val recent: StatsRatingSample,
    val earlier: StatsRatingSample,
    val delta: Double?,
    /** harsher | kinder | steady; null with [delta]. */
    val verdict: String?,
    @SerialName("reference_at") val referenceAt: String,
)

@Serializable
data class StatsRatingMonth(val month: String, val count: Int, val average: Double?)

@Serializable
data class StatsRatingSample(val count: Int, val average: Double?)

@Serializable
data class StatsCrowd(
    @SerialName("user_average") val userAverage: Double,
    @SerialName("crowd_average") val crowdAverage: Double,
    val delta: Double,
    /** harsher | kinder | in_sync. */
    val verdict: String,
    val compared: Int,
    @SerialName("total_rated") val totalRated: Int,
    @SerialName("loved_more") val lovedMore: List<StatsCrowdEntry>,
    @SerialName("loved_less") val lovedLess: List<StatsCrowdEntry>,
)

@Serializable
data class StatsCrowdEntry(
    val title: StatsTitle,
    @SerialName("user_rating") val userRating: Int,
    @SerialName("tmdb_average") val tmdbAverage: Double,
    @SerialName("tmdb_votes") val tmdbVotes: Long,
    val gap: Double,
)

@Serializable
data class StatsUnratedMovies(val total: Int, val recent: List<StatsUnratedMovie>)

@Serializable
data class StatsUnratedMovie(
    val movie: StatsTitle,
    @SerialName("last_played_at") val lastPlayedAt: String,
)

@Serializable
data class StatsActivity(
    val months: List<StatsMonthPlays>,
    val heatmap: StatsHeatmap,
    /** Monday first. */
    val weekday: List<Int>,
    @SerialName("time_of_day") val timeOfDay: StatsTimeOfDay?,
    @SerialName("longest_streak") val longestStreak: Int,
    /** Null in a past year. */
    @SerialName("current_streak") val currentStreak: Int?,
    /** All time only, once something was watched. */
    @SerialName("tracking_days") val trackingDays: Int?,
    /** A year only. */
    @SerialName("active_days") val activeDays: Int?,
    @SerialName("first_play") val firstPlay: StatsFirstPlay?,
)

@Serializable
data class StatsMonthPlays(val month: String, val plays: Int)

/** One cell per day from [start]: 365 days ending today for all time, the whole year otherwise. */
@Serializable
data class StatsHeatmap(val start: String, val plays: List<Int>, val levels: List<Int>)

@Serializable
data class StatsTimeOfDay(
    val morning: Int,
    val afternoon: Int,
    val evening: Int,
    val night: Int,
    /** morning | afternoon | evening | night; a tie goes to the earlier. */
    val dominant: String,
)

@Serializable
data class StatsFirstPlay(
    val title: StatsTitle,
    val episode: StatsEpisodeRef?,
    @SerialName("watched_at") val watchedAt: String,
)

@Serializable
data class StatsEpisodeRef(val season: Int, val number: Int)

@Serializable
data class StatsDecades(
    val buckets: List<StatsDecade>,
    val favorite: Int,
    val oldest: StatsEraTitle?,
    val newest: StatsEraTitle?,
    val covered: Int,
    val total: Int,
)

@Serializable
data class StatsDecade(val decade: Int, val titles: Int)

@Serializable
data class StatsEraTitle(val title: StatsTitle, val year: Int)

@Serializable
data class StatsRewatched(
    val title: StatsTitle,
    val plays: Int,
    @SerialName("last_played_at") val lastPlayedAt: String,
)

@Serializable
data class StatsCompletion(
    val tracked: Int,
    val completed: Int,
    @SerialName("in_progress") val inProgress: Int,
    @SerialName("average_completion") val averageCompletion: Double,
    @SerialName("almost_there") val almostThere: List<StatsAlmostThere>,
)

@Serializable
data class StatsAlmostThere(val show: StatsTitle, val watched: Int, val aired: Int, val left: Int)

@Serializable
data class StatsGenres(val entries: List<StatsGenre>, val covered: Int, val total: Int)

@Serializable
data class StatsGenre(val name: String, val titles: Int)

// ------------------------------------------------------------------------------------ input

/**
 * One watch-log row with the catalog facts the fold needs carried along — the server's `playRow`.
 * Exactly one half is set, by [targetType]. [present] is false for a row whose target is gone: it
 * still counts toward the first-play bound, and nowhere else.
 */
data class StatsPlay(
    val id: Long,
    val targetType: String,
    val targetId: Long,
    val watchedAt: String,
    val movieRuntime: Int? = null,
    val movieRelease: String? = null,
    val movieGenres: List<String>? = null,
    val showId: Long? = null,
    val season: Int? = null,
    val number: Int? = null,
    val episodeRuntime: Int? = null,
    val showFirstAir: String? = null,
    val showGenres: List<String>? = null,
    val present: Boolean = true,
)

/** One rating of a movie, a show or an episode. [present] as on [StatsPlay]. */
data class StatsRating(
    val targetType: String,
    val targetId: Long,
    val rating: Int,
    val updatedAt: String,
    val present: Boolean = true,
)

/** What "You vs the crowd" reads out of a stored TMDB body. */
data class StatsVoteFacts(val voteAverage: Double, val voteCount: Long)

/** One followed, non-dropped show's derived progress: aired non-special episodes, and how many are watched. */
data class StatsCompletionRow(val showId: Long, val aired: Int, val watched: Int)

/**
 * Everything the fold reads, for ALL time — the period is applied inside [computeStats], over the
 * rows' own instants. Nothing here is a provider call's answer: it is the device's database.
 */
data class StatsInput(
    val plays: List<StatsPlay>,
    val ratings: List<StatsRating>,
    val titles: Map<StatsTitleKey, StatsTitle>,
    /** TMDB's regular episode runtime per show, where positive — the fallback for an episode without one. */
    val showRuntimeFallback: Map<Long, Int> = emptyMap(),
    val voteFacts: Map<StatsTitleKey, StatsVoteFacts> = emptyMap(),
    val completion: List<StatsCompletionRow> = emptyList(),
)

/** The three facts the fold reads out of a stored TMDB body, and nothing else of it. */
@Serializable
private class StatsBody(
    @SerialName("vote_average") val voteAverage: Double = 0.0,
    @SerialName("vote_count") val voteCount: Long = 0,
    @SerialName("episode_run_time") val episodeRunTime: List<Int> = emptyList(),
)

private fun statsBody(body: String): StatsBody? = try {
    snapshotJson.decodeFromString<StatsBody>(body)
} catch (e: SerializationException) {
    null
} catch (e: IllegalArgumentException) {
    null
}

/** TMDB's average and vote count out of a stored movie or show body; null when it cannot be read. */
fun parseVoteFacts(body: String): StatsVoteFacts? = statsBody(body)?.let { StatsVoteFacts(it.voteAverage, it.voteCount) }

/** A show's regular episode runtime — the first entry of `episode_run_time`, when positive. */
fun parseEpisodeRuntime(body: String): Int? = statsBody(body)?.episodeRunTime?.firstOrNull()?.takeIf { it > 0 }

/** A stored genre column ("Drama, Crime") as its names; null when there is none. */
fun splitGenres(stored: String?): List<String>? =
    stored?.split(',')?.map { it.trim() }?.filter { it.isNotEmpty() }?.takeIf { it.isNotEmpty() }

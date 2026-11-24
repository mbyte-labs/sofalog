package ro.mbyte.sofalog.data.tmdb

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable
data class TmdbGenre(val id: Long = 0, val name: String = "")

@Serializable
data class TmdbSearchPage(val results: List<TmdbSearchResult> = emptyList())

/** One page of a trending or discover list: the search rows inside TMDB's paging envelope. */
@Serializable
data class TmdbListPage(
    val page: Int = 1,
    @SerialName("total_pages") val totalPages: Int = 1,
    val results: List<TmdbSearchResult> = emptyList(),
)

@Serializable
data class TmdbGenreList(val genres: List<TmdbGenre> = emptyList())

@Serializable
data class TmdbLanguage(
    @SerialName("iso_639_1") val code: String = "",
    @SerialName("english_name") val englishName: String = "",
)

@Serializable
data class TmdbSearchResult(
    val id: Long,
    @SerialName("media_type") val mediaType: String? = null,
    val title: String? = null,
    val name: String? = null,
    @SerialName("release_date") val releaseDate: String? = null,
    @SerialName("first_air_date") val firstAirDate: String? = null,
    @SerialName("poster_path") val posterPath: String? = null,
    @SerialName("vote_average") val voteAverage: Double? = null,
    val popularity: Double? = null,
    val overview: String? = null,
)

@Serializable
data class TmdbMovie(
    val id: Long,
    val title: String = "",
    @SerialName("release_date") val releaseDate: String? = null,
    val runtime: Int? = null,
    val overview: String? = null,
    val genres: List<TmdbGenre> = emptyList(),
    @SerialName("poster_path") val posterPath: String? = null,
    @SerialName("backdrop_path") val backdropPath: String? = null,
    @SerialName("vote_average") val voteAverage: Double? = null,
)

@Serializable
data class TmdbShow(
    val id: Long,
    val name: String = "",
    @SerialName("first_air_date") val firstAirDate: String? = null,
    val status: String? = null,
    val overview: String? = null,
    val genres: List<TmdbGenre> = emptyList(),
    @SerialName("poster_path") val posterPath: String? = null,
    @SerialName("backdrop_path") val backdropPath: String? = null,
    @SerialName("vote_average") val voteAverage: Double? = null,
    val seasons: List<TmdbSeasonSummary> = emptyList(),
)

@Serializable
data class TmdbSeasonSummary(
    @SerialName("season_number") val seasonNumber: Int,
    val name: String? = null,
    @SerialName("air_date") val airDate: String? = null,
    val overview: String? = null,
    @SerialName("poster_path") val posterPath: String? = null,
    @SerialName("episode_count") val episodeCount: Int = 0,
)

@Serializable
data class TmdbSeason(
    @SerialName("season_number") val seasonNumber: Int,
    val name: String? = null,
    @SerialName("air_date") val airDate: String? = null,
    val overview: String? = null,
    @SerialName("poster_path") val posterPath: String? = null,
    val episodes: List<TmdbEpisode> = emptyList(),
)

@Serializable
data class TmdbEpisode(
    val id: Long? = null,
    @SerialName("episode_number") val episodeNumber: Int,
    val name: String? = null,
    @SerialName("air_date") val airDate: String? = null,
    val runtime: Int? = null,
    val overview: String? = null,
    @SerialName("still_path") val stillPath: String? = null,
    @SerialName("episode_type") val episodeType: String? = null,
)

/** A parsed body together with the text it was parsed from, which is what gets stored. */
data class Fetched<T>(val value: T, val body: String)

// ---- the user's own account (D-029, D-036)

@Serializable
data class TmdbRequestToken(@SerialName("request_token") val requestToken: String)

@Serializable
data class TmdbSession(@SerialName("session_id") val sessionId: String)

@Serializable
data class TmdbAccount(val id: Long, val username: String = "")

/** TMDB's own error body; `status_code` 7 is "invalid API key". */
@Serializable
data class TmdbStatus(@SerialName("status_code") val statusCode: Int? = null)

/** One page of an account list. A rated entry carries `rating`; an episode its show and position. */
@Serializable
data class TmdbAccountPage(
    val page: Int = 1,
    @SerialName("total_pages") val totalPages: Int = 1,
    val results: List<TmdbAccountEntry> = emptyList(),
)

@Serializable
data class TmdbAccountEntry(
    val id: Long = 0,
    val rating: Double? = null,
    @SerialName("show_id") val showId: Long? = null,
    @SerialName("season_number") val seasonNumber: Int? = null,
    @SerialName("episode_number") val episodeNumber: Int? = null,
)

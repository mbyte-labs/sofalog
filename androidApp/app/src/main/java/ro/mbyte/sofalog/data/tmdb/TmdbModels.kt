package ro.mbyte.sofalog.data.tmdb

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable
data class TmdbGenre(val id: Long = 0, val name: String = "")

@Serializable
data class TmdbSearchPage(val results: List<TmdbSearchResult> = emptyList())

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

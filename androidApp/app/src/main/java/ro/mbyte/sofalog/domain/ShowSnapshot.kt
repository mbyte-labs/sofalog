package ro.mbyte.sofalog.domain

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.SerializationException

/*
 * The show page's read of a stored TMDB body — the server's ShowDetails projection
 * (server/features/video/details/service_show_details.go) redone on the device, the twin of
 * MovieSnapshot.kt. Everything here is derived from `shows.metadata_json` per view. The video,
 * image, streaming and similar-title rules are the movie's own, shared rather than repeated.
 */

/** TMDB's `next_episode_to_air`, as far as the page shows it. */
data class NextEpisode(val season: Int, val number: Int, val airDate: String?)

data class ShowDetails(
    val tagline: String?,
    val createdBy: List<String>,
    val networks: List<String>,
    val votes: Long?,
    val homepage: String?,
    val imdbId: String?,
    val originalLanguage: String?,
    val popularity: Double?,
    val lastAirDate: String?,
    val inProduction: Boolean,
    val seasonsCount: Int,
    val episodesCount: Int,
    /** The show's typical episode length, in minutes. */
    val episodeRuntimeMin: Int?,
    val nextEpisode: NextEpisode?,
    /** TMDB's community score per season number; a season without votes is absent. */
    val seasonScores: Map<Int, Double>,
    val trailerUrl: String?,
    val cast: List<CastMember>,
    val videos: List<MovieVideo>,
    val backdrops: List<MovieImage>,
    val posters: List<MovieImage>,
    /** Null when the body carries no listing to read — the section is hidden, not "empty". */
    val streaming: Streaming?,
    val similar: List<SimilarTitle>,
) {
    companion object {
        val EMPTY = ShowDetails(
            null, emptyList(), emptyList(), null, null, null, null, null, null, false, 0, 0, null, null,
            emptyMap(), null, emptyList(), emptyList(), emptyList(), emptyList(), null, emptyList(),
        )
    }
}

/** Sections `tv/{id}` is asked to append; a stored body missing one predates the request. */
private val SHOW_APPENDED_SECTIONS =
    listOf("aggregate_credits", "external_ids", "videos", "images", "watch/providers", "recommendations")

/**
 * True when a stored show body should be refetched whatever its age: it predates an appended
 * section — every show tracked before the page read them — or was pruned to another country.
 */
fun showSnapshotIncomplete(body: String, region: String = WATCH_REGION): Boolean =
    snapshotIncomplete(body, SHOW_APPENDED_SECTIONS, region)

/** Projects a stored body. A body that cannot be parsed reads as a show with no extras. */
fun parseShowDetails(body: String, region: String = WATCH_REGION): ShowDetails {
    val snap = try {
        snapshotJson.decodeFromString<ShowSnapshot>(body)
    } catch (e: SerializationException) {
        return ShowDetails.EMPTY
    }
    val videos = snap.videos?.results.orEmpty()
    return ShowDetails(
        tagline = snap.tagline.clean(),
        createdBy = snap.createdBy.mapNotNull { it.name.clean() },
        networks = snap.networks.mapNotNull { it.name.clean() },
        votes = snap.voteCount.takeIf { it > 0 },
        homepage = snap.homepage.clean(),
        imdbId = snap.externalIds?.imdbId.clean(),
        originalLanguage = snap.originalLanguage.clean(),
        popularity = snap.popularity.takeIf { it > 0 },
        lastAirDate = snap.lastAirDate.clean(),
        inProduction = snap.inProduction,
        seasonsCount = snap.numberOfSeasons,
        episodesCount = snap.numberOfEpisodes,
        episodeRuntimeMin = snap.episodeRunTime.firstOrNull()?.takeIf { it > 0 },
        nextEpisode = snap.nextEpisode?.let { NextEpisode(it.seasonNumber, it.episodeNumber, it.airDate.clean()) },
        seasonScores = snap.seasons.filter { it.voteAverage > 0 }.associate { it.seasonNumber to it.voteAverage },
        trailerUrl = trailerOf(videos)?.let { "https://www.youtube.com/watch?v=${it.key}" },
        cast = aggregateCast(snap.aggregateCredits?.cast.orEmpty()),
        videos = rankedVideos(videos).map {
            MovieVideo(it.key, it.name, it.type, it.official, it.publishedAt.take(10).takeIf { d -> d.length == 10 })
        },
        backdrops = rankedImages(snap.images?.backdrops.orEmpty()),
        posters = rankedImages(snap.images?.posters.orEmpty()),
        streaming = streamingOf(snap.watchProviders, region),
        similar = snap.recommendations?.results.orEmpty().filter { it.name.isNotBlank() }.take(SIMILAR_MAX).map {
            SimilarTitle(
                tmdbId = it.id,
                title = it.name,
                year = it.firstAirDate.clean()?.take(4),
                rating = it.voteAverage.takeIf { v -> v > 0 },
                posterPath = it.posterPath.clean(),
            )
        },
    )
}

/**
 * Everyone credited across every episode, in billing order — ties on who appears in more
 * episodes. An aggregate entry has roles rather than one character: they are joined
 * most-episodes-first, which is how TMDB's own page reads them.
 */
internal fun aggregateCast(cast: List<SnapAggregateCast>): List<CastMember> = cast
    .filter { it.name.isNotBlank() }
    .sortedWith(compareBy<SnapAggregateCast> { it.order }.thenByDescending { it.totalEpisodeCount })
    .map { member ->
        CastMember(
            name = member.name,
            character = member.roles.sortedByDescending { it.episodeCount }
                .mapNotNull { it.character.clean() }.joinToString(" / ").clean(),
            profilePath = member.profilePath.clean(),
            episodes = member.totalEpisodeCount.takeIf { it > 0 },
        )
    }

@Serializable
private data class ShowSnapshot(
    val tagline: String? = null,
    val homepage: String? = null,
    @SerialName("original_language") val originalLanguage: String? = null,
    val popularity: Double = 0.0,
    @SerialName("vote_count") val voteCount: Long = 0,
    @SerialName("last_air_date") val lastAirDate: String? = null,
    @SerialName("in_production") val inProduction: Boolean = false,
    @SerialName("number_of_seasons") val numberOfSeasons: Int = 0,
    @SerialName("number_of_episodes") val numberOfEpisodes: Int = 0,
    @SerialName("episode_run_time") val episodeRunTime: List<Int> = emptyList(),
    @SerialName("created_by") val createdBy: List<SnapNamed> = emptyList(),
    val networks: List<SnapNamed> = emptyList(),
    @SerialName("next_episode_to_air") val nextEpisode: SnapNextEpisode? = null,
    val seasons: List<SnapSeasonScore> = emptyList(),
    @SerialName("aggregate_credits") val aggregateCredits: SnapAggregateCredits? = null,
    @SerialName("external_ids") val externalIds: SnapExternalIds? = null,
    val videos: SnapVideos? = null,
    val images: SnapImages? = null,
    @SerialName("watch/providers") val watchProviders: SnapWatchProviders? = null,
    val recommendations: SnapShowRecommendations? = null,
)

@Serializable
private data class SnapNamed(val name: String? = null)

@Serializable
private data class SnapNextEpisode(
    @SerialName("season_number") val seasonNumber: Int = 0,
    @SerialName("episode_number") val episodeNumber: Int = 0,
    @SerialName("air_date") val airDate: String? = null,
)

@Serializable
private data class SnapSeasonScore(
    @SerialName("season_number") val seasonNumber: Int = 0,
    @SerialName("vote_average") val voteAverage: Double = 0.0,
)

@Serializable
internal data class SnapAggregateCredits(val cast: List<SnapAggregateCast> = emptyList())

@Serializable
internal data class SnapAggregateCast(
    val name: String = "",
    @SerialName("profile_path") val profilePath: String? = null,
    val order: Int = 0,
    val roles: List<SnapRole> = emptyList(),
    @SerialName("total_episode_count") val totalEpisodeCount: Int = 0,
)

@Serializable
internal data class SnapRole(
    val character: String? = null,
    @SerialName("episode_count") val episodeCount: Int = 0,
)

@Serializable
private data class SnapShowRecommendations(val results: List<SnapShowRecommendation> = emptyList())

@Serializable
private data class SnapShowRecommendation(
    val id: Long = 0,
    val name: String = "",
    @SerialName("first_air_date") val firstAirDate: String? = null,
    @SerialName("vote_average") val voteAverage: Double = 0.0,
    @SerialName("poster_path") val posterPath: String? = null,
)

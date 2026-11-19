package ro.mbyte.sofalog.domain

import kotlinx.serialization.EncodeDefault
import kotlinx.serialization.ExperimentalSerializationApi
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.SerializationException
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.JsonObject

/*
 * The season and episode pages' read of a season's TMDB document — the part of the server's
 * SeasonDetails and EpisodeDetails projections (server/features/video/details) that the season's
 * and its episodes' own columns do not already hold: the season's score, its cast and its
 * regulars, and each episode's score, vote count and guest stars. Everything here is derived
 * from `seasons.metadata_json` per view.
 */

data class SeasonDetails(
    /** TMDB's community score of the season; null when it has no votes. */
    val score: Double?,
    /** Everyone credited in this season, with their episode counts within it. */
    val cast: List<CastMember>,
    /** TMDB's score per episode number; an episode without votes is absent. */
    val episodeScores: Map<Int, Double>,
    /** The season's regular cast, in billing order — the episode page's "Cast". */
    val regulars: List<CastMember> = emptyList(),
    /** TMDB's vote count per episode number; an episode without votes is absent. */
    val episodeVotes: Map<Int, Long> = emptyMap(),
    /** Each episode's guest stars by episode number, in billing order; an episode with none is absent. */
    val guestStars: Map<Int, List<CastMember>> = emptyMap(),
) {
    companion object {
        val EMPTY = SeasonDetails(null, emptyList(), emptyMap())
    }
}

/**
 * Cuts a season document down to what [parseSeasonDetails] reads. TMDB repeats the crew and the
 * guest stars under every episode, so the full body of a long season runs to megabytes; the
 * episodes' own facts are stored as columns, the crew is dropped, and each guest is kept as the
 * four fields a cast card shows. A body that cannot be parsed is returned as is.
 */
fun compactSeasonBody(body: String): String = try {
    snapshotJson.encodeToString(snapshotJson.decodeFromString<SeasonSnapshot>(body))
} catch (e: SerializationException) {
    body
}

/** Projects a stored body. One that is missing or cannot be parsed reads as a season with no extras. */
fun parseSeasonDetails(body: String?): SeasonDetails {
    val snap = try {
        body?.let { snapshotJson.decodeFromString<SeasonSnapshot>(it) } ?: return SeasonDetails.EMPTY
    } catch (e: SerializationException) {
        return SeasonDetails.EMPTY
    }
    return SeasonDetails(
        score = snap.voteAverage.takeIf { it > 0 },
        cast = aggregateCast(snap.aggregateCredits?.cast.orEmpty()),
        episodeScores = snap.episodes.filter { it.voteAverage > 0 }.associate { it.episodeNumber to it.voteAverage },
        regulars = billedCast(snap.credits.cast),
        episodeVotes = snap.episodes.filter { it.voteCount > 0 }.associate { it.episodeNumber to it.voteCount },
        guestStars = snap.episodes.associate { it.episodeNumber to billedCast(it.guestStars) }.filterValues { it.isNotEmpty() },
    )
}

/**
 * True when a stored season body predates the episode page's sections — the regulars and the
 * guest stars — and should be refetched whatever its age. [compactSeasonBody] always writes the
 * `credits` key, so its absence is what marks an older body.
 */
fun seasonSnapshotIncomplete(body: String): Boolean {
    val root = try {
        snapshotJson.parseToJsonElement(body) as? JsonObject
    } catch (e: SerializationException) {
        null
    } catch (e: IllegalArgumentException) {
        null
    }
    return root == null || "credits" !in root
}

private fun billedCast(cast: List<SnapCast>): List<CastMember> = cast
    .filter { it.name.isNotBlank() }
    .sortedBy { it.order }
    .map { CastMember(it.name, it.character.clean(), it.profilePath.clean()) }

@OptIn(ExperimentalSerializationApi::class)
@Serializable
private data class SeasonSnapshot(
    @SerialName("vote_average") val voteAverage: Double = 0.0,
    val episodes: List<SnapEpisodeScore> = emptyList(),
    @SerialName("aggregate_credits") val aggregateCredits: SnapAggregateCredits? = null,
    /** Always written, even empty: the key is [seasonSnapshotIncomplete]'s marker. */
    @EncodeDefault val credits: SnapSeasonCredits = SnapSeasonCredits(),
)

/** The season's own `credits`, its cast only — the crew is not read. */
@Serializable
private data class SnapSeasonCredits(val cast: List<SnapCast> = emptyList())

@Serializable
private data class SnapEpisodeScore(
    @SerialName("episode_number") val episodeNumber: Int = 0,
    @SerialName("vote_average") val voteAverage: Double = 0.0,
    @SerialName("vote_count") val voteCount: Long = 0,
    @SerialName("guest_stars") val guestStars: List<SnapCast> = emptyList(),
)

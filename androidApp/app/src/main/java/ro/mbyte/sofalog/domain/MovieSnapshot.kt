package ro.mbyte.sofalog.domain

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonObject

/*
 * The movie page's read of a stored TMDB body — the server's MovieDetails projection
 * (server/features/video/common/service_movie_details.go, service_streaming.go,
 * service_similar.go) redone on the device. Everything here is derived from `movies.metadata_json`
 * per view; nothing is stored twice. Image fields are TMDB paths ("/abc.jpg"), turned into URLs
 * by the UI.
 */

/** The default country of the "where to watch" listing, until the user picks one in Settings. */
const val WATCH_REGION = "RO"

/**
 * The one country whose "where to watch" listing the app shows (D-022) — a device setting. A body
 * pruned to another country reads as incomplete, so changing this costs each title one refetch on
 * its next view.
 */
object WatchRegion {
    @Volatile var current: String = WATCH_REGION
}

const val MEDIA_GALLERY_MAX = 30
const val STREAMING_MAX = 12
const val SIMILAR_MAX = 20

data class CastMember(
    val name: String,
    val character: String?,
    val profilePath: String?,
    /** How many episodes the person is credited in — a show's aggregate cast only. */
    val episodes: Int? = null,
)

data class MovieVideo(
    val key: String,
    val name: String,
    val type: String,
    val official: Boolean,
    /** YYYY-MM-DD, or null when TMDB sent no usable instant. */
    val publishedDay: String?,
) {
    val url: String get() = "https://www.youtube.com/watch?v=$key"
    val thumbUrl: String get() = "https://img.youtube.com/vi/$key/hqdefault.jpg"
}

data class MovieImage(val path: String, val width: Int, val height: Int) {
    /** Width over height; a sane default when TMDB sent no dimensions. */
    fun aspect(fallback: Float): Float = if (width > 0 && height > 0) width.toFloat() / height else fallback
}

enum class StreamingOffer(val label: String) {
    STREAM("Stream"), FREE("Free"), ADS("Free with ads"), RENT("Rent"), BUY("Buy"),
}

data class StreamingProvider(val id: Long, val name: String, val offer: StreamingOffer, val logoPath: String?)

/** Non-null with no providers means TMDB answered and named nobody in [region]. */
data class Streaming(val region: String, val link: String?, val providers: List<StreamingProvider>)

data class SimilarTitle(
    val tmdbId: Long,
    val title: String,
    val year: String?,
    val rating: Double?,
    val posterPath: String?,
)

data class MovieDetails(
    val tagline: String?,
    val director: String?,
    val votes: Long?,
    val homepage: String?,
    val imdbId: String?,
    val originalLanguage: String?,
    val budget: Long?,
    val revenue: Long?,
    val popularity: Double?,
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
        val EMPTY = MovieDetails(
            null, null, null, null, null, null, null, null, null, null,
            emptyList(), emptyList(), emptyList(), emptyList(), null, emptyList(),
        )
    }
}

internal val snapshotJson = Json { ignoreUnknownKeys = true; coerceInputValues = true }

/** Sections `movie/{id}` is asked to append; a stored body missing one predates the request. */
private val APPENDED_SECTIONS = listOf("credits", "external_ids", "videos", "images", "watch/providers", "recommendations")

internal const val REGION_MARKER = "sofalog_region"

/**
 * Keeps one country of the `watch/providers` section and marks which — TMDB sends every country,
 * close to half the body. A body that cannot be parsed, or has no section, is returned as is.
 */
fun pruneWatchProviders(body: String, region: String = WatchRegion.current): String {
    val root = try {
        snapshotJson.parseToJsonElement(body) as? JsonObject
    } catch (e: SerializationException) {
        null
    } ?: return body
    val section = root["watch/providers"] as? JsonObject ?: return body
    val listing = (section["results"] as? JsonObject)?.get(region)
    val pruned = buildJsonObject {
        put("results", buildJsonObject { if (listing != null) put(region, listing) })
        put(REGION_MARKER, JsonPrimitive(region))
    }
    return JsonObject(root.toMutableMap().apply { put("watch/providers", pruned) }).toString()
}

/**
 * True when a stored body should be refetched whatever its age: it predates an appended section,
 * or its listing was pruned to another country.
 */
fun movieSnapshotIncomplete(body: String, region: String = WatchRegion.current): Boolean =
    snapshotIncomplete(body, APPENDED_SECTIONS, region)

internal fun snapshotIncomplete(body: String, sections: List<String>, region: String): Boolean {
    val root = try {
        snapshotJson.parseToJsonElement(body).jsonObject
    } catch (e: SerializationException) {
        return true
    } catch (e: IllegalArgumentException) {
        return true
    }
    if (sections.any { it !in root }) return true
    val marker = ((root["watch/providers"] as? JsonObject)?.get(REGION_MARKER) as? JsonPrimitive)?.content
    return marker != region
}

/** Projects a stored body. A body that cannot be parsed reads as a movie with no extras. */
fun parseMovieDetails(body: String, region: String = WatchRegion.current): MovieDetails {
    val snap = try {
        snapshotJson.decodeFromString<Snapshot>(body)
    } catch (e: SerializationException) {
        return MovieDetails.EMPTY
    }
    val videos = rankedVideos(snap.videos?.results.orEmpty())
    return MovieDetails(
        tagline = snap.tagline.clean(),
        director = snap.credits?.crew.orEmpty().firstOrNull { it.job == "Director" && it.name.isNotBlank() }?.name,
        votes = snap.voteCount.takeIf { it > 0 },
        homepage = snap.homepage.clean(),
        imdbId = (snap.externalIds?.imdbId ?: snap.imdbId).clean(),
        originalLanguage = snap.originalLanguage.clean(),
        budget = snap.budget.takeIf { it > 0 },
        revenue = snap.revenue.takeIf { it > 0 },
        popularity = snap.popularity.takeIf { it > 0 },
        trailerUrl = trailerOf(snap.videos?.results.orEmpty())?.let { "https://www.youtube.com/watch?v=${it.key}" },
        cast = snap.credits?.cast.orEmpty().sortedBy { it.order }.filter { it.name.isNotBlank() }
            .map { CastMember(it.name, it.character.clean(), it.profilePath.clean()) },
        videos = videos.map {
            MovieVideo(it.key, it.name, it.type, it.official, it.publishedAt.take(10).takeIf { d -> d.length == 10 })
        },
        backdrops = rankedImages(snap.images?.backdrops.orEmpty()),
        posters = rankedImages(snap.images?.posters.orEmpty()),
        streaming = streamingOf(snap.watchProviders, region),
        similar = snap.recommendations?.results.orEmpty().filter { it.title.isNotBlank() }.take(SIMILAR_MAX).map {
            SimilarTitle(
                tmdbId = it.id,
                title = it.title,
                year = it.releaseDate.clean()?.take(4),
                rating = it.voteAverage.takeIf { v -> v > 0 },
                posterPath = it.posterPath.clean(),
            )
        },
    )
}

internal fun String?.clean(): String? = this?.trim()?.ifEmpty { null }

private fun usable(v: SnapVideo) = v.site == "YouTube" && YOUTUBE_KEY.matches(v.key)

private val YOUTUBE_KEY = Regex("[A-Za-z0-9_-]{6,20}")

private fun trailerRank(v: SnapVideo) = (if (v.type == "Trailer") 2 else 0) + (if (v.official) 1 else 0)

/** The video worth linking: a trailer beats a teaser, official beats not, then the newest. */
internal fun trailerOf(videos: List<SnapVideo>): SnapVideo? = videos
    .filter { usable(it) && (it.type == "Trailer" || it.type == "Teaser") }
    .sortedWith(compareByDescending<SnapVideo> { trailerRank(it) }.thenByDescending { it.publishedAt })
    .firstOrNull()

internal fun rankedVideos(videos: List<SnapVideo>): List<SnapVideo> = videos.filter(::usable)
    .sortedWith(compareByDescending<SnapVideo> { trailerRank(it) }.thenByDescending { it.publishedAt })
    .take(MEDIA_GALLERY_MAX)

internal fun rankedImages(images: List<SnapImage>): List<MovieImage> = images.filter { it.filePath.isNotBlank() }
    .sortedWith(
        compareByDescending<SnapImage> { it.voteAverage }.thenByDescending { it.voteCount }.thenBy { it.filePath },
    )
    .take(MEDIA_GALLERY_MAX)
    .map { MovieImage(it.filePath, it.width, it.height) }

internal fun streamingOf(section: SnapWatchProviders?, region: String): Streaming? {
    if (section == null) return null
    // Pruned to another country: no answer is held for this one, and "nothing here" would be a lie.
    if (section.region.isNotEmpty() && section.region != region) return null
    val listing = section.results[region] ?: return Streaming(region, null, emptyList())
    val buckets = listOf(
        StreamingOffer.STREAM to listing.flatrate,
        StreamingOffer.FREE to listing.free,
        StreamingOffer.ADS to listing.ads,
        StreamingOffer.RENT to listing.rent,
        StreamingOffer.BUY to listing.buy,
    )
    // First bucket wins: a service under both rent and buy is one tile, labelled Rent.
    val seen = HashSet<Long>()
    val tiles = ArrayList<Pair<StreamingProvider, Int>>()
    for ((offer, providers) in buckets) {
        for (p in providers) {
            if (p.name.isBlank() || !seen.add(p.id)) continue
            tiles += StreamingProvider(p.id, p.name, offer, p.logoPath.clean()) to p.displayPriority
        }
    }
    val ordered = tiles
        .sortedWith(compareBy<Pair<StreamingProvider, Int>> { it.first.offer.ordinal }.thenBy { it.second }.thenBy { it.first.name })
        .map { it.first }
        .take(STREAMING_MAX)
    return Streaming(region, listing.link.clean(), ordered)
}

@Serializable
private data class Snapshot(
    val tagline: String? = null,
    val homepage: String? = null,
    @SerialName("imdb_id") val imdbId: String? = null,
    @SerialName("original_language") val originalLanguage: String? = null,
    val budget: Long = 0,
    val revenue: Long = 0,
    val popularity: Double = 0.0,
    @SerialName("vote_count") val voteCount: Long = 0,
    val credits: SnapCredits? = null,
    @SerialName("external_ids") val externalIds: SnapExternalIds? = null,
    val videos: SnapVideos? = null,
    val images: SnapImages? = null,
    @SerialName("watch/providers") val watchProviders: SnapWatchProviders? = null,
    val recommendations: SnapRecommendations? = null,
)

@Serializable
internal data class SnapCredits(val cast: List<SnapCast> = emptyList(), val crew: List<SnapCrew> = emptyList())

@Serializable
internal data class SnapCast(
    val name: String = "",
    val character: String? = null,
    @SerialName("profile_path") val profilePath: String? = null,
    val order: Int = 0,
)

@Serializable
internal data class SnapCrew(val name: String = "", val job: String = "")

@Serializable
internal data class SnapExternalIds(@SerialName("imdb_id") val imdbId: String? = null)

@Serializable
internal data class SnapVideos(val results: List<SnapVideo> = emptyList())

@Serializable
internal data class SnapVideo(
    val key: String = "",
    val name: String = "",
    val site: String = "",
    val type: String = "",
    val official: Boolean = false,
    @SerialName("published_at") val publishedAt: String = "",
)

@Serializable
internal data class SnapImages(val backdrops: List<SnapImage> = emptyList(), val posters: List<SnapImage> = emptyList())

@Serializable
internal data class SnapImage(
    @SerialName("file_path") val filePath: String = "",
    val width: Int = 0,
    val height: Int = 0,
    @SerialName("vote_average") val voteAverage: Double = 0.0,
    @SerialName("vote_count") val voteCount: Long = 0,
)

@Serializable
internal data class SnapWatchProviders(
    val results: Map<String, SnapWatchRegion> = emptyMap(),
    @SerialName(REGION_MARKER) val region: String = "",
)

@Serializable
internal data class SnapWatchRegion(
    val link: String? = null,
    val flatrate: List<SnapProvider> = emptyList(),
    val free: List<SnapProvider> = emptyList(),
    val ads: List<SnapProvider> = emptyList(),
    val rent: List<SnapProvider> = emptyList(),
    val buy: List<SnapProvider> = emptyList(),
)

@Serializable
internal data class SnapProvider(
    @SerialName("provider_id") val id: Long = 0,
    @SerialName("provider_name") val name: String = "",
    @SerialName("logo_path") val logoPath: String? = null,
    @SerialName("display_priority") val displayPriority: Int = 0,
)

@Serializable
internal data class SnapRecommendations(val results: List<SnapRecommendation> = emptyList())

@Serializable
internal data class SnapRecommendation(
    val id: Long = 0,
    val title: String = "",
    @SerialName("release_date") val releaseDate: String? = null,
    @SerialName("vote_average") val voteAverage: Double = 0.0,
    @SerialName("poster_path") val posterPath: String? = null,
)

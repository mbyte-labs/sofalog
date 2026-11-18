package ro.mbyte.sofalog.data.tmdb

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.Json
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.OkHttpClient
import okhttp3.Request
import java.io.IOException

/** Why a TMDB call failed. The message never carries the URL — the key rides in it. */
class TmdbException(val reason: Reason, cause: Throwable? = null) : Exception(reason.name, cause) {
    enum class Reason { NO_KEY, BAD_KEY, NOT_FOUND, UNAVAILABLE }
}

/**
 * The device's own TMDB access (SPEC D-040). The key is built into the app; a v4 read token (a
 * JWT) is sent as a bearer header, a v3 key as the `api_key` parameter.
 */
class TmdbClient(private val apiKey: String, private val http: OkHttpClient) {
    private val json = Json { ignoreUnknownKeys = true; coerceInputValues = true }

    val hasKey: Boolean get() = apiKey.isNotBlank()

    suspend fun search(query: String): List<TmdbSearchResult> {
        val body = get("search/multi", "query" to query, "include_adult" to "false")
        return decode<TmdbSearchPage>(body).results.filter { it.mediaType == "movie" || it.mediaType == "tv" }
    }

    suspend fun movie(tmdbId: Long): Fetched<TmdbMovie> = fetch("movie/$tmdbId")

    suspend fun show(tmdbId: Long): Fetched<TmdbShow> = fetch("tv/$tmdbId")

    suspend fun season(showTmdbId: Long, seasonNumber: Int): Fetched<TmdbSeason> =
        fetch("tv/$showTmdbId/season/$seasonNumber")

    private suspend inline fun <reified T> fetch(path: String): Fetched<T> {
        val body = get(path)
        return Fetched(decode<T>(body), body)
    }

    private inline fun <reified T> decode(body: String): T = try {
        json.decodeFromString<T>(body)
    } catch (e: SerializationException) {
        throw TmdbException(TmdbException.Reason.UNAVAILABLE, e)
    }

    private suspend fun get(path: String, vararg params: Pair<String, String>): String {
        if (!hasKey) throw TmdbException(TmdbException.Reason.NO_KEY)
        val bearer = apiKey.startsWith("eyJ")
        val url = "$BASE/$path".toHttpUrl().newBuilder().apply {
            params.forEach { (k, v) -> addQueryParameter(k, v) }
            if (!bearer) addQueryParameter("api_key", apiKey)
        }.build()
        val request = Request.Builder().url(url).apply {
            if (bearer) header("Authorization", "Bearer $apiKey")
            header("Accept", "application/json")
        }.build()
        return withContext(Dispatchers.IO) {
            try {
                http.newCall(request).execute().use { response ->
                    when {
                        response.isSuccessful -> response.body.string()
                        response.code == 401 -> throw TmdbException(TmdbException.Reason.BAD_KEY)
                        response.code == 404 -> throw TmdbException(TmdbException.Reason.NOT_FOUND)
                        else -> throw TmdbException(TmdbException.Reason.UNAVAILABLE)
                    }
                }
            } catch (e: IOException) {
                // Deliberately not chained: an OkHttp transport error names the full URL.
                throw TmdbException(TmdbException.Reason.UNAVAILABLE)
            }
        }
    }

    companion object {
        private const val BASE = "https://api.themoviedb.org/3"
        private const val IMAGES = "https://image.tmdb.org/t/p"

        /** TMDB's own size variants; the app never resizes an image. */
        fun posterUrl(path: String?): String? = path?.let { "$IMAGES/w342$it" }
        fun backdropUrl(path: String?): String? = path?.let { "$IMAGES/w780$it" }
        fun stillUrl(path: String?): String? = path?.let { "$IMAGES/w300$it" }
    }
}

package ro.mbyte.sofalog.data.tmdb

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.OkHttpClient
import okhttp3.Request
import ro.mbyte.sofalog.domain.TmdbFailure
import ro.mbyte.sofalog.domain.accountMediaType
import ro.mbyte.sofalog.domain.compactSeasonBody
import ro.mbyte.sofalog.domain.pruneWatchProviders
import ro.mbyte.sofalog.domain.ratingPath
import ro.mbyte.sofalog.domain.tmdbFailure
import java.io.IOException

/** Why a TMDB call failed. The message never carries the URL — the key rides in it. */
class TmdbException(val reason: Reason, cause: Throwable? = null) : Exception(reason.name, cause) {
    /** [SESSION_INVALID]: TMDB refused the user's own link. [REFUSED]: it refused the request itself. */
    enum class Reason { NO_KEY, BAD_KEY, NOT_FOUND, UNAVAILABLE, SESSION_INVALID, REFUSED }
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

    /**
     * One kind alone, filtered by TMDB: a full page of movies, not the mixed page thinned. These
     * two endpoints name no `media_type`, so the rows are stamped with the one that was asked for.
     */
    suspend fun searchMovies(query: String): List<TmdbSearchResult> = searchKind("search/movie", "movie", query)

    suspend fun searchShows(query: String): List<TmdbSearchResult> = searchKind("search/tv", "tv", query)

    private suspend fun searchKind(path: String, mediaType: String, query: String): List<TmdbSearchResult> {
        val body = get(path, "query" to query, "include_adult" to "false")
        return decode<TmdbSearchPage>(body).results.map { it.copy(mediaType = mediaType) }
    }

    /**
     * One page of this week's trending list (D-034). [media] is `all`, `movie` or `tv`; `all` is
     * TMDB's own cross-type ranking and also names people, which the caller drops.
     */
    suspend fun trending(media: String, page: Int): TmdbListPage =
        decode(get("trending/$media/week", "page" to page.toString()))

    /** One page of `/discover/{movie|tv}`; TMDB does the filtering, [params] say how. */
    suspend fun discover(media: String, params: List<Pair<String, String>>): TmdbListPage =
        decode(send("GET", "discover/$media", params))

    suspend fun genres(media: String): List<TmdbGenre> = decode<TmdbGenreList>(get("genre/$media/list")).genres

    suspend fun languages(): List<TmdbLanguage> = decode(get("configuration/languages"))

    /**
     * One request for everything the movie page shows — the server's own append list. The image
     * languages are bounded, or TMDB would send every localised poster. The watch-provider
     * section is pruned to one country before the body is handed on, so what gets stored is small.
     */
    suspend fun movie(tmdbId: Long): Fetched<TmdbMovie> {
        val body = pruneWatchProviders(
            get(
                "movie/$tmdbId",
                "append_to_response" to "credits,external_ids,videos,images,watch/providers,recommendations",
                "include_image_language" to "en,null",
            ),
        )
        return Fetched(decode<TmdbMovie>(body), body)
    }

    /**
     * The show page's one request, the movie's append list with the cast swapped for
     * `aggregate_credits` — everyone credited across every episode, not the latest season's.
     */
    suspend fun show(tmdbId: Long): Fetched<TmdbShow> {
        val body = pruneWatchProviders(
            get(
                "tv/$tmdbId",
                "append_to_response" to "aggregate_credits,external_ids,videos,images,watch/providers,recommendations",
                "include_image_language" to "en,null",
            ),
        )
        return Fetched(decode<TmdbShow>(body), body)
    }

    /**
     * One season with its own `aggregate_credits` — the season page's cast — and `credits`, its
     * regulars, which the episode page lists beside each episode's guest stars. The body handed on
     * is compacted to what those pages read beyond the stored columns (see [compactSeasonBody]):
     * the full document repeats every episode's crew, far too much to keep per season.
     */
    suspend fun season(showTmdbId: Long, seasonNumber: Int): Fetched<TmdbSeason> {
        val body = get("tv/$showTmdbId/season/$seasonNumber", "append_to_response" to "aggregate_credits,credits")
        return Fetched(decode<TmdbSeason>(body), compactSeasonBody(body))
    }

    private inline fun <reified T> decode(body: String): T = try {
        json.decodeFromString<T>(body)
    } catch (e: SerializationException) {
        throw TmdbException(TmdbException.Reason.UNAVAILABLE, e)
    }

    private suspend fun get(path: String, vararg params: Pair<String, String>): String = send("GET", path, params.toList())

    // ------------------------------------------------------------------ the user's own account
    // (D-029, D-036). Every call below carries the user's session; a 401 on one is the link's
    // fault, not the key's, and reads as SESSION_INVALID.

    /** Step one of linking: a token the user approves on TMDB's own page. */
    suspend fun newRequestToken(): String =
        decode<TmdbRequestToken>(get("authentication/token/new")).requestToken

    /** Step two: an approved token becomes a session. An unapproved one is SESSION_INVALID. */
    suspend fun createSession(requestToken: String): String = decode<TmdbSession>(
        send("POST", "authentication/session/new", body = buildJsonObject { put("request_token", requestToken) }.toString()),
    ).sessionId

    suspend fun deleteSession(session: String) {
        send("DELETE", "authentication/session", body = buildJsonObject { put("session_id", session) }.toString())
    }

    suspend fun account(session: String): TmdbAccount = decode(send("GET", "account", session = session))

    suspend fun setRating(session: String, targetType: String, tmdbId: Long, season: Int, episode: Int, value: Int) {
        send(
            "POST", ratingPath(targetType, tmdbId, season, episode),
            body = buildJsonObject { put("value", value) }.toString(), session = session,
        )
    }

    suspend fun deleteRating(session: String, targetType: String, tmdbId: Long, season: Int, episode: Int) {
        send("DELETE", ratingPath(targetType, tmdbId, season, episode), session = session)
    }

    suspend fun setWatchlist(session: String, accountId: Long, targetType: String, tmdbId: Long, on: Boolean) =
        setListed(session, accountId, "watchlist", targetType, tmdbId, on)

    suspend fun setFavorite(session: String, accountId: Long, targetType: String, tmdbId: Long, on: Boolean) =
        setListed(session, accountId, "favorite", targetType, tmdbId, on)

    private suspend fun setListed(session: String, accountId: Long, list: String, targetType: String, tmdbId: Long, on: Boolean) {
        val body = buildJsonObject {
            put("media_type", accountMediaType(targetType))
            put("media_id", tmdbId)
            put(list, on)
        }
        send("POST", "account/$accountId/$list", body = body.toString(), session = session)
    }

    /**
     * One page of one of the account's lists — the setup's one-time read. [list] is `rated`,
     * `watchlist` or `favorite`; [media] is `movies`, `tv` or (rated only) `tv/episodes`.
     */
    suspend fun accountList(session: String, accountId: Long, list: String, media: String, page: Int): TmdbAccountPage =
        decode(send("GET", "account/$accountId/$list/$media", listOf("page" to page.toString()), session = session))

    /**
     * The one request helper. [session] rides as `session_id`. Errors carry a reason and nothing
     * else: no URL, no body — the key and the session are both in the URL.
     */
    private suspend fun send(
        method: String,
        path: String,
        params: List<Pair<String, String>> = emptyList(),
        body: String? = null,
        session: String? = null,
    ): String {
        if (!hasKey) throw TmdbException(TmdbException.Reason.NO_KEY)
        val bearer = apiKey.startsWith("eyJ")
        val url = "$BASE/$path".toHttpUrl().newBuilder().apply {
            params.forEach { (k, v) -> addQueryParameter(k, v) }
            if (session != null) addQueryParameter("session_id", session)
            if (!bearer) addQueryParameter("api_key", apiKey)
        }.build()
        val payload = body?.toRequestBody(JSON_TYPE)
        val request = Request.Builder().url(url).apply {
            if (bearer) header("Authorization", "Bearer $apiKey")
            header("Accept", "application/json")
            method(method, payload ?: if (method == "POST") "".toRequestBody(JSON_TYPE) else null)
        }.build()
        val userSession = session != null || path == "authentication/session/new"
        return withContext(Dispatchers.IO) {
            try {
                http.newCall(request).execute().use { response ->
                    if (response.isSuccessful) return@use response.body.string()
                    val status = runCatching {
                        json.decodeFromString<TmdbStatus>(response.peekBody(512).string()).statusCode
                    }.getOrNull()
                    throw TmdbException(
                        when (tmdbFailure(response.code, userSession, status)) {
                            TmdbFailure.BAD_KEY -> TmdbException.Reason.BAD_KEY
                            TmdbFailure.SESSION_INVALID -> TmdbException.Reason.SESSION_INVALID
                            TmdbFailure.NOT_FOUND -> TmdbException.Reason.NOT_FOUND
                            TmdbFailure.REFUSED -> TmdbException.Reason.REFUSED
                            TmdbFailure.UNAVAILABLE -> TmdbException.Reason.UNAVAILABLE
                        },
                    )
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
        private val JSON_TYPE = "application/json; charset=utf-8".toMediaType()

        /** TMDB's own size variants; the app never resizes an image. */
        fun posterUrl(path: String?): String? = path?.let { "$IMAGES/w342$it" }
        fun backdropUrl(path: String?): String? = path?.let { "$IMAGES/w780$it" }
        fun stillUrl(path: String?): String? = path?.let { "$IMAGES/w300$it" }
        fun profileUrl(path: String?): String? = path?.let { "$IMAGES/w185$it" }
        fun logoUrl(path: String?): String? = path?.let { "$IMAGES/w185$it" }

        /** The lightbox's variants: the largest TMDB serves short of the original. */
        fun posterFullUrl(path: String?): String? = path?.let { "$IMAGES/w780$it" }
        fun backdropFullUrl(path: String?): String? = path?.let { "$IMAGES/w1280$it" }
    }
}

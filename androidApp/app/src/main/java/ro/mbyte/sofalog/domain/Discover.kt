package ro.mbyte.sofalog.domain

/**
 * The Discover page's rules (SPEC D-034, the subset the device shows): this week's trending list
 * and TMDB's popular list under a few filters. Everything here is pure; the server's twin is
 * `server/features/video/discover/service_trending.go`.
 */

/** TMDB serves no page past this one. */
const val DISCOVER_MAX_PAGE = 500

/** The score the Popular list starts at: low enough to keep most titles, high enough to drop the dross. */
const val DISCOVER_DEFAULT_MIN_SCORE = 5.5

/** The Popular list's filters. An unset one is not sent, so TMDB applies none. */
data class DiscoverFilters(
    /** ISO 639-1 code of the original language. */
    val language: String? = null,
    /** Genre ids; a title must carry every one of them. */
    val genres: Set<Long> = emptySet(),
    /** The lowest TMDB score listed, 0..10; null lists every score. */
    val voteMin: Double? = DISCOVER_DEFAULT_MIN_SCORE,
) {
    /**
     * How many filters the user changed — the badge on the Filters button. The score counts when
     * it is not the default it starts at.
     */
    val activeCount: Int
        get() = listOf(language != null, genres.isNotEmpty(), voteMin != DISCOVER_DEFAULT_MIN_SCORE).count { it }
}

/**
 * The query of one `/discover/{movie|tv}` request, sorted by popularity. [tv] picks the kind:
 * TMDB lists undated shows unless told not to.
 */
fun discoverParams(tv: Boolean, filters: DiscoverFilters, page: Int): List<Pair<String, String>> = buildList {
    add("sort_by" to "popularity.desc")
    add("include_adult" to "false")
    if (tv) add("include_null_first_air_dates" to "false")
    add("page" to page.toString())
    filters.voteMin?.let { add("vote_average.gte" to formatScoreParam(it)) }
    // Comma-joined is TMDB's AND: every selected genre must match.
    if (filters.genres.isNotEmpty()) add("with_genres" to filters.genres.sorted().joinToString(","))
    filters.language?.let { add("with_original_language" to it) }
}

/** 6.0 → "6", 6.5 → "6.5": the shortest form, as the server sends it. */
private fun formatScoreParam(value: Double): String =
    if (value % 1.0 == 0.0) value.toLong().toString() else value.toString()

/**
 * The kind of one trending row, `movie` or `tv`, or null for a row to drop. A typed list is the
 * kind that was asked for whatever the row says; under `all` the row's own `media_type` decides,
 * and a person — or anything TMDB adds later — is dropped, so a page can come up short of twenty.
 */
fun trendingRowKind(requested: String, rowMediaType: String?, tmdbId: Long): String? = when {
    tmdbId == 0L -> null
    requested != "all" -> requested
    rowMediaType == "movie" || rowMediaType == "tv" -> rowMediaType
    else -> null
}

/** One row of a list being merged, with the keys the merge orders by. */
data class RankedRow<T>(val item: T, val popularity: Double, val title: String, val tmdbId: Long)

/**
 * Both kinds' page of the Popular list as one: most popular first, then by title ignoring case,
 * then by TMDB id. Not capped — two pages of twenty make forty.
 */
fun <T> mergeByPopularity(rows: List<RankedRow<T>>): List<T> = rows.sortedWith(
    compareByDescending<RankedRow<T>> { it.popularity }.thenBy { it.title.lowercase() }.thenBy { it.tmdbId },
).map { it.item }

data class LanguageOption(val code: String, val name: String)

/**
 * TMDB's language list as the filter offers it: by English name, without the nameless rows and
 * without `xx`, TMDB's "no language".
 */
fun languageOptions(raw: List<Pair<String, String>>): List<LanguageOption> = raw
    .filter { (code, name) -> code.isNotEmpty() && code != "xx" && name.isNotEmpty() }
    .map { (code, name) -> LanguageOption(code, name) }
    .distinctBy { it.code }
    .sortedWith(compareBy<LanguageOption> { it.name.lowercase() }.thenBy { it.code })

data class GenreOption(val id: Long, val name: String)

/**
 * The genres the filter offers. They follow the kind: [movie] true is the movie list, false the
 * show list, and null — both kinds — their union by id, by name.
 */
fun genreOptions(movie: Boolean?, movieGenres: List<GenreOption>, showGenres: List<GenreOption>): List<GenreOption> {
    fun List<GenreOption>.valid() = filter { it.id != 0L && it.name.isNotEmpty() }
    return when (movie) {
        true -> movieGenres.valid()
        false -> showGenres.valid()
        null -> (movieGenres.valid() + showGenres.valid()).distinctBy { it.id }.sortedBy { it.name.lowercase() }
    }
}

/** Whether a further page exists to ask for. */
fun discoverHasMore(page: Int, totalPages: Int): Boolean = page < totalPages && page < DISCOVER_MAX_PAGE

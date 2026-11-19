package ro.mbyte.sofalog.domain

/**
 * What the library says about one title: the user's rating, whether it was watched (a movie: at
 * least once; a show: at least one episode, and not dropped — started, not finished), whether
 * it is on the watchlist or in the favorites, and whether it is a dropped show.
 */
data class LibraryMark(
    val userRating: Int? = null,
    val watched: Boolean = false,
    val watchlisted: Boolean = false,
    val favorited: Boolean = false,
    /** A show the user dropped; never set with [watched], and never for a movie. */
    val dropped: Boolean = false,
)

/**
 * The marks of a set of titles named by TMDB id. Movies and shows are kept apart because a TMDB
 * id is unique only within its kind: movie 5 and show 5 are different titles.
 */
class LibraryMarks(private val movies: Map<Long, LibraryMark>, private val shows: Map<Long, LibraryMark>) {
    /** A title the library does not hold reads as holding nothing. */
    fun of(movie: Boolean, tmdbId: Long): LibraryMark = (if (movie) movies else shows)[tmdbId] ?: LibraryMark()

    companion object {
        val EMPTY = LibraryMarks(emptyMap(), emptyMap())
    }
}

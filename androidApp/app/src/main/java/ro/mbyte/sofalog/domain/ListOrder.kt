package ro.mbyte.sofalog.domain

/** What a list entry is. The order is the tie-break's: a movie sorts before a show. */
enum class ListItemKind { MOVIE, SHOW }

/** One entry of the watchlist, the favorites or the dropped list. */
data class ListEntry(
    val kind: ListItemKind,
    /** The movie's or show's local id — unique only together with [kind]. */
    val localId: Long,
    val tmdbId: Long,
    val title: String,
    /** A movie's release date, a show's first air date (YYYY-MM-DD). */
    val date: String?,
    val posterPath: String?,
    val backdropPath: String?,
    /** TMDB's community score. */
    val score: Double?,
    val userRating: Int?,
    /** A movie: watched at least once. A show: finished, not merely started. */
    val watched: Boolean,
    /** When it joined the list (a dropped show: when it was dropped), RFC3339 UTC. */
    val addedAt: String,
)

enum class ListSort { ADDED, TITLE, RELEASE }

/** [kind] null lists movies and shows together. */
data class ListQuery(
    val kind: ListItemKind? = null,
    val sort: ListSort = ListSort.ADDED,
    val descending: Boolean = true,
)

/**
 * A list as its page shows it — the server's list ordering (`listWatchlistTargets`): titles
 * compare case-folded, an undated entry sorts last in BOTH directions, and ties break on the
 * (kind, id) pair in the main key's direction, since movie 5 and show 5 both exist.
 */
fun orderList(entries: List<ListEntry>, query: ListQuery): List<ListEntry> {
    val shown = if (query.kind == null) entries else entries.filter { it.kind == query.kind }
    val key: Comparator<ListEntry> = when (query.sort) {
        ListSort.ADDED -> compareBy { it.addedAt }
        ListSort.TITLE -> compareBy { it.title.lowercase() }
        ListSort.RELEASE -> compareBy { it.date.orEmpty() }
    }
    val directed = key.thenBy { it.kind }.thenBy { it.localId }.let { if (query.descending) it.reversed() else it }
    return if (query.sort == ListSort.RELEASE) {
        shown.sortedWith(compareBy<ListEntry> { it.date == null }.then(directed))
    } else {
        shown.sortedWith(directed)
    }
}

/** What the Lists page's card says about one list. */
data class ListSummary(
    val movies: Int,
    val shows: Int,
    /** Poster paths for the card's fan, newest entry first, at most [LIST_ART_MAX]. */
    val posters: List<String>,
) {
    val total: Int get() = movies + shows
}

const val LIST_ART_MAX = 5

/** How many of the newest entries are looked at for artwork; one without a poster is skipped. */
const val LIST_ART_SCAN = 12

fun summarize(entries: List<ListEntry>): ListSummary = ListSummary(
    movies = entries.count { it.kind == ListItemKind.MOVIE },
    shows = entries.count { it.kind == ListItemKind.SHOW },
    posters = orderList(entries, ListQuery()).take(LIST_ART_SCAN).mapNotNull { it.posterPath }.take(LIST_ART_MAX),
)

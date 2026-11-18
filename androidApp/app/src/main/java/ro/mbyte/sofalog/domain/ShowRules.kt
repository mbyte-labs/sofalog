package ro.mbyte.sofalog.domain

/**
 * The show rules, ported from the server (SPEC D-040). They are pure functions over one show's
 * episodes and the set of watched episode ids, and they are held to the Go implementation by the
 * shared fixtures in `fixtures/video-rules/` — change a rule here, change the fixture and the
 * server with it.
 */
data class EpisodeRef(val id: Long, val season: Int, val number: Int, val airDate: String?)

enum class ShowStatus(val value: String) {
    WATCHING("watching"), WATCHED("watched"), DROPPED("dropped");

    companion object {
        fun of(value: String?): ShowStatus? = entries.firstOrNull { it.value == value }
    }
}

/**
 * D-012's completion predicate: aired, non-special. An undated episode never reads as aired, or
 * one such row would make a show permanently uncompletable. [today] is YYYY-MM-DD in the user's
 * zone, and plain string order is date order.
 */
fun EpisodeRef.isEligible(today: String): Boolean =
    season > 0 && airDate != null && airDate <= today

/**
 * The server's RecomputeShowStatus, statement for statement: ensure, demote, promote, collapse.
 * Returns the status afterwards; null means no state row. `dropped` is the user's own value and
 * passes through untouched (D-010).
 */
fun recomputeShowStatus(
    current: ShowStatus?,
    episodes: List<EpisodeRef>,
    watched: Set<Long>,
    today: String,
): ShowStatus? {
    val anyWatch = episodes.any { it.id in watched }
    val eligible = episodes.filter { it.isEligible(today) }
    val eligibleUnwatched = eligible.any { it.id !in watched }

    var status = current
    if (status == null && anyWatch) status = ShowStatus.WATCHING
    if (status == ShowStatus.WATCHED && eligibleUnwatched) {
        status = ShowStatus.WATCHING
    } else if (status == ShowStatus.WATCHING && eligible.isNotEmpty() && !eligibleUnwatched) {
        status = ShowStatus.WATCHED
    }
    if (status != null && status != ShowStatus.DROPPED && !anyWatch) status = null
    return status
}

/** Derived, never stored: aired non-special episodes, and how many of them are watched. */
data class ShowProgress(val watched: Int, val airedTotal: Int)

fun showProgress(episodes: List<EpisodeRef>, watched: Set<Long>, today: String): ShowProgress {
    val eligible = episodes.filter { it.isEligible(today) }
    return ShowProgress(watched = eligible.count { it.id in watched }, airedTotal = eligible.size)
}

/**
 * The Up Next pick: the first non-special episode strictly after the highest watched position.
 * After S1E2 the answer is S1E3, never S1E1; a finished show has no pick rather than wrapping
 * around. Specials neither anchor the pick nor become it, and airedness is deliberately ignored —
 * an unaired next episode is a real answer the card labels.
 *
 * The caller decides whether the show is listed at all: started (any episode watch, specials
 * included) and not dropped.
 */
fun pickNextEpisode(episodes: List<EpisodeRef>, watched: Set<Long>): EpisodeRef? {
    val ordered = episodes.filter { it.season > 0 }
        .sortedWith(compareBy<EpisodeRef> { it.season }.thenBy { it.number })
    val lastWatched = ordered.indexOfLast { it.id in watched }
    return ordered.getOrNull(lastWatched + 1)
}

/** Whether a show belongs on Up Next at all, and with which episode. */
fun upNextEpisode(status: ShowStatus?, episodes: List<EpisodeRef>, watched: Set<Long>): EpisodeRef? {
    if (status == ShowStatus.DROPPED) return null
    if (episodes.none { it.id in watched }) return null
    return pickNextEpisode(episodes, watched)
}

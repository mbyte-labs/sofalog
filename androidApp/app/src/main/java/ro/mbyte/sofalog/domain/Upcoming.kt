package ro.mbyte.sofalog.domain

import java.time.LocalDate
import java.time.format.DateTimeFormatter
import java.time.temporal.ChronoUnit
import java.util.Locale

/*
 * The upcoming calendar (SPEC D-026), ported from the server's `features/video/upcoming` and the
 * web page's `upcoming.js`. Pure functions over what the device holds; the selection and the order
 * are held to the Go implementation by `fixtures/video-rules/upcoming.json` — change a rule here,
 * change the fixture and the server with it.
 */

enum class UpcomingType { ALL, EPISODES, MOVIES }

const val UPCOMING_DEFAULT_DAYS = 90

/** The windows the page offers, as the web's: days to label. */
val UPCOMING_RANGES: List<Pair<Int, String>> = listOf(
    14 to "Next 2 weeks",
    30 to "Next month",
    90 to "Next 3 months",
    365 to "Next year",
)

/** A show as the calendar needs it. [status] null means no state row: never started. */
data class UpcomingShow(
    val id: Long,
    val tmdbId: Long,
    val title: String,
    val status: ShowStatus?,
    val watchlisted: Boolean,
)

data class UpcomingEpisode(
    val id: Long,
    val show: UpcomingShow,
    val season: Int,
    val number: Int,
    val title: String?,
    val airDate: String?,
    val marker: EpisodeMarker?,
    val watched: Boolean,
    /** The season's poster, else the show's. */
    val posterPath: String?,
)

data class UpcomingMovie(
    val id: Long,
    val tmdbId: Long,
    val title: String,
    val releaseDate: String?,
    val watchlisted: Boolean,
    val watched: Boolean,
    val posterPath: String?,
)

/** One dated thing the user is waiting for: exactly one of [episode] and [movie] is set. */
data class UpcomingEntry(val date: String, val episode: UpcomingEpisode?, val movie: UpcomingMovie?)

/**
 * The server's `listUpcoming`, arm for arm. The window is `[from, from + days)` — today is in, and
 * that deliberately overlaps the aired predicate by one day: this is display, never completion.
 *
 * An episode is listed when its show is followed (a state row) or watchlisted, and not dropped —
 * dropped wins over the watchlist; specials, undated and already watched episodes are out. A
 * movie is listed when it is watchlisted and unwatched.
 *
 * Order: date, case-folded title, episodes before movies, then show id, season, episode and id —
 * the show id keeps two shows sharing a title from interleaving, which the fold depends on.
 */
fun upcomingEntries(
    episodes: List<UpcomingEpisode>,
    movies: List<UpcomingMovie>,
    from: String,
    days: Int,
    type: UpcomingType,
): List<UpcomingEntry> {
    val until = LocalDate.parse(from).plusDays(days.toLong()).toString()
    fun inWindow(date: String?): Boolean = date != null && date >= from && date < until

    val picked = ArrayList<UpcomingEntry>()
    if (type != UpcomingType.MOVIES) {
        episodes.filterTo(ArrayList()) { e ->
            e.season > 0 && inWindow(e.airDate) && !e.watched &&
                (e.show.watchlisted || e.show.status != null) && e.show.status != ShowStatus.DROPPED
        }.mapTo(picked) { UpcomingEntry(it.airDate!!, it, null) }
    }
    if (type != UpcomingType.EPISODES) {
        movies.filter { inWindow(it.releaseDate) && it.watchlisted && !it.watched }
            .mapTo(picked) { UpcomingEntry(it.releaseDate!!, null, it) }
    }
    return picked.sortedWith(
        compareBy<UpcomingEntry> { it.date }
            .thenBy { (it.episode?.show?.title ?: it.movie!!.title).lowercase(Locale.ROOT) }
            .thenBy { if (it.episode != null) 0 else 1 }
            .thenBy { it.episode?.show?.id ?: 0L }
            .thenBy { it.episode?.season ?: 0 }
            .thenBy { it.episode?.number ?: 0 }
            .thenBy { it.episode?.id ?: it.movie!!.id },
    )
}

/** Where a card leads. Titles are addressed by TMDB id, as everywhere in the app. */
sealed interface UpcomingTarget {
    data class Episode(val showTmdbId: Long, val season: Int, val number: Int) : UpcomingTarget
    data class Season(val showTmdbId: Long, val season: Int) : UpcomingTarget
    data class Movie(val tmdbId: Long) : UpcomingTarget
}

data class UpcomingCard(
    val key: String,
    val date: String,
    val title: String,
    val sub: String,
    /** "Episode", "8 episodes" or "Movie". */
    val chip: String,
    val markers: List<EpisodeMarker>,
    val posterPath: String?,
    val target: UpcomingTarget,
    val dateLabel: String,
    /** Today or tomorrow: the date badge is highlighted. */
    val soon: Boolean,
)

/**
 * The web's `buildCards`: the entries in their given order, with a run of ADJACENT episodes of one
 * show, one season and one day (a season released at once) collapsed into a single card that leads
 * to the season. Nothing is re-sorted, and a movie is never grouped.
 */
fun buildUpcomingCards(entries: List<UpcomingEntry>, today: String): List<UpcomingCard> {
    val groups = ArrayList<MutableList<UpcomingEntry>>()
    for (entry in entries) {
        val episode = entry.episode
        val head = groups.lastOrNull()?.first()?.takeIf { it.episode != null }
        if (episode != null && head != null && head.date == entry.date &&
            head.episode!!.show.id == episode.show.id && head.episode.season == episode.season
        ) {
            groups.last().add(entry)
        } else {
            groups.add(mutableListOf(entry))
        }
    }
    return groups.map { group ->
        val first = group.first()
        val offset = ChronoUnit.DAYS.between(LocalDate.parse(today), LocalDate.parse(first.date))
        val relative = when (offset) {
            0L -> "Today"
            1L -> "Tomorrow"
            else -> null
        }
        val label = relative ?: upcomingDayLabel(first.date, today)
        val movie = first.movie
        val episode = first.episode
        when {
            movie != null -> UpcomingCard(
                key = "movie:${movie.id}",
                date = first.date,
                title = movie.title,
                sub = "Release",
                chip = "Movie",
                markers = emptyList(),
                posterPath = movie.posterPath,
                target = UpcomingTarget.Movie(movie.tmdbId),
                dateLabel = label,
                soon = relative != null,
            )
            group.size == 1 -> {
                val code = "S${episode!!.season}xE${episode.number}"
                UpcomingCard(
                    key = "episode:${episode.id}",
                    date = first.date,
                    title = episode.show.title,
                    sub = if (episode.title.isNullOrEmpty()) code else "$code · ${episode.title}",
                    chip = "Episode",
                    markers = listOfNotNull(episode.marker),
                    posterPath = episode.posterPath,
                    target = UpcomingTarget.Episode(episode.show.tmdbId, episode.season, episode.number),
                    dateLabel = label,
                    soon = relative != null,
                )
            }
            else -> {
                val last = group.last().episode!!
                UpcomingCard(
                    key = "run:${episode!!.show.id}:s${episode.season}:${first.date}",
                    date = first.date,
                    title = episode.show.title,
                    // The ends and the count, so a gap in the numbering stays visible.
                    sub = "S${episode.season} · E${episode.number}–E${last.number}",
                    chip = "${group.size} episodes",
                    // A run's markers are its ends'; a mid-season finale inside it is not one.
                    markers = listOfNotNull(
                        EpisodeMarker.SEASON_PREMIERE.takeIf { episode.marker == it },
                        EpisodeMarker.SEASON_FINALE.takeIf { last.marker == it },
                    ),
                    posterPath = episode.posterPath,
                    target = UpcomingTarget.Season(episode.show.tmdbId, episode.season),
                    dateLabel = label,
                    soon = relative != null,
                )
            }
        }
    }
}

private val dayLabelFormat = DateTimeFormatter.ofPattern("EEE d MMM", Locale.ENGLISH)
private val dayLabelYearFormat = DateTimeFormatter.ofPattern("EEE d MMM yyyy", Locale.ENGLISH)

/** "Thu 18 Sep"; the year joins only when it is not today's. Plain days: no zone can move them. */
fun upcomingDayLabel(day: String, today: String): String =
    LocalDate.parse(day).format(if (day.take(4) == today.take(4)) dayLabelFormat else dayLabelYearFormat)

/** The empty page's sentence — the web page's copy. */
fun upcomingEmptyNote(type: UpcomingType, days: Int): String {
    val range = UPCOMING_RANGES.firstOrNull { it.first == days }?.second?.lowercase(Locale.ROOT) ?: "next $days days"
    return when (type) {
        UpcomingType.MOVIES -> "No movie on your watchlist is released in the $range."
        UpcomingType.EPISODES -> "No episode of a show you follow or have listed airs in the $range."
        UpcomingType.ALL -> "Nothing scheduled in the $range."
    }
}

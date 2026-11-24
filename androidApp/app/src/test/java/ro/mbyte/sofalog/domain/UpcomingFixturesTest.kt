package ro.mbyte.sofalog.domain

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Runs the shared upcoming-calendar cases (SPEC D-026, D-040) — the same JSON the server's
 * `upcoming_fixtures_test.go` runs against its query. `want` is the entry order both sides must
 * produce; `want_cards` is the fold, which only clients do.
 */
class UpcomingFixturesTest {
    @Serializable
    private data class FixtureEpisode(
        val season: Int,
        val number: Int,
        @SerialName("air_date") val airDate: String?,
        val watched: Boolean,
        val title: String? = null,
        val type: String? = null,
    )

    @Serializable
    private data class FixtureShow(
        val title: String,
        val status: String?,
        val watchlisted: Boolean,
        val episodes: List<FixtureEpisode>,
    )

    @Serializable
    private data class FixtureMovie(
        val title: String,
        @SerialName("release_date") val releaseDate: String?,
        val watchlisted: Boolean,
        val watched: Boolean,
    )

    @Serializable
    private data class WantCard(
        val title: String,
        val sub: String,
        val chip: String,
        val markers: List<String>,
        @SerialName("date_label") val dateLabel: String,
        val soon: Boolean,
    )

    @Serializable
    private data class Case(
        val name: String,
        val days: Int = UPCOMING_DEFAULT_DAYS,
        val type: String = "all",
        val shows: List<FixtureShow>,
        val movies: List<FixtureMovie>,
        val want: List<String>,
        @SerialName("want_cards") val wantCards: List<WantCard>? = null,
    )

    @Serializable
    private data class FixtureFile(val today: String, val cases: List<Case>)

    @Test
    fun upcoming() {
        // Gradle runs unit tests from the module directory (androidApp/app).
        val source = File("../../fixtures/video-rules/upcoming.json")
        assertTrue("fixture not found at ${source.absolutePath}", source.exists())
        val file = Json { ignoreUnknownKeys = true }.decodeFromString<FixtureFile>(source.readText())
        assertTrue(file.cases.isNotEmpty())

        for (c in file.cases) {
            // Ids in file order, each table counting from 1 — as the server's seeds number them.
            var episodeId = 0L
            val episodes = c.shows.flatMapIndexed { i, s ->
                val show = UpcomingShow(i + 1L, 100L + i, s.title, ShowStatus.of(s.status), s.watchlisted)
                s.episodes.map { e ->
                    UpcomingEpisode(++episodeId, show, e.season, e.number, e.title, e.airDate, episodeMarker(e.type, e.number), e.watched, null)
                }
            }
            val movies = c.movies.mapIndexed { i, m ->
                UpcomingMovie(i + 1L, 900L + i, m.title, m.releaseDate, m.watchlisted, m.watched, null)
            }
            val type = when (c.type) {
                "episode" -> UpcomingType.EPISODES
                "movie" -> UpcomingType.MOVIES
                else -> UpcomingType.ALL
            }

            val entries = upcomingEntries(episodes, movies, file.today, c.days, type)
            val keys = entries.map { e ->
                e.episode?.let { "${e.date} episode ${it.show.title} S${it.season}E${it.number}" }
                    ?: "${e.date} movie ${e.movie!!.title}"
            }
            assertEquals(c.name, c.want, keys)

            c.wantCards?.let { want ->
                val cards = buildUpcomingCards(entries, file.today).map { card ->
                    WantCard(card.title, card.sub, card.chip, card.markers.map { it.name.lowercase() }, card.dateLabel, card.soon)
                }
                assertEquals(c.name, want, cards)
            }
        }
    }

    @Test
    fun emptyNotes() {
        assertEquals("Nothing scheduled in the next 3 months.", upcomingEmptyNote(UpcomingType.ALL, 90))
        assertEquals("No movie on your watchlist is released in the next 2 weeks.", upcomingEmptyNote(UpcomingType.MOVIES, 14))
        assertEquals(
            "No episode of a show you follow or have listed airs in the next 45 days.",
            upcomingEmptyNote(UpcomingType.EPISODES, 45),
        )
    }
}

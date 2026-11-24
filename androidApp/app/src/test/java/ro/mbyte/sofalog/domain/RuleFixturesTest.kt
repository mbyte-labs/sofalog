package ro.mbyte.sofalog.domain

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Runs the shared rule fixtures (SPEC D-040) — the same JSON cases the server's Go tests run
 * against RecomputeShowStatus and the Up Next query.
 */
class RuleFixturesTest {
    @Serializable
    private data class FixtureEpisode(
        val season: Int,
        val number: Int,
        @SerialName("air_date") val airDate: String?,
        val watched: Boolean,
    )

    @Serializable
    private data class Position(val season: Int, val number: Int)

    @Serializable
    private data class StatusCase(
        val name: String,
        val current: String?,
        val want: String?,
        val episodes: List<FixtureEpisode>,
    )

    @Serializable
    private data class StatusFile(val today: String, val cases: List<StatusCase>)

    @Serializable
    private data class NextCase(
        val name: String,
        val dropped: Boolean,
        val want: Position?,
        val episodes: List<FixtureEpisode>,
    )

    @Serializable
    private data class NextFile(val today: String, val cases: List<NextCase>)

    private val json = Json { ignoreUnknownKeys = true }

    private fun fixture(name: String): String {
        // Gradle runs unit tests from the module directory (androidApp/app).
        val file = File("../../fixtures/video-rules/$name")
        assertTrue("fixture not found at ${file.absolutePath}", file.exists())
        return file.readText()
    }

    private fun refs(episodes: List<FixtureEpisode>): Pair<List<EpisodeRef>, Set<Long>> {
        val refs = episodes.mapIndexed { i, e -> EpisodeRef(i + 1L, e.season, e.number, e.airDate) }
        val watched = episodes.withIndex().filter { it.value.watched }.map { it.index + 1L }.toSet()
        return refs to watched
    }

    @Test
    fun showStatus() {
        val file = json.decodeFromString<StatusFile>(fixture("show-status.json"))
        assertTrue(file.cases.isNotEmpty())
        for (c in file.cases) {
            val (episodes, watched) = refs(c.episodes)
            val got = recomputeShowStatus(ShowStatus.of(c.current), episodes, watched, file.today)
            assertEquals(c.name, c.want, got?.value)
        }
    }

    @Test
    fun nextEpisode() {
        val file = json.decodeFromString<NextFile>(fixture("next-episode.json"))
        assertTrue(file.cases.isNotEmpty())
        for (c in file.cases) {
            val (episodes, watched) = refs(c.episodes)
            val status = if (c.dropped) ShowStatus.DROPPED else null
            val got = upNextEpisode(status, episodes, watched)
            assertEquals(c.name, c.want, got?.let { Position(it.season, it.number) })
        }
    }
}

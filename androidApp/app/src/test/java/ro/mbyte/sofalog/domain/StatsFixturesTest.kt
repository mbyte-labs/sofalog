package ro.mbyte.sofalog.domain

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.doubleOrNull
import kotlinx.serialization.json.encodeToJsonElement
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test
import java.io.File
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import kotlin.math.abs

/**
 * Runs the shared Stats cases (SPEC D-039, D-040) — the same JSON the server's Go test
 * (`stats_fixtures_test.go`) runs against its own fold. A case states the sections it cares about
 * as the page's JSON; the comparison is a subset, and a title is compared by kind, name and year,
 * since ids are local to each side.
 */
class StatsFixturesTest {
    @Serializable
    private data class FixtureMovie(
        val key: String,
        val title: String,
        @SerialName("release_date") val releaseDate: String? = null,
        val runtime: Int? = null,
        val genres: List<String>? = null,
        @SerialName("vote_average") val voteAverage: Double? = null,
        @SerialName("vote_count") val voteCount: Long? = null,
    )

    @Serializable
    private data class FixtureEpisode(
        val key: String,
        val season: Int,
        val number: Int,
        @SerialName("air_date") val airDate: String? = null,
        val runtime: Int? = null,
    )

    @Serializable
    private data class FixtureShow(
        val key: String,
        val title: String,
        @SerialName("first_air_date") val firstAirDate: String? = null,
        val genres: List<String>? = null,
        @SerialName("episode_run_time") val episodeRunTime: Int? = null,
        @SerialName("vote_average") val voteAverage: Double? = null,
        @SerialName("vote_count") val voteCount: Long? = null,
        val status: String? = null,
        val episodes: List<FixtureEpisode> = emptyList(),
    )

    @Serializable
    private data class FixturePlay(val target: String, val at: String)

    @Serializable
    private data class FixtureRating(val target: String, val rating: Int, val at: String)

    @Serializable
    private data class FixtureHeatmap(val start: String, val days: Int, val cells: Map<String, List<Int>>)

    @Serializable
    private data class Case(
        val name: String,
        val zone: String,
        val now: String? = null,
        val period: String,
        val movies: List<FixtureMovie>,
        val shows: List<FixtureShow>,
        val plays: List<FixturePlay>,
        val ratings: List<FixtureRating>,
        val want: JsonObject,
        @SerialName("want_heatmap") val wantHeatmap: FixtureHeatmap? = null,
    )

    @Serializable
    private data class FixtureFile(val now: String, val cases: List<Case>)

    private val json = Json { ignoreUnknownKeys = true }
    private val encoder = Json { encodeDefaults = true; explicitNulls = true }

    private class Target(val kind: String, val id: Long, val play: (Long, String) -> StatsPlay?)

    /** The case's library as the fold's input, numbered in the file's order as the Go seed does. */
    private fun input(c: Case, today: String): StatsInput {
        val targets = HashMap<String, Target>()
        val titles = HashMap<StatsTitleKey, StatsTitle>()
        val votes = HashMap<StatsTitleKey, StatsVoteFacts>()
        val fallbacks = HashMap<Long, Int>()
        val completion = ArrayList<StatsCompletionRow>()
        val watchedEpisodes = HashSet<Long>()
        val episodeIds = HashMap<String, Long>()

        c.movies.forEachIndexed { i, m ->
            val id = i + 1L
            val key = StatsTitleKey(STATS_MOVIE, id)
            titles[key] = StatsTitle(STATS_MOVIE, id, 1000L + i, m.title, statsYear(m.releaseDate), null, null)
            votes[key] = StatsVoteFacts(m.voteAverage ?: 0.0, m.voteCount ?: 0)
            targets[m.key] = Target("movie", id) { playId, at ->
                StatsPlay(playId, "movie", id, at, movieRuntime = m.runtime, movieRelease = m.releaseDate, movieGenres = m.genres)
            }
        }
        var nextEpisode = 1L
        c.shows.forEachIndexed { i, sh ->
            val showId = i + 1L
            val key = StatsTitleKey(STATS_SHOW, showId)
            titles[key] = StatsTitle(STATS_SHOW, showId, 2000L + i, sh.title, statsYear(sh.firstAirDate), null, null)
            votes[key] = StatsVoteFacts(sh.voteAverage ?: 0.0, sh.voteCount ?: 0)
            sh.episodeRunTime?.takeIf { it > 0 }?.let { fallbacks[showId] = it }
            targets[sh.key] = Target("show", showId) { _, _ -> null }
            for (e in sh.episodes) {
                val id = nextEpisode++
                episodeIds[e.key] = id
                targets[e.key] = Target("episode", id) { playId, at ->
                    StatsPlay(
                        playId, "episode", id, at,
                        showId = showId, season = e.season, number = e.number, episodeRuntime = e.runtime,
                        showFirstAir = sh.firstAirDate, showGenres = sh.genres,
                    )
                }
            }
        }
        fun target(key: String) = targets[key] ?: error("fixture names an unknown target $key")
        val plays = c.plays.mapIndexed { i, p ->
            val t = target(p.target)
            if (t.kind == "episode") watchedEpisodes += t.id
            t.play(i + 1L, p.at) ?: error("${p.target} cannot be played")
        }
        // Progress is derived exactly as the repository derives it: the show rules, over the log.
        c.shows.forEachIndexed { i, sh ->
            if (sh.status == null || sh.status == "dropped") return@forEachIndexed
            val refs = sh.episodes.map { EpisodeRef(episodeIds.getValue(it.key), it.season, it.number, it.airDate) }
            val progress = showProgress(refs, watchedEpisodes, today)
            completion += StatsCompletionRow(i + 1L, aired = progress.airedTotal, watched = progress.watched)
        }
        val ratings = c.ratings.map { r -> target(r.target).let { StatsRating(it.kind, it.id, r.rating, r.at) } }
        return StatsInput(plays, ratings, titles, fallbacks, votes, completion)
    }

    /** Every title object as its kind, name and year. */
    private fun normalise(node: JsonElement): JsonElement = when (node) {
        is JsonObject ->
            if ("kind" in node && "title" in node && "id" in node) {
                JsonObject(mapOf("kind" to node.getValue("kind"), "title" to node.getValue("title"), "year" to node.getValue("year")))
            } else {
                JsonObject(node.mapValues { normalise(it.value) })
            }
        is JsonArray -> JsonArray(node.map(::normalise))
        else -> node
    }

    /** Fails at the first place [got] does not carry what [want] states. */
    private fun assertSubset(case: String, path: String, want: JsonElement, got: JsonElement?) {
        when (want) {
            is JsonObject -> {
                assertTrue("$case: $path = $got, want an object", got is JsonObject)
                for ((k, child) in want) {
                    assertTrue("$case: $path.$k is missing", k in (got as JsonObject))
                    assertSubset(case, "$path.$k", child, got[k])
                }
            }
            is JsonArray -> {
                assertTrue("$case: $path = $got, want $want", got is JsonArray && got.size == want.size)
                want.forEachIndexed { i, child -> assertSubset(case, "$path[$i]", child, (got as JsonArray)[i]) }
            }
            is JsonNull -> assertTrue("$case: $path = $got, want null", got is JsonNull)
            is JsonPrimitive -> {
                val number = want.doubleOrNull
                val other = (got as? JsonPrimitive)?.takeIf { it !is JsonNull }
                if (number != null && !want.isString) {
                    val value = other?.doubleOrNull
                    if (value == null || abs(value - number) > 1e-9) fail("$case: $path = $got, want $want")
                } else {
                    assertEquals("$case: $path", want, other)
                }
            }
        }
    }

    @Test
    fun stats() {
        val raw = File("../../fixtures/video-rules/stats.json")
        assertTrue("fixture not found at ${raw.absolutePath}", raw.exists())
        val file = json.decodeFromString<FixtureFile>(raw.readText())
        assertTrue(file.cases.isNotEmpty())
        for (c in file.cases) {
            val now = Instant.parse(c.now ?: file.now)
            val zone = ZoneId.of(c.zone)
            val today = LocalDate.ofInstant(now, zone).toString()
            val got = computeStats(input(c, today), c.period, now, zone)

            assertTrue("${c.name}: the case asserts nothing", c.want.isNotEmpty() || c.wantHeatmap != null)
            assertSubset(c.name, "stats", c.want, normalise(encoder.encodeToJsonElement(got)))
            c.wantHeatmap?.let { want ->
                val heatmap = got.activity.heatmap
                assertEquals("${c.name}: heatmap start", want.start, heatmap.start)
                assertEquals("${c.name}: heatmap days", want.days, heatmap.plays.size)
                assertEquals("${c.name}: heatmap levels", want.days, heatmap.levels.size)
                val start = LocalDate.parse(heatmap.start)
                for (i in heatmap.plays.indices) {
                    val day = start.plusDays(i.toLong()).toString()
                    assertEquals("${c.name}: heatmap $day", want.cells[day] ?: listOf(0, 0), listOf(heatmap.plays[i], heatmap.levels[i]))
                }
            }
        }
    }

    @Test
    fun periodIsValidatedBeforeAnythingIsRead() {
        val empty = StatsInput(emptyList(), emptyList(), emptyMap())
        val now = Instant.parse("2026-08-18T12:00:00Z")
        for (bad in listOf("2026-01", "abc", "20261", "-1", " 2026", "ALL", "all ", "202")) {
            try {
                computeStats(empty, bad, now, ZoneId.of("UTC"))
                fail("period \"$bad\" must be refused")
            } catch (expected: IllegalArgumentException) {
                // the server's 422
            }
        }
        val stats = computeStats(empty, null, now, ZoneId.of("UTC"))
        assertEquals("all", stats.period)
        assertEquals(emptyList<Int>(), stats.years)
        assertNull(stats.completion)
        assertTrue(stats.isEmpty())
    }

    @Test
    fun heatLevelsAreQuartilesOfTheActiveDays() {
        assertEquals(listOf(1, 1, 2, 3, 4, 0), heatLevels(listOf(1, 1, 2, 3, 10, 0)))
        assertEquals(listOf(4, 0, 4, 4), heatLevels(listOf(2, 0, 2, 2)))
        assertEquals(listOf(0, 4, 0), heatLevels(listOf(0, 7, 0)))
        assertEquals(listOf(0, 0), heatLevels(listOf(0, 0)))
    }
}

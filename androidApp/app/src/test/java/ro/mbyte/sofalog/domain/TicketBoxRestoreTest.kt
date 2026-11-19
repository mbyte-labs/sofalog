package ro.mbyte.sofalog.domain

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import java.time.Instant

class TicketBoxRestoreTest {
    @Serializable
    private data class InstantCase(val raw: String, val want: String? = null, val reason: String = "")

    @Serializable
    private data class SkipCase(val kind: String, val reason: String)

    @Serializable
    private data class ValidateCase(val name: String, val dump: JsonObject, val skips: List<SkipCase>, val movies: List<Long>, val shows: List<Long>)

    @Serializable
    private data class HintCase(val numbers: List<Int>, val season: Int, val episode: Int, val hint: String, val last: Int?)

    @Serializable
    private data class RestoreFile(
        val now: String,
        val instants: List<InstantCase>,
        val validate: List<ValidateCase>,
        @SerialName("episode_hints") val hints: List<HintCase>,
    )

    private fun parse(body: String) = parseTicketBoxDump { body.byteInputStream() }

    /** The shared cases the server's `restore` tests run too (SPEC D-040). */
    @Test
    fun sharedFixtures() {
        val source = File("../../fixtures/video-rules/ticket-box-restore.json")
        assertTrue("fixture not found at ${source.absolutePath}", source.exists())
        val file = Json.decodeFromString<RestoreFile>(source.readText())
        val now = Instant.parse(file.now)
        assertTrue(file.instants.isNotEmpty() && file.validate.isNotEmpty() && file.hints.isNotEmpty())

        for (c in file.instants) {
            val got = normaliseInstant(c.raw, now)
            val reason = when (got) {
                is InstantResult.Ok -> ""
                InstantResult.Future -> SKIP_FUTURE_TIMESTAMP
                InstantResult.Unparsable -> SKIP_INVALID_TIMESTAMP
            }
            assertEquals("reason of ${c.raw}", c.reason, reason)
            if (c.want != null) assertEquals(c.raw, c.want, (got as InstantResult.Ok).at)
        }
        for (c in file.validate) {
            val dump = (parse(c.dump.toString()) as DumpParse.Ok).dump
            val (plan, skips) = validateTicketBoxDump(dump, now)
            assertEquals(c.name, c.skips, skips.map { SkipCase(it.kind, it.reason) })
            assertEquals(c.name, c.movies, plan.movies.map { it.tmdbId })
            assertEquals(c.name, c.shows, plan.shows.map { it.tmdbId })
        }
        for (c in file.hints) {
            assertEquals("${c.numbers} ${c.season}x${c.episode}", c.hint to c.last, episodeSkipHint(c.numbers, c.season, c.episode))
        }
    }

    @Test
    fun anOlderFileIsToldToExportAgainBeforeAnythingElseIsRead() {
        // Version 1 has fields version 5 does not define; the version is still what is reported.
        val old = parse("""{"schemaVersion":1,"history":[{"traktId":5}]}""") as DumpParse.Rejected
        assertEquals("version", old.code)
        assertTrue(old.message, "version 1" in old.message && "version 5 only" in old.message)
        assertEquals("version", (parse("""{"history":[]}""") as DumpParse.Rejected).code)
        assertTrue("unknown" in (parse("""{"schemaVersion":"5"}""") as DumpParse.Rejected).message)
        assertEquals("version", (parse("""{"schemaVersion":[1,2]}""") as DumpParse.Rejected).code)
    }

    @Test
    fun whatIsNotJsonIsNotAVersionProblem() {
        assertEquals("not_json", (parse("not json at all") as DumpParse.Rejected).code)
        assertEquals("not_json", (parse("""{"schemaVersion":5,"history":[""") as DumpParse.Rejected).code)
        assertEquals("That isn't a JSON file.", (parse("") as DumpParse.Rejected).message)
    }

    @Test
    fun aFieldTheVersionDoesNotDefineIsRefusedByName() {
        val top = parse("""{"schemaVersion":5,"collection":[]}""") as DumpParse.Rejected
        assertEquals("shape", top.code)
        assertTrue(top.message, "unknown field \"collection\"" in top.message)
        val entry = parse("""{"schemaVersion":5,"favorites":[{"type":"movie","tmdbId":1,"listedAt":"x"}]}""") as DumpParse.Rejected
        assertTrue(entry.message, "unknown field \"listedAt\"" in entry.message)
    }

    @Test
    fun aVersionFiveFileDecodesWithNullsAndMissingLists() {
        val dump = (
            parse(
                """{"schemaVersion":5,"exportedAt":"2026-01-01T00:00:00Z","history":[
                {"type":"episode","tmdbId":null,"imdbId":"tt1","showTmdbId":1399,"season":1,"episode":2,
                 "addedAt":"2026-01-01T00:00:00Z","historyType":"watch"}]}""",
            ) as DumpParse.Ok
            ).dump
        assertEquals(1, dump.history?.size)
        assertNull(dump.history?.first()?.tmdbId)
        assertNull(dump.ratings)
        assertNull(dump.favorites)
    }

    @Test
    fun aGroupKeepsEveryWatchAndTheLastRating() {
        val dump = (
            parse(
                """{"schemaVersion":5,
                "history":[
                  {"type":"movie","tmdbId":603,"addedAt":"2026-01-02T10:00:00+02:00","historyType":"watch"},
                  {"type":"movie","tmdbId":603,"addedAt":"2026-01-02T10:00:00+02:00","historyType":"checkin"},
                  {"type":"episode","tmdbId":77,"showTmdbId":1399,"season":1,"episode":2,"addedAt":"2026-01-03T10:00:00Z","historyType":"watch"}],
                "ratings":[
                  {"type":"movie","tmdbId":603,"rating":6,"ratedAt":"2026-01-01T00:00:00Z"},
                  {"type":"movie","tmdbId":603,"rating":9,"ratedAt":"2026-01-05T00:00:00Z"},
                  {"type":"episode","showTmdbId":1399,"season":2,"episode":1,"rating":7,"ratedAt":"2026-01-05T00:00:00Z"},
                  {"type":"episode","showTmdbId":1399,"season":1,"episode":9,"rating":8,"ratedAt":"2026-01-05T00:00:00Z"}]}""",
            ) as DumpParse.Ok
            ).dump
        val (plan, skips) = validateTicketBoxDump(dump, Instant.parse("2026-11-19T12:00:00Z"))
        assertTrue(skips.isEmpty())
        val movie = plan.movies.single()
        // Duplicates are kept — each is a viewing — and the offset is folded into UTC.
        assertEquals(listOf("2026-01-02T08:00:00Z", "2026-01-02T08:00:00Z"), movie.watches)
        assertEquals(PlannedRating(9, "2026-01-05T00:00:00Z"), movie.rating)
        val show = plan.shows.single()
        assertEquals(PlannedEpisodeWatch(1, 2, 77, "2026-01-03T10:00:00Z"), show.watches.single())
        assertEquals(listOf(1 to 9, 2 to 1), show.episodeRatings.map { it.season to it.episode })
        // A whole-group refusal is one skip per entry.
        assertEquals(3, movie.skips(SKIP_MOVIE_NOT_FOUND).size)
        assertEquals(3, show.skips(SKIP_SHOW_UNAVAILABLE).size)
    }

    @Test
    fun anEpisodeIsFoundByItsOwnIdBeforeItsPosition() {
        val resolver = EpisodeResolver(
            listOf(
                EpisodeKey(1, 0, 1, tmdbId = 500),
                EpisodeKey(2, 0, 401, tmdbId = 900),
                EpisodeKey(3, 1, 1, tmdbId = null),
            ),
        )
        // The file still says 0x1, but TMDB moved that episode to 0x401: the id wins.
        assertEquals(2L, resolver.resolve(900, 0, 1))
        // An id nobody holds, or none at all, falls back to the position.
        assertEquals(1L, resolver.resolve(12345, 0, 1))
        assertEquals(3L, resolver.resolve(null, 1, 1))
        assertNull(resolver.resolve(null, 1, 2))
        assertNull(resolver.resolve(12345, 3, 3))
        assertEquals(listOf(1, 401), resolver.numbers(0))
        assertEquals(emptyList<Int>(), resolver.numbers(9))
    }

    @Test
    fun twoRowsWithOneIdPreferTheFilesOwnPositionThenTheNewest() {
        val resolver = EpisodeResolver(listOf(EpisodeKey(1, 1, 5, tmdbId = 42), EpisodeKey(7, 1, 6, tmdbId = 42)))
        assertEquals(1L, resolver.resolve(42, 1, 5))
        assertEquals(7L, resolver.resolve(42, 1, 6))
        assertEquals(7L, resolver.resolve(42, 3, 3))
    }

    @Test
    fun theSkipListIsCappedButTheCountIsNot() {
        val skip = RestoreSkip(RESTORE_HISTORY, "movie", 1, null, null, null, SKIP_MOVIE_NOT_FOUND)
        var report = RestoreReport().withSkips(List(150) { skip })
        report = report.withSkips(List(150) { skip })
        report = report.withSkips(listOf(skip))
        assertEquals(301, report.skippedTotal)
        assertEquals(RESTORE_SKIP_CAP, report.skipped.size)
        val summed = RestoreReport() + RestoreCounts(2, 1, 3, 1, 1) + RestoreCounts(historyInserted = 5)
        assertEquals(7, summed.historyInserted)
        assertEquals(1, summed.historyReplaced)
        assertEquals(3, summed.ratingsUpserted)
    }

    @Test
    fun theSentencesThePageShows() {
        assertEquals("Movie not found on TMDB", skipReasonLabel(SKIP_MOVIE_NOT_FOUND))
        assertEquals("some new reason", skipReasonLabel("some_new_reason"))
        val base = RestoreSkip(RESTORE_HISTORY, "episode", null, 1399, 2, 4, SKIP_EPISODE_NOT_FOUND)
        assertEquals(
            "TMDB's season 2 ends at episode 3, so this is most likely the second half of a two-part episode that TMDB lists as one.",
            skipReasonExplanation(base.copy(hint = HINT_PAST_SEASON_END, tmdbLastEpisode = 3)),
        )
        assertEquals("TMDB lists no season 2 for this show.", skipReasonExplanation(base.copy(hint = HINT_SEASON_MISSING)))
        assertTrue(skipReasonExplanation(base.copy(hint = HINT_NUMBERING_GAP))!!.startsWith("TMDB skips episode 4 in season 2, so episode 3"))
        assertTrue(skipReasonExplanation(base.copy(episode = 1, hint = HINT_NUMBERING_GAP))!!.startsWith("TMDB lists no episode 1"))
        assertNull(skipReasonExplanation(base))
        assertTrue(skipReasonExplanation(base.copy(reason = SKIP_PROVIDER_UNAVAILABLE))!!.endsWith("so that's safe."))

        assertEquals("Show 1399 · Season 2 · Episode 4", skipTarget(base))
        assertEquals("Top Gear · Specials · Episode 1", skipTarget(base.copy(title = "Top Gear", season = 0, episode = 1)))
        assertEquals("Movie 603", skipTarget(RestoreSkip(RESTORE_RATING, "movie", 603, null, null, null, SKIP_MOVIE_NOT_FOUND)))
        assertEquals("Watch", skipKindLabel(RESTORE_HISTORY))

        assertEquals("Preparing…", restoreProgressLine(RestoreProgress(4)))
        assertEquals(
            "Importing show 3 of 4 — Fetching…",
            restoreProgressLine(RestoreProgress(4, 2, RestoreCurrent("show", 1399, null))),
        )
        assertEquals(50, restorePercent(RestoreProgress(4, 2)))
        assertEquals(0, restorePercent(RestoreProgress(0)))
    }
}

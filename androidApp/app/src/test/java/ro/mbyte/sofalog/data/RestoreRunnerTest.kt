package ro.mbyte.sofalog.data

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import ro.mbyte.sofalog.data.tmdb.TmdbException
import ro.mbyte.sofalog.domain.Clock
import ro.mbyte.sofalog.domain.ClockReading
import ro.mbyte.sofalog.domain.MovieGroup
import ro.mbyte.sofalog.domain.PlannedCopy
import ro.mbyte.sofalog.domain.PlannedEpisodeWatch
import ro.mbyte.sofalog.domain.PlannedSeasonCopy
import ro.mbyte.sofalog.domain.RESTORE_CANCELLED_MESSAGE
import ro.mbyte.sofalog.domain.RESTORE_DONE
import ro.mbyte.sofalog.domain.RESTORE_FAILED
import ro.mbyte.sofalog.domain.RESTORE_HISTORY
import ro.mbyte.sofalog.domain.RESTORE_RUNNING
import ro.mbyte.sofalog.domain.RestoreCounts
import ro.mbyte.sofalog.domain.RestoreJob
import ro.mbyte.sofalog.domain.RestorePlan
import ro.mbyte.sofalog.domain.RestoreProgress
import ro.mbyte.sofalog.domain.RestoreReport
import ro.mbyte.sofalog.domain.RestoreSkip
import ro.mbyte.sofalog.domain.SKIP_EPISODE_NOT_FOUND
import ro.mbyte.sofalog.domain.SKIP_MOVIE_NOT_FOUND
import ro.mbyte.sofalog.domain.SKIP_MOVIE_UNAVAILABLE
import ro.mbyte.sofalog.domain.SKIP_PROVIDER_UNAVAILABLE
import ro.mbyte.sofalog.domain.ShowGroup

/** The restore's run loop against a fake library: what is fetched, skipped, counted and reported. */
class RestoreRunnerTest {
    private val clock = Clock { ClockReading("2026-11-19T12:00:00Z", "2026-11-19") }

    private class FakeWrites(
        /** Movies already in the library, by TMDB id. */
        val held: Set<Long> = emptySet(),
        /** What fetching a movie that is not held does. */
        val fetch: (Long) -> Unit = {},
        val onWrite: () -> Unit = {},
    ) : RestoreWrites {
        val fetched = mutableListOf<Long>()
        val written = mutableListOf<Long>()

        override suspend fun resolveRestoreMovie(tmdbId: Long, mayFetch: Boolean): RestoreResolved? {
            if (tmdbId in held) return RestoreResolved(tmdbId, "Movie $tmdbId", materialised = false)
            if (!mayFetch) return null
            fetched += tmdbId
            fetch(tmdbId)
            return RestoreResolved(tmdbId, "Movie $tmdbId", materialised = true)
        }

        override suspend fun resolveRestoreShow(tmdbId: Long, mayFetch: Boolean) =
            RestoreResolved(tmdbId, "Show $tmdbId", materialised = false)

        override suspend fun writeRestoreMovie(target: RestoreResolved, group: MovieGroup): RestoreCounts {
            onWrite()
            written += group.tmdbId
            return RestoreCounts(historyInserted = group.watches.size)
        }

        override suspend fun writeRestoreShow(target: RestoreResolved, group: ShowGroup): Pair<RestoreCounts, List<RestoreSkip>> =
            RestoreCounts(historyInserted = 1) to
                listOf(RestoreSkip(RESTORE_HISTORY, "episode", null, group.tmdbId, 1, 9, SKIP_EPISODE_NOT_FOUND, "Show ${group.tmdbId}"))

        override suspend fun restoreTitle(movie: Boolean, tmdbId: Long): String? = if (tmdbId in held) "Held $tmdbId" else null
    }

    private fun movie(id: Long, watches: Int = 1) = MovieGroup(id, List(watches) { "2026-01-01T00:00:00Z" }, null, null, null)

    private fun run(plan: RestorePlan, writes: RestoreWrites): RestoreJob {
        var job = RestoreJob(
            state = RESTORE_RUNNING,
            startedAt = clock.read().now,
            progress = RestoreProgress(plan.groups),
            report = RestoreReport(moviesTotal = plan.movies.size, showsTotal = plan.shows.size),
        )
        runBlocking { runRestore(plan, writes, clock) { change -> job = change(job) } }
        return job
    }

    private fun outage() = TmdbException(TmdbException.Reason.UNAVAILABLE)

    @Test
    fun everyGroupIsWrittenAndCounted() {
        val writes = FakeWrites(held = setOf(1))
        val plan = RestorePlan(
            listOf(movie(1, watches = 2), movie(2)),
            listOf(ShowGroup(10, listOf(PlannedEpisodeWatch(1, 9, null, "2026-01-01T00:00:00Z")), null, emptyList(), null, null)),
        )
        val job = run(plan, writes)
        assertEquals(RESTORE_DONE, job.state)
        assertNull(job.error)
        assertEquals(3, job.progress.groupsDone)
        assertNull(job.progress.current)
        assertEquals("2026-11-19T12:00:00Z", job.finishedAt)
        assertEquals(4, job.report.historyInserted)
        // Only the movie that had to be fetched counts as added to the library.
        assertEquals(1, job.report.moviesMaterialised)
        assertEquals(listOf(2L), writes.fetched)
        assertEquals(SKIP_EPISODE_NOT_FOUND, job.report.skipped.single().reason)
    }

    @Test
    fun copiesAndDropsAreCountedInTheReport() {
        val writes = object : RestoreWrites by FakeWrites(held = setOf(1)) {
            override suspend fun writeRestoreMovie(target: RestoreResolved, group: MovieGroup) =
                RestoreCounts(copiesInserted = group.copies.size)

            override suspend fun writeRestoreShow(target: RestoreResolved, group: ShowGroup) =
                RestoreCounts(copiesInserted = group.seasonCopies.size, showsDropped = if (group.droppedAt != null) 1 else 0) to
                    emptyList<RestoreSkip>()
        }
        val at = "2026-01-01T00:00:00Z"
        val plan = RestorePlan(
            listOf(MovieGroup(1, emptyList(), null, null, null, copies = listOf(PlannedCopy("dvd", at), PlannedCopy("uhd", at)), exact = true)),
            listOf(ShowGroup(10, emptyList(), null, emptyList(), null, null, listOf(PlannedSeasonCopy(1, "dvd", at)), droppedAt = at, exact = true)),
        )
        val job = run(plan, writes)
        assertEquals(RESTORE_DONE, job.state)
        assertEquals(3, job.report.copiesInserted)
        assertEquals(1, job.report.showsDropped)
    }

    @Test
    fun aTitleTmdbDoesNotKnowSkipsItsWholeGroup() {
        val writes = FakeWrites(fetch = { throw TmdbException(TmdbException.Reason.NOT_FOUND) })
        val job = run(RestorePlan(listOf(movie(5, watches = 3)), emptyList()), writes)
        assertEquals(RESTORE_DONE, job.state)
        assertEquals(3, job.report.skippedTotal)
        assertTrue(job.report.skipped.all { it.reason == SKIP_MOVIE_NOT_FOUND })
        assertTrue(writes.written.isEmpty())
        assertEquals(1, job.progress.groupsDone)
    }

    @Test
    fun anOutageIsRetriedOnceBeforeItCounts() {
        var failures = 1
        val writes = FakeWrites(fetch = { if (failures-- > 0) throw outage() })
        val job = run(RestorePlan(listOf(movie(5)), emptyList()), writes)
        assertEquals(0, job.report.skippedTotal)
        assertEquals(listOf(5L, 5L), writes.fetched)
        assertEquals(listOf(5L), writes.written)
    }

    @Test
    fun afterThreeOutagesInARowNothingMoreIsFetchedButHeldTitlesStillImport() {
        val writes = FakeWrites(held = setOf(9), fetch = { throw outage() })
        val job = run(RestorePlan((1L..5L).map { movie(it) } + movie(9), emptyList()), writes)
        assertEquals(RESTORE_DONE, job.state)
        // Three groups asked (twice each, the retry); the next two are skipped without a call.
        assertEquals(listOf(1L, 1L, 2L, 2L, 3L, 3L), writes.fetched)
        assertEquals(
            listOf(SKIP_MOVIE_UNAVAILABLE, SKIP_MOVIE_UNAVAILABLE, SKIP_MOVIE_UNAVAILABLE, SKIP_PROVIDER_UNAVAILABLE, SKIP_PROVIDER_UNAVAILABLE),
            job.report.skipped.map { it.reason },
        )
        assertEquals(listOf(9L), writes.written)
        assertEquals(6, job.progress.groupsDone)
    }

    @Test
    fun withoutAKeyTheImportFailsInsteadOfSkippingEverything() {
        val writes = FakeWrites(fetch = { throw TmdbException(TmdbException.Reason.NO_KEY) })
        val job = run(RestorePlan(listOf(movie(1), movie(2)), emptyList()), writes)
        assertEquals(RESTORE_FAILED, job.state)
        assertTrue(job.error!!.contains("no TMDB key"))
        assertEquals(listOf(1L), writes.fetched)
    }

    @Test
    fun aStoppedImportKeepsWhatItWroteAndSaysSo() {
        var writes = 0
        val fake = FakeWrites(held = setOf(1, 2, 3), onWrite = { if (++writes == 2) throw CancellationException("stopped") })
        val job = run(RestorePlan(listOf(movie(1), movie(2), movie(3)), emptyList()), fake)
        assertEquals(RESTORE_FAILED, job.state)
        assertEquals(RESTORE_CANCELLED_MESSAGE, job.error)
        assertEquals(listOf(1L), fake.written)
        assertEquals(1, job.report.historyInserted)
    }

    @Test
    fun skipsGetTheirTitleFromTheLibraryAtTheEnd() {
        val writes = FakeWrites(held = setOf(7))
        var job = RestoreJob(
            state = RESTORE_RUNNING,
            startedAt = clock.read().now,
            progress = RestoreProgress(0),
            report = RestoreReport(
                skippedTotal = 2,
                skipped = listOf(
                    RestoreSkip(RESTORE_HISTORY, "movie", 7, null, null, null, "future_timestamp"),
                    RestoreSkip(RESTORE_HISTORY, "movie", 8, null, null, null, "future_timestamp"),
                ),
            ),
        )
        runBlocking { runRestore(RestorePlan(emptyList(), emptyList()), writes, clock) { change -> job = change(job) } }
        assertEquals(listOf("Held 7", null), job.report.skipped.map { it.title })
    }
}

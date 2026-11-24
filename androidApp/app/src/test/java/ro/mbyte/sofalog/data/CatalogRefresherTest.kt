package ro.mbyte.sofalog.data

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import ro.mbyte.sofalog.data.tmdb.TmdbException
import ro.mbyte.sofalog.domain.RefreshCandidate
import java.time.Duration
import java.time.Instant

/** The refresh tick against a fake library: what is fetched, what stops it, what always runs. */
class CatalogRefresherTest {
    private var now = Instant.parse("2026-11-23T12:00:00Z")
    private val old = "2026-01-01T00:00:00Z"
    private val fresh = "2026-11-23T00:00:00Z"

    private class FakeWrites(
        val movies: List<RefreshCandidate> = emptyList(),
        val shows: List<RefreshCandidate> = emptyList(),
        val fail: (Long) -> TmdbException.Reason? = { null },
    ) : CatalogRefreshWrites {
        val fetchedMovies = mutableListOf<Long>()
        val fetchedShows = mutableListOf<Long>()
        var sweeps = 0

        override suspend fun refreshCandidates() = RefreshCandidates(movies, shows)

        override suspend fun refreshMovie(tmdbId: Long) {
            fetchedMovies += tmdbId
            fail(tmdbId)?.let { throw TmdbException(it) }
        }

        override suspend fun refreshShow(tmdbId: Long) {
            fetchedShows += tmdbId
            fail(tmdbId)?.let { throw TmdbException(it) }
        }

        override suspend fun recomputeFollowedShows() {
            sweeps++
        }
    }

    private fun followed(tmdbId: Long, fetchedAt: String) =
        RefreshCandidate(tmdbId, fetchedAt, listed = false, held = true, ended = false)

    private fun refresher(writes: CatalogRefreshWrites) =
        CatalogRefresher(writes, CoroutineScope(Dispatchers.Unconfined)) { now }

    @Test
    fun `a tick fetches only what is due and ends with the sweep`() = runBlocking {
        val writes = FakeWrites(
            movies = listOf(RefreshCandidate(7, old, listed = true, held = false, ended = true)),
            shows = listOf(followed(1, old), followed(2, fresh)),
        )
        val report = refresher(writes).tick()
        assertEquals(listOf(7L), writes.fetchedMovies)
        assertEquals(listOf(1L), writes.fetchedShows)
        assertEquals(1, writes.sweeps)
        assertEquals(CatalogRefreshReport(refreshed = 2, failed = 0, completed = true), report)
    }

    @Test
    fun `three outages in a row abandon fetching but the sweep still runs`() = runBlocking {
        val writes = FakeWrites(
            shows = (1L..5L).map { followed(it, old) },
            fail = { TmdbException.Reason.UNAVAILABLE },
        )
        val report = refresher(writes).tick()
        assertEquals(listOf(1L, 2L, 3L), writes.fetchedShows)
        assertEquals(1, writes.sweeps)
        assertFalse(report.completed)
    }

    @Test
    fun `a missing key stops at the first title and still sweeps`() = runBlocking {
        val writes = FakeWrites(
            shows = listOf(followed(1, old), followed(2, old)),
            fail = { TmdbException.Reason.NO_KEY },
        )
        val report = refresher(writes).tick()
        assertEquals(listOf(1L), writes.fetchedShows)
        assertEquals(1, writes.sweeps)
        assertFalse(report.completed)
    }

    @Test
    fun `a title TMDB no longer has is not asked for again`() = runBlocking {
        val writes = FakeWrites(
            shows = listOf(followed(1, old), followed(2, old)),
            fail = { if (it == 1L) TmdbException.Reason.NOT_FOUND else null },
        )
        val refresher = refresher(writes)
        assertTrue(refresher.tick().completed)
        refresher.tick()
        assertEquals(listOf(1L, 2L, 2L), writes.fetchedShows)
    }

    @Test
    fun `a finished tick starts the cooldown and an outage does not`() = runBlocking {
        val ok = FakeWrites()
        val calm = refresher(ok)
        calm.tickIfDue()
        now = now.plus(Duration.ofMinutes(14))
        assertNull(calm.tickIfDue())
        now = now.plus(Duration.ofMinutes(1))
        calm.tickIfDue()
        assertEquals(2, ok.sweeps)

        val down = FakeWrites(shows = (1L..3L).map { followed(it, old) }, fail = { TmdbException.Reason.UNAVAILABLE })
        val retrying = refresher(down)
        retrying.tickIfDue()
        retrying.tickIfDue()
        assertEquals(2, down.sweeps)
    }
}

package ro.mbyte.sofalog.domain

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.Instant
import java.time.ZoneId

/** Which snapshots the catalog refresh picks: the server's two classes over one library. */
class CatalogRefreshTest {
    private val now = Instant.parse("2026-11-23T12:00:00Z")

    private fun title(
        fetchedAt: String,
        listed: Boolean = false,
        held: Boolean = false,
        ended: Boolean = false,
        tmdbId: Long = 1,
    ) = RefreshCandidate(tmdbId, fetchedAt, listed, held, ended)

    @Test
    fun `a followed show in production is on the weekly cadence`() {
        assertFalse(refreshDue(title("2026-11-17T12:00:00Z", held = true), now))
        assertTrue(refreshDue(title("2026-11-16T11:59:59Z", held = true), now))
    }

    @Test
    fun `the cutoff itself is not yet due`() {
        assertFalse(refreshDue(title("2026-11-16T12:00:00Z", held = true), now))
    }

    @Test
    fun `an ended show and a watched movie wait ninety days`() {
        assertFalse(refreshDue(title("2026-09-01T00:00:00Z", held = true, ended = true), now))
        assertTrue(refreshDue(title("2026-08-01T00:00:00Z", held = true, ended = true), now))
    }

    @Test
    fun `a list always means the faster cadence`() {
        assertTrue(refreshDue(title("2026-11-01T00:00:00Z", listed = true, held = true, ended = true), now))
        assertTrue(refreshDue(title("2026-11-01T00:00:00Z", listed = true), now))
    }

    @Test
    fun `a title nothing refers to is never refreshed`() {
        val orphan = title("2020-01-01T00:00:00Z")
        assertNull(refreshInterval(orphan))
        assertFalse(refreshDue(orphan, now))
    }

    @Test
    fun `an unreadable instant counts as due`() {
        assertTrue(refreshDue(title("", held = true), now))
    }

    @Test
    fun `an episode that aired since the snapshot makes a followed show due whatever its age`() {
        val yesterday = "2026-11-22T12:00:00Z"
        assertFalse(refreshDue(title(yesterday, held = true), now))
        assertTrue(refreshDue(title(yesterday, held = true).copy(airedSinceFetch = true), now))
        assertTrue(refreshDue(title(yesterday, held = true, ended = true).copy(airedSinceFetch = true), now))
        // Not followed: nothing is waiting on it.
        assertFalse(refreshDue(title(yesterday).copy(airedSinceFetch = true), now))
    }

    @Test
    fun `aired since the snapshot compares the snapshot's day in the user's zone`() {
        val utc = ZoneId.of("UTC")
        assertTrue(airedSinceFetch("2026-11-22T10:00:00Z", "2026-11-23", utc))
        // A snapshot from the air day is current, which is what ends the rule after one refetch.
        assertFalse(airedSinceFetch("2026-11-23T00:05:00Z", "2026-11-23", utc))
        assertFalse(airedSinceFetch("2026-11-24T00:00:00Z", "2026-11-23", utc))
        // 22:30 UTC on the 22nd is already the 23rd in Bucharest.
        assertFalse(airedSinceFetch("2026-11-22T22:30:00Z", "2026-11-23", ZoneId.of("Europe/Bucharest")))
        assertTrue(airedSinceFetch("", "2026-11-23", utc))
    }

    @Test
    fun `only Ended and Canceled end a show`() {
        assertTrue(showEnded("Ended"))
        assertTrue(showEnded("Canceled"))
        assertFalse(showEnded("Returning Series"))
        assertFalse(showEnded(null))
    }

    @Test
    fun `the due ids keep the order given`() {
        val due = dueForRefresh(
            listOf(
                title("2026-01-01T00:00:00Z", held = true, tmdbId = 30),
                title("2026-11-22T00:00:00Z", held = true, tmdbId = 20),
                title("2026-01-01T00:00:00Z", listed = true, tmdbId = 10),
            ),
            now,
        )
        assertEquals(listOf(30L, 10L), due)
    }
}

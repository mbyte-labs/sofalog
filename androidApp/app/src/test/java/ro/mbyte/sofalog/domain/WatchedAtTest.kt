package ro.mbyte.sofalog.domain

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.Instant
import java.time.LocalDate
import java.time.LocalTime
import java.time.ZoneId

class WatchedAtTest {
    private val bucharest = ZoneId.of("Europe/Bucharest")

    @Test
    fun releaseIsMidnightInTheUsersZone() {
        // Winter (UTC+2) and summer (UTC+3): the offset is the date's own.
        assertEquals(Instant.parse("2026-01-14T22:00:00Z"), releaseInstant("2026-01-15", bucharest))
        assertEquals(Instant.parse("2026-07-14T21:00:00Z"), releaseInstant("2026-07-15", bucharest))
    }

    @Test
    fun releaseNeedsAUsableDate() {
        assertNull(releaseInstant(null, bucharest))
        assertNull(releaseInstant("", bucharest))
        assertNull(releaseInstant("soon", bucharest))
    }

    @Test
    fun wallTimeIsReadInTheUsersZone() {
        assertEquals(
            Instant.parse("2026-11-18T19:30:00Z"),
            wallTimeInstant(LocalDate.of(2026, 11, 18), LocalTime.of(21, 30), bucharest),
        )
    }

    @Test
    fun storedInstantDropsFractions() {
        assertEquals("2026-11-18T19:30:05Z", storedInstant(Instant.parse("2026-11-18T19:30:05.987Z")))
    }

    @Test
    fun ageCountsCalendarDaysInTheUsersZone() {
        val today = LocalDate.of(2026, 11, 19)
        assertEquals("Today", watchAge("2026-11-19T08:00:00Z", today, bucharest))
        // 22:30 UTC on the 17th is already the 18th in Bucharest: yesterday, not two days ago.
        assertEquals("Yesterday", watchAge("2026-11-17T22:30:00Z", today, bucharest))
        assertEquals("5 days ago", watchAge("2026-11-14T19:05:00Z", today, bucharest))
        assertEquals("1 month ago", watchAge("2026-10-19T19:05:00Z", today, bucharest))
        assertEquals("8 months ago", watchAge("2026-03-02T18:30:00Z", today, bucharest))
        assertEquals("1 year ago", watchAge("2025-03-02T18:30:00Z", today, bucharest))
        assertEquals("16 years ago", watchAge("2010-07-15T21:00:00Z", today, bucharest))
    }

    @Test
    fun ageOfAFutureOrUnreadableWatch() {
        val today = LocalDate.of(2026, 11, 19)
        assertEquals("Today", watchAge("2026-11-25T08:00:00Z", today, bucharest))
        assertNull(watchAge("soon", today, bucharest))
    }

    @Test
    fun aReleaseDateWatchHasNoTime() {
        assertTrue(isDateOnlyWatch(storedInstant(releaseInstant("2010-07-16", bucharest)!!), bucharest))
        assertFalse(isDateOnlyWatch("2026-11-14T19:05:00Z", bucharest))
        // Midnight UTC is 02:00 in Bucharest: a real time.
        assertFalse(isDateOnlyWatch("2026-11-14T00:00:00Z", bucharest))
        assertFalse(isDateOnlyWatch("soon", bucharest))
    }

    private val now = "2026-11-19T10:00:00Z"
    private val today = "2026-11-19"

    private fun bulk(mode: BulkWatchMode, episode: String?, season: String?) =
        bulkWatchInstant(mode, episode, season, today, bucharest, now)

    @Test
    fun bulkNowAndFixedStampOneInstantOnEveryEpisode() {
        assertEquals(now, bulk(BulkWatchMode.Now, "2021-01-01", "2021-01-01"))
        // Sub-second precision is dropped, so the log's string order stays chronological.
        val fixed = BulkWatchMode.Fixed(Instant.parse("2024-03-02T18:30:15.250Z"))
        assertEquals("2024-03-02T18:30:15Z", bulk(fixed, "2021-01-01", null))
    }

    @Test
    fun bulkEpisodeAirIsMidnightOfEachEpisodesOwnDay() {
        assertEquals("2026-01-14T22:00:00Z", bulk(BulkWatchMode.EpisodeAir, "2026-01-15", "2025-09-01"))
        assertEquals("2026-07-14T21:00:00Z", bulk(BulkWatchMode.EpisodeAir, "2026-07-15", "2025-09-01"))
    }

    @Test
    fun bulkSeasonAirUsesThePremiereAndFallsBackWhenThereIsNone() {
        val fallback = Instant.parse("2025-05-05T12:00:00Z")
        // A usable premiere wins over the fallback, whatever the episode's own date.
        assertEquals("2025-08-31T21:00:00Z", bulk(BulkWatchMode.SeasonAir(fallback), "2025-10-10", "2025-09-01"))
        // Undated, or not premiered yet: the fallback stands in.
        assertEquals("2025-05-05T12:00:00Z", bulk(BulkWatchMode.SeasonAir(fallback), "2025-10-10", null))
        assertEquals("2025-05-05T12:00:00Z", bulk(BulkWatchMode.SeasonAir(fallback), "2025-10-10", "2027-01-01"))
        // Without one the instant is unknown, and the caller refuses the whole mark.
        assertNull(bulk(BulkWatchMode.SeasonAir(null), "2025-10-10", null))
    }

    @Test
    fun aPremiereIsUsableOnceItHasHappened() {
        assertTrue(usablePremiere("2025-09-01", today))
        assertTrue(usablePremiere(today, today))
        assertFalse(usablePremiere("2026-11-20", today))
        assertFalse(usablePremiere(null, today))
        assertFalse(usablePremiere("soon", today))
    }
}

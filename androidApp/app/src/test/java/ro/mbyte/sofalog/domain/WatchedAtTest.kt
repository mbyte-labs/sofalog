package ro.mbyte.sofalog.domain

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
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
}

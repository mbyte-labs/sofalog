package ro.mbyte.sofalog.domain

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.ZoneId

class UserZoneTest {
    @Test
    fun aStoredZoneIsUsedOnlyWhenTheDeviceKnowsIt() {
        assertEquals(ZoneId.of("Europe/Bucharest"), resolveZone("Europe/Bucharest"))
        assertNull(resolveZone(null))
        assertNull(resolveZone("  "))
        assertNull(resolveZone("Mars/Olympus_Mons"))
    }

    @Test
    fun aPickedZoneWinsAndClearingItFollowsTheDevice() {
        try {
            UserZone.set("Pacific/Kiritimati")
            assertEquals(ZoneId.of("Pacific/Kiritimati"), UserZone.current)
            UserZone.set("nonsense")
            assertEquals(ZoneId.systemDefault(), UserZone.current)
        } finally {
            UserZone.set(null)
        }
        assertEquals(ZoneId.systemDefault(), UserZone.current)
    }

    @Test
    fun theClockReadsTodayInTheUsersZone() {
        try {
            // Kiritimati is UTC+14 and Niue UTC-11: their days differ at every instant.
            UserZone.set("Pacific/Kiritimati")
            val ahead = Clock.System.read().today
            UserZone.set("Pacific/Niue")
            val behind = Clock.System.read().today
            assertTrue("$ahead should be after $behind", ahead > behind)
        } finally {
            UserZone.set(null)
        }
    }

    @Test
    fun zoneNamesAreOfferedSortedWithUtc() {
        val names = zoneNames()
        assertTrue("UTC" in names)
        assertTrue("Europe/Bucharest" in names)
        assertEquals(names.sorted(), names)
        assertEquals("America/New York", zoneLabel("America/New_York"))
    }

    @Test
    fun aStoredCountryMustBeTwoLetters() {
        assertEquals("DE", resolveRegion("de"))
        assertEquals(WATCH_REGION, resolveRegion(null))
        assertEquals(WATCH_REGION, resolveRegion("Germany"))
        assertEquals(WATCH_REGION, resolveRegion("1!"))
        assertEquals("Romania", countryName("RO"))
        assertTrue(countries().any { it.code == "RO" })
    }
}

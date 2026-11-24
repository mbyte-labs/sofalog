package ro.mbyte.sofalog.domain

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AirsTodayTest {
    private val today = "2026-08-18"

    private fun episode(airDate: String?) = EpisodeRef(id = 1, season = 1, number = 2, airDate = airDate)

    @Test
    fun onlyTodaysEpisodeAirsToday() {
        assertTrue(episode("2026-08-18").airsToday(today))
        assertFalse(episode("2026-08-17").airsToday(today))
        assertFalse(episode("2026-08-19").airsToday(today))
        assertFalse(episode(null).airsToday(today))
    }

    /** The reason the flag exists: today's episode is already aired, so "not aired" cannot badge it. */
    @Test
    fun todaysEpisodeIsAlreadyEligible() {
        assertTrue(episode("2026-08-18").isEligible(today))
    }
}

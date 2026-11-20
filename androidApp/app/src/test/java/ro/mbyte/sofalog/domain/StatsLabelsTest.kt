package ro.mbyte.sofalog.domain

import org.junit.Assert.assertEquals
import org.junit.Test
import java.time.LocalDate

class StatsLabelsTest {
    @Test
    fun roundTenthRoundsHalfAwayFromZero() {
        // 0.25 is exact in binary, so this is the true half: half-to-even would say 0.2.
        assertEquals(0.3, roundTenth(0.25), 0.0)
        assertEquals(-0.3, roundTenth(-0.25), 0.0)
        assertEquals(2.3, roundTenth(2.3333), 1e-12)
        assertEquals(0.0, roundTenth(0.04), 0.0)
    }

    @Test
    fun signedDeltaCarriesTheRoundedSign() {
        assertEquals("+0.4", signedDelta(0.42))
        assertEquals("−0.4", signedDelta(-0.42))
        assertEquals("±0.0", signedDelta(0.04))
        assertEquals("±0.0", signedDelta(-0.04))
        assertEquals(0, deltaSign(-0.04))
        assertEquals(-1, deltaSign(-0.18))
    }

    @Test
    fun compactNumberKeepsSmallCountsWhole() {
        assertEquals("9999", compactNumber(9_999))
        assertEquals("12.3k", compactNumber(12_345))
        assertEquals("1.2M", compactNumber(1_234_567))
    }

    @Test
    fun watchedAgoCountsCalendarDays() {
        val today = LocalDate.parse("2026-08-18")
        fun ago(days: Long) = watchedAgo(today.minusDays(days), today)
        assertEquals("today", ago(0))
        assertEquals("today", ago(-1))
        assertEquals("yesterday", ago(1))
        assertEquals("13 days ago", ago(13))
        assertEquals("2 weeks ago", ago(14))
        assertEquals("8 weeks ago", ago(59))
        assertEquals("2 months ago", ago(60))
        assertEquals("24 months ago", ago(729))
        assertEquals("2 years ago", ago(730))
    }

    @Test
    fun splitPercentNeverRoundsARealShareAway() {
        assertEquals("0%", splitPercent(0, 0))
        assertEquals("<1%", splitPercent(1, 1000))
        assertEquals(">99%", splitPercent(999, 1000))
        assertEquals("100%", splitPercent(10, 10))
        assertEquals("60%", splitPercent(3, 5))
    }

    @Test
    fun decadeLabels() {
        assertEquals("'90s", decadeLabel(1990, short = true))
        assertEquals("'00s", decadeLabel(2000, short = true))
        assertEquals("1990", decadeLabel(1990, short = false))
    }

    @Test
    fun placeholderInitialsSkipALeadingArticle() {
        assertEquals("M", titleInitials("The Matrix"))
        assertEquals("BB", titleInitials("Breaking Bad"))
        assertEquals("A", titleInitials("A"))
    }

    @Test
    fun genreColumnSplitsIntoItsNames() {
        assertEquals(listOf("Drama", "Sci-Fi & Fantasy"), splitGenres("Drama, Sci-Fi & Fantasy"))
        assertEquals(null, splitGenres(""))
        assertEquals(null, splitGenres(null))
    }

    @Test
    fun storedBodyFacts() {
        val body = """{"vote_average": 8.2, "vote_count": 1500, "episode_run_time": [45, 60], "name": "x"}"""
        assertEquals(StatsVoteFacts(8.2, 1500), parseVoteFacts(body))
        assertEquals(45, parseEpisodeRuntime(body))
        assertEquals(null, parseEpisodeRuntime("""{"episode_run_time": []}"""))
        assertEquals(null, parseVoteFacts("not json"))
    }
}

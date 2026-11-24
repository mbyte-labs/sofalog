package ro.mbyte.sofalog.domain

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.YearMonth
import java.time.ZoneId
import java.time.ZoneOffset

class HistoryMonthsTest {
    private val bucharest = ZoneId.of("Europe/Bucharest")

    private fun months(instants: List<String>, zone: ZoneId) =
        groupByMonth(instants, zone) { it }.map { it.month to it.items }

    @Test
    fun emptyFeedHasNoSections() {
        assertTrue(groupByMonth(emptyList<String>(), bucharest) { it }.isEmpty())
    }

    @Test
    fun theMonthIsTheZonesNotTheStoredStrings() {
        // 22:30 UTC on 31 August is already 01:30 on 1 September in Bucharest (UTC+3).
        val late = "2026-08-31T22:30:00Z"
        assertEquals(listOf(YearMonth.of(2026, 9) to listOf(late)), months(listOf(late), bucharest))
        assertEquals(listOf(YearMonth.of(2026, 8) to listOf(late)), months(listOf(late), ZoneOffset.UTC))
    }

    @Test
    fun sectionsFollowTheListsOrderBothWays() {
        val newest = listOf(
            "2026-09-10T18:00:00Z",
            "2026-09-02T18:00:00Z",
            "2026-08-20T18:00:00Z",
            "2026-06-01T18:00:00Z",
        )
        val september = YearMonth.of(2026, 9)
        val august = YearMonth.of(2026, 8)
        val june = YearMonth.of(2026, 6)
        // A month with nothing watched (July) is simply absent.
        assertEquals(
            listOf(september to newest.take(2), august to listOf(newest[2]), june to listOf(newest[3])),
            months(newest, bucharest),
        )
        val oldest = newest.reversed()
        assertEquals(
            listOf(june to listOf(oldest[0]), august to listOf(oldest[1]), september to oldest.drop(2)),
            months(oldest, bucharest),
        )
    }

    @Test
    fun sameInstantRowsStayTogether() {
        // A bulk mark stamps one instant on many rows.
        val bulk = List(3) { "2026-08-15T18:05:00Z" }
        assertEquals(listOf(YearMonth.of(2026, 8) to bulk), months(bulk, bucharest))
    }
}

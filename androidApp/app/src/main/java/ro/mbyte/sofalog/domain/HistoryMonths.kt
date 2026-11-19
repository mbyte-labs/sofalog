package ro.mbyte.sofalog.domain

import java.time.Instant
import java.time.YearMonth
import java.time.ZoneId

data class MonthSection<T>(val month: YearMonth, val items: List<T>)

/**
 * Folds a history feed into month sections, in one pass and in the list's own order: a new
 * section starts whenever the month changes. The month is the instant's month in [zone] — never a
 * prefix of the stored UTC string, which would file a late-evening watch under the wrong month.
 * An unreadable instant stays with the section before it rather than being dropped.
 */
fun <T> groupByMonth(items: List<T>, zone: ZoneId, instantOf: (T) -> String): List<MonthSection<T>> {
    val sections = mutableListOf<MonthSection<T>>()
    var month: YearMonth? = null
    var current = mutableListOf<T>()
    for (item in items) {
        val itemMonth = runCatching { YearMonth.from(Instant.parse(instantOf(item)).atZone(zone)) }.getOrNull()
            ?: month ?: YearMonth.of(1970, 1)
        if (itemMonth != month) {
            month?.let { sections += MonthSection(it, current) }
            month = itemMonth
            current = mutableListOf()
        }
        current += item
    }
    month?.let { sections += MonthSection(it, current) }
    return sections
}

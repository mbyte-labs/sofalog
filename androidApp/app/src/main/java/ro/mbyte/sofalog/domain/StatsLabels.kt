package ro.mbyte.sofalog.domain

import java.time.LocalDate
import java.time.temporal.ChronoUnit
import java.util.Locale
import kotlin.math.abs
import kotlin.math.roundToInt

/*
 * The Stats page's words and number formats — the web page's (`frontend/src/features/video/stats/
 * labels.js` and `common/format.js`), verbatim, so the two pages read the same. Pure: the screen
 * only places what these return.
 */

/** "1.2M", "12.3k", or the plain number below ten thousand. */
fun compactNumber(n: Int): String = when {
    n >= 1_000_000 -> "%.1fM".format(Locale.ENGLISH, n / 1_000_000.0)
    n >= 10_000 -> "%.1fk".format(Locale.ENGLISH, n / 1_000.0)
    else -> n.toString()
}

/** "+0.4", "−0.4" (a real minus sign) or "±0.0" — the sign is the ROUNDED value's. */
fun signedDelta(v: Double): String {
    val rounded = roundTenth(v)
    val text = "%.1f".format(Locale.ENGLISH, abs(rounded))
    return when {
        rounded > 0 -> "+$text"
        rounded < 0 -> "−$text"
        else -> "±$text"
    }
}

/** -1, 0 or 1: which way [signedDelta] reads — what colours it. */
fun deltaSign(v: Double): Int {
    // Not compareTo: a small negative rounds to -0.0, which the total order puts below 0.0.
    val rounded = roundTenth(v)
    return if (rounded > 0) 1 else if (rounded < 0) -1 else 0
}

fun plural(n: Int, one: String, many: String): String = "$n ${if (n == 1) one else many}"

/** One decimal, as every average on the page is shown. */
fun oneDecimal(v: Double): String = "%.1f".format(Locale.ENGLISH, v)

/** How long before [today] a day was: "today", "yesterday", then days, weeks, months, years. */
fun watchedAgo(day: LocalDate, today: LocalDate): String {
    val n = ChronoUnit.DAYS.between(day, today)
    return when {
        n <= 0 -> "today"
        n == 1L -> "yesterday"
        n < 14 -> "$n days ago"
        n < 60 -> "${n / 7} weeks ago"
        n < 730 -> "${n / 30} months ago"
        else -> "${n / 365} years ago"
    }
}

/** A whole percent that never rounds a real share away: "<1%" and ">99%" at the edges. */
fun splitPercent(part: Int, total: Int): String {
    if (total <= 0) return "0%"
    val p = (part * 100.0 / total).roundToInt()
    return when {
        p == 0 && part > 0 -> "<1%"
        p == 100 && part < total -> ">99%"
        else -> "$p%"
    }
}

fun percent(share: Double): String = "${(share * 100).roundToInt()}%"

/** "'90s" where the chart has room for it, "1990" otherwise. */
fun decadeLabel(decade: Int, short: Boolean): String =
    if (short) "'%02ds".format(Locale.ENGLISH, decade % 100) else decade.toString()

fun trendVerdict(verdict: String): String = when (verdict) {
    "harsher" -> "Rating more harshly lately"
    "kinder" -> "Rating more generously lately"
    else -> "Rating consistently"
}

fun crowdVerdict(verdict: String): String = when (verdict) {
    "harsher" -> "Tougher critic than the crowd"
    "kinder" -> "Easier to please than the crowd"
    else -> "In tune with the crowd"
}

/** One part of the day as the time-of-day chart names it. */
data class DayPart(val key: String, val label: String, val range: String, val persona: String, val span: String)

val DAY_PARTS = listOf(
    DayPart("morning", "Morning", "05-12", "Early bird", "between 05:00 and 12:00"),
    DayPart("afternoon", "Afternoon", "12-17", "Afternoon viewer", "between 12:00 and 17:00"),
    DayPart("evening", "Evening", "17-22", "Prime-time viewer", "between 17:00 and 22:00"),
    DayPart("night", "Night", "22-05", "Night owl", "after 22:00"),
)

val WEEKDAYS_SHORT = listOf("Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun")
val WEEKDAYS_LONG = listOf("Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday")

/** The plays of each part of the day, in [DAY_PARTS]' order. */
fun StatsTimeOfDay.counts(): List<Int> = listOf(morning, afternoon, evening, night)

/**
 * Whether the page has nothing to show for its period: no play, no rating, and — all time only —
 * no followed show either.
 */
fun Stats.isEmpty(): Boolean =
    headline.plays == 0 && ratings == null && (period != STATS_PERIOD_ALL || completion == null)

/** Up to two initials for a title without artwork, skipping a leading article. */
fun titleInitials(title: String): String {
    val words = title.trim().split(Regex("\\s+")).filter { it.isNotEmpty() }
    val meaningful = if (words.size > 1 && words.first().lowercase() in setOf("the", "a", "an")) words.drop(1) else words
    return meaningful.take(2).mapNotNull { it.firstOrNull()?.uppercaseChar() }.joinToString("")
}

/** A stable hue (0..359) per title, for the artwork placeholder. */
fun titleHue(title: String): Int {
    var hue = 0
    var i = 0
    while (i < title.length) {
        val cp = title.codePointAt(i)
        hue = ((hue * 31L + cp) % 360).toInt()
        i += Character.charCount(cp)
    }
    return hue
}

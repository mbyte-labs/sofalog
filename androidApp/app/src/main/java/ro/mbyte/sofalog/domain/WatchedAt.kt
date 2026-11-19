package ro.mbyte.sofalog.domain

import java.time.Instant
import java.time.LocalDate
import java.time.LocalTime
import java.time.Period
import java.time.ZoneId
import java.time.temporal.ChronoUnit

/*
 * When a dated watch happened (the server's dated mode of marking, D-009). Whatever the user
 * picks becomes one UTC instant at second precision, so the log's plain string order stays
 * chronological. A date becomes an instant in the user's zone (D-038) — the device's, standalone.
 */

/** The instant as the log stores it: RFC3339 UTC, whole seconds. */
fun storedInstant(instant: Instant): String = instant.truncatedTo(ChronoUnit.SECONDS).toString()

/**
 * "On its release date": midnight in the user's zone on the air date — the server's
 * `episode_air` rule. Null when there is no usable date.
 */
fun releaseInstant(airDate: String?, zone: ZoneId): Instant? =
    airDate?.let { runCatching { LocalDate.parse(it) }.getOrNull() }?.atStartOfDay(zone)?.toInstant()

/** A date and a time the user picked, read as wall-clock time in their zone. */
fun wallTimeInstant(date: LocalDate, time: LocalTime, zone: ZoneId): Instant =
    date.atTime(time).atZone(zone).toInstant()

/** The watch's calendar day in [zone]; null when the stored instant is unreadable. */
fun watchDay(instant: String, zone: ZoneId): LocalDate? =
    runCatching { Instant.parse(instant).atZone(zone).toLocalDate() }.getOrNull()

/**
 * True when the watch sits at exactly midnight in [zone] — what "on its release date" stamps. Such
 * a watch has a day but no time worth showing.
 */
fun isDateOnlyWatch(instant: String, zone: ZoneId): Boolean =
    runCatching { Instant.parse(instant).atZone(zone).toLocalTime() == LocalTime.MIDNIGHT }.getOrDefault(false)

/**
 * How long ago a watch happened, counted in calendar days, months and years in [zone] — never in
 * elapsed hours, so a watch late yesterday evening reads "Yesterday" this morning. Null when the
 * stored instant is unreadable.
 */
fun watchAge(instant: String, today: LocalDate, zone: ZoneId): String? {
    val day = watchDay(instant, zone) ?: return null
    if (!day.isBefore(today)) return "Today"
    val age = Period.between(day, today)
    return when {
        age.years > 0 -> plural(age.years, "year")
        age.months > 0 -> plural(age.months, "month")
        age.days == 1 -> "Yesterday"
        else -> plural(age.days, "day")
    }
}

private fun plural(count: Int, unit: String) = "$count $unit${if (count == 1) "" else "s"} ago"

/** When a bulk mark says its episodes were watched — the server's `watched_at_mode`. */
sealed interface BulkWatchMode {
    /** Every episode at the moment of the write. */
    data object Now : BulkWatchMode

    /** Every episode at one instant the user picked. */
    data class Fixed(val at: Instant) : BulkWatchMode

    /** Each episode at midnight on its own air date. */
    data object EpisodeAir : BulkWatchMode

    /**
     * Each episode at midnight on its season's premiere; [fallback] stands in for a season with no
     * usable premiere — undated, or itself still in the future.
     */
    data class SeasonAir(val fallback: Instant?) : BulkWatchMode
}

/** Whether [BulkWatchMode.SeasonAir] can use this season's premiere: dated, and not after [today]. */
fun usablePremiere(seasonAirDate: String?, today: String): Boolean =
    seasonAirDate != null && seasonAirDate <= today && releaseInstant(seasonAirDate, ZoneId.of("UTC")) != null

/**
 * The stored instant one episode of a bulk mark gets. A date becomes midnight in [zone] (D-038).
 * Null only when [BulkWatchMode.SeasonAir] meets a season without a usable premiere and was given
 * no fallback — the caller refuses the whole mark, so nothing half-lands.
 */
fun bulkWatchInstant(
    mode: BulkWatchMode,
    episodeAirDate: String?,
    seasonAirDate: String?,
    today: String,
    zone: ZoneId,
    now: String,
): String? = when (mode) {
    BulkWatchMode.Now -> now
    is BulkWatchMode.Fixed -> storedInstant(mode.at)
    BulkWatchMode.EpisodeAir -> releaseInstant(episodeAirDate, zone)?.let(::storedInstant) ?: now
    is BulkWatchMode.SeasonAir ->
        if (usablePremiere(seasonAirDate, today)) {
            releaseInstant(seasonAirDate, zone)?.let(::storedInstant)
        } else {
            mode.fallback?.let(::storedInstant)
        }
}

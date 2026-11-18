package ro.mbyte.sofalog.domain

import java.time.Instant
import java.time.LocalDate
import java.time.LocalTime
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

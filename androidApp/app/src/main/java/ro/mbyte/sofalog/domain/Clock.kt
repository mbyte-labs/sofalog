package ro.mbyte.sofalog.domain

import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.temporal.ChronoUnit

/**
 * One clock reading: [now] is the RFC3339 UTC instant that gets stored, [today] is the user's day
 * (D-038) — in standalone mode the device's zone. Take one reading per operation so every
 * statement in it agrees on where "aired" ends.
 */
data class ClockReading(val now: String, val today: String)

fun interface Clock {
    fun read(): ClockReading

    companion object {
        val System = Clock {
            val instant = Instant.now().truncatedTo(ChronoUnit.SECONDS)
            ClockReading(
                now = instant.toString(),
                today = LocalDate.ofInstant(instant, ZoneId.systemDefault()).toString(),
            )
        }
    }
}

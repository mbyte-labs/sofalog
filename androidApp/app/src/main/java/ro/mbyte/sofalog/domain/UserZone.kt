package ro.mbyte.sofalog.domain

import java.time.ZoneId
import java.util.Locale

/**
 * The one definition of the user's time zone (D-038): where their day ends and the zone every
 * instant is shown in. A zone the user picked in Settings wins; with none picked it follows the
 * device, live. Everything is stored in UTC, so changing it moves nothing — only how it reads.
 */
object UserZone {
    @Volatile private var chosen: ZoneId? = null

    val current: ZoneId get() = chosen ?: ZoneId.systemDefault()

    /** [name] null, or a name this device does not know, means "follow the device". */
    fun set(name: String?) {
        chosen = resolveZone(name)
    }
}

/** The zone a stored setting names, or null when there is none or it is not a known zone. */
fun resolveZone(stored: String?): ZoneId? =
    stored?.trim()?.takeIf { it.isNotEmpty() }?.let { runCatching { ZoneId.of(it) }.getOrNull() }

/** The zones offered: the region/city names plus UTC, sorted. */
fun zoneNames(): List<String> =
    (ZoneId.getAvailableZoneIds().filter { '/' in it && !it.startsWith("Etc/") && !it.startsWith("SystemV/") } + "UTC")
        .distinct().sorted()

/** A zone name as shown: "America/New York". */
fun zoneLabel(name: String): String = name.replace('_', ' ')

/** One country the where-to-watch listing can be set to. */
data class Country(val code: String, val name: String)

/** Every ISO-3166-1 alpha-2 country, by English name. */
fun countries(): List<Country> = Locale.getISOCountries()
    .map { Country(it, Locale.Builder().setRegion(it).build().getDisplayCountry(Locale.ENGLISH)) }
    .sortedBy { it.name }

fun countryName(code: String): String =
    runCatching { Locale.Builder().setRegion(code).build().getDisplayCountry(Locale.ENGLISH) }.getOrNull()
        ?.takeIf { it.isNotBlank() } ?: code

/** A stored country is used only when it is two letters; anything else falls back to the default. */
fun resolveRegion(stored: String?): String =
    stored?.trim()?.uppercase()?.takeIf { it.length == 2 && it.all(Char::isLetter) } ?: WATCH_REGION

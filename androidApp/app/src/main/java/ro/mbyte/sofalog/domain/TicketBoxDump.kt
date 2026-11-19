package ro.mbyte.sofalog.domain

import kotlinx.serialization.ExperimentalSerializationApi
import kotlinx.serialization.Serializable
import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.decodeFromStream
import kotlinx.serialization.json.intOrNull
import java.io.IOException
import java.io.InputStream

/*
 * A Ticket Box export, schema version 5 (D-023) — the server's `TicketBoxDump`, read on the
 * device. Every entry field is nullable: an absent or null field is not a parse failure but a
 * skip, decided by validateTicketBoxDump. `imdbId` is declared only so the strict decode accepts
 * it; nothing reads it.
 */

const val TICKET_BOX_SCHEMA_VERSION = 5
const val RESTORE_MAX_BYTES = 32 * 1024 * 1024

interface TicketBoxAddress {
    val type: String?
    val tmdbId: Long?
    val showTmdbId: Long?
    val season: Int?
    val episode: Int?
}

@Serializable
data class TicketBoxDump(
    val schemaVersion: Int,
    val exportedAt: String? = null,
    val history: List<TicketBoxHistoryEntry>? = null,
    val ratings: List<TicketBoxRatingEntry>? = null,
    val watchlist: List<TicketBoxWatchlistEntry>? = null,
    val favorites: List<TicketBoxFavoriteEntry>? = null,
)

@Serializable
data class TicketBoxHistoryEntry(
    override val type: String? = null,
    override val tmdbId: Long? = null,
    val imdbId: String? = null,
    override val showTmdbId: Long? = null,
    override val season: Int? = null,
    override val episode: Int? = null,
    val addedAt: String? = null,
    val historyType: String? = null,
) : TicketBoxAddress

@Serializable
data class TicketBoxRatingEntry(
    override val type: String? = null,
    override val tmdbId: Long? = null,
    val imdbId: String? = null,
    override val showTmdbId: Long? = null,
    override val season: Int? = null,
    override val episode: Int? = null,
    val rating: Int? = null,
    val ratedAt: String? = null,
) : TicketBoxAddress

@Serializable
data class TicketBoxWatchlistEntry(
    override val type: String? = null,
    override val tmdbId: Long? = null,
    val imdbId: String? = null,
    override val showTmdbId: Long? = null,
    override val season: Int? = null,
    override val episode: Int? = null,
    val listedAt: String? = null,
) : TicketBoxAddress

@Serializable
data class TicketBoxFavoriteEntry(
    override val type: String? = null,
    override val tmdbId: Long? = null,
    val imdbId: String? = null,
    override val showTmdbId: Long? = null,
    override val season: Int? = null,
    override val episode: Int? = null,
    val favoritedAt: String? = null,
) : TicketBoxAddress

sealed interface DumpParse {
    data class Ok(val dump: TicketBoxDump) : DumpParse

    /** [code] is `not_json` | `version` | `shape`; [message] is what the page says. */
    data class Rejected(val code: String, val message: String) : DumpParse
}

private val probeJson = Json { ignoreUnknownKeys = true }
private val strictJson = Json { ignoreUnknownKeys = false }

@Serializable
private class VersionProbe(val schemaVersion: JsonElement? = null)

private val UNKNOWN_KEY = Regex("unknown key '([^']+)'")

/**
 * Reads the file in two passes over [open]'s stream. The version probe comes FIRST and is
 * lenient, because the useful answer to an older file is "export again" — a strict decode would
 * fail on its first unknown field and say nothing about why. The second pass is strict: the
 * version is the compatibility mechanism, so a field it does not define is refused by name.
 */
@OptIn(ExperimentalSerializationApi::class)
fun parseTicketBoxDump(open: () -> InputStream): DumpParse {
    val probe = try {
        open().use { probeJson.decodeFromStream<VersionProbe>(it) }
    } catch (e: SerializationException) {
        return DumpParse.Rejected("not_json", "That isn't a JSON file.")
    } catch (e: IOException) {
        return DumpParse.Rejected("not_json", "That file couldn't be read.")
    }
    val version = (probe.schemaVersion as? JsonPrimitive)?.takeIf { !it.isString }?.intOrNull
    if (version != TICKET_BOX_SCHEMA_VERSION) {
        return DumpParse.Rejected(
            "version",
            "This file is Ticket Box schema version ${version ?: "unknown"}; sofalog imports version " +
                "$TICKET_BOX_SCHEMA_VERSION only. Update Ticket Box and export again.",
        )
    }
    return try {
        DumpParse.Ok(open().use { strictJson.decodeFromStream<TicketBoxDump>(it) })
    } catch (e: SerializationException) {
        val unknown = UNKNOWN_KEY.find(e.message.orEmpty())?.groupValues?.get(1)
        val detail = unknown?.let { "unknown field \"$it\"" }
            ?: e.message.orEmpty().lineSequence().firstOrNull().orEmpty().take(200)
        DumpParse.Rejected("shape", "This file isn't a valid Ticket Box version $TICKET_BOX_SCHEMA_VERSION export: $detail")
    } catch (e: IOException) {
        DumpParse.Rejected("not_json", "That file couldn't be read.")
    }
}

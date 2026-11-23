package ro.mbyte.sofalog.domain

import kotlinx.serialization.ExperimentalSerializationApi
import kotlinx.serialization.Serializable
import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.decodeFromStream
import kotlinx.serialization.json.encodeToStream
import kotlinx.serialization.json.intOrNull
import java.io.IOException
import java.io.InputStream
import java.io.OutputStream
import java.time.Instant

/*
 * The app's own backup file (D-040): the user's watch history, ratings, watchlist, favorites,
 * dropped shows and owned copies, grouped per title. Every target is TMDB-addressed — a local id
 * never leaves the database — and the catalog is not in it: a restore fetches each title again.
 * The settings and the TMDB link are deliberately absent.
 *
 * `title` is there for whoever opens the file; a restore never trusts it. Entry fields are
 * nullable for the reason Ticket Box's are: an absent one is a skip, not a parse failure.
 */

const val BACKUP_FORMAT = "sofalog-backup"
const val BACKUP_SCHEMA_VERSION = 1

@Serializable
data class SofalogBackup(
    val format: String,
    val schemaVersion: Int,
    val exportedAt: String? = null,
    val appVersion: String? = null,
    val movies: List<BackupMovie> = emptyList(),
    val shows: List<BackupShow> = emptyList(),
)

@Serializable
data class BackupRating(val value: Int? = null, val at: String? = null)

@Serializable
data class BackupCopy(val kind: String? = null, val addedAt: String? = null)

@Serializable
data class BackupSeasonCopy(val season: Int? = null, val kind: String? = null, val addedAt: String? = null)

@Serializable
data class BackupMovie(
    val tmdbId: Long? = null,
    val title: String? = null,
    val watches: List<String> = emptyList(),
    val rating: BackupRating? = null,
    val watchlistAt: String? = null,
    val favoriteAt: String? = null,
    val copies: List<BackupCopy> = emptyList(),
)

/** An episode by its own TMDB id and by its position; a restore tries the id first. */
@Serializable
data class BackupEpisode(
    val tmdbId: Long? = null,
    val season: Int? = null,
    val episode: Int? = null,
    val watches: List<String> = emptyList(),
    val rating: BackupRating? = null,
)

@Serializable
data class BackupShow(
    val tmdbId: Long? = null,
    val title: String? = null,
    val rating: BackupRating? = null,
    val watchlistAt: String? = null,
    val favoriteAt: String? = null,
    /** When the show was dropped; absent for a show that is not. */
    val droppedAt: String? = null,
    val episodes: List<BackupEpisode> = emptyList(),
    val seasonCopies: List<BackupSeasonCopy> = emptyList(),
)

/** What a backup holds, counted — for the page's confirmation and the restore's preview. */
data class BackupSummary(
    val movies: Int = 0,
    val shows: Int = 0,
    val watches: Int = 0,
    val ratings: Int = 0,
    val watchlist: Int = 0,
    val favorites: Int = 0,
    val dropped: Int = 0,
    val copies: Int = 0,
)

fun SofalogBackup.summary(): BackupSummary {
    val episodes = shows.flatMap { it.episodes }
    return BackupSummary(
        movies = movies.size,
        shows = shows.size,
        watches = movies.sumOf { it.watches.size } + episodes.sumOf { it.watches.size },
        ratings = movies.count { it.rating != null } + shows.count { it.rating != null } + episodes.count { it.rating != null },
        watchlist = movies.count { it.watchlistAt != null } + shows.count { it.watchlistAt != null },
        favorites = movies.count { it.favoriteAt != null } + shows.count { it.favoriteAt != null },
        dropped = shows.count { it.droppedAt != null },
        copies = movies.sumOf { it.copies.size } + shows.sumOf { it.seasonCopies.size },
    )
}

fun backupSummaryLine(s: BackupSummary): String {
    fun n(count: Int, one: String, many: String = one + "s") = "$count ${if (count == 1) one else many}"
    return "${n(s.movies, "movie")} and ${n(s.shows, "show")}: ${n(s.watches, "watch", "watches")}, " +
        "${n(s.ratings, "rating")}, ${s.watchlist} on the watchlist, ${n(s.favorites, "favorite")}, " +
        "${s.dropped} dropped, ${n(s.copies, "copy", "copies")}."
}

/** `sofalog-backup-2026-11-23.json`, [today] being the user's day. */
fun backupFileName(today: String): String = "sofalog-backup-$today.json"

// ---- writing

/*
 * The library's user state as the database hands it over: flat rows, every one already joined to
 * its title's TMDB id. Room fills these by column name, so the queries alias to these fields.
 */
data class BackupMovieWatchRow(val tmdbId: Long, val title: String, val watchedAt: String)

data class BackupEpisodeWatchRow(
    val showTmdbId: Long,
    val showTitle: String,
    val season: Int,
    val episode: Int,
    val episodeTmdbId: Long?,
    val watchedAt: String,
)

/** [type] is `movie` or `show`. */
data class BackupRatingRow(val type: String, val tmdbId: Long, val title: String, val rating: Int, val at: String)

data class BackupEpisodeRatingRow(
    val showTmdbId: Long,
    val showTitle: String,
    val season: Int,
    val episode: Int,
    val episodeTmdbId: Long?,
    val rating: Int,
    val at: String,
)

/** A watchlist entry, a favorite or a dropped show. [type] is `movie` or `show`. */
data class BackupListRow(val type: String, val tmdbId: Long, val title: String, val at: String)

data class BackupMovieCopyRow(val tmdbId: Long, val title: String, val kind: String, val at: String)

data class BackupSeasonCopyRow(val showTmdbId: Long, val showTitle: String, val season: Int, val kind: String, val at: String)

data class BackupRows(
    val movieWatches: List<BackupMovieWatchRow> = emptyList(),
    val episodeWatches: List<BackupEpisodeWatchRow> = emptyList(),
    val ratings: List<BackupRatingRow> = emptyList(),
    val episodeRatings: List<BackupEpisodeRatingRow> = emptyList(),
    val watchlist: List<BackupListRow> = emptyList(),
    val favorites: List<BackupListRow> = emptyList(),
    val dropped: List<BackupListRow> = emptyList(),
    val movieCopies: List<BackupMovieCopyRow> = emptyList(),
    val seasonCopies: List<BackupSeasonCopyRow> = emptyList(),
)

private const val MOVIE = "movie"

/**
 * Folds the rows into the file. Only a title with some state of the user's is written, and only an
 * episode with a watch or a rating. The order is fixed — titles by TMDB id, episodes by position,
 * watches oldest first — so the same library always writes the same file.
 */
fun buildBackup(rows: BackupRows, exportedAt: String, appVersion: String?): SofalogBackup {
    class EpisodeDraft(var tmdbId: Long?, val watches: MutableList<String> = mutableListOf(), var rating: BackupRating? = null)
    class Draft(
        var title: String,
        val watches: MutableList<String> = mutableListOf(),
        var rating: BackupRating? = null,
        var watchlistAt: String? = null,
        var favoriteAt: String? = null,
        var droppedAt: String? = null,
        val copies: MutableList<BackupCopy> = mutableListOf(),
        val episodes: MutableMap<Pair<Int, Int>, EpisodeDraft> = mutableMapOf(),
        val seasonCopies: MutableList<BackupSeasonCopy> = mutableListOf(),
    )

    val movies = sortedMapOf<Long, Draft>()
    val shows = sortedMapOf<Long, Draft>()
    fun movie(id: Long, title: String) = movies.getOrPut(id) { Draft(title) }
    fun show(id: Long, title: String) = shows.getOrPut(id) { Draft(title) }
    fun title(type: String, id: Long, title: String) = if (type == MOVIE) movie(id, title) else show(id, title)
    fun Draft.episode(season: Int, episode: Int, tmdbId: Long?) =
        episodes.getOrPut(season to episode) { EpisodeDraft(tmdbId) }.also { if (it.tmdbId == null) it.tmdbId = tmdbId }

    rows.movieWatches.forEach { movie(it.tmdbId, it.title).watches += it.watchedAt }
    rows.episodeWatches.forEach { show(it.showTmdbId, it.showTitle).episode(it.season, it.episode, it.episodeTmdbId).watches += it.watchedAt }
    rows.ratings.forEach { title(it.type, it.tmdbId, it.title).rating = BackupRating(it.rating, it.at) }
    rows.episodeRatings.forEach {
        show(it.showTmdbId, it.showTitle).episode(it.season, it.episode, it.episodeTmdbId).rating = BackupRating(it.rating, it.at)
    }
    rows.watchlist.forEach { title(it.type, it.tmdbId, it.title).watchlistAt = it.at }
    rows.favorites.forEach { title(it.type, it.tmdbId, it.title).favoriteAt = it.at }
    rows.dropped.forEach { show(it.tmdbId, it.title).droppedAt = it.at }
    rows.movieCopies.forEach { movie(it.tmdbId, it.title).copies += BackupCopy(it.kind, it.at) }
    rows.seasonCopies.forEach { show(it.showTmdbId, it.showTitle).seasonCopies += BackupSeasonCopy(it.season, it.kind, it.at) }

    return SofalogBackup(
        format = BACKUP_FORMAT,
        schemaVersion = BACKUP_SCHEMA_VERSION,
        exportedAt = exportedAt,
        appVersion = appVersion,
        movies = movies.map { (id, d) ->
            BackupMovie(id, d.title, d.watches.sorted(), d.rating, d.watchlistAt, d.favoriteAt, d.copies.sortedBy { it.kind })
        },
        shows = shows.map { (id, d) ->
            BackupShow(
                tmdbId = id,
                title = d.title,
                rating = d.rating,
                watchlistAt = d.watchlistAt,
                favoriteAt = d.favoriteAt,
                droppedAt = d.droppedAt,
                episodes = d.episodes.entries
                    .sortedWith(compareBy({ it.key.first }, { it.key.second }))
                    .map { (at, e) -> BackupEpisode(e.tmdbId, at.first, at.second, e.watches.sorted(), e.rating) },
                seasonCopies = d.seasonCopies.sortedWith(compareBy({ it.season }, { it.kind })),
            )
        },
    )
}

// Absent beats null and empty: the file states what there is and nothing else.
@OptIn(ExperimentalSerializationApi::class)
private val writeJson = Json {
    encodeDefaults = false
    explicitNulls = false
}

@OptIn(ExperimentalSerializationApi::class)
fun writeSofalogBackup(backup: SofalogBackup, out: OutputStream) = writeJson.encodeToStream(backup, out)

// ---- reading

sealed interface BackupParse {
    data class Ok(val backup: SofalogBackup) : BackupParse

    /** [code] is `not_json` | `format` | `version` | `shape`; [message] is what the page says. */
    data class Rejected(val code: String, val message: String) : BackupParse
}

private val probeJson = Json { ignoreUnknownKeys = true }
private val strictJson = Json { ignoreUnknownKeys = false }

@Serializable
private class BackupProbe(val format: JsonElement? = null, val schemaVersion: JsonElement? = null)

private val UNKNOWN_KEY = Regex("unknown key '([^']+)'")

/**
 * Reads a backup in two passes, as the Ticket Box reader does and for its reason: the lenient
 * probe answers "what is this file" in a sentence — another app's export, or a newer sofalog's —
 * before the strict decode refuses a field this version does not define, by name.
 */
@OptIn(ExperimentalSerializationApi::class)
fun parseSofalogBackup(open: () -> InputStream): BackupParse {
    val probe = try {
        open().use { probeJson.decodeFromStream<BackupProbe>(it) }
    } catch (e: SerializationException) {
        return BackupParse.Rejected("not_json", "That isn't a JSON file.")
    } catch (e: IOException) {
        return BackupParse.Rejected("not_json", "That file couldn't be read.")
    }
    if ((probe.format as? JsonPrimitive)?.takeIf { it.isString }?.contentOrNull != BACKUP_FORMAT) {
        return BackupParse.Rejected(
            "format",
            "That isn't a sofalog backup. A Ticket Box export is restored from its own page.",
        )
    }
    val version = (probe.schemaVersion as? JsonPrimitive)?.takeIf { !it.isString }?.intOrNull
    if (version != BACKUP_SCHEMA_VERSION) {
        return BackupParse.Rejected(
            "version",
            if (version != null && version > BACKUP_SCHEMA_VERSION) {
                "This backup was written by a newer sofalog (format version $version). Update the app, then restore it."
            } else {
                "This backup's format version (${version ?: "unknown"}) isn't one sofalog can read."
            },
        )
    }
    return try {
        BackupParse.Ok(open().use { strictJson.decodeFromStream<SofalogBackup>(it) })
    } catch (e: SerializationException) {
        val unknown = UNKNOWN_KEY.find(e.message.orEmpty())?.groupValues?.get(1)
        val detail = unknown?.let { "unknown field \"$it\"" }
            ?: e.message.orEmpty().lineSequence().firstOrNull().orEmpty().take(200)
        BackupParse.Rejected("shape", "This file isn't a valid sofalog backup: $detail")
    } catch (e: IOException) {
        BackupParse.Rejected("not_json", "That file couldn't be read.")
    }
}

/**
 * The file as a restore plan, every value normalised. Each title becomes an EXACT group: the file
 * states the title's whole state, so the run replaces what the device holds for it — and leaves
 * every title the file does not name alone. A title whose every entry was refused yields no group,
 * so a damaged entry cannot wipe what it failed to describe. [copyKinds] are the kinds the app
 * knows.
 */
fun validateSofalogBackup(backup: SofalogBackup, now: Instant, copyKinds: Set<String>): Pair<RestorePlan, List<RestoreSkip>> {
    val skips = mutableListOf<RestoreSkip>()
    // An instant, or the skip reason it earns.
    fun instant(raw: String?): Pair<String?, String?> = when (val r = normaliseInstant(raw, now)) {
        is InstantResult.Ok -> r.at to null
        InstantResult.Future -> null to SKIP_FUTURE_TIMESTAMP
        InstantResult.Unparsable -> null to SKIP_INVALID_TIMESTAMP
    }

    val movies = sortedMapOf<Long, MovieGroup>()
    for (m in backup.movies) {
        val id = m.tmdbId?.takeIf { it >= 1 }
        fun skip(kind: String, reason: String) {
            skips += RestoreSkip(kind, MOVIE, m.tmdbId, null, null, null, reason, m.title)
        }
        fun at(kind: String, raw: String?): String? {
            val (at, reason) = instant(raw)
            if (at == null) skip(kind, reason!!)
            return at
        }
        if (id == null) { skip(RESTORE_TITLE, SKIP_MISSING_TMDB_ID); continue }
        val group = MovieGroup(
            tmdbId = id,
            watches = m.watches.mapNotNull { at(RESTORE_HISTORY, it) },
            rating = m.rating?.let { r ->
                val value = r.value
                if (value == null || value !in 1..10) { skip(RESTORE_RATING, SKIP_RATING_OUT_OF_RANGE); null }
                else at(RESTORE_RATING, r.at)?.let { PlannedRating(value, it) }
            },
            watchlistAt = m.watchlistAt?.let { at(RESTORE_WATCHLIST, it) },
            favoriteAt = m.favoriteAt?.let { at(RESTORE_FAVORITE, it) },
            copies = m.copies.mapNotNull { c ->
                if (c.kind == null || c.kind !in copyKinds) { skip(RESTORE_COPY, SKIP_UNKNOWN_COPY_KIND); null }
                else at(RESTORE_COPY, c.addedAt)?.let { PlannedCopy(c.kind, it) }
            }.distinctBy { it.kind },
            exact = true,
        )
        if (group.entries > 0) movies[id] = group
    }

    val shows = sortedMapOf<Long, ShowGroup>()
    for (s in backup.shows) {
        val id = s.tmdbId?.takeIf { it >= 1 }
        fun skip(kind: String, type: String, reason: String, season: Int? = null, episode: Int? = null) {
            val showLevel = type == "show"
            skips += RestoreSkip(kind, type, id.takeIf { showLevel }, id.takeIf { !showLevel }, season, episode, reason, s.title)
        }
        if (id == null) {
            skips += RestoreSkip(RESTORE_TITLE, "show", s.tmdbId, null, null, null, SKIP_MISSING_TMDB_ID, s.title)
            continue
        }
        fun at(kind: String, type: String, raw: String?, season: Int? = null, episode: Int? = null): String? {
            val (at, reason) = instant(raw)
            if (at == null) skip(kind, type, reason!!, season, episode)
            return at
        }
        val watches = mutableListOf<PlannedEpisodeWatch>()
        val episodeRatings = mutableMapOf<Pair<Int, Int>, PlannedEpisodeRating>()
        for (e in s.episodes) {
            val season = e.season
            val episode = e.episode
            // Season 0 is Specials and a pilot may be episode 0, so only negatives are refused.
            if (season == null || episode == null || season < 0 || episode < 0) {
                skip(if (e.watches.isEmpty()) RESTORE_RATING else RESTORE_HISTORY, "episode", SKIP_MISSING_POSITION, season, episode)
                continue
            }
            e.watches.forEach { raw ->
                at(RESTORE_HISTORY, "episode", raw, season, episode)?.let { watches += PlannedEpisodeWatch(season, episode, e.tmdbId, it) }
            }
            e.rating?.let { r ->
                val value = r.value
                if (value == null || value !in 1..10) {
                    skip(RESTORE_RATING, "episode", SKIP_RATING_OUT_OF_RANGE, season, episode)
                } else {
                    at(RESTORE_RATING, "episode", r.at, season, episode)?.let {
                        episodeRatings[season to episode] = PlannedEpisodeRating(season, episode, e.tmdbId, PlannedRating(value, it))
                    }
                }
            }
        }
        val group = ShowGroup(
            tmdbId = id,
            watches = watches,
            showRating = s.rating?.let { r ->
                val value = r.value
                if (value == null || value !in 1..10) { skip(RESTORE_RATING, "show", SKIP_RATING_OUT_OF_RANGE); null }
                else at(RESTORE_RATING, "show", r.at)?.let { PlannedRating(value, it) }
            },
            episodeRatings = episodeRatings.values.sortedWith(compareBy({ it.season }, { it.episode })),
            watchlistAt = s.watchlistAt?.let { at(RESTORE_WATCHLIST, "show", it) },
            favoriteAt = s.favoriteAt?.let { at(RESTORE_FAVORITE, "show", it) },
            seasonCopies = s.seasonCopies.mapNotNull { c ->
                when {
                    c.season == null || c.season < 0 -> { skip(RESTORE_COPY, "season", SKIP_MISSING_POSITION, c.season); null }
                    c.kind == null || c.kind !in copyKinds -> { skip(RESTORE_COPY, "season", SKIP_UNKNOWN_COPY_KIND, c.season); null }
                    else -> at(RESTORE_COPY, "season", c.addedAt, c.season)?.let { PlannedSeasonCopy(c.season, c.kind, it) }
                }
            }.distinctBy { it.season to it.kind },
            droppedAt = s.droppedAt?.let { at(RESTORE_DROPPED, "show", it) },
            exact = true,
        )
        if (group.entries > 0) shows[id] = group
    }

    return RestorePlan(movies.values.toList(), shows.values.toList()) to skips
}

package ro.mbyte.sofalog.data.db

import androidx.room.ColumnInfo
import androidx.room.Entity
import androidx.room.Index
import androidx.room.PrimaryKey

/*
 * The device's own database (SPEC D-040), shaped like the server's video schema: catalog tables
 * with bare names, the user's private state prefixed `user_`. There is no user id — one device,
 * one user. Ids are local to this database and never leave it; anything that will cross the wire
 * in linked mode is addressed by TMDB id.
 *
 * Dates are YYYY-MM-DD strings and instants RFC3339 UTC strings, as on the server, so plain
 * string order is chronological order.
 */

@Entity(tableName = "movies", indices = [Index("tmdb_id", unique = true)])
data class MovieEntity(
    @PrimaryKey(autoGenerate = true) val id: Long = 0,
    @ColumnInfo(name = "tmdb_id") val tmdbId: Long,
    val title: String,
    @ColumnInfo(name = "release_date") val releaseDate: String?,
    val runtime: Int?,
    val overview: String?,
    val genres: String?,
    @ColumnInfo(name = "poster_path") val posterPath: String?,
    @ColumnInfo(name = "backdrop_path") val backdropPath: String?,
    @ColumnInfo(name = "vote_average") val voteAverage: Double?,
    /** TMDB's body as fetched: the full text snapshot, not just the columns above. */
    @ColumnInfo(name = "metadata_json") val metadataJson: String,
    @ColumnInfo(name = "fetched_at") val fetchedAt: String,
)

@Entity(tableName = "shows", indices = [Index("tmdb_id", unique = true)])
data class ShowEntity(
    @PrimaryKey(autoGenerate = true) val id: Long = 0,
    @ColumnInfo(name = "tmdb_id") val tmdbId: Long,
    val title: String,
    @ColumnInfo(name = "first_air_date") val firstAirDate: String?,
    /** TMDB's production status ("Returning Series", "Ended", …) — not the user's status. */
    @ColumnInfo(name = "show_status") val showStatus: String?,
    val overview: String?,
    val genres: String?,
    @ColumnInfo(name = "poster_path") val posterPath: String?,
    @ColumnInfo(name = "backdrop_path") val backdropPath: String?,
    @ColumnInfo(name = "vote_average") val voteAverage: Double?,
    @ColumnInfo(name = "metadata_json") val metadataJson: String,
    @ColumnInfo(name = "fetched_at") val fetchedAt: String,
)

@Entity(
    tableName = "seasons",
    indices = [Index(value = ["show_id", "season_number"], unique = true)],
)
data class SeasonEntity(
    @PrimaryKey(autoGenerate = true) val id: Long = 0,
    @ColumnInfo(name = "show_id") val showId: Long,
    @ColumnInfo(name = "season_number") val seasonNumber: Int,
    val name: String?,
    @ColumnInfo(name = "air_date") val airDate: String?,
    val overview: String?,
    @ColumnInfo(name = "poster_path") val posterPath: String?,
    @ColumnInfo(name = "episode_count") val episodeCount: Int,
    /**
     * What the season page reads beyond the columns above — the season's score, its cast and its
     * episodes' scores — compacted from TMDB's body (`domain/SeasonSnapshot.kt`). Null on a row
     * written before the body was kept, and on a season the device only knows from its show.
     */
    @ColumnInfo(name = "metadata_json") val metadataJson: String? = null,
    @ColumnInfo(name = "fetched_at") val fetchedAt: String? = null,
)

@Entity(
    tableName = "episodes",
    indices = [
        Index(value = ["show_id", "season_number", "episode_number"], unique = true),
        Index("season_id"),
    ],
)
data class EpisodeEntity(
    @PrimaryKey(autoGenerate = true) val id: Long = 0,
    @ColumnInfo(name = "show_id") val showId: Long,
    @ColumnInfo(name = "season_id") val seasonId: Long,
    @ColumnInfo(name = "season_number") val seasonNumber: Int,
    @ColumnInfo(name = "episode_number") val episodeNumber: Int,
    /** The episode's own TMDB id — its identity on the wire once the app can link (D-040). */
    @ColumnInfo(name = "tmdb_id") val tmdbId: Long?,
    val title: String?,
    @ColumnInfo(name = "air_date") val airDate: String?,
    val runtime: Int?,
    val overview: String?,
    @ColumnInfo(name = "still_path") val stillPath: String?,
    /** TMDB's `episode_type` as sent (`standard` | `mid_season` | `finale`); read by [ro.mbyte.sofalog.domain.episodeMarker]. */
    @ColumnInfo(name = "episode_type") val episodeType: String? = null,
)

/**
 * Many rows per target: a rewatch is a new dated row (D-009). `target_type` is `movie` or
 * `episode`, and the pair identifies the target — movie 5 and episode 5 both exist, so every
 * query through this table carries its target type.
 */
@Entity(
    tableName = "user_watch_history",
    indices = [Index(value = ["target_type", "target_id"])],
)
data class WatchEntity(
    @PrimaryKey(autoGenerate = true) val id: Long = 0,
    @ColumnInfo(name = "target_type") val targetType: String,
    @ColumnInfo(name = "target_id") val targetId: Long,
    @ColumnInfo(name = "watched_at") val watchedAt: String,
)

/**
 * The one stored label (D-010): `watching` | `watched` | `dropped`. Written only by
 * VideoRepository's recompute, from the rules in `domain/ShowRules.kt`.
 */
@Entity(tableName = "user_show_states")
data class ShowStateEntity(
    @PrimaryKey @ColumnInfo(name = "show_id") val showId: Long,
    val status: String,
    @ColumnInfo(name = "updated_at") val updatedAt: String,
)

/**
 * One rating per target, 1..10; re-rating overwrites and there is no history. The target pair is
 * the key, as in the watch log — the id alone identifies nothing.
 */
@Entity(tableName = "user_video_ratings", primaryKeys = ["target_type", "target_id"])
data class RatingEntity(
    @ColumnInfo(name = "target_type") val targetType: String,
    @ColumnInfo(name = "target_id") val targetId: Long,
    val rating: Int,
    @ColumnInfo(name = "updated_at") val updatedAt: String,
)

/**
 * The watchlist: an independent axis, never a status (D-011). One row per movie or show.
 */
@Entity(tableName = "user_video_watchlist", primaryKeys = ["target_type", "target_id"])
data class WatchlistEntity(
    @ColumnInfo(name = "target_type") val targetType: String,
    @ColumnInfo(name = "target_id") val targetId: Long,
    @ColumnInfo(name = "added_at") val addedAt: String,
)

/** Favorites: a third axis, the watchlist's shape (D-030). */
@Entity(tableName = "user_video_favorites", primaryKeys = ["target_type", "target_id"])
data class FavoriteEntity(
    @ColumnInfo(name = "target_type") val targetType: String,
    @ColumnInfo(name = "target_id") val targetId: Long,
    @ColumnInfo(name = "added_at") val addedAt: String,
)

/**
 * Which formats the user holds an item on (D-021): `jellyfin` | `dvd` | `bluray` | `uhd` |
 * `physical` | `digital`. A shelf label per kind, never a status.
 */
@Entity(tableName = "user_video_copies", primaryKeys = ["target_type", "target_id", "kind"])
data class CopyEntity(
    @ColumnInfo(name = "target_type") val targetType: String,
    @ColumnInfo(name = "target_id") val targetId: Long,
    val kind: String,
    @ColumnInfo(name = "added_at") val addedAt: String,
)

const val TARGET_MOVIE = "movie"
const val TARGET_EPISODE = "episode"

/** A show is rated, watchlisted and favorited; it is never itself watched or owned. */
const val TARGET_SHOW = "show"

/** A season is what an owned copy of a show hangs on (D-021); it is never rated or watched. */
const val TARGET_SEASON = "season"

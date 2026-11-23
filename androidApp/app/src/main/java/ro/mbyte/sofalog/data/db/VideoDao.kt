package ro.mbyte.sofalog.data.db

import androidx.room.ColumnInfo
import androidx.room.Dao
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.Query
import androidx.room.Transaction
import androidx.room.Update
import androidx.room.Upsert
import kotlinx.coroutines.flow.Flow

data class StartedShow(
    @ColumnInfo(name = "show_id") val showId: Long,
    @ColumnInfo(name = "last_watched_at") val lastWatchedAt: String,
)

/** One episode's rating, read for a whole show at once. */
data class EpisodeRating(
    @ColumnInfo(name = "episode_id") val episodeId: Long,
    val rating: Int,
)

/** One owned copy of one season, by the season's number. */
data class SeasonCopy(
    @ColumnInfo(name = "season_number") val seasonNumber: Int,
    val kind: String,
)

data class EpisodeKeyRow(
    val id: Long,
    @ColumnInfo(name = "season_number") val seasonNumber: Int,
    @ColumnInfo(name = "episode_number") val episodeNumber: Int,
    @ColumnInfo(name = "tmdb_id") val tmdbId: Long?,
)

/**
 * One watch as the history feed lists it. Exactly one of the movie / episode halves is set, by
 * `target_type`; the poster is already the card's (an episode shows its season's poster, else its
 * show's).
 */
data class HistoryRow(
    val id: Long,
    @ColumnInfo(name = "watched_at") val watchedAt: String,
    @ColumnInfo(name = "target_type") val targetType: String,
    @ColumnInfo(name = "target_id") val targetId: Long,
    /** How many entries the whole log holds for this target — this row included. */
    @ColumnInfo(name = "target_watches") val targetWatches: Int,
    /** The episode's show, by local id; null for a movie. */
    @ColumnInfo(name = "show_id") val showId: Long?,
    /** The episode's season, by local id — where its TMDB score is stored; null for a movie. */
    @ColumnInfo(name = "season_id") val seasonId: Long?,
    @ColumnInfo(name = "movie_tmdb_id") val movieTmdbId: Long?,
    @ColumnInfo(name = "movie_title") val movieTitle: String?,
    @ColumnInfo(name = "movie_score") val movieScore: Double?,
    @ColumnInfo(name = "show_tmdb_id") val showTmdbId: Long?,
    @ColumnInfo(name = "show_title") val showTitle: String?,
    @ColumnInfo(name = "season_number") val seasonNumber: Int?,
    @ColumnInfo(name = "episode_number") val episodeNumber: Int?,
    @ColumnInfo(name = "episode_title") val episodeTitle: String?,
    @ColumnInfo(name = "poster_path") val posterPath: String?,
    @ColumnInfo(name = "user_rating") val userRating: Int?,
    /** The movie's own marks; an episode's are its show's. */
    val watchlisted: Boolean,
    val favorited: Boolean,
)

/*
 * The history feed. The log is the LEFT side, so nothing multiplies it: one row per watch. Each
 * catalog join is guarded by its own target type (movie 5 and episode 5 both exist), and a watch
 * whose target is gone is dropped before LIMIT. The order is the only thing the two queries
 * differ in — a sort direction cannot be bound.
 */
private const val HISTORY_SELECT =
    "SELECT w.id AS id, w.watched_at AS watched_at, w.target_type AS target_type, " +
        "w.target_id AS target_id, e.show_id AS show_id, e.season_id AS season_id, " +
        "(SELECT COUNT(*) FROM user_watch_history c " +
        "WHERE c.target_type = w.target_type AND c.target_id = w.target_id) AS target_watches, " +
        "m.tmdb_id AS movie_tmdb_id, m.title AS movie_title, m.vote_average AS movie_score, " +
        "s.tmdb_id AS show_tmdb_id, s.title AS show_title, " +
        "e.season_number AS season_number, e.episode_number AS episode_number, e.title AS episode_title, " +
        "CASE WHEN w.target_type = 'movie' THEN m.poster_path " +
        "ELSE COALESCE(se.poster_path, s.poster_path) END AS poster_path, " +
        "(SELECT r.rating FROM user_video_ratings r " +
        "WHERE r.target_type = w.target_type AND r.target_id = w.target_id) AS user_rating, " +
        // An episode is never itself listed: it borrows its show's marks.
        "CASE WHEN w.target_type = 'movie' THEN EXISTS(SELECT 1 FROM user_video_watchlist wl " +
        "WHERE wl.target_type = 'movie' AND wl.target_id = w.target_id) " +
        "ELSE EXISTS(SELECT 1 FROM user_video_watchlist wl " +
        "WHERE wl.target_type = 'show' AND wl.target_id = e.show_id) END AS watchlisted, " +
        "CASE WHEN w.target_type = 'movie' THEN EXISTS(SELECT 1 FROM user_video_favorites f " +
        "WHERE f.target_type = 'movie' AND f.target_id = w.target_id) " +
        "ELSE EXISTS(SELECT 1 FROM user_video_favorites f " +
        "WHERE f.target_type = 'show' AND f.target_id = e.show_id) END AS favorited " +
        "FROM user_watch_history w " +
        "LEFT JOIN movies m ON w.target_type = 'movie' AND m.id = w.target_id " +
        "LEFT JOIN episodes e ON w.target_type = 'episode' AND e.id = w.target_id " +
        "LEFT JOIN shows s ON s.id = e.show_id " +
        "LEFT JOIN seasons se ON se.id = e.season_id " +
        "WHERE (m.id IS NOT NULL OR s.id IS NOT NULL) " +
        "AND (:type IS NULL OR w.target_type = :type) "

/** One season's stored TMDB body, for a read that wants several at once. */
data class SeasonBodyRow(
    val id: Long,
    @ColumnInfo(name = "metadata_json") val metadataJson: String?,
)

/**
 * A dated, non-special episode inside the upcoming window, with the facts `upcomingEntries`
 * decides on. The watch log, the watchlist and the state row are probed, never joined.
 */
data class UpcomingEpisodeRow(
    val id: Long,
    @ColumnInfo(name = "show_id") val showId: Long,
    @ColumnInfo(name = "show_tmdb_id") val showTmdbId: Long,
    @ColumnInfo(name = "show_title") val showTitle: String,
    @ColumnInfo(name = "season_number") val seasonNumber: Int,
    @ColumnInfo(name = "episode_number") val episodeNumber: Int,
    val title: String?,
    @ColumnInfo(name = "air_date") val airDate: String,
    @ColumnInfo(name = "episode_type") val episodeType: String?,
    /** The season's poster, else the show's. */
    @ColumnInfo(name = "poster_path") val posterPath: String?,
    /** The show's stored status; null when it has no state row. */
    val status: String?,
    val watchlisted: Boolean,
    val watched: Boolean,
)

/** A watchlisted movie released inside the upcoming window. */
data class UpcomingMovieRow(
    val id: Long,
    @ColumnInfo(name = "tmdb_id") val tmdbId: Long,
    val title: String,
    @ColumnInfo(name = "release_date") val releaseDate: String,
    @ColumnInfo(name = "poster_path") val posterPath: String?,
    val watched: Boolean,
)

/**
 * One entry of a list page (watchlist, favorites, dropped): a movie or a show, by `target_type`.
 * [addedAt] is when it joined the list — for a dropped show, the instant it was dropped.
 */
data class ListRow(
    @ColumnInfo(name = "target_type") val targetType: String,
    @ColumnInfo(name = "local_id") val localId: Long,
    @ColumnInfo(name = "tmdb_id") val tmdbId: Long,
    val title: String,
    /** A movie's release date, a show's first air date. */
    val date: String?,
    @ColumnInfo(name = "poster_path") val posterPath: String?,
    @ColumnInfo(name = "backdrop_path") val backdropPath: String?,
    @ColumnInfo(name = "vote_average") val voteAverage: Double?,
    @ColumnInfo(name = "user_rating") val userRating: Int?,
    /** A movie: any watch in the log. A show: finished — its status reached `watched`. */
    val watched: Boolean,
    @ColumnInfo(name = "added_at") val addedAt: String,
)

/*
 * The watchlist and favorites pages, which differ in their table alone. Each catalog join is
 * guarded by its own target type (movie 5 and show 5 both exist), so exactly one side is set and
 * COALESCE picks it; a row whose target is gone is dropped. The watch log is probed, never joined.
 * No ORDER BY: the lists are read whole and ordered by `orderList`.
 */
private const val LIST_COLUMNS =
    "SELECT l.target_type AS target_type, l.target_id AS local_id, l.added_at AS added_at, " +
        "COALESCE(m.tmdb_id, sh.tmdb_id) AS tmdb_id, COALESCE(m.title, sh.title) AS title, " +
        "COALESCE(m.release_date, sh.first_air_date) AS date, " +
        "COALESCE(m.poster_path, sh.poster_path) AS poster_path, " +
        "COALESCE(m.backdrop_path, sh.backdrop_path) AS backdrop_path, " +
        "COALESCE(m.vote_average, sh.vote_average) AS vote_average, " +
        "(SELECT r.rating FROM user_video_ratings r " +
        "WHERE r.target_type = l.target_type AND r.target_id = l.target_id) AS user_rating, " +
        "CASE WHEN l.target_type = 'movie' THEN EXISTS(SELECT 1 FROM user_watch_history w " +
        "WHERE w.target_type = 'movie' AND w.target_id = l.target_id) " +
        "ELSE EXISTS(SELECT 1 FROM user_show_states st " +
        "WHERE st.show_id = l.target_id AND st.status = 'watched') END AS watched "

private const val LIST_JOINS =
    "LEFT JOIN movies m ON l.target_type = 'movie' AND m.id = l.target_id " +
        "LEFT JOIN shows sh ON l.target_type = 'show' AND sh.id = l.target_id " +
        "WHERE (m.id IS NOT NULL OR sh.id IS NOT NULL)"

/** What the library holds about one title, by its TMDB id: a search card's marks. */
data class LibraryMarkRow(
    @ColumnInfo(name = "tmdb_id") val tmdbId: Long,
    @ColumnInfo(name = "user_rating") val userRating: Int?,
    /**
     * A movie: any watch in the log. A show: STARTED — any episode watch, specials included —
     * unless it was dropped, which shows alone. Looser than the Lists pages' "finished".
     */
    val watched: Boolean,
    val watchlisted: Boolean,
    val favorited: Boolean,
    /** A show the user gave up on. Never a movie: only a show can be dropped. */
    val dropped: Boolean,
)

/**
 * One watch as the Stats fold reads it (D-039): the log row with its movie's or its episode's
 * facts carried along. [present] is false when the target row is gone.
 */
data class StatsPlayRow(
    val id: Long,
    @ColumnInfo(name = "target_type") val targetType: String,
    @ColumnInfo(name = "target_id") val targetId: Long,
    @ColumnInfo(name = "watched_at") val watchedAt: String,
    @ColumnInfo(name = "movie_runtime") val movieRuntime: Int?,
    @ColumnInfo(name = "movie_release") val movieRelease: String?,
    @ColumnInfo(name = "movie_genres") val movieGenres: String?,
    @ColumnInfo(name = "show_id") val showId: Long?,
    @ColumnInfo(name = "season_number") val seasonNumber: Int?,
    @ColumnInfo(name = "episode_number") val episodeNumber: Int?,
    @ColumnInfo(name = "episode_runtime") val episodeRuntime: Int?,
    @ColumnInfo(name = "show_first_air") val showFirstAir: String?,
    @ColumnInfo(name = "show_genres") val showGenres: String?,
    val present: Boolean,
)

/** One rating as the Stats fold reads it; [present] is false when what it rates is gone. */
data class StatsRatingRow(
    @ColumnInfo(name = "target_type") val targetType: String,
    @ColumnInfo(name = "target_id") val targetId: Long,
    val rating: Int,
    @ColumnInfo(name = "updated_at") val updatedAt: String,
    val present: Boolean,
)

/** A movie or a show as a Stats card names it. */
data class StatsTitleRow(
    val id: Long,
    @ColumnInfo(name = "tmdb_id") val tmdbId: Long,
    val title: String,
    /** A movie's release date, a show's first air date. */
    val date: String?,
    @ColumnInfo(name = "poster_path") val posterPath: String?,
    @ColumnInfo(name = "backdrop_path") val backdropPath: String?,
)

/** One title's stored TMDB body, for a read that wants only that. */
data class TitleBodyRow(
    val id: Long,
    @ColumnInfo(name = "metadata_json") val metadataJson: String,
)

/*
 * The Stats page's bulk read of the watch log. The log is the LEFT side, so nothing multiplies it:
 * one row per watch. Each catalog join is guarded by its own target type (movie 5 and episode 5
 * both exist). No period: the fold applies it, and a row whose target is gone stays in — flagged —
 * because it still dates the first play.
 */
private const val STATS_PLAYS =
    "SELECT w.id AS id, w.target_type AS target_type, w.target_id AS target_id, w.watched_at AS watched_at, " +
        "m.runtime AS movie_runtime, m.release_date AS movie_release, m.genres AS movie_genres, " +
        "e.show_id AS show_id, e.season_number AS season_number, e.episode_number AS episode_number, " +
        "e.runtime AS episode_runtime, sh.first_air_date AS show_first_air, sh.genres AS show_genres, " +
        "(m.id IS NOT NULL OR e.id IS NOT NULL) AS present " +
        "FROM user_watch_history w " +
        "LEFT JOIN movies m ON w.target_type = 'movie' AND m.id = w.target_id " +
        "LEFT JOIN episodes e ON w.target_type = 'episode' AND e.id = w.target_id " +
        "LEFT JOIN shows sh ON sh.id = e.show_id " +
        "ORDER BY w.watched_at, w.id"

/* Every rating of a movie, a show or an episode — a season is not rateable (D-020). */
private const val STATS_RATINGS =
    "SELECT r.target_type AS target_type, r.target_id AS target_id, r.rating AS rating, r.updated_at AS updated_at, " +
        "((r.target_type = 'movie' AND EXISTS(SELECT 1 FROM movies m WHERE m.id = r.target_id)) " +
        "OR (r.target_type = 'show' AND EXISTS(SELECT 1 FROM shows sh WHERE sh.id = r.target_id)) " +
        "OR (r.target_type = 'episode' AND EXISTS(SELECT 1 FROM episodes e WHERE e.id = r.target_id))) AS present " +
        "FROM user_video_ratings r " +
        "ORDER BY r.updated_at, r.target_type, r.target_id"

@Dao
interface VideoDao {
    // ---- movies
    @Query("SELECT * FROM movies WHERE tmdb_id = :tmdbId")
    suspend fun movieByTmdbId(tmdbId: Long): MovieEntity?

    @Query("SELECT * FROM movies WHERE tmdb_id = :tmdbId")
    fun observeMovieByTmdbId(tmdbId: Long): Flow<MovieEntity?>

    @Insert
    suspend fun insertMovie(movie: MovieEntity): Long

    @Update
    suspend fun updateMovie(movie: MovieEntity)

    // ---- shows
    @Query("SELECT * FROM shows WHERE tmdb_id = :tmdbId")
    suspend fun showByTmdbId(tmdbId: Long): ShowEntity?

    @Query("SELECT * FROM shows WHERE id = :id")
    suspend fun showById(id: Long): ShowEntity?

    @Query("SELECT * FROM shows WHERE tmdb_id = :tmdbId")
    fun observeShowByTmdbId(tmdbId: Long): Flow<ShowEntity?>

    @Query("SELECT * FROM shows WHERE id IN (:ids)")
    suspend fun showsByIds(ids: List<Long>): List<ShowEntity>

    @Insert
    suspend fun insertShow(show: ShowEntity): Long

    // ---- library marks, for titles named by TMDB id (search results). The watch log is probed,
    // never joined, and every probe carries its target type: movie 5 and show 5 both exist.
    @Query(
        "SELECT m.tmdb_id AS tmdb_id, " +
            "(SELECT r.rating FROM user_video_ratings r " +
            "WHERE r.target_type = 'movie' AND r.target_id = m.id) AS user_rating, " +
            "EXISTS(SELECT 1 FROM user_watch_history w " +
            "WHERE w.target_type = 'movie' AND w.target_id = m.id) AS watched, " +
            "EXISTS(SELECT 1 FROM user_video_watchlist wl " +
            "WHERE wl.target_type = 'movie' AND wl.target_id = m.id) AS watchlisted, " +
            "EXISTS(SELECT 1 FROM user_video_favorites f " +
            "WHERE f.target_type = 'movie' AND f.target_id = m.id) AS favorited, " +
            "0 AS dropped " +
            "FROM movies m WHERE m.tmdb_id IN (:tmdbIds)",
    )
    fun observeMovieMarks(tmdbIds: List<Long>): Flow<List<LibraryMarkRow>>

    @Query(
        "SELECT sh.tmdb_id AS tmdb_id, " +
            "(SELECT r.rating FROM user_video_ratings r " +
            "WHERE r.target_type = 'show' AND r.target_id = sh.id) AS user_rating, " +
            "(EXISTS(SELECT 1 FROM user_watch_history w JOIN episodes e ON e.id = w.target_id " +
            "WHERE w.target_type = 'episode' AND e.show_id = sh.id) " +
            "AND NOT EXISTS(SELECT 1 FROM user_show_states st " +
            "WHERE st.show_id = sh.id AND st.status = 'dropped')) AS watched, " +
            "EXISTS(SELECT 1 FROM user_video_watchlist wl " +
            "WHERE wl.target_type = 'show' AND wl.target_id = sh.id) AS watchlisted, " +
            "EXISTS(SELECT 1 FROM user_video_favorites f " +
            "WHERE f.target_type = 'show' AND f.target_id = sh.id) AS favorited, " +
            "EXISTS(SELECT 1 FROM user_show_states st " +
            "WHERE st.show_id = sh.id AND st.status = 'dropped') AS dropped " +
            "FROM shows sh WHERE sh.tmdb_id IN (:tmdbIds)",
    )
    fun observeShowMarks(tmdbIds: List<Long>): Flow<List<LibraryMarkRow>>

    @Update
    suspend fun updateShow(show: ShowEntity)

    // ---- seasons
    @Query("SELECT * FROM seasons WHERE show_id = :showId ORDER BY season_number")
    fun observeSeasons(showId: Long): Flow<List<SeasonEntity>>

    @Query("SELECT * FROM seasons WHERE show_id = :showId")
    suspend fun seasons(showId: Long): List<SeasonEntity>

    @Query("SELECT * FROM seasons WHERE show_id IN (:showIds)")
    suspend fun seasonsForShows(showIds: List<Long>): List<SeasonEntity>

    @Query("SELECT id, metadata_json FROM seasons WHERE id IN (:ids)")
    suspend fun seasonBodies(ids: List<Long>): List<SeasonBodyRow>

    @Insert
    suspend fun insertSeason(season: SeasonEntity): Long

    @Update
    suspend fun updateSeason(season: SeasonEntity)

    // ---- episodes
    @Query("SELECT * FROM episodes WHERE show_id = :showId ORDER BY season_number, episode_number")
    fun observeEpisodes(showId: Long): Flow<List<EpisodeEntity>>

    @Query("SELECT * FROM episodes WHERE show_id = :showId")
    suspend fun episodes(showId: Long): List<EpisodeEntity>

    @Query("SELECT * FROM episodes WHERE show_id IN (:showIds)")
    suspend fun episodesForShows(showIds: List<Long>): List<EpisodeEntity>

    @Query("SELECT * FROM episodes WHERE id = :id")
    suspend fun episodeById(id: Long): EpisodeEntity?

    @Query(
        "SELECT * FROM episodes WHERE show_id = :showId " +
            "AND season_number = :season AND episode_number = :number",
    )
    suspend fun episodeByKey(showId: Long, season: Int, number: Int): EpisodeEntity?

    /** Every episode of a show by both of its keys — what a restore resolves the file's entries against. */
    @Query("SELECT id, season_number, episode_number, tmdb_id FROM episodes WHERE show_id = :showId ORDER BY id")
    suspend fun episodeKeys(showId: Long): List<EpisodeKeyRow>

    @Insert
    suspend fun insertEpisode(episode: EpisodeEntity): Long

    @Update
    suspend fun updateEpisode(episode: EpisodeEntity)

    // ---- the watch log. It fans out (a rewatch is another row), so it is probed with
    // EXISTS / DISTINCT and never joined onto a list it could multiply.
    @Insert
    suspend fun insertWatch(watch: WatchEntity): Long

    @Insert
    suspend fun insertWatches(watches: List<WatchEntity>)

    @Query("SELECT * FROM user_watch_history WHERE id = :id")
    suspend fun watchById(id: Long): WatchEntity?

    @Query("DELETE FROM user_watch_history WHERE id = :id")
    suspend fun deleteWatch(id: Long)

    /** Every entry of one target. The pair names it: the id alone would reach the other kind. */
    @Query("DELETE FROM user_watch_history WHERE target_type = :type AND target_id = :targetId")
    suspend fun deleteTargetWatches(type: String, targetId: Long): Int

    @Query(
        "DELETE FROM user_watch_history WHERE target_type = 'episode' AND target_id IN " +
            "(SELECT id FROM episodes WHERE show_id = :showId AND season_number = :season)",
    )
    suspend fun deleteSeasonWatches(showId: Long, season: Int)

    @Query(
        "DELETE FROM user_watch_history WHERE target_type = 'episode' AND target_id IN " +
            "(SELECT id FROM episodes WHERE show_id = :showId)",
    )
    suspend fun deleteShowWatches(showId: Long): Int

    @Query(
        "SELECT w.* FROM user_watch_history w JOIN movies m ON m.id = w.target_id " +
            "WHERE w.target_type = 'movie' AND m.tmdb_id = :tmdbId ORDER BY w.watched_at DESC, w.id DESC",
    )
    fun observeMovieWatches(tmdbId: Long): Flow<List<WatchEntity>>

    @Query(
        "SELECT w.* FROM user_watch_history w JOIN episodes e ON e.id = w.target_id " +
            "WHERE w.target_type = 'episode' AND e.show_id = :showId ORDER BY w.watched_at DESC, w.id DESC",
    )
    fun observeShowWatches(showId: Long): Flow<List<WatchEntity>>

    @Query(
        "SELECT DISTINCT w.target_id FROM user_watch_history w JOIN episodes e ON e.id = w.target_id " +
            "WHERE w.target_type = 'episode' AND e.show_id = :showId",
    )
    suspend fun watchedEpisodeIds(showId: Long): List<Long>

    /** Whether the log holds anything at all — a movie or an episode. */
    @Query("SELECT EXISTS(SELECT 1 FROM user_watch_history)")
    fun observeHasWatches(): Flow<Boolean>

    @Query("SELECT DISTINCT target_id FROM user_watch_history WHERE target_type = 'episode'")
    suspend fun allWatchedEpisodeIds(): List<Long>

    /** One row per started show: the log alone defines the set, as on the server. */
    @Query(
        "SELECT e.show_id AS show_id, MAX(w.watched_at) AS last_watched_at " +
            "FROM user_watch_history w JOIN episodes e ON e.id = w.target_id " +
            "WHERE w.target_type = 'episode' GROUP BY e.show_id",
    )
    fun observeStartedShows(): Flow<List<StartedShow>>

    /** [type] null lists movies and episodes together. The id breaks ties: a bulk mark stamps one instant on many rows. */
    @Query(HISTORY_SELECT + "ORDER BY w.watched_at DESC, w.id DESC LIMIT :limit")
    fun observeHistoryNewest(type: String?, limit: Int): Flow<List<HistoryRow>>

    @Query(HISTORY_SELECT + "ORDER BY w.watched_at ASC, w.id ASC LIMIT :limit")
    fun observeHistoryOldest(type: String?, limit: Int): Flow<List<HistoryRow>>

    // ---- ratings: one row per target, overwritten on re-rating.
    @Upsert
    suspend fun upsertRating(rating: RatingEntity)

    @Query("SELECT rating FROM user_video_ratings WHERE target_type = :type AND target_id = :targetId")
    fun observeRating(type: String, targetId: Long): Flow<Int?>

    @Query("DELETE FROM user_video_ratings WHERE target_type = :type AND target_id = :targetId")
    suspend fun deleteRating(type: String, targetId: Long)

    /** Every episode rating of one show. The join carries the target type: episode 5 is not movie 5. */
    @Query(
        "SELECT r.target_id AS episode_id, r.rating AS rating FROM user_video_ratings r " +
            "JOIN episodes e ON e.id = r.target_id WHERE r.target_type = 'episode' AND e.show_id = :showId",
    )
    fun observeEpisodeRatings(showId: Long): Flow<List<EpisodeRating>>

    /** Every episode rating there is — Up Next's read, which spans every started show. */
    @Query("SELECT target_id AS episode_id, rating FROM user_video_ratings WHERE target_type = 'episode'")
    fun observeAllEpisodeRatings(): Flow<List<EpisodeRating>>

    // ---- watchlist, favorites, owned copies: independent axes, each keyed by the target pair.
    @Insert(onConflict = OnConflictStrategy.IGNORE)
    suspend fun insertWatchlist(row: WatchlistEntity): Long

    @Query("DELETE FROM user_video_watchlist WHERE target_type = :type AND target_id = :targetId")
    suspend fun deleteWatchlist(type: String, targetId: Long)

    @Query("SELECT EXISTS(SELECT 1 FROM user_video_watchlist WHERE target_type = :type AND target_id = :targetId)")
    fun observeWatchlisted(type: String, targetId: Long): Flow<Boolean>

    @Insert(onConflict = OnConflictStrategy.IGNORE)
    suspend fun insertFavorite(row: FavoriteEntity): Long

    @Query("DELETE FROM user_video_favorites WHERE target_type = :type AND target_id = :targetId")
    suspend fun deleteFavorite(type: String, targetId: Long)

    @Query("SELECT EXISTS(SELECT 1 FROM user_video_favorites WHERE target_type = :type AND target_id = :targetId)")
    fun observeFavorited(type: String, targetId: Long): Flow<Boolean>

    // ---- the list pages: each list read whole, live.
    @Query(LIST_COLUMNS + "FROM user_video_watchlist l " + LIST_JOINS)
    fun observeWatchlistRows(): Flow<List<ListRow>>

    @Query(LIST_COLUMNS + "FROM user_video_favorites l " + LIST_JOINS)
    fun observeFavoriteRows(): Flow<List<ListRow>>

    /**
     * Dropped shows: a view over the state rows, not a table. `updated_at` is the drop instant —
     * dropping stamps it and no recompute touches a dropped row.
     */
    @Query(
        "SELECT 'show' AS target_type, sh.id AS local_id, st.updated_at AS added_at, " +
            "sh.tmdb_id AS tmdb_id, sh.title AS title, sh.first_air_date AS date, " +
            "sh.poster_path AS poster_path, sh.backdrop_path AS backdrop_path, " +
            "sh.vote_average AS vote_average, " +
            "(SELECT r.rating FROM user_video_ratings r " +
            "WHERE r.target_type = 'show' AND r.target_id = sh.id) AS user_rating, " +
            "0 AS watched " +
            "FROM user_show_states st JOIN shows sh ON sh.id = st.show_id WHERE st.status = 'dropped'",
    )
    fun observeDroppedRows(): Flow<List<ListRow>>

    // ---- the upcoming calendar (D-026): the window's rows, live. SQL narrows by date alone;
    // `upcomingEntries` decides what is listed. No ORDER BY: that function owns the order.
    @Query(
        "SELECT e.id AS id, e.show_id AS show_id, sh.tmdb_id AS show_tmdb_id, sh.title AS show_title, " +
            "e.season_number AS season_number, e.episode_number AS episode_number, e.title AS title, " +
            "e.air_date AS air_date, e.episode_type AS episode_type, " +
            "COALESCE(se.poster_path, sh.poster_path) AS poster_path, " +
            "(SELECT st.status FROM user_show_states st WHERE st.show_id = sh.id) AS status, " +
            "EXISTS(SELECT 1 FROM user_video_watchlist l " +
            "WHERE l.target_type = 'show' AND l.target_id = sh.id) AS watchlisted, " +
            "EXISTS(SELECT 1 FROM user_watch_history w " +
            "WHERE w.target_type = 'episode' AND w.target_id = e.id) AS watched " +
            "FROM episodes e JOIN shows sh ON sh.id = e.show_id " +
            "LEFT JOIN seasons se ON se.id = e.season_id " +
            "WHERE e.season_number > 0 AND e.air_date >= :from AND e.air_date < :until",
    )
    fun observeUpcomingEpisodeRows(from: String, until: String): Flow<List<UpcomingEpisodeRow>>

    @Query(
        "SELECT m.id AS id, m.tmdb_id AS tmdb_id, m.title AS title, m.release_date AS release_date, " +
            "m.poster_path AS poster_path, " +
            "EXISTS(SELECT 1 FROM user_watch_history w " +
            "WHERE w.target_type = 'movie' AND w.target_id = m.id) AS watched " +
            "FROM movies m WHERE m.release_date >= :from AND m.release_date < :until " +
            "AND EXISTS(SELECT 1 FROM user_video_watchlist l " +
            "WHERE l.target_type = 'movie' AND l.target_id = m.id)",
    )
    fun observeUpcomingMovieRows(from: String, until: String): Flow<List<UpcomingMovieRow>>

    @Insert(onConflict = OnConflictStrategy.IGNORE)
    suspend fun insertCopy(row: CopyEntity)

    @Query("DELETE FROM user_video_copies WHERE target_type = :type AND target_id = :targetId AND kind = :kind")
    suspend fun deleteCopy(type: String, targetId: Long, kind: String)

    @Query("SELECT kind FROM user_video_copies WHERE target_type = :type AND target_id = :targetId")
    fun observeCopies(type: String, targetId: Long): Flow<List<String>>

    /** A show's copies: they hang on its seasons, one row per season and kind. */
    @Query(
        "SELECT s.season_number AS season_number, c.kind AS kind FROM user_video_copies c " +
            "JOIN seasons s ON s.id = c.target_id WHERE c.target_type = 'season' AND s.show_id = :showId",
    )
    fun observeSeasonCopies(showId: Long): Flow<List<SeasonCopy>>

    // ---- show states (written only by VideoRepository.recompute and its drop / un-drop pair)
    @Query("SELECT * FROM user_show_states WHERE show_id = :showId")
    suspend fun showState(showId: Long): ShowStateEntity?

    @Query("SELECT * FROM user_show_states WHERE show_id = :showId")
    fun observeShowState(showId: Long): Flow<ShowStateEntity?>

    @Query("SELECT * FROM user_show_states")
    fun observeShowStates(): Flow<List<ShowStateEntity>>

    @Upsert
    suspend fun upsertShowState(state: ShowStateEntity)

    /**
     * Drop and un-drop: an UPDATE, never an upsert — dropping must not fabricate a state for a show
     * that was never started. Returns how many rows it touched.
     */
    @Query("UPDATE user_show_states SET status = :status, updated_at = :now WHERE show_id = :showId")
    suspend fun updateShowStateStatus(showId: Long, status: String, now: String): Int

    @Query("DELETE FROM user_show_states WHERE show_id = :showId")
    suspend fun deleteShowState(showId: Long)

    // ------------------------------------------------------------------ stats (D-039)
    // The page's reads. Nothing here is filtered by period: the fold in `domain/Stats.kt` applies
    // it, in the user's zone.

    @Query(STATS_PLAYS)
    fun observeStatsPlays(): Flow<List<StatsPlayRow>>

    @Query(STATS_RATINGS)
    fun observeStatsRatings(): Flow<List<StatsRatingRow>>

    @Query("SELECT id, tmdb_id, title, release_date AS date, poster_path, backdrop_path FROM movies")
    suspend fun statsMovieTitles(): List<StatsTitleRow>

    @Query("SELECT id, tmdb_id, title, first_air_date AS date, poster_path, backdrop_path FROM shows")
    suspend fun statsShowTitles(): List<StatsTitleRow>

    /** The stored body of every movie the user rated: where its TMDB average and vote count are. */
    @Query(
        "SELECT m.id AS id, m.metadata_json AS metadata_json FROM movies m WHERE EXISTS(" +
            "SELECT 1 FROM user_video_ratings r WHERE r.target_type = 'movie' AND r.target_id = m.id)",
    )
    suspend fun statsRatedMovieBodies(): List<TitleBodyRow>

    /**
     * The stored body of every show the user rated, or watched an episode of that has no runtime
     * of its own — the two things the Stats fold reads a show body for.
     */
    @Query(
        "SELECT sh.id AS id, sh.metadata_json AS metadata_json FROM shows sh WHERE EXISTS(" +
            "SELECT 1 FROM user_video_ratings r WHERE r.target_type = 'show' AND r.target_id = sh.id) " +
            "OR EXISTS(SELECT 1 FROM user_watch_history w JOIN episodes e ON e.id = w.target_id " +
            "WHERE w.target_type = 'episode' AND e.show_id = sh.id AND e.runtime IS NULL)",
    )
    suspend fun statsShowBodies(): List<TitleBodyRow>

    // ------------------------------------------------------------------ delete data

    @Query("DELETE FROM user_watch_history")
    suspend fun deleteAllWatches()

    @Query("DELETE FROM user_show_states")
    suspend fun deleteAllShowStates()

    @Query("DELETE FROM user_video_ratings")
    suspend fun deleteAllRatings()

    @Query("DELETE FROM user_video_watchlist")
    suspend fun deleteAllWatchlist()

    @Query("DELETE FROM user_video_favorites")
    suspend fun deleteAllFavorites()

    @Query("DELETE FROM user_video_copies")
    suspend fun deleteAllCopies()

    @Query("DELETE FROM episodes")
    suspend fun deleteAllEpisodes()

    @Query("DELETE FROM seasons")
    suspend fun deleteAllSeasons()

    @Query("DELETE FROM shows")
    suspend fun deleteAllShows()

    @Query("DELETE FROM movies")
    suspend fun deleteAllMovies()

    /**
     * Empties the video library: every per-user table and the catalog rows they hang off, which
     * exist on a device only because something tracked them. One transaction, so a reader never
     * sees half a library.
     */
    @Transaction
    suspend fun deleteLibrary() {
        deleteAllWatches()
        deleteAllShowStates()
        deleteAllRatings()
        deleteAllWatchlist()
        deleteAllFavorites()
        deleteAllCopies()
        deleteAllEpisodes()
        deleteAllSeasons()
        deleteAllShows()
        deleteAllMovies()
    }
}

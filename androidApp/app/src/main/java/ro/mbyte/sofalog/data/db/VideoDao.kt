package ro.mbyte.sofalog.data.db

import androidx.room.ColumnInfo
import androidx.room.Dao
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.Query
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
)

/*
 * The history feed. The log is the LEFT side, so nothing multiplies it: one row per watch. Each
 * catalog join is guarded by its own target type (movie 5 and episode 5 both exist), and a watch
 * whose target is gone is dropped before LIMIT. The order is the only thing the two queries
 * differ in — a sort direction cannot be bound.
 */
private const val HISTORY_SELECT =
    "SELECT w.id AS id, w.watched_at AS watched_at, w.target_type AS target_type, " +
        "w.target_id AS target_id, e.show_id AS show_id, " +
        "(SELECT COUNT(*) FROM user_watch_history c " +
        "WHERE c.target_type = w.target_type AND c.target_id = w.target_id) AS target_watches, " +
        "m.tmdb_id AS movie_tmdb_id, m.title AS movie_title, m.vote_average AS movie_score, " +
        "s.tmdb_id AS show_tmdb_id, s.title AS show_title, " +
        "e.season_number AS season_number, e.episode_number AS episode_number, e.title AS episode_title, " +
        "CASE WHEN w.target_type = 'movie' THEN m.poster_path " +
        "ELSE COALESCE(se.poster_path, s.poster_path) END AS poster_path, " +
        "(SELECT r.rating FROM user_video_ratings r " +
        "WHERE r.target_type = w.target_type AND r.target_id = w.target_id) AS user_rating " +
        "FROM user_watch_history w " +
        "LEFT JOIN movies m ON w.target_type = 'movie' AND m.id = w.target_id " +
        "LEFT JOIN episodes e ON w.target_type = 'episode' AND e.id = w.target_id " +
        "LEFT JOIN shows s ON s.id = e.show_id " +
        "LEFT JOIN seasons se ON se.id = e.season_id " +
        "WHERE (m.id IS NOT NULL OR s.id IS NOT NULL) " +
        "AND (:type IS NULL OR w.target_type = :type) "

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

    @Update
    suspend fun updateShow(show: ShowEntity)

    // ---- seasons
    @Query("SELECT * FROM seasons WHERE show_id = :showId ORDER BY season_number")
    fun observeSeasons(showId: Long): Flow<List<SeasonEntity>>

    @Query("SELECT * FROM seasons WHERE show_id = :showId")
    suspend fun seasons(showId: Long): List<SeasonEntity>

    @Query("SELECT * FROM seasons WHERE show_id IN (:showIds)")
    suspend fun seasonsForShows(showIds: List<Long>): List<SeasonEntity>

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
    suspend fun deleteTargetWatches(type: String, targetId: Long)

    @Query(
        "DELETE FROM user_watch_history WHERE target_type = 'episode' AND target_id IN " +
            "(SELECT id FROM episodes WHERE show_id = :showId AND season_number = :season)",
    )
    suspend fun deleteSeasonWatches(showId: Long, season: Int)

    @Query(
        "DELETE FROM user_watch_history WHERE target_type = 'episode' AND target_id IN " +
            "(SELECT id FROM episodes WHERE show_id = :showId)",
    )
    suspend fun deleteShowWatches(showId: Long)

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

    // ---- watchlist, favorites, owned copies: independent axes, each keyed by the target pair.
    @Insert(onConflict = OnConflictStrategy.IGNORE)
    suspend fun insertWatchlist(row: WatchlistEntity)

    @Query("DELETE FROM user_video_watchlist WHERE target_type = :type AND target_id = :targetId")
    suspend fun deleteWatchlist(type: String, targetId: Long)

    @Query("SELECT EXISTS(SELECT 1 FROM user_video_watchlist WHERE target_type = :type AND target_id = :targetId)")
    fun observeWatchlisted(type: String, targetId: Long): Flow<Boolean>

    @Insert(onConflict = OnConflictStrategy.IGNORE)
    suspend fun insertFavorite(row: FavoriteEntity)

    @Query("DELETE FROM user_video_favorites WHERE target_type = :type AND target_id = :targetId")
    suspend fun deleteFavorite(type: String, targetId: Long)

    @Query("SELECT EXISTS(SELECT 1 FROM user_video_favorites WHERE target_type = :type AND target_id = :targetId)")
    fun observeFavorited(type: String, targetId: Long): Flow<Boolean>

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
}

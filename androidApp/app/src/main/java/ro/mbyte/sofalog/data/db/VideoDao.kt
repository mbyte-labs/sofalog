package ro.mbyte.sofalog.data.db

import androidx.room.ColumnInfo
import androidx.room.Dao
import androidx.room.Insert
import androidx.room.Query
import androidx.room.Update
import androidx.room.Upsert
import kotlinx.coroutines.flow.Flow

data class StartedShow(
    @ColumnInfo(name = "show_id") val showId: Long,
    @ColumnInfo(name = "last_watched_at") val lastWatchedAt: String,
)

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

    @Query("SELECT DISTINCT target_id FROM user_watch_history WHERE target_type = 'episode'")
    suspend fun allWatchedEpisodeIds(): List<Long>

    /** One row per started show: the log alone defines the set, as on the server. */
    @Query(
        "SELECT e.show_id AS show_id, MAX(w.watched_at) AS last_watched_at " +
            "FROM user_watch_history w JOIN episodes e ON e.id = w.target_id " +
            "WHERE w.target_type = 'episode' GROUP BY e.show_id",
    )
    fun observeStartedShows(): Flow<List<StartedShow>>

    // ---- ratings: one row per target, overwritten on re-rating.
    @Upsert
    suspend fun upsertRating(rating: RatingEntity)

    // ---- show states (written only by VideoRepository.recompute)
    @Query("SELECT * FROM user_show_states WHERE show_id = :showId")
    suspend fun showState(showId: Long): ShowStateEntity?

    @Query("SELECT * FROM user_show_states WHERE show_id = :showId")
    fun observeShowState(showId: Long): Flow<ShowStateEntity?>

    @Query("SELECT * FROM user_show_states")
    fun observeShowStates(): Flow<List<ShowStateEntity>>

    @Upsert
    suspend fun upsertShowState(state: ShowStateEntity)

    @Query("DELETE FROM user_show_states WHERE show_id = :showId")
    suspend fun deleteShowState(showId: Long)
}

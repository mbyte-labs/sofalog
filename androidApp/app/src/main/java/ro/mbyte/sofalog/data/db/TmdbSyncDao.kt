package ro.mbyte.sofalog.data.db

import androidx.room.Dao
import androidx.room.Insert
import androidx.room.Query
import androidx.room.Upsert
import kotlinx.coroutines.flow.Flow

/*
 * The TMDB account link and its outbox (D-029, D-036). The backfills join the catalog because the
 * user tables are keyed by local id while the outbox is TMDB-addressed; each join carries its
 * target type, as every query through a polymorphic column must.
 */

private const val SYNC_COLUMNS =
    "(kind, target_type, tmdb_id, season_number, episode_number, op, rating, attempts, " +
        "next_retry_at, last_error, created_at, updated_at)"

@Dao
interface TmdbSyncDao {
    // ---- the link
    @Query("SELECT * FROM user_tmdb_accounts WHERE id = 1")
    fun observeAccount(): Flow<TmdbAccountEntity?>

    @Query("SELECT * FROM user_tmdb_accounts WHERE id = 1")
    suspend fun account(): TmdbAccountEntity?

    @Upsert
    suspend fun upsertAccount(row: TmdbAccountEntity)

    @Query("UPDATE user_tmdb_accounts SET broken_at = :at WHERE id = 1 AND broken_at IS NULL")
    suspend fun markBroken(at: String)

    @Query(
        "UPDATE user_tmdb_accounts SET sync_ratings = :ratings, sync_watchlist = :watchlist, " +
            "sync_favorites = :favorites WHERE id = 1",
    )
    suspend fun setKinds(ratings: Boolean, watchlist: Boolean, favorites: Boolean)

    @Query("UPDATE user_tmdb_accounts SET setup_done = 1, pull_started_at = NULL WHERE id = 1")
    suspend fun setSetupDone()

    @Query("UPDATE user_tmdb_accounts SET pull_started_at = :at WHERE id = 1")
    suspend fun setPullStartedAt(at: String?)

    @Query("DELETE FROM user_tmdb_accounts")
    suspend fun deleteAccount()

    // ---- the outbox

    /**
     * Whether a change of [kind] is to be queued: linked, not broken, and that kind's switch on.
     * The guard lives in SQL so no caller can forget part of it.
     */
    @Query(
        "SELECT EXISTS(SELECT 1 FROM user_tmdb_accounts WHERE id = 1 AND broken_at IS NULL AND (" +
            "(:kind = 'rating' AND sync_ratings = 1) OR (:kind = 'watchlist' AND sync_watchlist = 1) OR " +
            "(:kind = 'favorite' AND sync_favorites = 1)))",
    )
    suspend fun syncEnabled(kind: String): Boolean

    @Query(
        "SELECT id FROM user_tmdb_sync WHERE kind = :kind AND target_type = :targetType AND tmdb_id = :tmdbId " +
            "AND season_number = :season AND episode_number = :episode",
    )
    suspend fun syncRowId(kind: String, targetType: String, tmdbId: Long, season: Int, episode: Int): Long?

    @Insert
    suspend fun insertSync(row: TmdbSyncEntity)

    /** The newer intent replaces the queued one and starts its retries over. */
    @Query(
        "UPDATE user_tmdb_sync SET op = :op, rating = :rating, attempts = 0, next_retry_at = NULL, " +
            "last_error = NULL, updated_at = :now WHERE id = :id",
    )
    suspend fun replaceSync(id: Long, op: String, rating: Int?, now: String)

    @Query("SELECT COUNT(*) FROM user_tmdb_sync")
    fun observePending(): Flow<Int>

    @Query(
        "SELECT * FROM user_tmdb_sync WHERE next_retry_at IS NULL OR next_retry_at <= :now " +
            "ORDER BY id LIMIT :limit",
    )
    suspend fun due(now: String, limit: Int): List<TmdbSyncEntity>

    @Query("SELECT MIN(next_retry_at) FROM user_tmdb_sync")
    suspend fun nextRetryAt(): String?

    /** Guarded by the row's whole intent: a row rewritten while in flight stays to be sent again. */
    @Query(
        "DELETE FROM user_tmdb_sync WHERE id = :id AND updated_at = :updatedAt AND op = :op " +
            "AND ((rating IS NULL AND :rating IS NULL) OR rating = :rating)",
    )
    suspend fun settle(id: Long, updatedAt: String, op: String, rating: Int?): Int

    @Query(
        "UPDATE user_tmdb_sync SET attempts = :attempts, next_retry_at = :retryAt, last_error = :lastError " +
            "WHERE id = :id AND updated_at = :updatedAt AND op = :op " +
            "AND ((rating IS NULL AND :rating IS NULL) OR rating = :rating)",
    )
    suspend fun defer(id: Long, updatedAt: String, op: String, rating: Int?, attempts: Int, retryAt: String, lastError: String): Int

    @Query("DELETE FROM user_tmdb_sync")
    suspend fun deleteAllSync()

    @Query("DELETE FROM user_tmdb_sync WHERE kind = :kind")
    suspend fun deleteSyncKind(kind: String)

    // ---- the setup's Push: everything already held, queued once. No switch guard on purpose —
    // a Push with its switch off still sends once. Seasons are never rated (D-020).
    @Query(
        "INSERT OR REPLACE INTO user_tmdb_sync $SYNC_COLUMNS " +
            "SELECT 'rating', 'movie', m.tmdb_id, 0, 0, 'set', r.rating, 0, NULL, NULL, :now, :now " +
            "FROM user_video_ratings r JOIN movies m ON m.id = r.target_id WHERE r.target_type = 'movie'",
    )
    suspend fun backfillMovieRatings(now: String)

    @Query(
        "INSERT OR REPLACE INTO user_tmdb_sync $SYNC_COLUMNS " +
            "SELECT 'rating', 'show', s.tmdb_id, 0, 0, 'set', r.rating, 0, NULL, NULL, :now, :now " +
            "FROM user_video_ratings r JOIN shows s ON s.id = r.target_id WHERE r.target_type = 'show'",
    )
    suspend fun backfillShowRatings(now: String)

    @Query(
        "INSERT OR REPLACE INTO user_tmdb_sync $SYNC_COLUMNS " +
            "SELECT 'rating', 'episode', s.tmdb_id, e.season_number, e.episode_number, 'set', r.rating, " +
            "0, NULL, NULL, :now, :now FROM user_video_ratings r JOIN episodes e ON e.id = r.target_id " +
            "JOIN shows s ON s.id = e.show_id WHERE r.target_type = 'episode'",
    )
    suspend fun backfillEpisodeRatings(now: String)

    @Query(
        "INSERT OR REPLACE INTO user_tmdb_sync $SYNC_COLUMNS " +
            "SELECT 'watchlist', 'movie', m.tmdb_id, 0, 0, 'set', NULL, 0, NULL, NULL, :now, :now " +
            "FROM user_video_watchlist w JOIN movies m ON m.id = w.target_id WHERE w.target_type = 'movie'",
    )
    suspend fun backfillMovieWatchlist(now: String)

    @Query(
        "INSERT OR REPLACE INTO user_tmdb_sync $SYNC_COLUMNS " +
            "SELECT 'watchlist', 'show', s.tmdb_id, 0, 0, 'set', NULL, 0, NULL, NULL, :now, :now " +
            "FROM user_video_watchlist w JOIN shows s ON s.id = w.target_id WHERE w.target_type = 'show'",
    )
    suspend fun backfillShowWatchlist(now: String)

    @Query(
        "INSERT OR REPLACE INTO user_tmdb_sync $SYNC_COLUMNS " +
            "SELECT 'favorite', 'movie', m.tmdb_id, 0, 0, 'set', NULL, 0, NULL, NULL, :now, :now " +
            "FROM user_video_favorites f JOIN movies m ON m.id = f.target_id WHERE f.target_type = 'movie'",
    )
    suspend fun backfillMovieFavorites(now: String)

    @Query(
        "INSERT OR REPLACE INTO user_tmdb_sync $SYNC_COLUMNS " +
            "SELECT 'favorite', 'show', s.tmdb_id, 0, 0, 'set', NULL, 0, NULL, NULL, :now, :now " +
            "FROM user_video_favorites f JOIN shows s ON s.id = f.target_id WHERE f.target_type = 'show'",
    )
    suspend fun backfillShowFavorites(now: String)
}

/**
 * Queues one change for TMDB, replacing whatever was queued for the same target. Call it inside
 * the transaction of the local write it mirrors; an unlinked or broken account, or a kind whose
 * switch is off, queues nothing.
 */
suspend fun TmdbSyncDao.enqueue(
    kind: String,
    targetType: String,
    tmdbId: Long,
    season: Int,
    episode: Int,
    op: String,
    rating: Int?,
    now: String,
) {
    if (!syncEnabled(kind)) return
    val held = syncRowId(kind, targetType, tmdbId, season, episode)
    if (held == null) {
        insertSync(
            TmdbSyncEntity(
                kind = kind, targetType = targetType, tmdbId = tmdbId, seasonNumber = season,
                episodeNumber = episode, op = op, rating = rating, createdAt = now, updatedAt = now,
            ),
        )
    } else {
        replaceSync(held, op, rating, now)
    }
}

package ro.mbyte.sofalog.data.db

import android.content.Context
import androidx.room.Database
import androidx.room.Room
import androidx.room.RoomDatabase
import androidx.room.migration.Migration
import androidx.sqlite.db.SupportSQLiteDatabase

@Database(
    entities = [
        MovieEntity::class,
        ShowEntity::class,
        SeasonEntity::class,
        EpisodeEntity::class,
        WatchEntity::class,
        ShowStateEntity::class,
        RatingEntity::class,
        WatchlistEntity::class,
        FavoriteEntity::class,
        CopyEntity::class,
        SettingEntity::class,
        TmdbAccountEntity::class,
        TmdbSyncEntity::class,
    ],
    version = 7,
    // Schema export is off while the app is unreleased, but devices already hold data, so every
    // version change carries a real migration below — never a destructive fallback.
    exportSchema = false,
)
abstract class AppDatabase : RoomDatabase() {
    abstract fun videoDao(): VideoDao
    abstract fun settingsDao(): SettingsDao
    abstract fun tmdbSyncDao(): TmdbSyncDao

    companion object {
        fun open(context: Context): AppDatabase =
            Room.databaseBuilder(context, AppDatabase::class.java, "sofalog.db")
                .addMigrations(MIGRATION_1_2, MIGRATION_2_3, MIGRATION_3_4, MIGRATION_4_5, MIGRATION_5_6, MIGRATION_6_7)
                .build()

        /**
         * Adds `episodes.episode_type`. Existing rows start null, so every show is marked stale:
         * its next view refetches the open seasons and fills the column in.
         */
        val MIGRATION_1_2 = object : Migration(1, 2) {
            override fun migrate(db: SupportSQLiteDatabase) {
                db.execSQL("ALTER TABLE episodes ADD COLUMN episode_type TEXT")
                db.execSQL("UPDATE shows SET fetched_at = '1970-01-01T00:00:00Z'")
            }
        }

        /** Adds `user_video_ratings`. */
        val MIGRATION_2_3 = object : Migration(2, 3) {
            override fun migrate(db: SupportSQLiteDatabase) {
                db.execSQL(
                    "CREATE TABLE IF NOT EXISTS user_video_ratings (" +
                        "target_type TEXT NOT NULL, target_id INTEGER NOT NULL, " +
                        "rating INTEGER NOT NULL, updated_at TEXT NOT NULL, " +
                        "PRIMARY KEY(target_type, target_id))",
                )
            }
        }

        /** Adds the watchlist, favorites and owned-copies tables. */
        val MIGRATION_3_4 = object : Migration(3, 4) {
            override fun migrate(db: SupportSQLiteDatabase) {
                for (table in listOf("user_video_watchlist", "user_video_favorites")) {
                    db.execSQL(
                        "CREATE TABLE IF NOT EXISTS $table (" +
                            "target_type TEXT NOT NULL, target_id INTEGER NOT NULL, " +
                            "added_at TEXT NOT NULL, PRIMARY KEY(target_type, target_id))",
                    )
                }
                db.execSQL(
                    "CREATE TABLE IF NOT EXISTS user_video_copies (" +
                        "target_type TEXT NOT NULL, target_id INTEGER NOT NULL, kind TEXT NOT NULL, " +
                        "added_at TEXT NOT NULL, PRIMARY KEY(target_type, target_id, kind))",
                )
            }
        }

        /**
         * Adds the season's own snapshot. Existing rows start null, which the season page reads as
         * "not fetched yet": each season fills in on its next view.
         */
        val MIGRATION_4_5 = object : Migration(4, 5) {
            override fun migrate(db: SupportSQLiteDatabase) {
                db.execSQL("ALTER TABLE seasons ADD COLUMN metadata_json TEXT")
                db.execSQL("ALTER TABLE seasons ADD COLUMN fetched_at TEXT")
            }
        }

        /** Adds the app's settings: the user's time zone and the where-to-watch country. */
        val MIGRATION_5_6 = object : Migration(5, 6) {
            override fun migrate(db: SupportSQLiteDatabase) {
                db.execSQL(
                    "CREATE TABLE IF NOT EXISTS settings (name TEXT NOT NULL, value TEXT NOT NULL, PRIMARY KEY(name))",
                )
            }
        }

        /** Adds the TMDB account link and its outbox (D-029, D-036). */
        val MIGRATION_6_7 = object : Migration(6, 7) {
            override fun migrate(db: SupportSQLiteDatabase) {
                db.execSQL(
                    "CREATE TABLE IF NOT EXISTS user_tmdb_accounts (" +
                        "id INTEGER NOT NULL, tmdb_account_id INTEGER NOT NULL, tmdb_username TEXT NOT NULL, " +
                        "linked_at TEXT NOT NULL, broken_at TEXT, setup_done INTEGER NOT NULL, " +
                        "sync_ratings INTEGER NOT NULL, sync_watchlist INTEGER NOT NULL, " +
                        "sync_favorites INTEGER NOT NULL, pull_started_at TEXT, PRIMARY KEY(id))",
                )
                db.execSQL(
                    "CREATE TABLE IF NOT EXISTS user_tmdb_sync (" +
                        "id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL, kind TEXT NOT NULL, " +
                        "target_type TEXT NOT NULL, tmdb_id INTEGER NOT NULL, season_number INTEGER NOT NULL, " +
                        "episode_number INTEGER NOT NULL, op TEXT NOT NULL, rating INTEGER, " +
                        "attempts INTEGER NOT NULL, next_retry_at TEXT, last_error TEXT, " +
                        "created_at TEXT NOT NULL, updated_at TEXT NOT NULL)",
                )
                db.execSQL(
                    "CREATE UNIQUE INDEX IF NOT EXISTS " +
                        "index_user_tmdb_sync_kind_target_type_tmdb_id_season_number_episode_number " +
                        "ON user_tmdb_sync (kind, target_type, tmdb_id, season_number, episode_number)",
                )
            }
        }
    }
}

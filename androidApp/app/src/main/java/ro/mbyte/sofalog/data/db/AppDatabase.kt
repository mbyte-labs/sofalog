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
    ],
    version = 5,
    // Schema export is off while the app is unreleased, but devices already hold data, so every
    // version change carries a real migration below — never a destructive fallback.
    exportSchema = false,
)
abstract class AppDatabase : RoomDatabase() {
    abstract fun videoDao(): VideoDao

    companion object {
        fun open(context: Context): AppDatabase =
            Room.databaseBuilder(context, AppDatabase::class.java, "sofalog.db")
                .addMigrations(MIGRATION_1_2, MIGRATION_2_3, MIGRATION_3_4, MIGRATION_4_5)
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
    }
}

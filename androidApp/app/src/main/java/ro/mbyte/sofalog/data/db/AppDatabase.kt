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
    ],
    version = 3,
    // Schema export is off while the app is unreleased, but devices already hold data, so every
    // version change carries a real migration below — never a destructive fallback.
    exportSchema = false,
)
abstract class AppDatabase : RoomDatabase() {
    abstract fun videoDao(): VideoDao

    companion object {
        fun open(context: Context): AppDatabase =
            Room.databaseBuilder(context, AppDatabase::class.java, "sofalog.db")
                .addMigrations(MIGRATION_1_2, MIGRATION_2_3)
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
    }
}

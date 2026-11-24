package ro.mbyte.sofalog.data.db

import android.content.Context
import androidx.room.Database
import androidx.room.Room
import androidx.room.RoomDatabase

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
    // Version 1 is the baseline: the schema is created from the entities, with no migration
    // history behind it. From here on every schema change bumps the version and carries a real
    // Migration, registered in open() — never a destructive fallback.
    version = 1,
    exportSchema = false,
)
abstract class AppDatabase : RoomDatabase() {
    abstract fun videoDao(): VideoDao
    abstract fun settingsDao(): SettingsDao
    abstract fun tmdbSyncDao(): TmdbSyncDao

    companion object {
        fun open(context: Context): AppDatabase =
            Room.databaseBuilder(context, AppDatabase::class.java, "sofalog.db").build()
    }
}

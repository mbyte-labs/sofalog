package ro.mbyte.sofalog.data.db

import androidx.room.Dao
import androidx.room.Query
import androidx.room.Upsert
import kotlinx.coroutines.flow.Flow

@Dao
interface SettingsDao {
    @Query("SELECT * FROM settings")
    fun observeAll(): Flow<List<SettingEntity>>

    @Query("SELECT * FROM settings")
    suspend fun all(): List<SettingEntity>

    @Upsert
    suspend fun set(row: SettingEntity)

    /** Back to the default: an absent row is what "not chosen" means. */
    @Query("DELETE FROM settings WHERE name = :name")
    suspend fun clear(name: String)
}

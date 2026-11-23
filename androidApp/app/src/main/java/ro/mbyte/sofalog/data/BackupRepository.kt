package ro.mbyte.sofalog.data

import android.content.Context
import android.net.Uri
import androidx.core.content.FileProvider
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import ro.mbyte.sofalog.BuildConfig
import ro.mbyte.sofalog.domain.BackupSummary
import ro.mbyte.sofalog.domain.Clock
import ro.mbyte.sofalog.domain.SofalogBackup
import ro.mbyte.sofalog.domain.backupFileName
import ro.mbyte.sofalog.domain.buildBackup
import ro.mbyte.sofalog.domain.summary
import ro.mbyte.sofalog.domain.writeSofalogBackup
import java.io.File
import java.io.IOException

/** A backup cannot be made while a restore is rewriting the library. */
class RestoreRunningException : IllegalStateException("a restore is running")

/** A backup written for the share sheet: where it is, what it is called, what it holds. */
data class SharedBackup(val uri: Uri, val fileName: String, val summary: BackupSummary)

/**
 * Writing the app's own backup file (D-040): the library's user state as one JSON document,
 * either into a place the user picked or into the cache for another app to take. The file holds
 * no secret and nothing of the TMDB link; the cache copy is outside every system backup.
 */
class BackupRepository(
    private val context: Context,
    private val repository: VideoRepository,
    private val restore: RestoreRepository,
    private val clock: Clock,
) {
    /** The name a new backup gets: today's, in the user's zone. */
    fun fileName(): String = backupFileName(clock.read().today)

    private suspend fun build(): SofalogBackup {
        // Mid-restore the library is half one thing and half another: not a moment worth keeping.
        if (restore.isRunning) throw RestoreRunningException()
        return buildBackup(repository.backupRows(), clock.read().now, BuildConfig.VERSION_NAME)
    }

    /** Writes a backup into the document the user created. */
    suspend fun writeTo(uri: Uri): BackupSummary = withContext(Dispatchers.IO) {
        val backup = build()
        // "wt": a document that already had content is truncated, not overwritten in place.
        val out = context.contentResolver.openOutputStream(uri, "wt") ?: throw IOException("the document cannot be written")
        out.use { writeSofalogBackup(backup, it) }
        backup.summary()
    }

    /**
     * Writes a backup into the cache and returns the content Uri another app can read it through.
     * Only the newest one is kept there.
     */
    suspend fun writeShareFile(): SharedBackup = withContext(Dispatchers.IO) {
        val backup = build()
        val dir = File(context.cacheDir, SHARE_DIR)
        dir.listFiles()?.forEach { it.delete() }
        if (!dir.isDirectory && !dir.mkdirs()) throw IOException("the cache cannot be written")
        val file = File(dir, fileName())
        file.outputStream().use { writeSofalogBackup(backup, it) }
        SharedBackup(FileProvider.getUriForFile(context, AUTHORITY, file), file.name, backup.summary())
    }

    private companion object {
        /** Matches the `<cache-path>` in res/xml/file_paths.xml. */
        const val SHARE_DIR = "backups"

        /** Matches the provider's authority in the manifest. */
        const val AUTHORITY = BuildConfig.APPLICATION_ID + ".files"
    }
}

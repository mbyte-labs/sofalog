package ro.mbyte.sofalog

import android.app.Application
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import okhttp3.OkHttpClient
import ro.mbyte.sofalog.data.CatalogRefresher
import ro.mbyte.sofalog.data.RestoreRepository
import ro.mbyte.sofalog.data.VideoRepository
import ro.mbyte.sofalog.data.db.AppDatabase
import ro.mbyte.sofalog.data.tmdb.TmdbClient
import ro.mbyte.sofalog.data.tmdbaccount.TmdbAccountService
import ro.mbyte.sofalog.data.tmdbaccount.TmdbSessionStore
import ro.mbyte.sofalog.domain.Clock

/** Holds the app's few long-lived objects. Wiring is by hand; there is no DI framework. */
class SofalogApp : Application() {
    /**
     * For work that must outlive a screen — a restore, the TMDB setup's import, the outbox, the
     * catalog refresh. It lives as long as the process and no longer: the app does no background
     * work (SPEC D-040).
     */
    private val appScope = CoroutineScope(SupervisorJob() + Dispatchers.Default)

    private val db by lazy { AppDatabase.open(this) }
    private val tmdb by lazy { TmdbClient(BuildConfig.TMDB_API_KEY, OkHttpClient()) }

    val repository: VideoRepository by lazy {
        VideoRepository(db = db, tmdb = tmdb, clock = Clock.System).also { repo ->
            // The user's zone and country are read before anything is shown: every date on the
            // first screen depends on them. Two rows from a local table.
            runBlocking { repo.loadSettings() }
        }
    }

    val tmdbAccount: TmdbAccountService by lazy {
        TmdbAccountService(db, tmdb, repository, TmdbSessionStore(this), Clock.System, appScope).also { service ->
            repository.onSyncWrite = service::kick
            appScope.launch { service.reconcile() }
        }
    }

    /** Re-fetches stale snapshots while the app is on screen; started and stopped by the activity. */
    val catalogRefresh: CatalogRefresher by lazy { CatalogRefresher(repository, appScope) }

    val restore: RestoreRepository by lazy {
        RestoreRepository(contentResolver, repository, Clock.System, appScope)
    }
}

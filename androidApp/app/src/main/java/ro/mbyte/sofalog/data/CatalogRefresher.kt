package ro.mbyte.sofalog.data

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import ro.mbyte.sofalog.data.tmdb.TmdbException
import ro.mbyte.sofalog.domain.ProviderBreaker
import ro.mbyte.sofalog.domain.RefreshCandidate
import ro.mbyte.sofalog.domain.dueForRefresh
import java.time.Duration
import java.time.Instant

/** Every tracked title with what refers to it. */
data class RefreshCandidates(val movies: List<RefreshCandidate>, val shows: List<RefreshCandidate>)

/** What the refresh needs from the library; [VideoRepository] in the app, a fake in its test. */
interface CatalogRefreshWrites {
    suspend fun refreshCandidates(): RefreshCandidates

    /** Re-fetches one tracked movie. Throws [TmdbException] when TMDB does not answer with it. */
    suspend fun refreshMovie(tmdbId: Long)

    /** Re-fetches one tracked show: its root, new seasons and open seasons. */
    suspend fun refreshShow(tmdbId: Long)

    /** The status sweep. Local: it never fetches. */
    suspend fun recomputeFollowedShows()
}

/** What one tick did. [completed] is false when TMDB could not be reached for all of it. */
data class CatalogRefreshReport(val refreshed: Int, val failed: Int, val completed: Boolean)

/**
 * Keeps the library's snapshots fresh without anyone opening a page (SPEC D-025, on the device):
 * what brings a finished show back to Up Next when a new season is announced.
 *
 * It is **not background work** (D-040): a tick runs when the app comes on screen and every
 * [PERIOD] while it stays there, and is cancelled when the app leaves. A tick re-fetches the
 * titles [dueForRefresh] picks, one at a time, and always ends with the status sweep — which is
 * local, and runs even with no key and no network. A cancelled or cut-short tick needs no
 * recovery: each title's `fetched_at` is its own schedule, so the next tick picks up the rest.
 */
class CatalogRefresher(
    private val writes: CatalogRefreshWrites,
    private val scope: CoroutineScope,
    private val now: () -> Instant = Instant::now,
) {
    private var job: Job? = null
    private val running = Mutex()

    /** When the last tick that ran to its end finished. In memory: a new process simply ticks. */
    @Volatile
    private var lastCompleted: Instant? = null

    /**
     * Titles TMDB answered 404 for, silenced until the process ends: the row is kept (it may
     * carry watches) and not asked for again on every tick.
     */
    private val gone = mutableSetOf<Pair<MediaKind, Long>>()

    /** Starts ticking while the app is on screen. */
    @Synchronized
    fun start() {
        if (job?.isActive == true) return
        job = scope.launch {
            while (true) {
                try {
                    tickIfDue()
                } catch (e: CancellationException) {
                    throw e
                } catch (e: Exception) {
                    // A tick never takes the app down; the next one starts from the same rows.
                }
                delay(PERIOD.toMillis())
            }
        }
    }

    /** Stops when the app leaves the screen. The title in flight is dropped, not half-written. */
    @Synchronized
    fun stop() {
        job?.cancel()
        job = null
    }

    /** Runs a tick unless one finished within [COOLDOWN] — coming back to the app is frequent. */
    suspend fun tickIfDue(): CatalogRefreshReport? {
        val last = lastCompleted
        if (last != null && Duration.between(last, now()) < COOLDOWN) return null
        return tick()
    }

    suspend fun tick(): CatalogRefreshReport = running.withLock {
        val at = now()
        val candidates = writes.refreshCandidates()
        val breaker = ProviderBreaker()
        var refreshed = 0
        var failed = 0
        // No key, or a refused one: nothing can be fetched until the app is rebuilt.
        var blocked = false

        suspend fun pass(kind: MediaKind, ids: List<Long>, refresh: suspend (Long) -> Unit) {
            for (id in ids) {
                if (blocked || breaker.open) return
                if (kind to id in gone) continue
                try {
                    refresh(id)
                    breaker.observe(false)
                    refreshed++
                } catch (e: TmdbException) {
                    failed++
                    when (e.reason) {
                        TmdbException.Reason.UNAVAILABLE -> breaker.observe(true)
                        TmdbException.Reason.NO_KEY, TmdbException.Reason.BAD_KEY -> blocked = true
                        TmdbException.Reason.NOT_FOUND -> {
                            gone += kind to id
                            breaker.observe(false)
                        }
                        else -> breaker.observe(false)
                    }
                }
            }
        }

        pass(MediaKind.MOVIE, dueForRefresh(candidates.movies, at), writes::refreshMovie)
        pass(MediaKind.SHOW, dueForRefresh(candidates.shows, at), writes::refreshShow)

        // Always, whatever happened above: an episode that aired overnight needs no fetch.
        writes.recomputeFollowedShows()

        val completed = !blocked && !breaker.open
        // An outage does not start the cooldown: the next return to the app tries again.
        if (completed) lastCompleted = now()
        CatalogRefreshReport(refreshed, failed, completed)
    }

    companion object {
        val COOLDOWN: Duration = Duration.ofMinutes(15)
        val PERIOD: Duration = Duration.ofHours(6)
    }
}

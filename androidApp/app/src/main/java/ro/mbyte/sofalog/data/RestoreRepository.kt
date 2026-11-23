package ro.mbyte.sofalog.data

import android.content.ContentResolver
import android.net.Uri
import android.provider.OpenableColumns
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import ro.mbyte.sofalog.data.tmdb.TmdbException
import ro.mbyte.sofalog.domain.BackupParse
import ro.mbyte.sofalog.domain.Clock
import ro.mbyte.sofalog.domain.DumpParse
import ro.mbyte.sofalog.domain.ProviderBreaker
import ro.mbyte.sofalog.domain.RESTORE_CANCELLED_MESSAGE
import ro.mbyte.sofalog.domain.RESTORE_DONE
import ro.mbyte.sofalog.domain.RESTORE_FAILED
import ro.mbyte.sofalog.domain.RESTORE_INTERNAL_MESSAGE
import ro.mbyte.sofalog.domain.RESTORE_MAX_BYTES
import ro.mbyte.sofalog.domain.RESTORE_RUNNING
import ro.mbyte.sofalog.domain.RestoreCurrent
import ro.mbyte.sofalog.domain.RestoreJob
import ro.mbyte.sofalog.domain.RestorePlan
import ro.mbyte.sofalog.domain.RestoreProgress
import ro.mbyte.sofalog.domain.RestoreReport
import ro.mbyte.sofalog.domain.RestoreSkip
import ro.mbyte.sofalog.domain.SKIP_MOVIE_NOT_FOUND
import ro.mbyte.sofalog.domain.SKIP_MOVIE_UNAVAILABLE
import ro.mbyte.sofalog.domain.SKIP_PROVIDER_UNAVAILABLE
import ro.mbyte.sofalog.domain.SKIP_SHOW_NOT_FOUND
import ro.mbyte.sofalog.domain.SKIP_SHOW_UNAVAILABLE
import ro.mbyte.sofalog.domain.parseSofalogBackup
import ro.mbyte.sofalog.domain.parseTicketBoxDump
import ro.mbyte.sofalog.domain.plus
import ro.mbyte.sofalog.domain.summary
import ro.mbyte.sofalog.domain.validateSofalogBackup
import ro.mbyte.sofalog.domain.validateTicketBoxDump
import ro.mbyte.sofalog.domain.withSkips
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.time.Instant
import kotlin.coroutines.coroutineContext

/** Which kind of file a restore page reads. */
enum class RestoreSource { TICKET_BOX, SOFALOG }

/** Where a restore page stands. Both pages share it: one file, one job, at a time. */
sealed interface RestoreState {
    /** Nothing picked. [notice] says why the last file was refused. */
    data class Idle(val notice: String = "") : RestoreState

    data class Reading(val fileName: String?) : RestoreState

    /** A file was read and understood; nothing is written until the user confirms. */
    data class Ready(
        val fileName: String,
        val exportedAt: String?,
        val watches: Int,
        val ratings: Int,
        val watchlist: Int,
        val favorites: Int,
        val source: RestoreSource = RestoreSource.TICKET_BOX,
        /** The app's own backup only: a Ticket Box file has neither. */
        val copies: Int = 0,
        val dropped: Int = 0,
    ) : RestoreState

    /** A job: running, or its report once it ended. */
    data class Active(val job: RestoreJob) : RestoreState
}

/**
 * Restoring a backup on the device: a Ticket Box export (D-023) or the app's own backup file
 * (D-040). It reads the FILE — nothing here calls Trakt. The two differ in how the file is parsed
 * and in what a group replaces; the run itself is one. The job lives in the app's scope: it survives leaving the page and a rotation, not the
 * process. Committed groups stay committed, and the import replaces rather than duplicates, so
 * running the same file again is the recovery for anything that cut it short.
 */
class RestoreRepository(
    private val resolver: ContentResolver,
    private val writes: RestoreWrites,
    private val clock: Clock,
    private val scope: CoroutineScope,
    /** The copy kinds the app knows; a backup naming another is skipped, not refused. */
    private val copyKinds: Set<String> = CopyKind.entries.map { it.value }.toSet(),
) {
    private val _state = MutableStateFlow<RestoreState>(RestoreState.Idle())
    val state: StateFlow<RestoreState> = _state

    private var reading: Job? = null
    private var running: Job? = null
    private var held: Pair<RestorePlan, List<RestoreSkip>>? = null

    private var source: RestoreSource? = null

    val isRunning: Boolean
        get() = (_state.value as? RestoreState.Active)?.job?.state == RESTORE_RUNNING

    /** Reads and validates a picked file. Refused while a job runs; a newer pick wins over an older read. */
    @Synchronized
    fun pick(uri: Uri, from: RestoreSource = RestoreSource.TICKET_BOX) {
        if (isRunning) return
        reading?.cancel()
        held = null
        source = from
        _state.value = RestoreState.Reading(null)
        reading = scope.launch(Dispatchers.IO) {
            val next = try {
                read(uri, from)
            } catch (e: CancellationException) {
                throw e
            } catch (e: OutOfMemoryError) {
                RestoreState.Idle("That file is too large for this device to read.")
            } catch (e: Exception) {
                RestoreState.Idle("That file couldn't be read.")
            }
            coroutineContext.ensureActive()
            _state.value = next
        }
    }

    /**
     * A page coming on screen. A file picked on the OTHER page — or that page's refusal notice —
     * is put away, so this page never offers to import it under its own name. A job is left alone:
     * there is one, and both pages show it.
     */
    @Synchronized
    fun enter(page: RestoreSource) {
        if (source == null || source == page || _state.value is RestoreState.Active) return
        reading?.cancel()
        held = null
        source = page
        _state.value = RestoreState.Idle()
    }

    private fun read(uri: Uri, from: RestoreSource): RestoreState {
        var name: String? = null
        var size: Long? = null
        resolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE), null, null, null)?.use { c ->
            if (c.moveToFirst()) {
                name = c.getString(0)
                if (!c.isNull(1)) size = c.getLong(1)
            }
        }
        _state.value = RestoreState.Reading(name)
        val tooLarge = RestoreState.Idle("That file is too large (32 MB limit).")
        if ((size ?: 0) > RESTORE_MAX_BYTES) return tooLarge
        // A provider's size can be missing or wrong, so the read itself is capped too.
        val bytes = resolver.openInputStream(uri)?.use { input ->
            val out = ByteArrayOutputStream((size ?: 0L).toInt().coerceIn(64 * 1024, RESTORE_MAX_BYTES))
            val chunk = ByteArray(64 * 1024)
            while (true) {
                val n = input.read(chunk)
                if (n < 0) break
                out.write(chunk, 0, n)
                if (out.size() > RESTORE_MAX_BYTES) return tooLarge
            }
            out.toByteArray()
        } ?: return RestoreState.Idle("That file couldn't be read.")
        val now = Instant.parse(clock.read().now)
        if (from == RestoreSource.SOFALOG) {
            val backup = when (val parsed = parseSofalogBackup { ByteArrayInputStream(bytes) }) {
                is BackupParse.Rejected -> return RestoreState.Idle(parsed.message)
                is BackupParse.Ok -> parsed.backup
            }
            held = validateSofalogBackup(backup, now, copyKinds)
            val summary = backup.summary()
            return RestoreState.Ready(
                fileName = name ?: "Sofalog backup",
                exportedAt = backup.exportedAt,
                watches = summary.watches,
                ratings = summary.ratings,
                watchlist = summary.watchlist,
                favorites = summary.favorites,
                source = from,
                copies = summary.copies,
                dropped = summary.dropped,
            )
        }
        val dump = when (val parsed = parseTicketBoxDump { ByteArrayInputStream(bytes) }) {
            is DumpParse.Rejected -> return RestoreState.Idle(parsed.message)
            is DumpParse.Ok -> parsed.dump
        }
        held = validateTicketBoxDump(dump, now)
        return RestoreState.Ready(
            fileName = name ?: "Ticket Box export",
            exportedAt = dump.exportedAt,
            watches = dump.history.orEmpty().size,
            ratings = dump.ratings.orEmpty().size,
            watchlist = dump.watchlist.orEmpty().size,
            favorites = dump.favorites.orEmpty().size,
        )
    }

    /** Starts the import of the file that is ready. False when there is none, or a job already runs. */
    @Synchronized
    fun start(): Boolean {
        val (plan, skips) = held ?: return false
        if (_state.value !is RestoreState.Ready) return false
        held = null
        val job = RestoreJob(
            state = RESTORE_RUNNING,
            startedAt = clock.read().now,
            progress = RestoreProgress(groupsTotal = plan.groups),
            report = RestoreReport(moviesTotal = plan.movies.size, showsTotal = plan.shows.size).withSkips(skips),
        )
        _state.value = RestoreState.Active(job)
        running = scope.launch {
            runRestore(plan, writes, clock) { change ->
                _state.update { if (it is RestoreState.Active) RestoreState.Active(change(it.job)) else it }
            }
        }
        return true
    }

    /** Stops a running import between two groups. What was written stays. */
    fun stop() {
        running?.cancel()
    }

    /** Back to the file picker, from a file that is ready or a job that ended. */
    @Synchronized
    fun discard() {
        if (isRunning) return
        reading?.cancel()
        held = null
        _state.value = RestoreState.Idle()
    }
}

/**
 * The run: movies, then shows, in TMDB-id order. Each group is resolved (one retry on an outage —
 * a call can die when the app is paused in the background), written in one transaction and
 * counted. After three consecutive outages nothing more is fetched: groups already held still
 * import, the rest are skipped. Every exit path lands in a finished job.
 */
internal suspend fun runRestore(
    plan: RestorePlan,
    writes: RestoreWrites,
    clock: Clock,
    update: ((RestoreJob) -> RestoreJob) -> Unit,
) {
    val breaker = ProviderBreaker()
    var error: String? = null
    try {
        suspend fun resolve(movie: Boolean, id: Long, skipAll: (String) -> Unit): RestoreResolved? {
            var retried = false
            while (true) {
                try {
                    val target = (if (movie) writes.resolveRestoreMovie(id, !breaker.open) else writes.resolveRestoreShow(id, !breaker.open))
                    if (target == null) skipAll(SKIP_PROVIDER_UNAVAILABLE) else if (target.materialised) breaker.observe(false)
                    return target
                } catch (e: TmdbException) {
                    when (e.reason) {
                        TmdbException.Reason.NOT_FOUND -> {
                            skipAll(if (movie) SKIP_MOVIE_NOT_FOUND else SKIP_SHOW_NOT_FOUND)
                            return null
                        }
                        // Without a working key every later fetch fails the same way.
                        TmdbException.Reason.NO_KEY, TmdbException.Reason.BAD_KEY -> throw e
                        else -> {
                            if (!retried) {
                                retried = true
                                continue
                            }
                            breaker.observe(true)
                            skipAll(if (movie) SKIP_MOVIE_UNAVAILABLE else SKIP_SHOW_UNAVAILABLE)
                            return null
                        }
                    }
                }
            }
        }
        fun begin(type: String, id: Long) = update { it.copy(progress = it.progress.copy(current = RestoreCurrent(type, id, null))) }
        fun done() = update { it.copy(progress = it.progress.copy(groupsDone = it.progress.groupsDone + 1)) }

        for (group in plan.movies) {
            coroutineContext.ensureActive()
            begin("movie", group.tmdbId)
            val target = resolve(true, group.tmdbId) { reason -> update { it.copy(report = it.report.withSkips(group.skips(reason))) } }
            if (target != null) {
                update { it.copy(progress = it.progress.copy(current = RestoreCurrent("movie", group.tmdbId, target.title))) }
                val counts = writes.writeRestoreMovie(target, group)
                update {
                    it.copy(report = (it.report + counts).copy(moviesMaterialised = it.report.moviesMaterialised + if (target.materialised) 1 else 0))
                }
            }
            done()
        }
        for (group in plan.shows) {
            coroutineContext.ensureActive()
            begin("show", group.tmdbId)
            val target = resolve(false, group.tmdbId) { reason -> update { it.copy(report = it.report.withSkips(group.skips(reason))) } }
            if (target != null) {
                update { it.copy(progress = it.progress.copy(current = RestoreCurrent("show", group.tmdbId, target.title))) }
                val (counts, skips) = writes.writeRestoreShow(target, group)
                update {
                    it.copy(
                        report = (it.report + counts).withSkips(skips)
                            .copy(showsMaterialised = it.report.showsMaterialised + if (target.materialised) 1 else 0),
                    )
                }
            }
            done()
        }
    } catch (e: CancellationException) {
        error = RESTORE_CANCELLED_MESSAGE
    } catch (e: TmdbException) {
        error = if (e.reason == TmdbException.Reason.NO_KEY) {
            "This build has no TMDB key, so titles cannot be fetched."
        } else {
            "TMDB refused the app's key, so titles cannot be fetched."
        }
    } catch (e: Exception) {
        error = RESTORE_INTERNAL_MESSAGE
    }
    withContext(NonCancellable) {
        // Titles for the recorded skips: local point reads, memoised; a miss stays without one.
        val titles = HashMap<Pair<Boolean, Long>, String?>()
        var snapshot: RestoreJob? = null
        update { it.also { job -> snapshot = job } }
        val named = snapshot?.report?.skipped.orEmpty().map { skip ->
            if (skip.title != null) return@map skip
            val movie = skip.type == "movie"
            val id = (if (movie) skip.tmdbId else skip.showTmdbId ?: skip.tmdbId) ?: return@map skip
            val title = titles.getOrPut(movie to id) { runCatching { writes.restoreTitle(movie, id) }.getOrNull() }
            skip.copy(title = title)
        }
        update {
            it.copy(
                state = if (error == null) RESTORE_DONE else RESTORE_FAILED,
                finishedAt = clock.read().now,
                progress = it.progress.copy(current = null),
                report = it.report.copy(skipped = named),
                error = error,
            )
        }
    }
}

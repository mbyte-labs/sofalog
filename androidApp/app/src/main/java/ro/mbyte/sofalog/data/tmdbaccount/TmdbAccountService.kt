package ro.mbyte.sofalog.data.tmdbaccount

import androidx.room.withTransaction
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import ro.mbyte.sofalog.data.MediaKind
import ro.mbyte.sofalog.data.PullWrite
import ro.mbyte.sofalog.data.VideoRepository
import ro.mbyte.sofalog.data.db.AppDatabase
import ro.mbyte.sofalog.data.db.SYNC_FAVORITE
import ro.mbyte.sofalog.data.db.SYNC_RATING
import ro.mbyte.sofalog.data.db.SYNC_SET
import ro.mbyte.sofalog.data.db.SYNC_WATCHLIST
import ro.mbyte.sofalog.data.db.TmdbAccountEntity
import ro.mbyte.sofalog.data.db.TmdbSyncEntity
import ro.mbyte.sofalog.data.tmdb.TmdbAccountEntry
import ro.mbyte.sofalog.data.tmdb.TmdbClient
import ro.mbyte.sofalog.data.tmdb.TmdbException
import ro.mbyte.sofalog.domain.Clock
import ro.mbyte.sofalog.domain.LinkDecision
import ro.mbyte.sofalog.domain.PULL_DONE
import ro.mbyte.sofalog.domain.PULL_FAILED
import ro.mbyte.sofalog.domain.ProviderBreaker
import ro.mbyte.sofalog.domain.SendOutcome
import ro.mbyte.sofalog.domain.SetupDraft
import ro.mbyte.sofalog.domain.SyncAction
import ro.mbyte.sofalog.domain.SyncKinds
import ro.mbyte.sofalog.domain.TmdbFailure
import ro.mbyte.sofalog.domain.TmdbPullJob
import ro.mbyte.sofalog.domain.authenticateUrl
import ro.mbyte.sofalog.domain.classifySend
import ro.mbyte.sofalog.domain.linkReturnDecision
import ro.mbyte.sofalog.domain.parseLinkReturn
import ro.mbyte.sofalog.domain.roundTmdbRating
import ro.mbyte.sofalog.domain.syncBackoff
import java.time.Duration
import java.time.Instant
import kotlin.coroutines.coroutineContext

/** The TMDB account page's whole state. */
data class TmdbLinkStatus(
    /** False when this build has no TMDB key: nothing can be linked. */
    val configured: Boolean = true,
    val account: TmdbAccountEntity? = null,
    /** Changes waiting to be sent. */
    val pending: Int = 0,
    /** A link attempt is waiting for the approval on TMDB's page. */
    val awaitingApproval: Boolean = false,
    val pull: TmdbPullJob? = null,
) {
    val linked: Boolean get() = account != null
    val broken: Boolean get() = account?.brokenAt != null

    /** The setup's import was cut short by the app closing; the setup is offered again. */
    val pullInterrupted: Boolean get() = account != null && !account.setupDone && account.pullStartedAt != null && pull == null
}

enum class SetupResult { OK, NOT_LINKED, BROKEN, ALREADY_DONE, PULL_RUNNING }

/**
 * The user's own TMDB account on the device (D-029, D-036): the link, the one-time setup, and the
 * one-way mirror of ratings, watchlist and favorites. The watch history is never sent.
 *
 * Nothing here runs in the background. The outbox is sent only while the app is on screen
 * ([startWorker] / [stopWorker] follow the activity), and the setup's import is a coroutine in the
 * app's scope that dies with the process — its end is what marks the setup done, so an import cut
 * short is simply offered again and converges.
 */
class TmdbAccountService(
    private val db: AppDatabase,
    private val tmdb: TmdbClient,
    private val repo: VideoRepository,
    private val store: TmdbSessionStore,
    private val clock: Clock,
    private val scope: CoroutineScope,
) {
    private val dao = db.tmdbSyncDao()
    private val linkLock = Mutex()
    private val kicks = Channel<Unit>(Channel.CONFLATED)
    private var worker: Job? = null
    private var pullRun: Job? = null

    private val awaiting = MutableStateFlow(store.read().pendingToken != null)
    private val pull = MutableStateFlow<TmdbPullJob?>(null)

    /** What a link attempt ended with, as a sentence for the page; consumed once shown. */
    val message = MutableStateFlow<String?>(null)

    /** Counts returns from TMDB's page, so the app can bring the account page to the front. */
    private val _returns = MutableStateFlow(0)
    val returns: StateFlow<Int> = _returns

    val status: Flow<TmdbLinkStatus> =
        combine(dao.observeAccount(), dao.observePending(), awaiting, pull) { account, pending, waiting, job ->
            TmdbLinkStatus(tmdb.hasKey, account, pending, waiting, job)
        }

    // ------------------------------------------------------------------ linking

    /**
     * Starts a link attempt and returns the page the user approves it on. Throws
     * [TmdbException] when TMDB cannot be reached.
     */
    suspend fun startLink(): String {
        val token = tmdb.newRequestToken()
        store.write(store.read().copy(pendingToken = token, pendingStartedAt = clock.read().now, pendingSession = null))
        awaiting.value = true
        return authenticateUrl(token)
    }

    /** The pending attempt's approval page again, or null when none is pending. */
    fun approvalUrl(): String? = store.read().pendingToken?.let(::authenticateUrl)

    fun cancelLink() {
        clearPending()
    }

    /** The app's link was opened: by TMDB's page coming back, or by anything else that knows the scheme. */
    fun onLinkReturn(uri: String?) {
        val returned = parseLinkReturn(uri) ?: return
        _returns.update { it + 1 }
        scope.launch { finishLink(manual = false, returned = returned) }
    }

    /**
     * Tries to finish a pending attempt. Run when the page comes back to the front ([manual]
     * false: stays quiet when the user simply has not approved yet) and from its button. One at a
     * time, so the single-use token is never exchanged twice.
     */
    suspend fun finishLink(manual: Boolean, returned: ro.mbyte.sofalog.domain.LinkReturn? = null) = linkLock.withLock {
        val secrets = store.read()
        val now = Instant.parse(clock.read().now)
        when (linkReturnDecision(secrets.pendingToken, secrets.pendingStartedAt, now, returned)) {
            LinkDecision.IGNORE -> return@withLock
            LinkDecision.EXPIRED -> {
                clearPending()
                message.value = "That link attempt expired. Please try again."
                return@withLock
            }
            LinkDecision.DENIED -> {
                clearPending()
                message.value = "TMDB did not approve the link. Nothing was changed."
                return@withLock
            }
            LinkDecision.EXCHANGE -> Unit
        }
        try {
            // A session already exchanged survives a failed account read: the token is spent.
            val session = secrets.pendingSession ?: tmdb.createSession(checkNotNull(secrets.pendingToken)).also {
                store.write(secrets.copy(pendingSession = it))
            }
            val account = tmdb.account(session)
            link(session, account.id, account.username)
            message.value = "Your TMDB account is linked."
            kick()
        } catch (e: TmdbException) {
            when (e.reason) {
                TmdbException.Reason.SESSION_INVALID ->
                    if (manual) message.value = "TMDB has not approved the link yet. Approve it in the browser, then come back."
                TmdbException.Reason.UNAVAILABLE -> if (manual || returned != null) message.value = "Could not reach TMDB. Try again shortly."
                else -> {
                    clearPending()
                    message.value = "Linking failed. Please try again."
                }
            }
        }
    }

    private suspend fun link(session: String, accountId: Long, username: String) {
        val now = clock.read().now
        val replaced = store.read().session
        val previous = dao.account()
        db.withTransaction {
            if (previous != null && previous.tmdbAccountId == accountId) {
                // The same account again: the setup and the switches stand.
                dao.upsertAccount(previous.copy(tmdbUsername = username, brokenAt = null))
            } else {
                dao.upsertAccount(TmdbAccountEntity(tmdbAccountId = accountId, tmdbUsername = username, linkedAt = now))
                dao.deleteAllSync()
            }
        }
        store.write(TmdbLinkSecrets(session = session))
        awaiting.value = false
        pull.value = null
        if (replaced != null && replaced != session && previous?.brokenAt == null) revoke(replaced)
    }

    private fun clearPending() {
        store.update { it.copy(pendingToken = null, pendingStartedAt = null, pendingSession = null) }
        awaiting.value = false
    }

    /**
     * Stops the sending and forgets the link. What is on TMDB stays exactly as it is.
     */
    suspend fun unlink() {
        pullRun?.cancel()
        val account = dao.account()
        db.withTransaction {
            dao.deleteAllSync()
            dao.deleteAccount()
        }
        val session = store.read().session
        store.write(TmdbLinkSecrets())
        awaiting.value = false
        pull.value = null
        if (session != null && account?.brokenAt == null) revoke(session)
    }

    private suspend fun revoke(session: String) {
        try {
            tmdb.deleteSession(session)
        } catch (e: TmdbException) {
            // Best effort: the session is forgotten here either way.
        }
    }

    /**
     * Run once at startup. A link whose session cannot be read — the database came from a backup,
     * or the keystore lost its key — is marked broken, so the page asks to link again; a session
     * with no link is dropped.
     */
    suspend fun reconcile() {
        val account = dao.account()
        val session = store.read().session
        if (account != null && session == null && account.brokenAt == null) markBroken()
        if (account == null && session != null) store.update { it.copy(session = null) }
    }

    private suspend fun markBroken() {
        val now = clock.read().now
        db.withTransaction {
            dao.markBroken(now)
            dao.deleteAllSync()
        }
        store.update { it.copy(session = null) }
    }

    // ------------------------------------------------------------------ setup and switches

    /**
     * The one-time answer, per kind: import TMDB's, send ours, or neither — and whether to keep it
     * in sync afterwards. Switches and the Push rows land in one transaction. A Push with its
     * switch off still sends once.
     */
    suspend fun setup(draft: SetupDraft): SetupResult {
        val account = dao.account() ?: return SetupResult.NOT_LINKED
        if (account.brokenAt != null) return SetupResult.BROKEN
        if (account.setupDone) return SetupResult.ALREADY_DONE
        if (pullRun?.isActive == true) return SetupResult.PULL_RUNNING
        val now = clock.read().now
        db.withTransaction {
            dao.setKinds(draft.ratings.sync, draft.watchlist.sync, draft.favorites.sync)
            if (draft.ratings.action == SyncAction.PUSH) {
                dao.backfillMovieRatings(now)
                dao.backfillShowRatings(now)
                dao.backfillEpisodeRatings(now)
            }
            if (draft.watchlist.action == SyncAction.PUSH) {
                dao.backfillMovieWatchlist(now)
                dao.backfillShowWatchlist(now)
            }
            if (draft.favorites.action == SyncAction.PUSH) {
                dao.backfillMovieFavorites(now)
                dao.backfillShowFavorites(now)
            }
            if (draft.pulls) dao.setPullStartedAt(now) else dao.setSetupDone()
        }
        if (draft.pushes) kick()
        if (draft.pulls) {
            pull.value = TmdbPullJob()
            pullRun = scope.launch { runPull(draft, account.tmdbAccountId) }
        }
        return SetupResult.OK
    }

    /** A switch turned on never resends what is already held; one turned off forgets what was queued. */
    suspend fun setKinds(kinds: SyncKinds) {
        db.withTransaction {
            dao.setKinds(kinds.ratings, kinds.watchlist, kinds.favorites)
            if (!kinds.ratings) dao.deleteSyncKind(SYNC_RATING)
            if (!kinds.watchlist) dao.deleteSyncKind(SYNC_WATCHLIST)
            if (!kinds.favorites) dao.deleteSyncKind(SYNC_FAVORITE)
        }
    }

    // ------------------------------------------------------------------ the setup's import

    private suspend fun runPull(draft: SetupDraft, accountId: Long) {
        var error: String? = null
        try {
            val session = store.read().session ?: throw TmdbException(TmdbException.Reason.SESSION_INVALID)
            val breaker = ProviderBreaker()
            if (draft.ratings.action == SyncAction.PULL) {
                pullList(session, accountId, breaker, "ratings", "movies", "rated", "movies") { e ->
                    counted(repo.pullRating(MediaKind.MOVIE, e.id, roundTmdbRating(e.rating ?: 0.0)), MediaKind.MOVIE) { it.copy(ratings = it.ratings + 1) }
                }
                pullList(session, accountId, breaker, "ratings", "shows", "rated", "tv") { e ->
                    counted(repo.pullRating(MediaKind.SHOW, e.id, roundTmdbRating(e.rating ?: 0.0)), MediaKind.SHOW) { it.copy(ratings = it.ratings + 1) }
                }
                pullList(session, accountId, breaker, "ratings", "episodes", "rated", "tv/episodes") { e ->
                    val show = e.showId
                    val season = e.seasonNumber
                    val number = e.episodeNumber
                    if (show == null || season == null || number == null) {
                        skipped()
                    } else {
                        counted(repo.pullEpisodeRating(show, season, number, roundTmdbRating(e.rating ?: 0.0)), MediaKind.SHOW) {
                            it.copy(episodeRatings = it.episodeRatings + 1)
                        }
                    }
                }
            }
            if (draft.watchlist.action == SyncAction.PULL) {
                pullList(session, accountId, breaker, "watchlist", "movies", "watchlist", "movies") { e ->
                    counted(repo.pullListed(false, MediaKind.MOVIE, e.id), MediaKind.MOVIE) { it.copy(watchlist = it.watchlist + 1) }
                }
                pullList(session, accountId, breaker, "watchlist", "shows", "watchlist", "tv") { e ->
                    counted(repo.pullListed(false, MediaKind.SHOW, e.id), MediaKind.SHOW) { it.copy(watchlist = it.watchlist + 1) }
                }
            }
            if (draft.favorites.action == SyncAction.PULL) {
                pullList(session, accountId, breaker, "favorites", "movies", "favorite", "movies") { e ->
                    counted(repo.pullListed(true, MediaKind.MOVIE, e.id), MediaKind.MOVIE) { it.copy(favorites = it.favorites + 1) }
                }
                pullList(session, accountId, breaker, "favorites", "shows", "favorite", "tv") { e ->
                    counted(repo.pullListed(true, MediaKind.SHOW, e.id), MediaKind.SHOW) { it.copy(favorites = it.favorites + 1) }
                }
            }
            // The END of the import is what marks the setup done.
            dao.setSetupDone()
        } catch (e: CancellationException) {
            error = "cancelled"
        } catch (e: TmdbException) {
            error = if (e.reason == TmdbException.Reason.SESSION_INVALID) "session_invalid" else "provider_unavailable"
        } catch (e: Exception) {
            error = "internal"
        }
        withContext(NonCancellable) {
            if (error == "session_invalid") markBroken()
            // A failed import leaves the setup to be run again, without the "app was closed" note.
            if (error != null) dao.setPullStartedAt(null)
            pull.update { it?.copy(state = if (error == null) PULL_DONE else PULL_FAILED, section = null, error = error) }
        }
    }

    /**
     * Pages one account list and hands each entry to [take]. A page that cannot be read fails the
     * import; one ENTRY that cannot be fetched is a skip, and after three such outages in a row
     * the rest of the entries that would need a fetch are skipped without asking.
     */
    private suspend fun pullList(
        session: String,
        accountId: Long,
        breaker: ProviderBreaker,
        section: String,
        label: String,
        list: String,
        media: String,
        take: suspend (TmdbAccountEntry) -> Unit,
    ) {
        var page = 1
        while (true) {
            coroutineContext.ensureActive()
            val result = tmdb.accountList(session, accountId, list, media, page)
            pull.update { it?.copy(section = section, list = label, page = page, totalPages = result.totalPages) }
            for (entry in result.results) {
                coroutineContext.ensureActive()
                pull.update { it?.copy(entries = it.entries + 1) }
                if (breaker.open) {
                    skipped()
                    continue
                }
                try {
                    take(entry)
                    breaker.observe(false)
                } catch (e: TmdbException) {
                    if (e.reason == TmdbException.Reason.NO_KEY || e.reason == TmdbException.Reason.BAD_KEY) throw e
                    breaker.observe(e.reason == TmdbException.Reason.UNAVAILABLE)
                    skipped()
                } catch (e: NoSuchElementException) {
                    skipped()
                }
            }
            if (page >= result.totalPages || page >= MAX_PAGES) return
            page++
        }
    }

    private fun skipped() = pull.update { it?.copy(report = it.report.copy(skipped = it.report.skipped + 1)) }

    private fun counted(write: PullWrite, kind: MediaKind, count: (ro.mbyte.sofalog.domain.TmdbPullReport) -> ro.mbyte.sofalog.domain.TmdbPullReport) =
        pull.update { job ->
            job?.copy(
                report = job.report.let { r ->
                    val added = if (!write.materialised) r else if (kind == MediaKind.MOVIE) r.copy(moviesAdded = r.moviesAdded + 1) else r.copy(showsAdded = r.showsAdded + 1)
                    when {
                        write.episodeMissing -> added.copy(skipped = added.skipped + 1)
                        write.created -> count(added)
                        else -> added
                    }
                },
            )
        }

    // ------------------------------------------------------------------ the outbox worker

    /** Wakes the worker: after a write that queued something, and when the app comes on screen. */
    fun kick() {
        kicks.trySend(Unit)
    }

    /** Starts sending while the app is on screen. */
    @Synchronized
    fun startWorker() {
        if (worker?.isActive == true) return
        worker = scope.launch {
            while (true) {
                do {
                    val report = runBatch()
                } while (!report.aborted && report.total == BATCH)
                // Sleep until a kick, or until the earliest retry falls due.
                val retry = dao.nextRetryAt()?.let { runCatching { Instant.parse(it) }.getOrNull() }
                val wait = retry?.let { Duration.between(Instant.now(), it).toMillis().coerceIn(1_000, IDLE_MS) } ?: IDLE_MS
                withTimeoutOrNull(wait) { kicks.receive() }
            }
        }
        kick()
    }

    /** Stops sending when the app leaves the screen. A row in flight still settles. */
    @Synchronized
    fun stopWorker() {
        worker?.cancel()
        worker = null
    }

    data class SyncReport(val total: Int = 0, val sent: Int = 0, val aborted: Boolean = false)

    /**
     * Sends one batch of due rows. Success deletes the row; an outage defers it with backoff and,
     * three in a row, ends the batch; a refused session marks the link broken and empties the
     * queue; a title TMDB does not know, or a body it refuses, is dropped for good.
     */
    suspend fun runBatch(): SyncReport {
        if (!tmdb.hasKey) return SyncReport()
        val account = dao.account()?.takeIf { it.brokenAt == null } ?: return SyncReport()
        val session = store.read().session ?: return SyncReport()
        val rows = dao.due(clock.read().now, BATCH)
        val breaker = ProviderBreaker()
        var sent = 0
        for (row in rows) {
            if (breaker.open) return SyncReport(rows.size, sent, aborted = true)
            coroutineContext.ensureActive()
            // A send and its settling are one step: leaving the screen must not split them.
            val outcome = withContext(NonCancellable) {
                val failure = send(session, account.tmdbAccountId, row)
                breaker.observe(failure == TmdbFailure.UNAVAILABLE)
                val outcome = classifySend(failure)
                when (outcome) {
                    SendOutcome.SENT, SendOutcome.DROP -> dao.settle(row.id, row.updatedAt, row.op, row.rating)
                    SendOutcome.DEFER -> {
                        val attempts = row.attempts + 1
                        val retryAt = Instant.parse(clock.read().now).plus(syncBackoff(attempts)).toString()
                        dao.defer(row.id, row.updatedAt, row.op, row.rating, attempts, retryAt, failure?.name.orEmpty())
                    }
                    SendOutcome.BROKEN -> markBroken()
                }
                outcome
            }
            if (outcome == SendOutcome.SENT) sent++
            if (outcome == SendOutcome.BROKEN) return SyncReport(rows.size, sent, aborted = true)
        }
        return SyncReport(rows.size, sent)
    }

    /** Null on success; otherwise what the failure means. Never the message: it could name the URL. */
    private suspend fun send(session: String, accountId: Long, row: TmdbSyncEntity): TmdbFailure? = try {
        val on = row.op == SYNC_SET
        when (row.kind) {
            SYNC_WATCHLIST -> tmdb.setWatchlist(session, accountId, row.targetType, row.tmdbId, on)
            SYNC_FAVORITE -> tmdb.setFavorite(session, accountId, row.targetType, row.tmdbId, on)
            else -> {
                val rating = row.rating
                if (on && rating != null) {
                    tmdb.setRating(session, row.targetType, row.tmdbId, row.seasonNumber, row.episodeNumber, rating)
                } else {
                    tmdb.deleteRating(session, row.targetType, row.tmdbId, row.seasonNumber, row.episodeNumber)
                }
            }
        }
        null
    } catch (e: TmdbException) {
        when (e.reason) {
            TmdbException.Reason.SESSION_INVALID -> TmdbFailure.SESSION_INVALID
            TmdbException.Reason.NOT_FOUND -> TmdbFailure.NOT_FOUND
            TmdbException.Reason.REFUSED -> TmdbFailure.REFUSED
            TmdbException.Reason.BAD_KEY, TmdbException.Reason.NO_KEY -> TmdbFailure.BAD_KEY
            TmdbException.Reason.UNAVAILABLE -> TmdbFailure.UNAVAILABLE
        }
    }

    private companion object {
        const val BATCH = 50
        const val IDLE_MS = 60_000L
        const val MAX_PAGES = 500
    }
}

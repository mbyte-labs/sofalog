package ro.mbyte.sofalog.ui.details

import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.coerceAtLeast
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.lifecycle.viewmodel.initializer
import androidx.lifecycle.viewmodel.viewModelFactory
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import ro.mbyte.sofalog.data.CopyKind
import ro.mbyte.sofalog.data.ShowBundle
import ro.mbyte.sofalog.data.ShowUserState
import ro.mbyte.sofalog.data.VideoRepository
import ro.mbyte.sofalog.domain.SeasonDetails
import ro.mbyte.sofalog.domain.ShowDetails
import ro.mbyte.sofalog.domain.ShowStatus
import ro.mbyte.sofalog.domain.episodeMarker
import ro.mbyte.sofalog.domain.parseSeasonDetails
import ro.mbyte.sofalog.domain.parseShowDetails
import ro.mbyte.sofalog.ui.common.CenteredLoading
import ro.mbyte.sofalog.ui.common.CenteredMessage
import ro.mbyte.sofalog.ui.common.WatchLogForm
import ro.mbyte.sofalog.ui.common.attempt
import ro.mbyte.sofalog.ui.common.formatDay
import ro.mbyte.sofalog.ui.common.formatWatchInstant
import ro.mbyte.sofalog.ui.common.repository
import ro.mbyte.sofalog.ui.common.userMessage
import ro.mbyte.sofalog.ui.details.parts.CastSection
import ro.mbyte.sofalog.ui.details.parts.ChoiceSheet
import ro.mbyte.sofalog.ui.details.parts.CopyTileState
import ro.mbyte.sofalog.ui.details.parts.DetailHero
import ro.mbyte.sofalog.ui.details.parts.DroppedNotice
import ro.mbyte.sofalog.ui.details.parts.HistorySection
import ro.mbyte.sofalog.ui.details.parts.Note
import ro.mbyte.sofalog.ui.details.parts.OverlaidTopBar
import ro.mbyte.sofalog.ui.details.parts.RatingsSection
import ro.mbyte.sofalog.ui.details.parts.SheetAction
import ro.mbyte.sofalog.ui.details.parts.StatusBadges
import ro.mbyte.sofalog.ui.details.parts.TimelineEntry
import ro.mbyte.sofalog.ui.details.parts.WatchNowSection
import ro.mbyte.sofalog.ui.details.parts.fetchedDay
import ro.mbyte.sofalog.ui.details.parts.formatRuntime
import ro.mbyte.sofalog.ui.details.parts.rememberHeroCollapse
import java.time.Instant
import java.util.Locale

data class EpisodeUiState(
    val bundle: ShowBundle? = null,
    /** The owning season's stored body: this episode's score, votes and guests, and the season's regulars. */
    val details: SeasonDetails = SeasonDetails.EMPTY,
    /** The owning show's read — an episode borrows its streaming listing. */
    val show: ShowDetails = ShowDetails.EMPTY,
    val user: ShowUserState = ShowUserState(),
    /** A rating asked for and not yet stored; it wins over the stored one while it is up. */
    val pendingRating: PendingRating? = null,
    val loadError: String? = null,
    val busy: Boolean = false,
    val message: String? = null,
)

/** Wrapped, because "cleared" (null) is itself a pending value. */
data class PendingRating(val rating: Int?)

private data class EpisodeExtras(
    val pendingRating: PendingRating? = null,
    val loadError: String? = null,
    val busy: Boolean = false,
    val message: String? = null,
)

/**
 * Backs the episode page. It reads what the season page reads — the show's rows and the season's
 * stored body — and never fetches an episode of its own: a tracked show renders from the database
 * and stays live, an untracked one is the season's preview, and the first mark or rating tracks
 * the whole show. Every write names the episode by the show's TMDB id and its position.
 */
class EpisodeViewModel(
    private val repo: VideoRepository,
    private val tmdbId: Long,
    private val seasonNumber: Int,
    private val episodeNumber: Int,
) : ViewModel() {
    private val preview = MutableStateFlow<ShowBundle?>(null)
    private val extras = MutableStateFlow(EpisodeExtras())

    /** Rating changes, latest wins: sliding along the bar asks for many and only the last matters. */
    private val ratings = Channel<Int?>(Channel.CONFLATED)
    private var ratingsAsked = 0

    private val bundle = combine(repo.observeShow(tmdbId), preview) { local, live -> local ?: live }

    /** Parsed once per stored body, not once per watch: the bundle re-emits on every mark. */
    private val details = bundle
        .map { it?.seasons?.firstOrNull { season -> season.seasonNumber == seasonNumber }?.metadataJson }
        .distinctUntilChanged()
        .map(::parseSeasonDetails)
        .flowOn(Dispatchers.Default)

    private val show = bundle.map { it?.show?.metadataJson }.distinctUntilChanged()
        .map { it?.let(::parseShowDetails) ?: ShowDetails.EMPTY }
        .flowOn(Dispatchers.Default)

    val state: StateFlow<EpisodeUiState> = combine(
        bundle, details, show, repo.observeShowUserState(tmdbId), extras,
    ) { held, season, parent, user, extra ->
        EpisodeUiState(held, season, parent, user, extra.pendingRating, extra.loadError, extra.busy, extra.message)
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), EpisodeUiState())

    init {
        viewModelScope.launch {
            if (repo.observeShow(tmdbId).first() == null) {
                attempt { repo.previewShow(tmdbId, seasonNumber) }
                    .onSuccess { preview.value = it }
                    .onFailure { e -> extras.value = extras.value.copy(loadError = e.userMessage()) }
            } else {
                // Best effort, both: a tracked show keeps rendering from its snapshot when TMDB is
                // down. The season's refresh is what brings in the regulars and the guest stars of
                // a season stored before they were kept.
                attempt { repo.refreshShowIfStale(tmdbId) }
                attempt { repo.refreshSeasonIfStale(tmdbId, seasonNumber) }
            }
        }
        viewModelScope.launch {
            for (rating in ratings) {
                val asked = ratingsAsked
                val failure = attempt { repo.rateEpisode(tmdbId, seasonNumber, episodeNumber, rating) }.exceptionOrNull()
                // A newer request is already queued: its pending value stays up.
                if (asked == ratingsAsked) {
                    extras.value = extras.value.copy(pendingRating = null, message = failure?.userMessage() ?: extras.value.message)
                }
            }
        }
    }

    fun rate(rating: Int?) {
        ratingsAsked++
        extras.value = extras.value.copy(pendingRating = PendingRating(rating))
        ratings.trySend(rating)
    }

    fun logWatch(watchedAt: Instant?, rating: Int?) =
        act { repo.markEpisodeWatched(tmdbId, seasonNumber, episodeNumber, watchedAt, rating); null }

    fun deleteWatch(id: Long) = act { repo.deleteWatch(id); null }

    fun messageShown() {
        extras.value = extras.value.copy(message = null)
    }

    /** One write at a time. [block] returns a word for the user, or null when the result speaks for itself. */
    private fun act(block: suspend () -> String?) {
        if (extras.value.busy) return
        extras.value = extras.value.copy(busy = true)
        viewModelScope.launch {
            val result = attempt { block() }
            extras.value = extras.value.copy(busy = false, message = result.fold({ it }, { it.userMessage() }))
        }
    }
}

/** Which sheet the page has open — one at a time. */
private sealed interface EpisodeOverlay {
    data object Log : EpisodeOverlay
    data class RemoveWatch(val entry: TimelineEntry) : EpisodeOverlay

    /** A copy tile only reports here: copies are the season's to change. */
    data class CopyInfo(val kind: CopyKind, val owned: Boolean) : EpisodeOverlay
}

/**
 * The episode page — the web page's sections (EpisodeDetailsPage.svelte) laid out the way the show
 * and season pages are: the still as the band under a clear app bar, every choice a bottom sheet.
 * A phone stacks the sections; from 840dp the paired blocks go side by side and the text keeps a
 * readable measure.
 *
 * An episode is rated and watched itself, but owns nothing else: its copies are its season's and
 * its streaming listing its show's, both shown read-only, and its cast is the season's regulars
 * beside its own guest stars.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun EpisodeScreen(
    tmdbId: Long,
    seasonNumber: Int,
    episodeNumber: Int,
    onMenu: () -> Unit,
    onOpenShow: () -> Unit,
    onOpenSeason: () -> Unit,
) {
    val repo = repository()
    val vm: EpisodeViewModel = viewModel(
        factory = viewModelFactory { initializer { EpisodeViewModel(repo, tmdbId, seasonNumber, episodeNumber) } },
    )
    val state by vm.state.collectAsStateWithLifecycle()
    var overlay by remember { mutableStateOf<EpisodeOverlay?>(null) }
    val bundle = state.bundle
    val show = bundle?.show
    val season = bundle?.seasons?.firstOrNull { it.seasonNumber == seasonNumber }
    val seasonName = season?.name ?: if (seasonNumber == 0) "Specials" else "Season $seasonNumber"
    // By position, never by id: a preview's rows have none.
    val episode = bundle?.episodes?.firstOrNull { it.seasonNumber == seasonNumber && it.episodeNumber == episodeNumber }
    val code = "S%02dE%02d".format(Locale.ENGLISH, seasonNumber, episodeNumber)
    val title = episode?.title ?: "Episode $episodeNumber"

    val snackbar = remember { SnackbarHostState() }
    val list = rememberLazyListState()
    LaunchedEffect(state.message) {
        state.message?.let { snackbar.showSnackbar(it); vm.messageShown() }
    }

    val tracked = bundle?.tracked == true
    // An untracked episode has no row to hang a watch or a rating on.
    val held = episode?.takeIf { tracked }
    val watches = held?.let { bundle?.watches?.get(it.id) }.orEmpty()
    val timeline = remember(watches) { watches.map { TimelineEntry(it.id, it.watchedAt) } }
    val watched = watches.isNotEmpty()
    val storedRating = held?.let { state.user.episodeRatings[it.id] }
    val pending = state.pendingRating
    val userRating = if (pending != null) pending.rating else storedRating
    val copies = state.user.seasonCopies[seasonNumber].orEmpty()

    Scaffold(snackbarHost = { SnackbarHost(snackbar) }, contentWindowInsets = WindowInsets(0, 0, 0, 0)) { padding ->
        BoxWithConstraints(Modifier.fillMaxSize().padding(padding)) {
            val wide = maxWidth >= 840.dp
            val page = if (wide) 32.dp else 16.dp
            val bandHeight = minOf(maxWidth * 9 / 16, maxHeight * 0.45f).coerceAtLeast(180.dp)
            when {
                bundle == null && state.loadError != null -> CenteredMessage(state.loadError!!)
                bundle == null || show == null -> CenteredLoading()
                episode == null -> CenteredMessage("This episode could not be found.")
                else -> LazyColumn(
                    Modifier.fillMaxSize(),
                    state = list,
                    contentPadding = PaddingValues(
                        bottom = WindowInsets.navigationBars.asPaddingValues().calculateBottomPadding() + 32.dp,
                    ),
                ) {
                    val enabled = !state.busy
                    item(key = "hero") {
                        DetailHero(
                            title = title,
                            // Only what a missing still falls back on, blurred: an episode has no poster.
                            posterPath = season?.posterPath ?: show.posterPath,
                            backdropPath = episode.stillPath ?: show.backdropPath,
                            facts = listOfNotNull(
                                code,
                                formatDay(episode.airDate),
                                episode.runtime?.takeIf { it > 0 }?.let(::formatRuntime),
                                episodeMarker(episode.episodeType, episode.episodeNumber)?.label,
                            ),
                            byline = null,
                            genres = null,
                            tagline = null,
                            overview = episode.overview,
                            noOverview = "TMDB has no overview for this episode.",
                            wide = wide,
                            page = page,
                            bandHeight = bandHeight,
                            overline = show.title,
                            onOverline = onOpenShow,
                            overline2 = seasonName,
                            onOverline2 = onOpenSeason,
                            poster = false,
                        )
                    }
                    if (bundle.status == ShowStatus.DROPPED) {
                        item(key = "dropped") { DroppedNotice(show.title, page, onOpenShow) }
                    }
                    item(key = "ratings") {
                        val votes = state.details.episodeVotes[episodeNumber]
                        RatingsSection(
                            voteAverage = state.details.episodeScores[episodeNumber],
                            votes = votes,
                            userRating = userRating,
                            onRate = vm::rate,
                            wide = wide,
                            page = page,
                            scoreCaption = "episode score".takeIf { votes == null },
                        )
                    }
                    item(key = "watch-now") {
                        WatchNowSection(
                            tiles = CopyKind.entries.map { CopyTileState(it, owned = it in copies) },
                            streaming = state.show.streaming,
                            subject = "show",
                            enabled = true,
                            onCopyTap = { overlay = EpisodeOverlay.CopyInfo(it, it in copies) },
                            wide = wide,
                            page = page,
                            copiesLabel = "Your copies of this season",
                        )
                    }
                    item(key = "history") {
                        HistorySection(
                            entries = timeline,
                            watched = watched,
                            watchlisted = false,
                            favorited = false,
                            enabled = enabled,
                            onEntryTap = { overlay = EpisodeOverlay.RemoveWatch(it) },
                            onLogWatch = { overlay = EpisodeOverlay.Log },
                            onWatchlist = null,
                            onFavorite = null,
                            wide = wide,
                            page = page,
                            emptyNote = "You have not watched this episode yet.",
                            hint = "Logging this the first time downloads the whole show for offline use.".takeIf { !tracked },
                        )
                    }
                    item(key = "cast") {
                        CastSection(show.title, state.details.regulars, "TMDB lists no regular cast for this season.", wide, page)
                    }
                    item(key = "guests") {
                        CastSection(
                            title = show.title,
                            cast = state.details.guestStars[episodeNumber].orEmpty(),
                            emptyNote = "TMDB lists no guest stars for this episode.",
                            wide = wide,
                            page = page,
                            label = "Guest stars",
                        )
                    }
                    fetchedDay(season?.fetchedAt ?: show.fetchedAt)?.let { day ->
                        item(key = "provenance") {
                            Note("Snapshot as of $day.", Modifier.padding(start = page, end = page, top = 28.dp))
                        }
                    }
                }
            }
            val collapsed by rememberHeroCollapse(list, loaded = show != null && episode != null, bandHeight = bandHeight)
            OverlaidTopBar(if (show == null) code else "${show.title} — $code", collapsed, wide, onMenu) {
                if (episode != null) {
                    StatusBadges(
                        watched = watched,
                        watchlisted = false,
                        favorited = false,
                        dropped = bundle?.status == ShowStatus.DROPPED,
                    )
                }
            }
        }
    }

    val close = { overlay = null }
    val subject = listOfNotNull(show?.title, seasonName).joinToString(" — ")
    val label = "E%02d %s".format(Locale.ENGLISH, episodeNumber, title)
    when (val open = overlay) {
        null -> Unit
        is EpisodeOverlay.Log -> ModalBottomSheet(
            onDismissRequest = close,
            sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        ) {
            WatchLogForm(
                title = subject,
                subtitle = label,
                airDate = episode?.airDate,
                onConfirm = { watchedAt, rating ->
                    vm.logWatch(watchedAt, rating)
                    close()
                },
                modifier = Modifier.padding(start = 24.dp, end = 24.dp, bottom = 24.dp),
                initialRating = userRating,
            )
        }
        is EpisodeOverlay.RemoveWatch -> ChoiceSheet(
            title = "Remove this watch?",
            subject = "$subject · $label",
            note = "Watched ${formatWatchInstant(open.entry.watchedAt)}. This cannot be undone.",
            actions = listOf(
                SheetAction("Remove this watch", danger = true) {
                    vm.deleteWatch(open.entry.id)
                    close()
                },
            ),
            onDismiss = close,
        )
        is EpisodeOverlay.CopyInfo -> ChoiceSheet(
            title = if (open.owned) "Owned on ${open.kind.label}" else "Not owned on ${open.kind.label}",
            subject = subject,
            note = "Copies are recorded per season, not per episode — add or remove them from the season page.",
            actions = listOf(
                SheetAction("Go to $seasonName") {
                    close()
                    onOpenSeason()
                },
            ),
            onDismiss = close,
        )
    }
}

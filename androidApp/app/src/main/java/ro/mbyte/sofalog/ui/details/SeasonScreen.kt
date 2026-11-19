package ro.mbyte.sofalog.ui.details

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
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
import ro.mbyte.sofalog.data.db.EpisodeEntity
import ro.mbyte.sofalog.data.db.WatchEntity
import ro.mbyte.sofalog.domain.BulkWatchMode
import ro.mbyte.sofalog.domain.SeasonDetails
import ro.mbyte.sofalog.domain.ShowDetails
import ro.mbyte.sofalog.domain.ShowStatus
import ro.mbyte.sofalog.domain.episodeMarker
import ro.mbyte.sofalog.domain.episodeRatingMean
import ro.mbyte.sofalog.domain.parseSeasonDetails
import ro.mbyte.sofalog.domain.parseShowDetails
import ro.mbyte.sofalog.domain.usablePremiere
import ro.mbyte.sofalog.ui.common.BulkWatchForm
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
import ro.mbyte.sofalog.ui.details.parts.EpisodeAverage
import ro.mbyte.sofalog.ui.details.parts.EpisodeCard
import ro.mbyte.sofalog.ui.details.parts.HistorySection
import ro.mbyte.sofalog.ui.details.parts.Note
import ro.mbyte.sofalog.ui.details.parts.OverlaidTopBar
import ro.mbyte.sofalog.ui.details.parts.RatingsSection
import ro.mbyte.sofalog.ui.details.parts.SectionLabel
import ro.mbyte.sofalog.ui.details.parts.SheetAction
import ro.mbyte.sofalog.ui.details.parts.StatusBadges
import ro.mbyte.sofalog.ui.details.parts.TimelineEntry
import ro.mbyte.sofalog.ui.details.parts.WatchNowSection
import ro.mbyte.sofalog.ui.details.parts.fetchedDay
import ro.mbyte.sofalog.ui.details.parts.rememberHeroCollapse
import java.time.Instant
import java.util.Locale

data class SeasonUiState(
    val bundle: ShowBundle? = null,
    /** What the page reads out of the season's own stored body: its score, cast and episode scores. */
    val details: SeasonDetails = SeasonDetails.EMPTY,
    /** The owning show's read — a season borrows its streaming listing, and its score as a fallback. */
    val show: ShowDetails = ShowDetails.EMPTY,
    val user: ShowUserState = ShowUserState(),
    val loadError: String? = null,
    val busy: Boolean = false,
    val message: String? = null,
)

private data class SeasonExtras(val loadError: String? = null, val busy: Boolean = false, val message: String? = null)

/**
 * Backs the season page. A tracked show is read from the database and stays live; an untracked
 * one is a preview fetched once and never stored — the first mark or copy tracks the whole show.
 */
class SeasonViewModel(
    private val repo: VideoRepository,
    private val tmdbId: Long,
    private val seasonNumber: Int,
) : ViewModel() {
    private val preview = MutableStateFlow<ShowBundle?>(null)
    private val extras = MutableStateFlow(SeasonExtras())

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

    val state: StateFlow<SeasonUiState> = combine(
        bundle, details, show, repo.observeShowUserState(tmdbId), extras,
    ) { held, season, parent, user, extra ->
        SeasonUiState(held, season, parent, user, extra.loadError, extra.busy, extra.message)
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), SeasonUiState())

    init {
        viewModelScope.launch {
            if (repo.observeShow(tmdbId).first() == null) {
                attempt { repo.previewShow(tmdbId, seasonNumber) }
                    .onSuccess { preview.value = it }
                    .onFailure { e -> extras.value = extras.value.copy(loadError = e.userMessage()) }
            } else {
                // Best effort, both: a tracked show keeps rendering from its snapshot when TMDB is
                // down. The season's own refresh is what fills in a season stored without its body.
                attempt { repo.refreshShowIfStale(tmdbId) }
                attempt { repo.refreshSeasonIfStale(tmdbId, seasonNumber) }
            }
        }
    }

    fun markSeason(rewatch: Boolean, mode: BulkWatchMode) = act {
        val marked = repo.markAiredWatched(tmdbId, seasonNumber, rewatch = rewatch, mode = mode)
        when {
            marked > 0 -> null
            rewatch -> "Nothing to mark: no episode of this season has aired yet."
            else -> "Nothing left to mark: every aired episode is already watched."
        }
    }

    fun markEpisode(number: Int, watchedAt: Instant?, rating: Int?) =
        act { repo.markEpisodeWatched(tmdbId, seasonNumber, number, watchedAt, rating); null }

    fun setCopy(kind: CopyKind, owned: Boolean) = act { repo.setSeasonCopy(tmdbId, seasonNumber, kind, owned); null }

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
private sealed interface SeasonOverlay {
    data object MarkSeason : SeasonOverlay
    data class Copy(val kind: CopyKind, val owned: Boolean) : SeasonOverlay
    data class RemoveWatch(val entry: TimelineEntry) : SeasonOverlay

    /** The long-press fork on an episode card. */
    data class Episode(val episode: EpisodeEntity) : SeasonOverlay
    data class LogEpisode(val episode: EpisodeEntity) : SeasonOverlay
}

private fun episodes(count: Int) = if (count == 1) "1 episode" else "$count episodes"

private fun episodeLabel(episode: EpisodeEntity): String {
    val code = "E%02d".format(Locale.ENGLISH, episode.episodeNumber)
    return episode.title?.let { "$code $it" } ?: code
}

/**
 * The season page — the web page's sections (SeasonDetailsPage.svelte) laid out the way the show
 * page is: the backdrop band under a clear app bar, every choice a bottom sheet. A phone stacks
 * the sections two stills to a row; from 840dp the poster sits beside the title, paired blocks go
 * side by side and the episode grid takes as many columns as fit.
 *
 * A season is never itself rated, watchlisted or favorited: its rating slot reads the average of
 * the user's episode ratings, and its one action is the season-wide mark.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SeasonScreen(
    tmdbId: Long,
    seasonNumber: Int,
    onMenu: () -> Unit,
    onOpenEpisode: (Int) -> Unit,
    onOpenShow: () -> Unit,
) {
    val repo = repository()
    val vm: SeasonViewModel =
        viewModel(factory = viewModelFactory { initializer { SeasonViewModel(repo, tmdbId, seasonNumber) } })
    val state by vm.state.collectAsStateWithLifecycle()
    var overlay by remember { mutableStateOf<SeasonOverlay?>(null) }
    val bundle = state.bundle
    val show = bundle?.show
    val season = bundle?.seasons?.firstOrNull { it.seasonNumber == seasonNumber }
    val name = season?.name ?: if (seasonNumber == 0) "Specials" else "Season $seasonNumber"

    val snackbar = remember { SnackbarHostState() }
    val list = rememberLazyListState()
    LaunchedEffect(state.message) {
        state.message?.let { snackbar.showSnackbar(it); vm.messageShown() }
    }

    // Everything the sections derive from the bundle, once per bundle rather than per frame.
    val episodeList = remember(bundle?.episodes) {
        bundle?.episodes.orEmpty().filter { it.seasonNumber == seasonNumber }.sortedBy { it.episodeNumber }
    }
    val today = bundle?.today.orEmpty()
    // Aired as the page shows it: dated on or before today. Specials count their own here — this
    // is display, and the bulk mark, which does not take them, is simply not offered for them.
    val aired = episodeList.filter { it.airDate != null && it.airDate <= today }
    val watchedAired = aired.count { bundle?.watches?.containsKey(it.id) == true }
    val seasonWatched = aired.isNotEmpty() && watchedAired == aired.size
    val canMark = seasonNumber > 0 && aired.isNotEmpty()
    val timeline = remember(bundle?.watches, episodeList) {
        val byId = episodeList.associateBy { it.id }
        bundle?.watches.orEmpty().filterKeys { it in byId }.values.flatten()
            .sortedWith(compareByDescending<WatchEntity> { it.watchedAt }.thenByDescending { it.id })
            .map { TimelineEntry(it.id, it.watchedAt, byId[it.targetId]?.let(::episodeLabel) ?: "Episode") }
    }
    val ratings = episodeList.mapNotNull { state.user.episodeRatings[it.id] }
    val copies = state.user.seasonCopies[seasonNumber].orEmpty()
    val tracked = bundle?.tracked == true

    Scaffold(snackbarHost = { SnackbarHost(snackbar) }, contentWindowInsets = WindowInsets(0, 0, 0, 0)) { padding ->
        BoxWithConstraints(Modifier.fillMaxSize().padding(padding)) {
            val wide = maxWidth >= 840.dp
            val page = if (wide) 32.dp else 16.dp
            val bandHeight = minOf(maxWidth * 9 / 16, maxHeight * 0.45f).coerceAtLeast(180.dp)
            // The episode grid's columns: as many stills of a comfortable width as the window holds.
            val gap = 12.dp
            val cell = if (wide) 210.dp else 150.dp
            val columns = (((maxWidth - page * 2 + gap) / (cell + gap)).toInt()).coerceAtLeast(2)
            when {
                bundle == null && state.loadError != null -> CenteredMessage(state.loadError!!)
                bundle == null || show == null -> CenteredLoading()
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
                            title = name,
                            posterPath = season?.posterPath ?: show.posterPath,
                            backdropPath = show.backdropPath,
                            facts = listOfNotNull(
                                season?.airDate?.take(4),
                                episodeList.size.takeIf { it > 0 }?.let(::episodes),
                                formatDay(season?.airDate)?.let { "Premiered $it" },
                            ),
                            byline = null,
                            genres = null,
                            tagline = null,
                            overview = season?.overview,
                            noOverview = "TMDB has no overview for this season.",
                            wide = wide,
                            page = page,
                            bandHeight = bandHeight,
                            overline = show.title,
                            onOverline = onOpenShow,
                        )
                    }
                    if (bundle.status == ShowStatus.DROPPED) {
                        item(key = "dropped") { DroppedNotice(show.title, page, onOpenShow) }
                    }
                    item(key = "ratings") {
                        RatingsSection(
                            voteAverage = state.details.score ?: state.show.seasonScores[seasonNumber],
                            votes = null,
                            userRating = null,
                            onRate = null,
                            wide = wide,
                            page = page,
                            extra = {
                                EpisodeAverage(
                                    label = "Your rating",
                                    mean = episodeRatingMean(ratings),
                                    note = "${ratings.size} of ${episodes(episodeList.size)} rated — " +
                                        "seasons aren't rated directly.",
                                    emptyNote = "Rate episodes and their average appears here — seasons aren't rated directly.",
                                )
                            },
                            scoreCaption = "season score",
                        )
                    }
                    item(key = "watch-now") {
                        WatchNowSection(
                            tiles = CopyKind.entries.map { CopyTileState(it, owned = it in copies) },
                            streaming = state.show.streaming,
                            subject = "show",
                            enabled = enabled,
                            onCopyTap = { overlay = SeasonOverlay.Copy(it, it in copies) },
                            wide = wide,
                            page = page,
                        )
                    }
                    item(key = "episodes-label") {
                        Column(Modifier.fillMaxWidth().padding(start = page, end = page, top = 28.dp)) {
                            SectionLabel("Episodes")
                            if (episodeList.isEmpty()) {
                                Note("TMDB lists no episodes for this season.", Modifier.padding(top = 12.dp))
                            }
                        }
                    }
                    items(episodeList.chunked(columns), key = { "episodes-${it.first().episodeNumber}" }) { row ->
                        Row(
                            Modifier.fillMaxWidth().padding(start = page, end = page, top = 14.dp),
                            horizontalArrangement = Arrangement.spacedBy(gap),
                        ) {
                            row.forEach { episode ->
                                EpisodeCard(
                                    showTitle = show.title,
                                    number = episode.episodeNumber,
                                    title = episode.title,
                                    airDate = episode.airDate,
                                    runtime = episode.runtime,
                                    stillPath = episode.stillPath,
                                    watched = bundle.watches.containsKey(episode.id),
                                    userRating = state.user.episodeRatings[episode.id],
                                    score = state.details.episodeScores[episode.episodeNumber],
                                    marker = episodeMarker(episode.episodeType, episode.episodeNumber),
                                    modifier = Modifier.weight(1f),
                                    onClick = { onOpenEpisode(episode.episodeNumber) },
                                    onLongClick = { if (enabled) overlay = SeasonOverlay.Episode(episode) },
                                )
                            }
                            // A short last row keeps the column width of the full ones.
                            repeat(columns - row.size) { Spacer(Modifier.weight(1f)) }
                        }
                    }
                    item(key = "history") {
                        HistorySection(
                            entries = timeline,
                            watched = seasonWatched,
                            watchlisted = false,
                            favorited = false,
                            enabled = enabled,
                            onEntryTap = { overlay = SeasonOverlay.RemoveWatch(it) },
                            // Specials are never bulk-marked, and a season with nothing aired has nothing to mark.
                            onLogWatch = if (canMark) ({ overlay = SeasonOverlay.MarkSeason }) else null,
                            onWatchlist = null,
                            onFavorite = null,
                            wide = wide,
                            page = page,
                            emptyNote = "You have not watched any episode of this season yet.",
                            hint = when {
                                !canMark -> "Hold an episode to mark it as watched."
                                tracked -> "Marks every aired episode of this season. Hold an episode to mark just that one."
                                else -> "Marking downloads the whole show for offline use, and marks every aired episode " +
                                    "of this season. Hold an episode to mark just that one."
                            },
                        )
                    }
                    item(key = "cast") {
                        CastSection(show.title, state.details.cast, "TMDB lists no cast for this season.", wide, page)
                    }
                    fetchedDay(season?.fetchedAt ?: show.fetchedAt)?.let { day ->
                        item(key = "provenance") {
                            Note("Snapshot as of $day.", Modifier.padding(start = page, end = page, top = 28.dp))
                        }
                    }
                }
            }
            val collapsed by rememberHeroCollapse(list, loaded = show != null, bandHeight = bandHeight)
            OverlaidTopBar(if (show == null) name else "${show.title} — $name", collapsed, wide, onMenu) {
                if (show != null) {
                    StatusBadges(
                        watched = seasonWatched,
                        watchlisted = false,
                        favorited = false,
                        dropped = bundle.status == ShowStatus.DROPPED,
                    )
                }
            }
        }
    }

    val close = { overlay = null }
    val subject = listOfNotNull(show?.title, name).joinToString(" — ")
    val sheet = Modifier.padding(start = 24.dp, end = 24.dp, bottom = 24.dp)
    when (val open = overlay) {
        null -> Unit
        is SeasonOverlay.MarkSeason -> if (bundle != null) {
            ModalBottomSheet(onDismissRequest = close, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
                BulkWatchForm(
                    title = "Mark season as watched",
                    subtitle = subject,
                    watched = watchedAired,
                    aired = aired.size,
                    seasonsNeedingDate = if (usablePremiere(season?.airDate, bundle.today)) 0 else 1,
                    onConfirm = { rewatch, mode ->
                        vm.markSeason(rewatch, mode)
                        close()
                    },
                    modifier = sheet,
                    note = if (tracked) null else "Marking downloads the whole show for offline use.",
                )
            }
        }
        is SeasonOverlay.Copy -> {
            val verb = if (open.owned) "Remove" else "Add"
            ChoiceSheet(
                title = "$verb ${open.kind.label} copy?",
                subject = subject,
                note = "Adding a copy downloads the whole show for offline use.".takeIf { !tracked && !open.owned },
                actions = listOf(
                    SheetAction("$verb ${open.kind.label} copy", danger = open.owned) {
                        vm.setCopy(open.kind, !open.owned)
                        close()
                    },
                ),
                onDismiss = close,
            )
        }
        is SeasonOverlay.RemoveWatch -> ChoiceSheet(
            title = "Remove this watch?",
            subject = listOfNotNull(subject, open.entry.title).joinToString(" · "),
            note = "Watched ${formatWatchInstant(open.entry.watchedAt)}. This cannot be undone.",
            actions = listOf(
                SheetAction("Remove this watch", danger = true) {
                    vm.deleteWatch(open.entry.id)
                    close()
                },
            ),
            onDismiss = close,
        )
        is SeasonOverlay.Episode -> ChoiceSheet(
            title = episodeLabel(open.episode),
            subject = subject,
            note = null,
            actions = listOf(
                SheetAction(
                    if (bundle?.watches?.containsKey(open.episode.id) == true) "Log another watch" else "Mark as watched",
                ) { overlay = SeasonOverlay.LogEpisode(open.episode) },
                SheetAction("Episode details", secondary = true) {
                    close()
                    onOpenEpisode(open.episode.episodeNumber)
                },
            ),
            onDismiss = close,
        )
        is SeasonOverlay.LogEpisode -> ModalBottomSheet(
            onDismissRequest = close,
            sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        ) {
            WatchLogForm(
                title = subject,
                subtitle = episodeLabel(open.episode),
                airDate = open.episode.airDate,
                onConfirm = { watchedAt, rating ->
                    vm.markEpisode(open.episode.episodeNumber, watchedAt, rating)
                    close()
                },
                modifier = sheet,
                initialRating = state.user.episodeRatings[open.episode.id],
            )
        }
    }
}

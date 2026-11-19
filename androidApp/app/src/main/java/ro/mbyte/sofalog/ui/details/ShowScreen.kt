package ro.mbyte.sofalog.ui.details

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.coerceAtLeast
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.lifecycle.viewmodel.initializer
import androidx.lifecycle.viewmodel.viewModelFactory
import coil3.compose.AsyncImage
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
import ro.mbyte.sofalog.data.db.SeasonEntity
import ro.mbyte.sofalog.data.db.WatchEntity
import ro.mbyte.sofalog.data.tmdb.TmdbClient
import ro.mbyte.sofalog.domain.BulkWatchMode
import ro.mbyte.sofalog.domain.EpisodeRef
import ro.mbyte.sofalog.domain.SeasonProgress
import ro.mbyte.sofalog.domain.ShowDetails
import ro.mbyte.sofalog.domain.ShowStatus
import ro.mbyte.sofalog.domain.episodeRatingMean
import ro.mbyte.sofalog.domain.parseShowDetails
import ro.mbyte.sofalog.domain.seasonProgress
import ro.mbyte.sofalog.domain.usablePremiere
import ro.mbyte.sofalog.ui.common.BulkWatchForm
import ro.mbyte.sofalog.ui.common.CenteredLoading
import ro.mbyte.sofalog.ui.common.CenteredMessage
import ro.mbyte.sofalog.ui.common.attempt
import ro.mbyte.sofalog.ui.common.episodeCode
import ro.mbyte.sofalog.ui.common.formatDay
import ro.mbyte.sofalog.ui.common.formatWatchInstant
import ro.mbyte.sofalog.ui.common.repository
import ro.mbyte.sofalog.ui.common.userMessage
import ro.mbyte.sofalog.ui.details.parts.Byline
import ro.mbyte.sofalog.ui.details.parts.CardSub
import ro.mbyte.sofalog.ui.details.parts.CardTitle
import ro.mbyte.sofalog.ui.details.parts.CastSection
import ro.mbyte.sofalog.ui.details.parts.ChoiceSheet
import ro.mbyte.sofalog.ui.details.parts.CopyTileState
import ro.mbyte.sofalog.ui.details.parts.DetailFact
import ro.mbyte.sofalog.ui.details.parts.DetailHero
import ro.mbyte.sofalog.ui.details.parts.DetailIcons
import ro.mbyte.sofalog.ui.details.parts.DetailsSection
import ro.mbyte.sofalog.ui.details.parts.EpisodeAverage
import ro.mbyte.sofalog.ui.details.parts.HeartRed
import ro.mbyte.sofalog.ui.details.parts.HistorySection
import ro.mbyte.sofalog.ui.details.parts.Lightbox
import ro.mbyte.sofalog.ui.details.parts.MediaSection
import ro.mbyte.sofalog.ui.details.parts.Note
import ro.mbyte.sofalog.ui.details.parts.OverlaidTopBar
import ro.mbyte.sofalog.ui.details.parts.RatingsSection
import ro.mbyte.sofalog.ui.details.parts.Section
import ro.mbyte.sofalog.ui.details.parts.SectionLabel
import ro.mbyte.sofalog.ui.details.parts.SheetAction
import ro.mbyte.sofalog.ui.details.parts.SimilarSection
import ro.mbyte.sofalog.ui.details.parts.StatusBadges
import ro.mbyte.sofalog.ui.details.parts.TileShape
import ro.mbyte.sofalog.ui.details.parts.TimelineEntry
import ro.mbyte.sofalog.ui.details.parts.WatchNowSection
import ro.mbyte.sofalog.ui.details.parts.hueBrush
import ro.mbyte.sofalog.ui.details.parts.hueFor
import ro.mbyte.sofalog.ui.details.parts.initialsFor
import ro.mbyte.sofalog.ui.details.parts.languageName
import ro.mbyte.sofalog.ui.details.parts.popularityFact
import ro.mbyte.sofalog.ui.details.parts.rememberHeroCollapse
import java.util.Locale

data class ShowUiState(
    val bundle: ShowBundle? = null,
    /** Everything the page reads out of the stored body beyond the row's own columns. */
    val details: ShowDetails = ShowDetails.EMPTY,
    val user: ShowUserState = ShowUserState(),
    val loadError: String? = null,
    val busy: Boolean = false,
    val message: String? = null,
)

/** What the view model keeps beside the database's own flows. */
private data class ShowExtras(
    val loadError: String? = null,
    val busy: Boolean = false,
    val message: String? = null,
    /** The state a write in flight is expected to produce, shown until the database says so. */
    val optimistic: ShowUserState? = null,
)

/**
 * Backs the show page. A tracked show is read from the database and stays live; an untracked one
 * is a preview fetched once and never stored — the first rating, listing, copy or mark tracks it.
 */
class ShowViewModel(private val repo: VideoRepository, private val tmdbId: Long) : ViewModel() {
    /** The live, unsaved view of a show the library does not hold. */
    private val preview = MutableStateFlow<ShowBundle?>(null)
    private val extras = MutableStateFlow(ShowExtras())

    /** Rating changes, latest wins: sliding along the bar asks for many and only the last matters. */
    private val ratings = Channel<Int?>(Channel.CONFLATED)
    private var ratingsAsked = 0

    private val bundle = combine(repo.observeShow(tmdbId), preview) { local, live -> local ?: live }

    /** Parsed once per stored body, not once per watch: the bundle re-emits on every mark. */
    private val details = bundle.map { it?.show?.metadataJson }.distinctUntilChanged()
        .map { it?.let(::parseShowDetails) ?: ShowDetails.EMPTY }
        .flowOn(Dispatchers.Default)

    val state: StateFlow<ShowUiState> = combine(
        bundle, details, repo.observeShowUserState(tmdbId), extras,
    ) { show, parsed, user, extra ->
        ShowUiState(show, parsed, extra.optimistic ?: user, extra.loadError, extra.busy, extra.message)
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), ShowUiState())

    init {
        viewModelScope.launch {
            if (repo.observeShow(tmdbId).first() == null) {
                attempt { repo.previewShow(tmdbId, null) }
                    .onSuccess { preview.value = it }
                    .onFailure { e -> extras.value = extras.value.copy(loadError = e.userMessage()) }
            } else {
                // A stale snapshot refreshes on view; when TMDB is down the stored one keeps rendering.
                attempt { repo.refreshShowIfStale(tmdbId) }
            }
        }
        viewModelScope.launch {
            for (rating in ratings) {
                val asked = ratingsAsked
                val failure = attempt { repo.rateShow(tmdbId, rating) }.exceptionOrNull()
                // A newer request is already queued: its optimistic value stays up.
                if (asked == ratingsAsked) {
                    extras.value = extras.value.copy(optimistic = null, message = failure?.userMessage() ?: extras.value.message)
                }
            }
        }
    }

    fun rate(rating: Int?) {
        ratingsAsked++
        extras.value = extras.value.copy(optimistic = state.value.user.copy(rating = rating))
        ratings.trySend(rating)
    }

    fun setWatchlisted(on: Boolean) =
        act(state.value.user.copy(watchlisted = on)) { repo.setShowWatchlisted(tmdbId, on); null }

    fun setFavorited(on: Boolean) =
        act(state.value.user.copy(favorited = on)) { repo.setShowFavorited(tmdbId, on); null }

    fun setCopy(kind: CopyKind, owned: Boolean) = act { repo.setShowCopy(tmdbId, kind, owned); null }

    fun markWatched(rewatch: Boolean, mode: BulkWatchMode) = act {
        val marked = repo.markAiredWatched(tmdbId, season = null, rewatch = rewatch, mode = mode)
        when {
            marked > 0 -> null
            rewatch -> "Nothing to mark: no episode of this show has aired yet."
            else -> "Nothing left to mark: every aired episode is already watched."
        }
    }

    fun setDropped(dropped: Boolean) = act { repo.setShowDropped(tmdbId, dropped); null }

    fun deleteWatch(id: Long) = act { repo.deleteWatch(id); null }

    fun messageShown() {
        extras.value = extras.value.copy(message = null)
    }

    /**
     * One write at a time; [optimistic] is shown while it runs and dropped when it lands or fails.
     * [block] returns a word for the user, or null when the result speaks for itself.
     */
    private fun act(optimistic: ShowUserState? = null, block: suspend () -> String?) {
        if (extras.value.busy) return
        extras.value = extras.value.copy(busy = true, optimistic = optimistic)
        viewModelScope.launch {
            val result = attempt { block() }
            extras.value = extras.value.copy(
                busy = false,
                optimistic = null,
                message = result.fold({ it }, { it.userMessage() }),
            )
        }
    }
}

/** Which sheet or dialog the page has open — one at a time. */
private sealed interface ShowOverlay {
    data object MarkWatched : ShowOverlay
    data class Copy(val kind: CopyKind, val have: Int, val total: Int) : ShowOverlay
    data class Drop(val dropped: Boolean) : ShowOverlay
    data class RemoveWatch(val entry: TimelineEntry) : ShowOverlay
    data class Images(val urls: List<String>, val start: Int) : ShowOverlay
}

private fun plural(count: Int, one: String) = if (count == 1) "1 $one" else "$count ${one}s"

/** "2019–present", "2008–2013", "2020" — a returning show has no end, a one-year show no range. */
private fun yearRange(firstAirDate: String?, details: ShowDetails): String? {
    val year = firstAirDate?.take(4) ?: return null
    val end = details.lastAirDate?.take(4)
    return when {
        details.inProduction -> "$year–present"
        end != null && end != year -> "$year–$end"
        else -> year
    }
}

/**
 * The show page — the web page's sections laid out the way the movie page is: the backdrop band
 * under a clear app bar, rails that run edge to edge, every choice a bottom sheet. A phone stacks
 * the sections; from 840dp the poster sits beside the title, paired blocks go side by side and the
 * season grid takes as many columns as fit.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ShowScreen(tmdbId: Long, onMenu: () -> Unit, onOpenSeason: (Int) -> Unit, onOpenShow: (Long) -> Unit) {
    val repo = repository()
    val vm: ShowViewModel = viewModel(factory = viewModelFactory { initializer { ShowViewModel(repo, tmdbId) } })
    val state by vm.state.collectAsStateWithLifecycle()
    var overlay by remember { mutableStateOf<ShowOverlay?>(null) }
    val bundle = state.bundle
    val show = bundle?.show
    val details = state.details

    val snackbar = remember { SnackbarHostState() }
    val list = rememberLazyListState()
    LaunchedEffect(state.message) {
        state.message?.let { snackbar.showSnackbar(it); vm.messageShown() }
    }

    // Everything the sections derive from the bundle, once per bundle rather than per frame.
    val watchedIds = bundle?.watches?.keys.orEmpty()
    val refs = remember(bundle?.episodes) {
        bundle?.episodes.orEmpty().map { EpisodeRef(it.id, it.seasonNumber, it.episodeNumber, it.airDate) }
    }
    val perSeason = remember(refs, watchedIds, bundle?.today) {
        if (bundle == null) emptyMap() else seasonProgress(refs, watchedIds, bundle.today)
    }
    // Specials last: they are an appendix, whatever number TMDB files them under.
    val seasons = remember(bundle?.seasons) {
        bundle?.seasons.orEmpty().sortedBy { if (it.seasonNumber == 0) Int.MAX_VALUE else it.seasonNumber }
    }
    val regularSeasons = seasons.filter { it.seasonNumber > 0 }
    val timeline = remember(bundle?.watches, bundle?.episodes) {
        val episodes = bundle?.episodes.orEmpty().associateBy { it.id }
        bundle?.watches.orEmpty().values.flatten()
            .sortedWith(compareByDescending<WatchEntity> { it.watchedAt }.thenByDescending { it.id })
            .map { watch ->
                val episode = episodes[watch.targetId]
                val title = episode?.let {
                    listOfNotNull(episodeCode(it.seasonNumber, it.episodeNumber), it.title).joinToString(" ")
                }
                TimelineEntry(watch.id, watch.watchedAt, title ?: "Episode")
            }
    }
    val watched = bundle?.status == ShowStatus.WATCHED

    Scaffold(snackbarHost = { SnackbarHost(snackbar) }, contentWindowInsets = WindowInsets(0, 0, 0, 0)) { padding ->
        BoxWithConstraints(Modifier.fillMaxSize().padding(padding)) {
            val wide = maxWidth >= 840.dp
            val page = if (wide) 32.dp else 16.dp
            // The hero's backdrop: 16:9 across the window, but never most of a short one.
            val bandHeight = minOf(maxWidth * 9 / 16, maxHeight * 0.45f).coerceAtLeast(180.dp)
            // The season grid's columns: as many posters of a comfortable width as the window holds.
            val gap = 12.dp
            val cell = if (wide) 140.dp else 104.dp
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
                            title = show.title,
                            posterPath = show.posterPath,
                            backdropPath = show.backdropPath,
                            facts = listOfNotNull(
                                yearRange(show.firstAirDate, details),
                                details.seasonsCount.takeIf { it > 0 }?.let { plural(it, "season") },
                                details.episodesCount.takeIf { it > 0 }?.let { plural(it, "episode") },
                                details.episodeRuntimeMin?.let { "~$it min" },
                            ),
                            byline = details.createdBy.takeIf { it.isNotEmpty() }?.let { Byline("Created by", it.joinToString(", ")) },
                            score = show.voteAverage,
                            genres = show.genres,
                            tagline = details.tagline,
                            overview = show.overview,
                            noOverview = "TMDB has no overview for this show.",
                            wide = wide,
                            page = page,
                            bandHeight = bandHeight,
                        )
                    }
                    item(key = "details") {
                        DetailsSection(
                            tmdbUrl = "https://www.themoviedb.org/tv/${show.tmdbId}",
                            imdbId = details.imdbId,
                            homepage = details.homepage,
                            trailerUrl = details.trailerUrl,
                            facts = listOfNotNull(
                                details.originalLanguage?.let { DetailFact("Language", languageName(it)) },
                                details.networks.takeIf { it.isNotEmpty() }?.let { DetailFact("Network", it.joinToString(", ")) },
                                show.showStatus?.let { DetailFact("Status", it) },
                                formatDay(show.firstAirDate)?.let { DetailFact("First aired", it) },
                                formatDay(details.lastAirDate)?.let {
                                    DetailFact(if (details.inProduction) "Latest" else "Last aired", it)
                                },
                                details.nextEpisode?.let {
                                    DetailFact("Next episode", episodeCode(it.season, it.number), formatDay(it.airDate))
                                },
                                popularityFact(details.popularity, show.fetchedAt),
                            ),
                            wide = wide,
                            page = page,
                        )
                    }
                    item(key = "ratings") {
                        RatingsSection(
                            voteAverage = show.voteAverage,
                            votes = details.votes,
                            userRating = state.user.rating,
                            onRate = vm::rate,
                            wide = wide,
                            page = page,
                            extra = {
                                val rated = state.user.episodeRatings.size
                                EpisodeAverage(
                                    label = "Your average",
                                    mean = episodeRatingMean(state.user.episodeRatings.values),
                                    note = if (details.episodesCount > 0) {
                                        "$rated of ${plural(details.episodesCount, "episode")} rated, across every season."
                                    } else {
                                        "${plural(rated, "episode")} rated, across every season."
                                    },
                                    emptyNote = "Rate episodes and their average appears here.",
                                )
                            },
                        )
                    }
                    item(key = "watch-now") {
                        val total = regularSeasons.size
                        WatchNowSection(
                            tiles = CopyKind.entries.map { kind ->
                                val have = regularSeasons.count { kind in state.user.seasonCopies[it.seasonNumber].orEmpty() }
                                CopyTileState(
                                    kind = kind,
                                    owned = have > 0,
                                    partial = have in 1 until total,
                                    caption = when {
                                        have == 0 -> null
                                        have == total -> "All ${plural(total, "season")}"
                                        else -> "$have of $total seasons"
                                    },
                                )
                            },
                            streaming = details.streaming,
                            subject = "show",
                            // A show with nothing but Specials has no season a show-wide copy could hang on.
                            enabled = enabled && total > 0,
                            onCopyTap = { kind ->
                                val have = regularSeasons.count { kind in state.user.seasonCopies[it.seasonNumber].orEmpty() }
                                overlay = ShowOverlay.Copy(kind, have, total)
                            },
                            wide = wide,
                            page = page,
                        )
                    }
                    if (bundle.tracked) {
                        item(key = "progress") {
                            ProgressSection(
                                status = bundle.status,
                                watched = bundle.progress.watched,
                                aired = bundle.progress.airedTotal,
                                total = refs.count { it.season > 0 },
                                enabled = enabled,
                                onDrop = { overlay = ShowOverlay.Drop(bundle.status == ShowStatus.DROPPED) },
                                page = page,
                            )
                        }
                    }
                    item(key = "history") {
                        HistorySection(
                            entries = timeline,
                            watched = watched,
                            watchlisted = state.user.watchlisted,
                            favorited = state.user.favorited,
                            enabled = enabled,
                            onEntryTap = { overlay = ShowOverlay.RemoveWatch(it) },
                            onLogWatch = { overlay = ShowOverlay.MarkWatched },
                            onWatchlist = vm::setWatchlisted,
                            onFavorite = vm::setFavorited,
                            wide = wide,
                            page = page,
                            emptyNote = "You have not watched any episode of this show yet.",
                            hint = if (bundle.tracked) {
                                null
                            } else {
                                "Marking, watchlisting or favoriting downloads the whole show for offline use; " +
                                    "marking also marks every aired episode."
                            },
                        )
                    }
                    item(key = "cast") {
                        CastSection(show.title, details.cast, "TMDB lists no cast for this show.", wide, page)
                    }
                    item(key = "seasons-label") {
                        Column(Modifier.fillMaxWidth().padding(start = page, end = page, top = 28.dp)) {
                            SectionLabel("Seasons")
                            if (seasons.isEmpty()) Note("TMDB lists no seasons for this show.", Modifier.padding(top = 12.dp))
                        }
                    }
                    items(seasons.chunked(columns), key = { "seasons-${it.first().seasonNumber}" }) { row ->
                        Row(
                            Modifier.fillMaxWidth().padding(start = page, end = page, top = 12.dp),
                            horizontalArrangement = Arrangement.spacedBy(gap),
                        ) {
                            row.forEach { season ->
                                val ratings = bundle.episodes
                                    .filter { it.seasonNumber == season.seasonNumber }
                                    .mapNotNull { state.user.episodeRatings[it.id] }
                                SeasonCard(
                                    season = season,
                                    showTitle = show.title,
                                    progress = perSeason[season.seasonNumber],
                                    userRating = episodeRatingMean(ratings),
                                    score = details.seasonScores[season.seasonNumber],
                                    modifier = Modifier.weight(1f),
                                    onClick = { onOpenSeason(season.seasonNumber) },
                                )
                            }
                            // A short last row keeps the column width of the full ones.
                            repeat(columns - row.size) { Spacer(Modifier.weight(1f)) }
                        }
                    }
                    item(key = "media") {
                        MediaSection(
                            details.videos, details.backdrops, details.posters, "TMDB lists no media for this show.", wide, page,
                        ) { urls, start -> overlay = ShowOverlay.Images(urls, start) }
                    }
                    item(key = "similar") {
                        SimilarSection("Similar shows", "TMDB lists no similar shows.", details.similar, wide, page, onOpenShow)
                    }
                }
            }
            val collapsed by rememberHeroCollapse(list, loaded = show != null, bandHeight = bandHeight)
            OverlaidTopBar(show?.title ?: "Show", collapsed, wide, onMenu) { disc ->
                if (show != null) {
                    StatusBadges(
                        watched = watched,
                        watchlisted = state.user.watchlisted,
                        favorited = state.user.favorited,
                        disc = disc,
                    )
                }
            }
        }
    }

    val close = { overlay = null }
    val title = show?.title.orEmpty()
    when (val open = overlay) {
        null -> Unit
        is ShowOverlay.MarkWatched -> if (bundle != null) {
            ModalBottomSheet(onDismissRequest = close, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
                BulkWatchForm(
                    title = "Mark show as watched",
                    subtitle = title,
                    watched = bundle.progress.watched,
                    aired = bundle.progress.airedTotal,
                    seasonsNeedingDate = regularSeasons.count { !usablePremiere(it.airDate, bundle.today) },
                    onConfirm = { rewatch, mode ->
                        vm.markWatched(rewatch, mode)
                        close()
                    },
                    modifier = Modifier.padding(start = 24.dp, end = 24.dp, bottom = 24.dp),
                    note = if (bundle.tracked) null else "Marking downloads the whole show for offline use.",
                )
            }
        }
        is ShowOverlay.Copy -> {
            val label = open.kind.label
            val one = open.total == 1
            ChoiceSheet(
                title = when (open.have) {
                    0 -> "Add $label copy?"
                    open.total -> "Remove $label copy?"
                    else -> "$label copies"
                },
                subject = title,
                note = listOfNotNull(
                    "You have ${open.have} of ${open.total} seasons on $label.".takeIf { open.have in 1 until open.total },
                    "Adding a copy downloads the whole show for offline use.".takeIf { bundle?.tracked == false },
                    "Specials are not included.".takeIf { seasons.any { it.seasonNumber == 0 } },
                ).joinToString(" ").ifEmpty { null },
                actions = listOfNotNull(
                    SheetAction(if (one) "Add to the season" else "Add to all ${open.total} seasons") {
                        vm.setCopy(open.kind, true)
                        close()
                    }.takeIf { open.have < open.total },
                    SheetAction(
                        if (one) "Remove from the season" else "Remove from all seasons",
                        // Beside an add, removing is the lesser of the two.
                        secondary = open.have < open.total,
                    ) {
                        vm.setCopy(open.kind, false)
                        close()
                    }.takeIf { open.have > 0 },
                ),
                onDismiss = close,
            )
        }
        is ShowOverlay.Drop -> ChoiceSheet(
            title = if (open.dropped) "Resume this show?" else "Drop this show?",
            subject = title,
            note = if (open.dropped) {
                "Its status goes back to whatever your watch history implies."
            } else {
                "It stops appearing in Up Next and stays dropped as new episodes air. Your watch history is kept."
            },
            actions = listOf(
                SheetAction(if (open.dropped) "Resume show" else "Drop show", danger = !open.dropped) {
                    vm.setDropped(!open.dropped)
                    close()
                },
            ),
            onDismiss = close,
        )
        is ShowOverlay.RemoveWatch -> ChoiceSheet(
            title = "Remove this watch?",
            subject = listOfNotNull(title, open.entry.title).joinToString(" · "),
            note = "Watched ${formatWatchInstant(open.entry.watchedAt)}. This cannot be undone.",
            actions = listOf(
                SheetAction("Remove this watch", danger = true) {
                    vm.deleteWatch(open.entry.id)
                    close()
                },
            ),
            onDismiss = close,
        )
        is ShowOverlay.Images -> Lightbox(open.urls, open.start, onDismiss = close)
    }
}

// ------------------------------------------------------------------------ your progress

private fun statusLabel(status: ShowStatus): String = when (status) {
    ShowStatus.WATCHING -> "Watching"
    ShowStatus.WATCHED -> "Watched"
    ShowStatus.DROPPED -> "Dropped"
}

/**
 * Where the user stands with the show: its stored status, the derived counts, and the one thing
 * they can say about it themselves — that they gave up, or changed their mind. Only a started
 * show has a status, and only a show with a status can be dropped.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ProgressSection(
    status: ShowStatus?,
    watched: Int,
    aired: Int,
    total: Int,
    enabled: Boolean,
    onDrop: () -> Unit,
    page: Dp,
) {
    val scheme = MaterialTheme.colorScheme
    Section("Your progress", page) {
        FlowRow(
            Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(12.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
            itemVerticalAlignment = Alignment.CenterVertically,
        ) {
            status?.let {
                val tint = when (it) {
                    ShowStatus.WATCHING -> scheme.tertiary
                    ShowStatus.WATCHED -> scheme.primary
                    ShowStatus.DROPPED -> scheme.error
                }
                Surface(shape = CircleShape, color = tint.copy(alpha = 0.16f), border = BorderStroke(1.dp, tint)) {
                    Text(
                        statusLabel(it),
                        style = MaterialTheme.typography.labelLarge,
                        color = tint,
                        modifier = Modifier.padding(horizontal = 12.dp, vertical = 6.dp),
                    )
                }
            }
            Text(
                listOfNotNull(
                    "$watched / $aired aired episodes",
                    "$total in total".takeIf { total > aired },
                ).joinToString(" · "),
                style = MaterialTheme.typography.bodyLarge,
                modifier = Modifier.weight(1f, fill = false),
            )
            if (status != null) {
                val dropped = status == ShowStatus.DROPPED
                OutlinedButton(
                    onClick = onDrop,
                    enabled = enabled,
                    border = BorderStroke(1.dp, (if (dropped) scheme.primary else scheme.error).copy(alpha = if (enabled) 1f else 0.4f)),
                    modifier = Modifier.heightIn(min = 48.dp),
                ) {
                    Text(if (dropped) "Resume show" else "Drop show", color = if (dropped) scheme.primary else scheme.error)
                }
            }
        }
    }
}

// ------------------------------------------------------------------------------ seasons

/**
 * One season of the grid: its poster wearing what the user has done with it — progress top-left
 * once started, their episode average top-right — and TMDB's score at the foot, then the name and
 * the season's size and year.
 */
@Composable
private fun SeasonCard(
    season: SeasonEntity,
    showTitle: String,
    progress: SeasonProgress?,
    userRating: Double?,
    score: Double?,
    modifier: Modifier,
    onClick: () -> Unit,
) {
    val scheme = MaterialTheme.colorScheme
    val name = season.name ?: if (season.seasonNumber == 0) "Specials" else "Season ${season.seasonNumber}"
    Column(modifier.clip(TileShape).clickable(role = Role.Button, onClick = onClick)) {
        val hue = remember(showTitle, season.seasonNumber) { (hueFor(showTitle) + season.seasonNumber * 28) % 360 }
        Box(
            Modifier.fillMaxWidth().aspectRatio(2f / 3f).clip(TileShape).background(hueBrush(hue, 0.42f, 0.32f, 0.14f)),
            contentAlignment = Alignment.Center,
        ) {
            val poster = TmdbClient.posterUrl(season.posterPath)
            if (poster == null) {
                Text(initialsFor(name), fontSize = 24.sp, fontWeight = FontWeight.Bold, color = Color.White.copy(alpha = 0.5f))
            } else {
                AsyncImage(poster, name, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
            }
            if (progress != null && progress.watched > 0) {
                val complete = progress.watched == progress.airedTotal
                Text(
                    "${progress.watched}/${progress.airedTotal}",
                    style = MaterialTheme.typography.labelSmall,
                    fontWeight = FontWeight.SemiBold,
                    color = if (complete) scheme.onPrimary else Color.White,
                    modifier = Modifier
                        .align(Alignment.TopStart)
                        .padding(6.dp)
                        .clip(RoundedCornerShape(6.dp))
                        .background(if (complete) scheme.primary else Color.Black.copy(alpha = 0.65f))
                        .padding(horizontal = 6.dp, vertical = 2.dp)
                        .semantics {
                            contentDescription = "${progress.watched} of ${progress.airedTotal} aired episodes watched"
                        },
                )
            }
            userRating?.let {
                val text = "%.1f".format(Locale.ENGLISH, it)
                Text(
                    text,
                    style = MaterialTheme.typography.labelSmall,
                    fontWeight = FontWeight.SemiBold,
                    color = Color.White,
                    modifier = Modifier
                        .align(Alignment.TopEnd)
                        .padding(6.dp)
                        .clip(RoundedCornerShape(6.dp))
                        .background(HeartRed)
                        .padding(horizontal = 6.dp, vertical = 2.dp)
                        .semantics { contentDescription = "Your rating $text, the average of your episode ratings" },
                )
            }
            score?.let {
                Row(
                    Modifier.align(Alignment.BottomEnd)
                        .clip(RoundedCornerShape(topStart = 8.dp))
                        .background(Color.Black.copy(alpha = 0.6f))
                        .padding(horizontal = 7.dp, vertical = 3.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(3.dp),
                ) {
                    Icon(DetailIcons.Heart, null, Modifier.size(11.dp), tint = HeartRed)
                    Text(
                        "%.1f".format(Locale.ENGLISH, it),
                        style = MaterialTheme.typography.labelSmall,
                        fontWeight = FontWeight.SemiBold,
                        color = Color.White,
                    )
                }
            }
        }
        CardTitle(name)
        val sub = listOfNotNull(
            season.episodeCount.takeIf { it > 0 }?.let { if (it == 1) "1 ep" else "$it eps" },
            season.airDate?.take(4),
        ).joinToString(" · ")
        if (sub.isNotEmpty()) CardSub(sub)
    }
}

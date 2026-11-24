package ro.mbyte.sofalog.ui.history

import androidx.compose.foundation.background
import ro.mbyte.sofalog.domain.UserZone
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.text.style.TextAlign
import kotlinx.coroutines.launch
import ro.mbyte.sofalog.ui.common.ConfirmDialog
import ro.mbyte.sofalog.ui.common.attempt
import ro.mbyte.sofalog.ui.common.userMessage
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.lifecycle.viewmodel.initializer
import androidx.lifecycle.viewmodel.viewModelFactory
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import ro.mbyte.sofalog.data.HistoryItem
import ro.mbyte.sofalog.data.HistoryPage
import ro.mbyte.sofalog.data.HistoryType
import ro.mbyte.sofalog.data.VideoRepository
import ro.mbyte.sofalog.domain.groupByMonth
import ro.mbyte.sofalog.ui.common.CenteredLoading
import ro.mbyte.sofalog.ui.common.CenteredMessage
import ro.mbyte.sofalog.ui.common.Poster
import ro.mbyte.sofalog.ui.common.MarkTab
import ro.mbyte.sofalog.ui.common.PosterBadge
import ro.mbyte.sofalog.ui.common.PosterShape
import ro.mbyte.sofalog.ui.common.episodeCode
import ro.mbyte.sofalog.ui.common.formatHistoryInstant
import ro.mbyte.sofalog.ui.common.formatMonth
import ro.mbyte.sofalog.ui.common.repository
import ro.mbyte.sofalog.ui.details.parts.DetailIcons
import ro.mbyte.sofalog.ui.details.parts.HeartRed
import ro.mbyte.sofalog.ui.theme.NeutralBadge
import ro.mbyte.sofalog.ui.theme.OnBadge
import ro.mbyte.sofalog.ui.theme.UserRatingBadge
import ro.mbyte.sofalog.ui.theme.WatchlistItemColor

/** The feed's page size, the web page's. */
private const val PAGE = 48

data class HistoryQuery(
    val type: HistoryType = HistoryType.ALL,
    val newestFirst: Boolean = true,
    val limit: Int = PAGE,
)

/**
 * One thing a long-press can remove: its button, the confirmation's wording, and the write.
 * Every one is confirmed first — none can be undone.
 */
class HistoryRemoval(
    val label: String,
    val confirmText: String,
    val run: suspend (VideoRepository) -> Unit,
)

/**
 * The options for one card, narrowest first. The target-wide option appears only when the log
 * holds the target more than once — with a single entry it would be the first option again.
 */
fun removalsFor(item: HistoryItem): List<HistoryRemoval> = buildList {
    val watched = formatHistoryInstant(item.watchedAt)
    when (item) {
        is HistoryItem.Movie -> {
            add(
                HistoryRemoval(
                    "Remove entry from history",
                    "The watch of ${item.title} on $watched will be removed.",
                ) { it.deleteWatch(item.id) },
            )
            if (item.entries > 1) {
                add(
                    HistoryRemoval(
                        "Remove movie from history",
                        "All ${item.entries} watches of ${item.title} will be removed.",
                    ) { it.removeMovieFromHistory(item.targetId) },
                )
            }
        }
        is HistoryItem.Episode -> {
            val code = episodeCode(item.season, item.number)
            add(
                HistoryRemoval(
                    "Remove entry from history",
                    "The watch of ${item.showTitle} $code on $watched will be removed.",
                ) { it.deleteWatch(item.id) },
            )
            if (item.entries > 1) {
                add(
                    HistoryRemoval(
                        "Remove this episode from history",
                        "All ${item.entries} watches of ${item.showTitle} $code will be removed.",
                    ) { it.removeEpisodeFromHistory(item.targetId) },
                )
            }
            val season = if (item.season == 0) "the specials" else "season ${item.season}"
            add(
                HistoryRemoval(
                    "Remove season from history",
                    "Every watch of every episode of $season of ${item.showTitle} will be removed.",
                ) { it.removeSeasonFromHistory(item.showId, item.season) },
            )
            add(
                HistoryRemoval(
                    "Remove show from history",
                    "Every watch of every episode of ${item.showTitle} will be removed.",
                ) { it.removeShowFromHistory(item.showId) },
            )
        }
    }
}

class HistoryViewModel(private val repo: VideoRepository) : ViewModel() {
    val query = MutableStateFlow(HistoryQuery())

    val error = MutableStateFlow<String?>(null)

    /** The feed is live, so a removal needs no refetch: the list re-emits without the rows. */
    fun remove(removal: HistoryRemoval) {
        viewModelScope.launch {
            attempt { removal.run(repo) }.onFailure { error.value = it.userMessage() }
        }
    }

    /** null while the first read is in flight. The list is live: a watch logged elsewhere appears. */
    @OptIn(ExperimentalCoroutinesApi::class)
    val page: StateFlow<HistoryPage?> = query
        .flatMapLatest { repo.observeHistory(it.type, it.newestFirst, it.limit) }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), null)

    // A changed filter starts over at the first page.
    fun setType(type: HistoryType) = query.update { it.copy(type = type, limit = PAGE) }

    fun setNewestFirst(newestFirst: Boolean) = query.update { it.copy(newestFirst = newestFirst, limit = PAGE) }

    fun loadMore() = query.update { it.copy(limit = it.limit + PAGE) }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HistoryScreen(
    onOpenMovie: (tmdbId: Long) -> Unit,
    onOpenEpisode: (showTmdbId: Long, season: Int, episode: Int) -> Unit,
    onMenu: () -> Unit,
) {
    val repo = repository()
    val vm: HistoryViewModel = viewModel(factory = viewModelFactory { initializer { HistoryViewModel(repo) } })
    val page by vm.page.collectAsStateWithLifecycle()
    val query by vm.query.collectAsStateWithLifecycle()
    var filtersOpen by rememberSaveable { mutableStateOf(false) }
    val error by vm.error.collectAsStateWithLifecycle()
    val snackbar = remember { SnackbarHostState() }
    // A long-pressed card opens the sheet; picking an option swaps it for the confirmation. The
    // two are never up together.
    var chosen by remember { mutableStateOf<HistoryItem?>(null) }
    var pending by remember { mutableStateOf<HistoryRemoval?>(null) }

    LaunchedEffect(error) {
        error?.let { snackbar.showSnackbar(it); vm.error.value = null }
    }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = { Text("History") },
                navigationIcon = { TextButton(onClick = onMenu) { Text("☰") } },
                actions = {
                    TextButton(onClick = { filtersOpen = !filtersOpen }) {
                        Text(if (filtersOpen) "Filters ▴" else "Filters ▾")
                    }
                },
            )
        },
    ) { padding ->
        Column(Modifier.fillMaxSize().padding(padding)) {
            if (filtersOpen) {
                Filters(query, onType = vm::setType, onNewestFirst = vm::setNewestFirst)
            }
            val current = page
            when {
                current == null -> CenteredLoading()
                current.items.isEmpty() -> CenteredMessage("Nothing watched yet.")
                else -> HistoryGrid(
                    current,
                    onOpenMovie,
                    onOpenEpisode,
                    onLongClick = { chosen = it },
                    onLoadMore = vm::loadMore,
                )
            }
        }
    }

    chosen?.let { item ->
        ModalBottomSheet(
            onDismissRequest = { chosen = null },
            sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        ) {
            Column(
                Modifier.fillMaxWidth().padding(start = 24.dp, end = 24.dp, bottom = 24.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text(artTitle(item), style = MaterialTheme.typography.titleLarge, textAlign = TextAlign.Center)
                // Names the entry as its card does: the episode line (for an episode) and when.
                val line = listOfNotNull((item as? HistoryItem.Episode)?.let(::caption), formatHistoryInstant(item.watchedAt))
                Text(
                    line.joinToString(" · "),
                    style = MaterialTheme.typography.titleSmall,
                    textAlign = TextAlign.Center,
                    modifier = Modifier.padding(bottom = 8.dp),
                )
                removalsFor(item).forEachIndexed { index, removal ->
                    val pick = { chosen = null; pending = removal }
                    // The narrowest removal is the filled one; the wider ones read as the lesser choices.
                    if (index == 0) {
                        Button(onClick = pick, modifier = Modifier.fillMaxWidth()) { Text(removal.label) }
                    } else {
                        OutlinedButton(onClick = pick, modifier = Modifier.fillMaxWidth()) { Text(removal.label) }
                    }
                }
            }
        }
    }

    pending?.let { removal ->
        ConfirmDialog(
            title = "${removal.label}?",
            text = "${removal.confirmText} This cannot be undone.",
            confirm = "Remove",
            onConfirm = { vm.remove(removal); pending = null },
            onDismiss = { pending = null },
        )
    }
}

private fun caption(entry: HistoryItem): String = when (entry) {
    is HistoryItem.Movie -> entry.title
    is HistoryItem.Episode -> {
        val code = episodeCode(entry.season, entry.number)
        entry.title?.let { "$code $it" } ?: code
    }
}

/** What the poster stands for: the movie, or the episode's show. */
private fun artTitle(entry: HistoryItem): String = when (entry) {
    is HistoryItem.Movie -> entry.title
    is HistoryItem.Episode -> entry.showTitle
}

@Composable
private fun Filters(query: HistoryQuery, onType: (HistoryType) -> Unit, onNewestFirst: (Boolean) -> Unit) {
    Column(
        Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        val types = listOf(
            HistoryType.ALL to "Movies & Episodes",
            HistoryType.MOVIES to "Movies",
            HistoryType.EPISODES to "Episodes",
        )
        SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
            types.forEachIndexed { index, (type, label) ->
                SegmentedButton(
                    selected = query.type == type,
                    onClick = { onType(type) },
                    shape = SegmentedButtonDefaults.itemShape(index, types.size),
                    icon = {},
                ) { Text(label, maxLines = 1, overflow = TextOverflow.Ellipsis) }
            }
        }
        val orders = listOf(true to "Newest first", false to "Oldest first")
        SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
            orders.forEachIndexed { index, (newestFirst, label) ->
                SegmentedButton(
                    selected = query.newestFirst == newestFirst,
                    onClick = { onNewestFirst(newestFirst) },
                    shape = SegmentedButtonDefaults.itemShape(index, orders.size),
                    icon = {},
                ) { Text(label, maxLines = 1) }
            }
        }
    }
}

@Composable
private fun HistoryGrid(
    page: HistoryPage,
    onOpenMovie: (Long) -> Unit,
    onOpenEpisode: (Long, Int, Int) -> Unit,
    onLongClick: (HistoryItem) -> Unit,
    onLoadMore: () -> Unit,
) {
    // Months are the device's, as every instant the app shows is.
    val sections = remember(page.items) { groupByMonth(page.items, UserZone.current) { it.watchedAt } }
    LazyVerticalGrid(
        columns = GridCells.Adaptive(minSize = 150.dp),
        modifier = Modifier.fillMaxSize(),
        contentPadding = PaddingValues(16.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        for (section in sections) {
            item(key = "m-${section.month}", span = { GridItemSpan(maxLineSpan) }) {
                MonthBar(formatMonth(section.month))
            }
            // Keyed by the watch row: a rewatch repeats the title, so a target id would collide.
            items(section.items, key = { it.id }) { entry ->
                HistoryCard(
                    entry,
                    onClick = {
                        when (entry) {
                            is HistoryItem.Movie -> onOpenMovie(entry.tmdbId)
                            is HistoryItem.Episode -> onOpenEpisode(entry.showTmdbId, entry.season, entry.number)
                        }
                    },
                    onLongClick = { onLongClick(entry) },
                )
            }
        }
        if (page.hasMore) {
            item(key = "more", span = { GridItemSpan(maxLineSpan) }) {
                Box(Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) {
                    OutlinedButton(onClick = onLoadMore) { Text("Load more") }
                }
            }
        }
    }
}

@Composable
private fun MonthBar(label: String) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.width(4.dp).height(22.dp).background(MaterialTheme.colorScheme.primary))
        Spacer(Modifier.width(10.dp))
        Text(label, style = MaterialTheme.typography.titleMedium)
    }
}

/**
 * One watch as a poster, the search card's corners: top-left the library's marks (on the
 * watchlist, in the favorites — no watched tab, since every card here is a watch), the user's own
 * rating top-right, the kind bottom-left and TMDB's score bottom-right.
 */
@Composable
private fun HistoryCard(entry: HistoryItem, onClick: () -> Unit, onLongClick: () -> Unit) {
    // The poster names the show; the caption names the episode, as on the web card.
    val caption = caption(entry)
    val art = artTitle(entry)
    Column(
        Modifier.combinedClickable(onClick = onClick, onLongClick = onLongClick),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Box(Modifier.clip(PosterShape)) {
            Poster(entry.posterPath, art, Modifier.fillMaxWidth())
            // An episode wears its show's watchlist and favorite marks.
            Row(Modifier.align(Alignment.TopStart).clip(RoundedCornerShape(bottomEnd = 6.dp))) {
                if (entry.watchlisted) MarkTab(DetailIcons.Bookmark, "On your watchlist", WatchlistItemColor)
                if (entry.favorited) MarkTab(DetailIcons.Heart, "In your favorites", HeartRed)
            }
            PosterBadge(
                if (entry is HistoryItem.Movie) "Movie" else "Episode",
                NeutralBadge,
                OnBadge,
                RoundedCornerShape(topEnd = 6.dp),
                Modifier.align(Alignment.BottomStart),
            )
            entry.userRating?.let {
                PosterBadge(
                    it.toString(),
                    UserRatingBadge,
                    OnBadge,
                    RoundedCornerShape(bottomStart = 6.dp),
                    Modifier.align(Alignment.TopEnd).semantics { contentDescription = "Your rating $it" },
                )
            }
            entry.score?.let {
                PosterBadge(
                    "♥ %.1f".format(it),
                    NeutralBadge,
                    OnBadge,
                    RoundedCornerShape(topStart = 6.dp),
                    Modifier.align(Alignment.BottomEnd),
                )
            }
        }
        Text(
            caption,
            style = MaterialTheme.typography.titleSmall,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            textAlign = TextAlign.Center,
            modifier = Modifier.fillMaxWidth().padding(top = 6.dp),
        )
        Text(
            formatHistoryInstant(entry.watchedAt),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

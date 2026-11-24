package ro.mbyte.sofalog.ui.upnext

import androidx.compose.foundation.background
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.RectangleShape
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.lifecycle.viewmodel.initializer
import androidx.lifecycle.viewmodel.viewModelFactory
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import java.time.Instant
import ro.mbyte.sofalog.data.UpNextItem
import ro.mbyte.sofalog.data.VideoRepository
import ro.mbyte.sofalog.data.tmdb.TmdbException
import ro.mbyte.sofalog.ui.common.CenteredLoading
import ro.mbyte.sofalog.ui.common.Poster
import ro.mbyte.sofalog.ui.common.PosterBadge
import ro.mbyte.sofalog.ui.common.PosterShape
import ro.mbyte.sofalog.ui.common.PosterTab
import ro.mbyte.sofalog.ui.common.WatchLogForm
import ro.mbyte.sofalog.ui.common.attempt
import ro.mbyte.sofalog.ui.common.episodeCode
import ro.mbyte.sofalog.ui.common.formatDay
import ro.mbyte.sofalog.ui.common.repository
import ro.mbyte.sofalog.ui.common.userMessage
import ro.mbyte.sofalog.domain.Clock
import ro.mbyte.sofalog.domain.EpisodeMarker
import ro.mbyte.sofalog.domain.EpisodeRef
import ro.mbyte.sofalog.domain.airsToday
import ro.mbyte.sofalog.ui.theme.AirsBadge
import ro.mbyte.sofalog.ui.theme.FinaleBadge
import ro.mbyte.sofalog.ui.theme.OnBadge
import ro.mbyte.sofalog.ui.theme.SeasonPremiereBadge
import ro.mbyte.sofalog.ui.theme.UserRatingBadge

class UpNextViewModel(private val repo: VideoRepository) : ViewModel() {
    /** null while the first read is in flight. */
    val items: StateFlow<List<UpNextItem>?> =
        repo.observeUpNext().stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), null)

    /** Whether the log holds any watch; null while unread, so the empty page never flips its text. */
    val hasHistory: StateFlow<Boolean?> =
        repo.observeHasWatches().stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), null)

    val error = MutableStateFlow<String?>(null)

    /** True while a pull-to-refresh is fetching. */
    val refreshing = MutableStateFlow(false)

    /**
     * Re-fetches every show on the page and the season of each card's episode. A failure is said
     * once, in the snackbar; the stored list keeps rendering.
     */
    fun refresh() {
        val list = items.value ?: return
        if (refreshing.value) return
        refreshing.value = true
        viewModelScope.launch {
            try {
                attempt { repo.refreshUpNext(list) }
                    .onSuccess { report -> report.reason?.let { error.value = TmdbException(it).userMessage() } }
                    .onFailure { error.value = it.userMessage() }
            } finally {
                refreshing.value = false
            }
        }
    }

    /** [watchedAt] null is "now"; [rating] null leaves the episode's rating alone. */
    fun markWatched(item: UpNextItem, watchedAt: Instant?, rating: Int?) {
        viewModelScope.launch {
            attempt {
                repo.markEpisodeWatched(
                    item.show.tmdbId,
                    item.episode.seasonNumber,
                    item.episode.episodeNumber,
                    watchedAt,
                    rating,
                )
            }.onFailure { error.value = it.userMessage() }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun UpNextScreen(
    onOpenEpisode: (showTmdbId: Long, season: Int, episode: Int) -> Unit,
    onSearch: () -> Unit,
    onTrending: () -> Unit,
    onSettings: () -> Unit,
    onMenu: () -> Unit,
) {
    val repo = repository()
    val vm: UpNextViewModel = viewModel(factory = viewModelFactory { initializer { UpNextViewModel(repo) } })
    val items by vm.items.collectAsStateWithLifecycle()
    val hasHistory by vm.hasHistory.collectAsStateWithLifecycle()
    val error by vm.error.collectAsStateWithLifecycle()
    val refreshing by vm.refreshing.collectAsStateWithLifecycle()
    val snackbar = remember { SnackbarHostState() }
    val scope = rememberCoroutineScope()
    var chosen by remember { mutableStateOf<UpNextItem?>(null) }
    // The sheet's second step: the mark-as-watched form in place of the two actions.
    var marking by remember { mutableStateOf(false) }

    LaunchedEffect(error) {
        error?.let { snackbar.showSnackbar(it); vm.error.value = null }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("Up Next") },
                navigationIcon = { TextButton(onClick = onMenu) { Text("☰") } },
            )
        },
        snackbarHost = { SnackbarHost(snackbar) },
    ) { padding ->
        val list = items
        when {
            list == null || hasHistory == null -> CenteredLoading(Modifier.padding(padding))
            list.isEmpty() -> UpNextEmpty(
                hasHistory = hasHistory == true,
                onSearch = onSearch,
                onTrending = onTrending,
                onSettings = onSettings,
                modifier = Modifier.padding(padding),
            )
            else -> PullToRefreshBox(
                isRefreshing = refreshing,
                onRefresh = vm::refresh,
                modifier = Modifier.fillMaxSize().padding(padding),
            ) {
                LazyVerticalGrid(
                    columns = GridCells.Adaptive(minSize = 150.dp),
                    modifier = Modifier.fillMaxSize(),
                    contentPadding = PaddingValues(16.dp),
                    horizontalArrangement = Arrangement.spacedBy(12.dp),
                    verticalArrangement = Arrangement.spacedBy(16.dp),
                ) {
                    items(list, key = { it.show.id }) { item ->
                        UpNextCard(
                            item,
                            onClick = {
                                onOpenEpisode(item.show.tmdbId, item.episode.seasonNumber, item.episode.episodeNumber)
                            },
                            onLongClick = { marking = false; chosen = item },
                        )
                    }
                }
            }
        }
    }

    chosen?.let { item ->
        // Not dismissed by hand below: clearing `chosen` removes the sheet, as it did the dialog.
        ModalBottomSheet(
            onDismissRequest = { chosen = null },
            // The form is taller than half a screen; never open it half-way.
            sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        ) {
            if (marking) {
                WatchLogForm(
                    title = item.show.title,
                    subtitle = episodeLine(item),
                    airDate = item.episode.airDate,
                    onConfirm = { watchedAt, rating ->
                        vm.markWatched(item, watchedAt, rating)
                        chosen = null
                    },
                    modifier = Modifier.padding(start = 24.dp, end = 24.dp, bottom = 24.dp),
                )
            } else Column(
                Modifier.fillMaxWidth().padding(start = 24.dp, end = 24.dp, bottom = 24.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text(
                    item.show.title,
                    style = MaterialTheme.typography.titleLarge,
                    textAlign = TextAlign.Center,
                )
                // The card's own line, so the sheet names the episode exactly as the grid does.
                Text(
                    episodeLine(item),
                    style = MaterialTheme.typography.titleSmall,
                    textAlign = TextAlign.Center,
                    modifier = Modifier.padding(bottom = 8.dp),
                )
                Button(
                    onClick = { marking = true },
                    modifier = Modifier.fillMaxWidth(),
                ) { Text("Mark as watched") }
                OutlinedButton(
                    // A placeholder: check-in is not built, so this writes nothing.
                    onClick = {
                        chosen = null
                        scope.launch { snackbar.showSnackbar("Check-in isn't available yet.") }
                    },
                    modifier = Modifier.fillMaxWidth(),
                ) { Text("Check in") }
            }
        }
    }
}

private fun episodeLine(item: UpNextItem): String {
    val code = episodeCode(item.episode.seasonNumber, item.episode.episodeNumber)
    return item.episode.title?.let { "$code $it" } ?: code
}

@Composable
private fun UpNextCard(item: UpNextItem, onClick: () -> Unit, onLongClick: () -> Unit) {
    Column(Modifier.combinedClickable(onClick = onClick, onLongClick = onLongClick)) {
        // Clipped to the poster's own shape, so the corner badges follow its rounded corner —
        // the web card's `.corner` block.
        Box(Modifier.clip(PosterShape)) {
            Poster(item.posterPath, item.show.title, Modifier.fillMaxWidth())
            // Tabs stacked into the top-left corner, touching: the air date first, then the marker.
            // Today's episode already counts as aired, so "not aired" cannot badge it. Read here
            // rather than carried on UpNextItem; re-read whenever the list hands over a new item.
            val airsToday = remember(item) {
                val e = item.episode
                EpisodeRef(e.id, e.seasonNumber, e.episodeNumber, e.airDate)
                    .airsToday(Clock.System.read().today)
            }
            val airs = when {
                airsToday -> "Airs today"
                !item.aired -> formatDay(item.episode.airDate)?.let { "Airs $it" } ?: "Upcoming"
                else -> null
            }
            if (airs != null || item.marker != null) {
                Column(Modifier.align(Alignment.TopStart)) {
                    if (airs != null) {
                        PosterTab(
                            airs,
                            AirsBadge,
                            last = item.marker == null,
                        )
                    }
                    item.marker?.let { marker ->
                        val background = when (marker) {
                            EpisodeMarker.MID_SEASON_FINALE, EpisodeMarker.SEASON_FINALE -> FinaleBadge
                            EpisodeMarker.SEASON_PREMIERE -> SeasonPremiereBadge
                        }
                        PosterTab(marker.label, background, last = true)
                    }
                }
            }
            // The user's own rating, where the History card keeps it.
            item.userRating?.let {
                PosterBadge(
                    it.toString(),
                    UserRatingBadge,
                    OnBadge,
                    RoundedCornerShape(bottomStart = 6.dp),
                    Modifier.align(Alignment.TopEnd),
                )
            }
            // TMDB's score of the episode, where the History card keeps it.
            item.score?.let {
                PosterBadge(
                    "♥ %.1f".format(it),
                    Color.Black.copy(alpha = 0.6f),
                    OnBadge,
                    RoundedCornerShape(topStart = 6.dp),
                    Modifier.align(Alignment.BottomEnd),
                )
            }
        }
        // The episode alone, centered — the poster names the show, as on the web card.
        Text(
            episodeLine(item),
            style = MaterialTheme.typography.titleSmall,
            textAlign = TextAlign.Center,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.fillMaxWidth().padding(top = 6.dp),
        )
    }
}

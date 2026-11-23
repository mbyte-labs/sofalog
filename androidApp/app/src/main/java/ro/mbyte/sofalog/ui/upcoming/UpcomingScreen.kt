package ro.mbyte.sofalog.ui.upcoming

import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
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
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.onStart
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import ro.mbyte.sofalog.data.VideoRepository
import ro.mbyte.sofalog.domain.EpisodeMarker
import ro.mbyte.sofalog.domain.UPCOMING_DEFAULT_DAYS
import ro.mbyte.sofalog.domain.UPCOMING_RANGES
import ro.mbyte.sofalog.domain.UpcomingCard
import ro.mbyte.sofalog.domain.UpcomingTarget
import ro.mbyte.sofalog.domain.UpcomingType
import ro.mbyte.sofalog.domain.upcomingEmptyNote
import ro.mbyte.sofalog.ui.common.CenteredLoading
import ro.mbyte.sofalog.ui.common.CenteredMessage
import ro.mbyte.sofalog.ui.common.Poster
import ro.mbyte.sofalog.ui.common.PosterBadge
import ro.mbyte.sofalog.ui.common.PosterShape
import ro.mbyte.sofalog.ui.common.PosterTab
import ro.mbyte.sofalog.ui.common.SegmentedChoice
import ro.mbyte.sofalog.ui.common.repository
import ro.mbyte.sofalog.ui.theme.FinaleBadge
import ro.mbyte.sofalog.ui.theme.NeutralBadge
import ro.mbyte.sofalog.ui.theme.OnBadge
import ro.mbyte.sofalog.ui.theme.SeasonPremiereBadge

sealed interface UpcomingState {
    data object Loading : UpcomingState
    data object Failed : UpcomingState
    data class Ready(val cards: List<UpcomingCard>) : UpcomingState
}

data class UpcomingQuery(val days: Int = UPCOMING_DEFAULT_DAYS, val type: UpcomingType = UpcomingType.ALL)

@OptIn(ExperimentalCoroutinesApi::class)
class UpcomingViewModel(repo: VideoRepository) : ViewModel() {
    val query = MutableStateFlow(UpcomingQuery())
    private val attempts = MutableStateFlow(0)

    /** Live: marking an episode, or a watchlist change anywhere, re-emits. A new query starts over at Loading. */
    val state: StateFlow<UpcomingState> = combine(query, attempts) { q, _ -> q }
        .flatMapLatest { q ->
            repo.observeUpcoming(q.days, q.type)
                .map<List<UpcomingCard>, UpcomingState> { UpcomingState.Ready(it) }
                .onStart { emit(UpcomingState.Loading) }
                .catch { emit(UpcomingState.Failed) }
        }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), UpcomingState.Loading)

    fun setDays(days: Int) = query.update { it.copy(days = days) }

    fun setType(type: UpcomingType) = query.update { it.copy(type = type) }

    fun retry() = attempts.update { it + 1 }
}

/**
 * The upcoming calendar (D-026): one poster grid in date order, every poster wearing its date —
 * no day headings. On a phone the filters sit behind the top bar's toggle; from 840dp they stay
 * on screen in one row above the grid.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun UpcomingScreen(
    onMenu: () -> Unit,
    onOpenMovie: (tmdbId: Long) -> Unit,
    onOpenSeason: (showTmdbId: Long, season: Int) -> Unit,
    onOpenEpisode: (showTmdbId: Long, season: Int, episode: Int) -> Unit,
    onWatchlist: () -> Unit,
) {
    val repo = repository()
    val vm: UpcomingViewModel = viewModel(factory = viewModelFactory { initializer { UpcomingViewModel(repo) } })
    val state by vm.state.collectAsStateWithLifecycle()
    val query by vm.query.collectAsStateWithLifecycle()
    var filtersOpen by rememberSaveable { mutableStateOf(false) }

    BoxWithConstraints {
        val wide = maxWidth >= 840.dp
        val side: Dp = if (wide) 32.dp else 16.dp
        Scaffold(
            topBar = {
                TopAppBar(
                    title = { Text("Upcoming") },
                    navigationIcon = { TextButton(onClick = onMenu) { Text("☰") } },
                    actions = {
                        if (!wide) {
                            TextButton(onClick = { filtersOpen = !filtersOpen }) {
                                Text(if (filtersOpen) "Filters ▴" else "Filters ▾")
                            }
                        }
                    },
                )
            },
        ) { padding ->
            Column(Modifier.fillMaxSize().padding(padding)) {
                if (wide || filtersOpen) Filters(query, wide, side, vm::setDays, vm::setType)
                when (val current = state) {
                    UpcomingState.Loading -> CenteredLoading()
                    UpcomingState.Failed -> CenteredMessage("Could not load upcoming.") {
                        TextButton(onClick = vm::retry) { Text("Retry") }
                    }
                    is UpcomingState.Ready -> if (current.cards.isEmpty()) {
                        CenteredMessage(
                            upcomingEmptyNote(query.type, query.days) + "\nYour watchlist is where new entries come from.",
                        ) { TextButton(onClick = onWatchlist) { Text("Your watchlist") } }
                    } else {
                        LazyVerticalGrid(
                            columns = GridCells.Adaptive(minSize = 150.dp),
                            modifier = Modifier.fillMaxSize(),
                            contentPadding = PaddingValues(horizontal = side, vertical = 16.dp),
                            horizontalArrangement = Arrangement.spacedBy(12.dp),
                            verticalArrangement = Arrangement.spacedBy(16.dp),
                        ) {
                            items(current.cards, key = { it.key }) { card ->
                                UpcomingPoster(card) {
                                    when (val target = card.target) {
                                        is UpcomingTarget.Movie -> onOpenMovie(target.tmdbId)
                                        is UpcomingTarget.Season -> onOpenSeason(target.showTmdbId, target.season)
                                        is UpcomingTarget.Episode ->
                                            onOpenEpisode(target.showTmdbId, target.season, target.number)
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}

private val typeOptions = listOf(
    UpcomingType.ALL to "All",
    UpcomingType.EPISODES to "Episodes",
    UpcomingType.MOVIES to "Movies",
)

/** The window and the kind. Stacked on a phone; side by side where there is room for both. */
@Composable
private fun Filters(
    query: UpcomingQuery,
    wide: Boolean,
    side: Dp,
    onDays: (Int) -> Unit,
    onType: (UpcomingType) -> Unit,
) {
    val ranges: @Composable (Modifier) -> Unit = { modifier ->
        Row(modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            UPCOMING_RANGES.forEach { (days, label) ->
                FilterChip(selected = days == query.days, onClick = { onDays(days) }, label = { Text(label) })
            }
        }
    }
    if (wide) {
        Row(
            Modifier.fillMaxWidth().padding(horizontal = side, vertical = 8.dp),
            horizontalArrangement = Arrangement.spacedBy(24.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            ranges(Modifier.weight(1f))
            Box(Modifier.width(320.dp)) { SegmentedChoice(typeOptions, query.type, onType) }
        }
    } else {
        Column(
            Modifier.fillMaxWidth().padding(horizontal = side, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            ranges(Modifier.fillMaxWidth())
            SegmentedChoice(typeOptions, query.type, onType)
        }
    }
}

@Composable
private fun UpcomingPoster(card: UpcomingCard, onClick: () -> Unit) {
    Column(Modifier.clickable(onClick = onClick), horizontalAlignment = Alignment.CenterHorizontally) {
        Box(Modifier.clip(PosterShape)) {
            Poster(card.posterPath, card.title, Modifier.fillMaxWidth())
            // The kind first, then the markers, stacked into the top-left corner as on Up Next.
            Column(Modifier.align(Alignment.TopStart)) {
                PosterTab(card.chip, NeutralBadge, last = card.markers.isEmpty())
                card.markers.forEachIndexed { index, marker ->
                    val background = when (marker) {
                        EpisodeMarker.MID_SEASON_FINALE, EpisodeMarker.SEASON_FINALE -> FinaleBadge
                        EpisodeMarker.SEASON_PREMIERE -> SeasonPremiereBadge
                    }
                    PosterTab(marker.label, background, last = index == card.markers.lastIndex)
                }
            }
            // The date, where the History card keeps its score; today and tomorrow in the theme's colour.
            PosterBadge(
                card.dateLabel,
                if (card.soon) MaterialTheme.colorScheme.primary else NeutralBadge,
                if (card.soon) MaterialTheme.colorScheme.onPrimary else OnBadge,
                RoundedCornerShape(topStart = 6.dp),
                Modifier.align(Alignment.BottomEnd),
            )
        }
        Text(
            card.title,
            style = MaterialTheme.typography.titleSmall,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            textAlign = TextAlign.Center,
            modifier = Modifier.fillMaxWidth().padding(top = 6.dp),
        )
        Text(
            card.sub,
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

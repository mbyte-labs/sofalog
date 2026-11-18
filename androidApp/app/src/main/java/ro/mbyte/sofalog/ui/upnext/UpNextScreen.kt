package ro.mbyte.sofalog.ui.upnext

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
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
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
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
import ro.mbyte.sofalog.data.UpNextItem
import ro.mbyte.sofalog.data.VideoRepository
import ro.mbyte.sofalog.ui.common.CenteredLoading
import ro.mbyte.sofalog.ui.common.CenteredMessage
import ro.mbyte.sofalog.ui.common.Poster
import ro.mbyte.sofalog.ui.common.attempt
import ro.mbyte.sofalog.ui.common.episodeCode
import ro.mbyte.sofalog.ui.common.formatDay
import ro.mbyte.sofalog.ui.common.repository
import ro.mbyte.sofalog.ui.common.userMessage
import ro.mbyte.sofalog.domain.EpisodeMarker
import ro.mbyte.sofalog.ui.theme.AirsBadge
import ro.mbyte.sofalog.ui.theme.FinaleBadge
import ro.mbyte.sofalog.ui.theme.OnBadge
import ro.mbyte.sofalog.ui.theme.SeasonPremiereBadge

class UpNextViewModel(private val repo: VideoRepository) : ViewModel() {
    /** null while the first read is in flight. */
    val items: StateFlow<List<UpNextItem>?> =
        repo.observeUpNext().stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), null)

    val error = MutableStateFlow<String?>(null)

    fun markWatched(item: UpNextItem) {
        viewModelScope.launch {
            attempt {
                repo.markEpisodeWatched(item.show.tmdbId, item.episode.seasonNumber, item.episode.episodeNumber)
            }.onFailure { error.value = it.userMessage() }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun UpNextScreen(onOpenShow: (Long) -> Unit, onSearch: () -> Unit, onAbout: () -> Unit) {
    val repo = repository()
    val vm: UpNextViewModel = viewModel(factory = viewModelFactory { initializer { UpNextViewModel(repo) } })
    val items by vm.items.collectAsStateWithLifecycle()
    val error by vm.error.collectAsStateWithLifecycle()
    val snackbar = remember { SnackbarHostState() }
    var chosen by remember { mutableStateOf<UpNextItem?>(null) }

    LaunchedEffect(error) {
        error?.let { snackbar.showSnackbar(it); vm.error.value = null }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("Up Next") },
                actions = { TextButton(onClick = onAbout) { Text("About") } },
            )
        },
        snackbarHost = { SnackbarHost(snackbar) },
    ) { padding ->
        val list = items
        when {
            list == null -> CenteredLoading(Modifier.padding(padding))
            list.isEmpty() -> CenteredMessage(
                "Nothing in progress yet.\nFind a show and mark an episode watched to start.",
                Modifier.padding(padding),
            ) {
                Button(onClick = onSearch, modifier = Modifier.padding(top = 16.dp)) { Text("Search") }
            }
            else -> LazyVerticalGrid(
                columns = GridCells.Adaptive(minSize = 150.dp),
                modifier = Modifier.fillMaxSize().padding(padding),
                contentPadding = PaddingValues(16.dp),
                horizontalArrangement = Arrangement.spacedBy(12.dp),
                verticalArrangement = Arrangement.spacedBy(16.dp),
            ) {
                items(list, key = { it.show.id }) { item -> UpNextCard(item) { chosen = item } }
            }
        }
    }

    chosen?.let { item ->
        AlertDialog(
            onDismissRequest = { chosen = null },
            title = { Text(item.show.title) },
            text = {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text(episodeLine(item))
                    Button(
                        onClick = { vm.markWatched(item); chosen = null },
                        modifier = Modifier.fillMaxWidth(),
                    ) { Text("Mark as watched") }
                    OutlinedButton(
                        onClick = { chosen = null; onOpenShow(item.show.tmdbId) },
                        modifier = Modifier.fillMaxWidth(),
                    ) { Text("Open show") }
                }
            },
            confirmButton = {},
            dismissButton = { TextButton(onClick = { chosen = null }) { Text("Cancel") } },
        )
    }
}

private fun episodeLine(item: UpNextItem): String {
    val code = episodeCode(item.episode.seasonNumber, item.episode.episodeNumber)
    return item.episode.title?.let { "$code $it" } ?: code
}

@Composable
private fun UpNextCard(item: UpNextItem, onClick: () -> Unit) {
    Column(Modifier.clickable(onClick = onClick)) {
        // Clipped to the poster's own shape, so the corner badges follow its rounded corner —
        // the web card's `.corner` block.
        Box(Modifier.clip(PosterShape)) {
            Poster(item.posterPath, item.show.title, Modifier.fillMaxWidth())
            // Tabs stacked into the top-left corner, touching: the air date first, then the marker.
            if (!item.aired || item.marker != null) {
                Column(Modifier.align(Alignment.TopStart)) {
                    if (!item.aired) {
                        PosterTab(
                            formatDay(item.episode.airDate)?.let { "Airs $it" } ?: "Upcoming",
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

private val PosterShape = RoundedCornerShape(8.dp)

/** A tab in the poster's top-left corner; only the stack's [last] tab rounds its bottom-right corner. */
@Composable
private fun PosterTab(text: String, background: Color, last: Boolean) {
    Text(
        text,
        style = MaterialTheme.typography.labelSmall,
        color = OnBadge,
        fontWeight = FontWeight.SemiBold,
        modifier = Modifier
            .clip(if (last) RoundedCornerShape(bottomEnd = 6.dp) else RectangleShape)
            .background(background)
            .padding(horizontal = 9.dp, vertical = 4.dp),
    )
}

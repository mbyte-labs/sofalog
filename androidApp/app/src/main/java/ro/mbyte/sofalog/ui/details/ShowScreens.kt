package ro.mbyte.sofalog.ui.details

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
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
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import ro.mbyte.sofalog.data.ShowBundle
import ro.mbyte.sofalog.data.VideoRepository
import ro.mbyte.sofalog.data.db.EpisodeEntity
import ro.mbyte.sofalog.data.db.WatchEntity
import ro.mbyte.sofalog.domain.ShowStatus
import ro.mbyte.sofalog.ui.common.CenteredLoading
import ro.mbyte.sofalog.ui.common.CenteredMessage
import ro.mbyte.sofalog.ui.common.ConfirmDialog
import ro.mbyte.sofalog.ui.common.Poster
import ro.mbyte.sofalog.ui.common.attempt
import ro.mbyte.sofalog.ui.common.formatDay
import ro.mbyte.sofalog.ui.common.formatInstant
import ro.mbyte.sofalog.ui.common.formatScore
import ro.mbyte.sofalog.ui.common.repository
import ro.mbyte.sofalog.ui.common.userMessage

data class ShowUiState(
    val bundle: ShowBundle? = null,
    val loadError: String? = null,
    val busy: Boolean = false,
    val message: String? = null,
)

/**
 * Backs both the show page and the season page ([seasonNumber] set). A tracked show is read from
 * the database and stays live; an untracked one is a preview fetched once and never stored.
 */
class ShowViewModel(
    private val repo: VideoRepository,
    private val tmdbId: Long,
    private val seasonNumber: Int?,
) : ViewModel() {
    private val preview = MutableStateFlow<ShowBundle?>(null)
    private val extras = MutableStateFlow(ShowUiState())

    val state: StateFlow<ShowUiState> = combine(repo.observeShow(tmdbId), preview, extras) { local, live, extra ->
        extra.copy(bundle = local ?: live)
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), ShowUiState())

    init {
        viewModelScope.launch {
            if (repo.observeShow(tmdbId).first() == null) {
                attempt { repo.previewShow(tmdbId, seasonNumber) }
                    .onSuccess { preview.value = it }
                    .onFailure { e -> extras.value = extras.value.copy(loadError = e.userMessage()) }
            } else {
                // Best effort: a tracked show keeps rendering from its snapshot when TMDB is down.
                attempt { repo.refreshShowIfStale(tmdbId) }
            }
        }
    }

    fun markEpisode(episode: EpisodeEntity) =
        act { repo.markEpisodeWatched(tmdbId, episode.seasonNumber, episode.episodeNumber); null }

    fun markAired() = act {
        val marked = repo.markAiredWatched(tmdbId, seasonNumber)
        if (marked == 0) "Nothing left to mark: every aired episode is already watched." else null
    }

    fun deleteWatch(id: Long) = act { repo.deleteWatch(id); null }

    fun messageShown() {
        extras.value = extras.value.copy(message = null)
    }

    private fun act(block: suspend () -> String?) {
        if (extras.value.busy) return
        extras.value = extras.value.copy(busy = true)
        viewModelScope.launch {
            val result = attempt { block() }
            extras.value = extras.value.copy(
                busy = false,
                message = result.fold({ it }, { it.userMessage() }),
            )
        }
    }
}

@Composable
private fun showViewModel(tmdbId: Long, seasonNumber: Int?): ShowViewModel {
    val repo = repository()
    return viewModel(factory = viewModelFactory { initializer { ShowViewModel(repo, tmdbId, seasonNumber) } })
}

private fun statusLabel(status: ShowStatus?): String? = when (status) {
    ShowStatus.WATCHING -> "Watching"
    ShowStatus.WATCHED -> "Watched"
    ShowStatus.DROPPED -> "Dropped"
    null -> null
}

@Composable
fun ShowScreen(tmdbId: Long, onBack: () -> Unit, onOpenSeason: (Int) -> Unit) {
    val vm = showViewModel(tmdbId, null)
    val state by vm.state.collectAsStateWithLifecycle()
    var confirmAll by remember { mutableStateOf(false) }
    val bundle = state.bundle

    DetailScaffold(bundle?.show?.title ?: "Show", onBack, state.message, vm::messageShown) { modifier ->
        when {
            bundle == null && state.loadError != null -> CenteredMessage(state.loadError!!, modifier)
            bundle == null -> CenteredLoading(modifier)
            else -> LazyColumn(modifier) {
                val show = bundle.show
                item {
                    DetailHeader(
                        title = show.title,
                        facts = listOfNotNull(show.firstAirDate?.take(4), show.showStatus, formatScore(show.voteAverage)),
                        genres = show.genres,
                        posterPath = show.posterPath,
                        backdropPath = show.backdropPath,
                    )
                }
                if (bundle.tracked && bundle.progress.airedTotal > 0) {
                    item {
                        Text(
                            listOfNotNull(
                                statusLabel(bundle.status),
                                "${bundle.progress.watched} of ${bundle.progress.airedTotal} aired episodes watched",
                            ).joinToString(" · "),
                            Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
                            style = MaterialTheme.typography.bodyMedium,
                        )
                    }
                }
                item {
                    Button(
                        onClick = { confirmAll = true },
                        enabled = !state.busy,
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                    ) { Text(if (state.busy) "Working…" else "Mark all aired episodes watched") }
                }
                show.overview?.let {
                    item { Text(it, Modifier.padding(16.dp), style = MaterialTheme.typography.bodyMedium) }
                }
                item {
                    Text("Seasons", Modifier.padding(start = 16.dp, top = 8.dp), style = MaterialTheme.typography.titleMedium)
                }
                items(bundle.seasons.sortedBy { if (it.seasonNumber == 0) Int.MAX_VALUE else it.seasonNumber }, key = { it.seasonNumber }) { season ->
                    val episodes = bundle.episodes.filter { it.seasonNumber == season.seasonNumber }
                    val watched = episodes.count { it.id in bundle.watches }
                    Row(
                        Modifier.fillMaxWidth().clickable { onOpenSeason(season.seasonNumber) }
                            .padding(horizontal = 16.dp, vertical = 6.dp),
                        horizontalArrangement = Arrangement.spacedBy(12.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Poster(season.posterPath ?: show.posterPath, season.name ?: "", Modifier.width(48.dp))
                        Column {
                            Text(season.name ?: "Season ${season.seasonNumber}", style = MaterialTheme.typography.titleSmall)
                            Text(
                                if (bundle.tracked) "$watched of ${episodes.size} watched" else if (season.episodeCount == 1) "1 episode" else "${season.episodeCount} episodes",
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                }
            }
        }
    }

    if (confirmAll) {
        ConfirmDialog(
            title = "Mark the whole show watched?",
            text = "Every aired episode you haven't watched yet is logged as watched now. Specials are left out.",
            confirm = "Mark watched",
            onConfirm = { vm.markAired(); confirmAll = false },
            onDismiss = { confirmAll = false },
        )
    }
}

@Composable
fun SeasonScreen(tmdbId: Long, seasonNumber: Int, onBack: () -> Unit) {
    val vm = showViewModel(tmdbId, seasonNumber)
    val state by vm.state.collectAsStateWithLifecycle()
    var confirmSeason by remember { mutableStateOf(false) }
    var chosen by remember { mutableStateOf<EpisodeEntity?>(null) }
    var removing by remember { mutableStateOf<WatchEntity?>(null) }
    val bundle = state.bundle
    val season = bundle?.seasons?.firstOrNull { it.seasonNumber == seasonNumber }
    val title = season?.name ?: "Season $seasonNumber"

    DetailScaffold(title, onBack, state.message, vm::messageShown) { modifier ->
        when {
            bundle == null && state.loadError != null -> CenteredMessage(state.loadError!!, modifier)
            bundle == null -> CenteredLoading(modifier)
            else -> {
                val episodes = bundle.episodes.filter { it.seasonNumber == seasonNumber }
                LazyColumn(modifier) {
                    item {
                        Text(
                            bundle.show.title,
                            Modifier.padding(horizontal = 16.dp),
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    if (seasonNumber > 0) {
                        item {
                            Button(
                                onClick = { confirmSeason = true },
                                enabled = !state.busy,
                                modifier = Modifier.fillMaxWidth().padding(16.dp),
                            ) { Text(if (state.busy) "Working…" else "Mark season watched") }
                        }
                    }
                    if (episodes.isEmpty()) {
                        item { Text("TMDB lists no episodes for this season yet.", Modifier.padding(16.dp)) }
                    }
                    items(episodes, key = { it.episodeNumber }) { episode ->
                        val watches = bundle.watches[episode.id].orEmpty()
                        val aired = episode.airDate != null && episode.airDate <= bundle.today
                        Row(
                            Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp),
                            horizontalArrangement = Arrangement.spacedBy(12.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Column(Modifier.weight(1f)) {
                                Text(
                                    "${episode.episodeNumber}. ${episode.title ?: "Episode ${episode.episodeNumber}"}",
                                    style = MaterialTheme.typography.titleSmall,
                                    maxLines = 2,
                                    overflow = TextOverflow.Ellipsis,
                                )
                                Text(
                                    when {
                                        episode.airDate == null -> "No air date yet"
                                        aired -> formatDay(episode.airDate).orEmpty()
                                        else -> "Airs ${formatDay(episode.airDate)}"
                                    },
                                    style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                            }
                            if (watches.isEmpty()) {
                                OutlinedButton(onClick = { vm.markEpisode(episode) }, enabled = !state.busy) { Text("Mark") }
                            } else {
                                FilledTonalButton(onClick = { chosen = episode }, enabled = !state.busy) {
                                    Text(if (watches.size > 1) "✓ ×${watches.size}" else "✓ Watched")
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    chosen?.let { episode ->
        val latest = bundle?.watches?.get(episode.id)?.firstOrNull()
        AlertDialog(
            onDismissRequest = { chosen = null },
            title = { Text(episode.title ?: "Episode ${episode.episodeNumber}") },
            text = {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    latest?.let { Text("Last watched ${formatInstant(it.watchedAt)}") }
                    Button(onClick = { vm.markEpisode(episode); chosen = null }, modifier = Modifier.fillMaxWidth()) {
                        Text("Log another watch")
                    }
                    if (latest != null) {
                        OutlinedButton(onClick = { removing = latest; chosen = null }, modifier = Modifier.fillMaxWidth()) {
                            Text("Remove latest watch")
                        }
                    }
                }
            },
            confirmButton = {},
            dismissButton = { TextButton(onClick = { chosen = null }) { Text("Cancel") } },
        )
    }

    removing?.let { watch ->
        ConfirmDialog(
            title = "Remove this watch?",
            text = "Watched ${formatInstant(watch.watchedAt)}. This cannot be undone.",
            confirm = "Remove",
            onConfirm = { vm.deleteWatch(watch.id); removing = null },
            onDismiss = { removing = null },
        )
    }

    if (confirmSeason) {
        ConfirmDialog(
            title = "Mark this season watched?",
            text = "Every aired episode of this season you haven't watched yet is logged as watched now.",
            confirm = "Mark watched",
            onConfirm = { vm.markAired(); confirmSeason = false },
            onDismiss = { confirmSeason = false },
        )
    }
}

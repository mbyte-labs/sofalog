package ro.mbyte.sofalog.ui.details

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
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
import ro.mbyte.sofalog.data.VideoRepository
import ro.mbyte.sofalog.data.db.MovieEntity
import ro.mbyte.sofalog.data.db.WatchEntity
import ro.mbyte.sofalog.ui.common.CenteredLoading
import ro.mbyte.sofalog.ui.common.CenteredMessage
import ro.mbyte.sofalog.ui.common.ConfirmDialog
import ro.mbyte.sofalog.ui.common.attempt
import ro.mbyte.sofalog.ui.common.formatInstant
import ro.mbyte.sofalog.ui.common.formatRuntime
import ro.mbyte.sofalog.ui.common.formatScore
import ro.mbyte.sofalog.ui.common.repository
import ro.mbyte.sofalog.ui.common.userMessage

data class MovieUiState(
    val movie: MovieEntity? = null,
    val watches: List<WatchEntity> = emptyList(),
    val loadError: String? = null,
    val busy: Boolean = false,
    val message: String? = null,
)

class MovieViewModel(private val repo: VideoRepository, private val tmdbId: Long) : ViewModel() {
    /** The live, unsaved view of a movie the library does not hold. */
    private val preview = MutableStateFlow<MovieEntity?>(null)
    private val extras = MutableStateFlow(MovieUiState())

    val state: StateFlow<MovieUiState> = combine(
        repo.observeMovie(tmdbId), preview, repo.observeMovieWatches(tmdbId), extras,
    ) { local, live, watches, extra ->
        extra.copy(movie = local ?: live, watches = watches)
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), MovieUiState())

    init {
        viewModelScope.launch {
            if (repo.observeMovie(tmdbId).first() == null) {
                attempt { repo.previewMovie(tmdbId) }
                    .onSuccess { preview.value = it }
                    .onFailure { e -> extras.value = extras.value.copy(loadError = e.userMessage()) }
            }
        }
    }

    fun markWatched() = act { repo.markMovieWatched(tmdbId) }

    fun deleteWatch(id: Long) = act { repo.deleteWatch(id) }

    fun messageShown() {
        extras.value = extras.value.copy(message = null)
    }

    private fun act(block: suspend () -> Unit) {
        if (extras.value.busy) return
        extras.value = extras.value.copy(busy = true)
        viewModelScope.launch {
            val failure = attempt { block() }.exceptionOrNull()
            extras.value = extras.value.copy(busy = false, message = failure?.userMessage())
        }
    }
}

@Composable
fun MovieScreen(tmdbId: Long, onBack: () -> Unit) {
    val repo = repository()
    val vm: MovieViewModel = viewModel(factory = viewModelFactory { initializer { MovieViewModel(repo, tmdbId) } })
    val state by vm.state.collectAsStateWithLifecycle()
    var removing by remember { mutableStateOf<WatchEntity?>(null) }
    val movie = state.movie

    DetailScaffold(movie?.title ?: "Movie", onBack, state.message, vm::messageShown) { modifier ->
        when {
            movie == null && state.loadError != null -> CenteredMessage(state.loadError!!, modifier)
            movie == null -> CenteredLoading(modifier)
            else -> LazyColumn(modifier) {
                item {
                    DetailHeader(
                        title = movie.title,
                        facts = listOfNotNull(
                            movie.releaseDate?.take(4),
                            formatRuntime(movie.runtime),
                            formatScore(movie.voteAverage),
                        ),
                        genres = movie.genres,
                        posterPath = movie.posterPath,
                        backdropPath = movie.backdropPath,
                    )
                }
                item {
                    Button(
                        onClick = vm::markWatched,
                        enabled = !state.busy,
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                    ) { Text(if (state.watches.isEmpty()) "Mark as watched" else "Log another watch") }
                }
                movie.overview?.let {
                    item { Text(it, Modifier.padding(16.dp), style = MaterialTheme.typography.bodyMedium) }
                }
                if (state.watches.isNotEmpty()) {
                    item {
                        Text(
                            "History",
                            Modifier.padding(start = 16.dp, top = 8.dp),
                            style = MaterialTheme.typography.titleMedium,
                        )
                    }
                    items(state.watches, key = { it.id }) { watch ->
                        Row(
                            Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                            horizontalArrangement = Arrangement.SpaceBetween,
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Text("Watched ${formatInstant(watch.watchedAt)}")
                            TextButton(onClick = { removing = watch }, enabled = !state.busy) { Text("Remove") }
                        }
                    }
                }
            }
        }
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
}

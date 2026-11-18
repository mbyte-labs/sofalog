package ro.mbyte.sofalog.ui.search

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.lifecycle.viewmodel.initializer
import androidx.lifecycle.viewmodel.viewModelFactory
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import ro.mbyte.sofalog.data.MediaKind
import ro.mbyte.sofalog.data.SearchHit
import ro.mbyte.sofalog.data.VideoRepository
import ro.mbyte.sofalog.ui.common.CenteredLoading
import ro.mbyte.sofalog.ui.common.CenteredMessage
import ro.mbyte.sofalog.ui.common.Poster
import ro.mbyte.sofalog.ui.common.attempt
import ro.mbyte.sofalog.ui.common.formatScore
import ro.mbyte.sofalog.ui.common.repository
import ro.mbyte.sofalog.ui.common.userMessage

data class SearchState(
    val query: String = "",
    val loading: Boolean = false,
    val results: List<SearchHit>? = null,
    val error: String? = null,
)

class SearchViewModel(private val repo: VideoRepository) : ViewModel() {
    val state = MutableStateFlow(SearchState())
    private var job: Job? = null

    fun onQuery(query: String) {
        state.value = state.value.copy(query = query)
        job?.cancel()
        if (query.isBlank()) {
            state.value = SearchState(query = query)
            return
        }
        job = viewModelScope.launch {
            delay(400) // debounce: one request per pause in typing, not per keystroke
            state.value = state.value.copy(loading = true, error = null)
            attempt { repo.search(query.trim()) }
                .onSuccess { state.value = state.value.copy(loading = false, results = it) }
                .onFailure { state.value = state.value.copy(loading = false, results = null, error = it.userMessage()) }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SearchScreen(onOpen: (SearchHit) -> Unit) {
    val repo = repository()
    val vm: SearchViewModel = viewModel(factory = viewModelFactory { initializer { SearchViewModel(repo) } })
    val state by vm.state.collectAsStateWithLifecycle()

    Scaffold(topBar = { TopAppBar(title = { Text("Search") }) }) { padding ->
        Column(Modifier.fillMaxSize().padding(padding)) {
            OutlinedTextField(
                value = state.query,
                onValueChange = vm::onQuery,
                singleLine = true,
                placeholder = { Text("Movies and shows") },
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
            )
            val results = state.results
            when {
                state.loading -> CenteredLoading()
                state.error != null -> CenteredMessage(state.error!!)
                results == null -> CenteredMessage("Search TMDB for a movie or a show.")
                results.isEmpty() -> CenteredMessage("Nothing found.")
                else -> LazyColumn(
                    contentPadding = PaddingValues(16.dp),
                    verticalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    items(results, key = { "${it.kind}-${it.tmdbId}" }) { hit ->
                        Row(
                            Modifier.fillMaxWidth().clickable { onOpen(hit) },
                            horizontalArrangement = Arrangement.spacedBy(12.dp),
                        ) {
                            Poster(hit.posterPath, hit.title, Modifier.width(64.dp))
                            Column {
                                Text(
                                    hit.title,
                                    style = MaterialTheme.typography.titleMedium,
                                    maxLines = 2,
                                    overflow = TextOverflow.Ellipsis,
                                )
                                Text(
                                    listOfNotNull(
                                        if (hit.kind == MediaKind.MOVIE) "Movie" else "Show",
                                        hit.year,
                                        formatScore(hit.voteAverage?.takeIf { it > 0 }),
                                    ).joinToString(" · "),
                                    style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                            }
                        }
                    }
                }
            }
        }
    }
}

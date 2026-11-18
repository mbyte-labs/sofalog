package ro.mbyte.sofalog.ui.search

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
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
import ro.mbyte.sofalog.R
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
fun SearchScreen(onOpen: (SearchHit) -> Unit, onMenu: () -> Unit) {
    val repo = repository()
    val vm: SearchViewModel = viewModel(factory = viewModelFactory { initializer { SearchViewModel(repo) } })
    val state by vm.state.collectAsStateWithLifecycle()

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("Search") },
                navigationIcon = { TextButton(onClick = onMenu) { Text("☰") } },
            )
        },
    ) { padding ->
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
                results == null -> SearchIntro()
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

/**
 * What the page shows before anything is typed: the app's mark greyed out in a soft halo, then
 * the "Powered by TMDB" credit and one line saying what the field searches.
 *
 * The sofalog mark is the launcher's monochrome silhouette, tinted; TMDB's logo keeps its own
 * colours (their attribution rules forbid recolouring it) and stays the smaller of the two.
 * The column scrolls so the keyboard can never clip it on a short screen.
 */
@Composable
private fun SearchIntro() {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        Column(
            Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 32.dp, vertical = 24.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Box(
                Modifier
                    .size(176.dp)
                    .background(
                        Brush.radialGradient(listOf(muted.copy(alpha = 0.14f), Color.Transparent)),
                        CircleShape,
                    ),
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    painterResource(R.drawable.ic_launcher_monochrome),
                    contentDescription = null,
                    tint = muted.copy(alpha = 0.38f),
                    modifier = Modifier.size(168.dp),
                )
            }
            Spacer(Modifier.height(4.dp))
            Row(
                Modifier.semantics(mergeDescendants = true) { contentDescription = "Powered by TMDB" },
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                HorizontalDivider(Modifier.width(28.dp), color = muted.copy(alpha = 0.3f))
                Text(
                    "POWERED BY",
                    style = MaterialTheme.typography.labelSmall,
                    letterSpacing = 2.sp,
                    color = muted.copy(alpha = 0.8f),
                    modifier = Modifier.clearAndSetSemantics {},
                )
                HorizontalDivider(Modifier.width(28.dp), color = muted.copy(alpha = 0.3f))
            }
            Spacer(Modifier.height(14.dp))
            Image(
                painterResource(R.drawable.tmdb_logo),
                contentDescription = null,
                modifier = Modifier.width(132.dp),
            )
            Spacer(Modifier.height(28.dp))
            Text(
                "Search TMDB for movies or shows.",
                style = MaterialTheme.typography.bodyMedium,
                textAlign = TextAlign.Center,
                color = muted,
            )
        }
    }
}

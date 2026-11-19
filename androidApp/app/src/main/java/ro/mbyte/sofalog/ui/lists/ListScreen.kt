package ro.mbyte.sofalog.ui.lists

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
import androidx.compose.material3.ExperimentalMaterial3Api
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
import androidx.compose.ui.graphics.Color
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
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import ro.mbyte.sofalog.data.ListKey
import ro.mbyte.sofalog.data.VideoRepository
import ro.mbyte.sofalog.domain.ListEntry
import ro.mbyte.sofalog.domain.ListItemKind
import ro.mbyte.sofalog.domain.ListQuery
import ro.mbyte.sofalog.domain.ListSort
import ro.mbyte.sofalog.domain.orderList
import ro.mbyte.sofalog.ui.common.CenteredLoading
import ro.mbyte.sofalog.ui.common.CenteredMessage
import ro.mbyte.sofalog.ui.common.Poster
import ro.mbyte.sofalog.ui.common.PosterBadge
import ro.mbyte.sofalog.ui.common.PosterShape
import ro.mbyte.sofalog.ui.common.SegmentedChoice
import ro.mbyte.sofalog.ui.common.formatWatchDay
import ro.mbyte.sofalog.ui.common.repository
import ro.mbyte.sofalog.ui.theme.OnBadge
import ro.mbyte.sofalog.ui.theme.UserRatingBadge

/** A list as its page shows it: [total] is the whole list's size, [items] what the filters leave. */
data class ListPage(val total: Int, val items: List<ListEntry>)

class ListViewModel(repo: VideoRepository, key: ListKey) : ViewModel() {
    val query = MutableStateFlow(ListQuery())

    /** null while the first read is in flight. Live: a title taken off the list elsewhere leaves. */
    val page: StateFlow<ListPage?> = combine(repo.observeList(key), query) { entries, q ->
        ListPage(entries.size, orderList(entries, q))
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), null)

    fun setKind(kind: ListItemKind?) = query.update { it.copy(kind = kind) }

    fun setSort(sort: ListSort) = query.update { it.copy(sort = sort) }

    fun setDescending(descending: Boolean) = query.update { it.copy(descending = descending) }
}

private val ListKey.emptyText: String
    get() = when (this) {
        ListKey.WATCHLIST -> "Nothing on your watchlist yet."
        ListKey.FAVORITES -> "No favorites yet. Tap the heart on a movie or show."
        ListKey.DROPPED -> "No dropped shows. You can drop a show you've started from its page."
    }

/** One list's page — the watchlist, the favorites or the dropped shows, which differ in wording alone. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ListScreen(
    key: ListKey,
    onMenu: () -> Unit,
    onOpenMovie: (tmdbId: Long) -> Unit,
    onOpenShow: (tmdbId: Long) -> Unit,
    onSearch: () -> Unit,
) {
    val repo = repository()
    val vm: ListViewModel = viewModel(key = key.name, factory = viewModelFactory { initializer { ListViewModel(repo, key) } })
    val page by vm.page.collectAsStateWithLifecycle()
    val query by vm.query.collectAsStateWithLifecycle()
    var filtersOpen by rememberSaveable { mutableStateOf(false) }
    val dropped = key == ListKey.DROPPED

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(key.title) },
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
                Filters(query, dropped, vm::setKind, vm::setSort, vm::setDescending)
            }
            val current = page
            when {
                current == null -> CenteredLoading()
                current.total == 0 -> CenteredMessage(key.emptyText) {
                    // A dropped show comes from a show's own page, not from a search.
                    if (!dropped) TextButton(onClick = onSearch) { Text("Search") }
                }
                current.items.isEmpty() -> CenteredMessage("Nothing here matches the filters.")
                else -> LazyVerticalGrid(
                    columns = GridCells.Adaptive(minSize = 150.dp),
                    modifier = Modifier.fillMaxSize(),
                    contentPadding = PaddingValues(16.dp),
                    horizontalArrangement = Arrangement.spacedBy(12.dp),
                    verticalArrangement = Arrangement.spacedBy(16.dp),
                ) {
                    // Keyed by the pair: movie 5 and show 5 both exist.
                    items(current.items, key = { "${it.kind}:${it.localId}" }) { entry ->
                        ListCard(
                            entry,
                            subLine = subLine(entry, query.sort, if (dropped) "Dropped" else "Added"),
                            onClick = {
                                if (entry.kind == ListItemKind.MOVIE) onOpenMovie(entry.tmdbId) else onOpenShow(entry.tmdbId)
                            },
                        )
                    }
                }
            }
        }
    }
}

/** When it joined the list while that is the order; the year otherwise, or nothing when undated. */
private fun subLine(entry: ListEntry, sort: ListSort, verb: String): String? =
    if (sort == ListSort.ADDED) "$verb ${formatWatchDay(entry.addedAt)}" else entry.date?.take(4)

@Composable
private fun Filters(
    query: ListQuery,
    dropped: Boolean,
    onKind: (ListItemKind?) -> Unit,
    onSort: (ListSort) -> Unit,
    onDescending: (Boolean) -> Unit,
) {
    Column(
        Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        // The dropped list holds shows alone: a movie cannot be dropped.
        if (!dropped) {
            SegmentedChoice(
                listOf(null to "Movies & Shows", ListItemKind.MOVIE to "Movies", ListItemKind.SHOW to "Shows"),
                query.kind,
                onKind,
            )
        }
        SegmentedChoice(
            listOf(
                ListSort.ADDED to if (dropped) "Date dropped" else "Date added",
                ListSort.TITLE to "Title",
                ListSort.RELEASE to if (dropped) "First aired" else "Release date",
            ),
            query.sort,
            onSort,
        )
        val byTitle = query.sort == ListSort.TITLE
        SegmentedChoice(
            listOf(
                true to if (byTitle) "Z to A" else "Newest first",
                false to if (byTitle) "A to Z" else "Oldest first",
            ),
            query.descending,
            onDescending,
        )
    }
}

@Composable
private fun ListCard(entry: ListEntry, subLine: String?, onClick: () -> Unit) {
    Column(Modifier.clickable(onClick = onClick), horizontalAlignment = Alignment.CenterHorizontally) {
        Box(Modifier.clip(PosterShape)) {
            Poster(entry.posterPath, entry.title, Modifier.fillMaxWidth())
            if (entry.watched) {
                PosterBadge(
                    "✓",
                    MaterialTheme.colorScheme.primary,
                    MaterialTheme.colorScheme.onPrimary,
                    RoundedCornerShape(bottomEnd = 6.dp),
                    Modifier.align(Alignment.TopStart),
                )
            }
            entry.userRating?.let {
                PosterBadge(
                    it.toString(),
                    UserRatingBadge,
                    OnBadge,
                    RoundedCornerShape(bottomStart = 6.dp),
                    Modifier.align(Alignment.TopEnd),
                )
            }
            entry.score?.let {
                PosterBadge(
                    "♥ %.1f".format(it),
                    Color.Black.copy(alpha = 0.6f),
                    OnBadge,
                    RoundedCornerShape(topStart = 6.dp),
                    Modifier.align(Alignment.BottomEnd),
                )
            }
        }
        Text(
            entry.title,
            style = MaterialTheme.typography.titleSmall,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            textAlign = TextAlign.Center,
            modifier = Modifier.fillMaxWidth().padding(top = 6.dp),
        )
        subLine?.let {
            Text(
                it,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

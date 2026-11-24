package ro.mbyte.sofalog.ui.lists

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.lifecycle.viewmodel.initializer
import androidx.lifecycle.viewmodel.viewModelFactory
import coil3.compose.AsyncImage
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.stateIn
import ro.mbyte.sofalog.data.ListKey
import ro.mbyte.sofalog.data.VideoRepository
import ro.mbyte.sofalog.data.tmdb.TmdbClient
import ro.mbyte.sofalog.domain.ListSummary
import ro.mbyte.sofalog.ui.common.repository
import ro.mbyte.sofalog.ui.details.parts.DetailIcons
import ro.mbyte.sofalog.ui.settings.SettingsPage

internal val ListKey.title: String
    get() = when (this) {
        ListKey.WATCHLIST -> "Watchlist"
        ListKey.FAVORITES -> "Favorites"
        ListKey.DROPPED -> "Dropped shows"
    }

internal val ListKey.icon: ImageVector
    get() = when (this) {
        ListKey.WATCHLIST -> DetailIcons.Bookmark
        ListKey.FAVORITES -> DetailIcons.Heart
        ListKey.DROPPED -> DetailIcons.Dropped
    }

/** "3 movies · 2 shows", "1 show", or the empty list's line. */
internal fun breakdown(summary: ListSummary): String {
    if (summary.total == 0) return "Nothing here yet"
    fun plural(n: Int, one: String, many: String) = "$n ${if (n == 1) one else many}"
    return listOfNotNull(
        summary.movies.takeIf { it > 0 }?.let { plural(it, "movie", "movies") },
        summary.shows.takeIf { it > 0 }?.let { plural(it, "show", "shows") },
    ).joinToString(" · ")
}

class ListsViewModel(repo: VideoRepository) : ViewModel() {
    /** null while the first read is in flight. Live: the counts follow every add and removal. */
    val lists: StateFlow<List<Pair<ListKey, ListSummary>>?> =
        repo.observeListSummaries().stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), null)
}

/** The Lists page: one card per list. Every list is always shown, an empty one included. */
@Composable
fun ListsScreen(onMenu: () -> Unit, onOpen: (ListKey) -> Unit) {
    val repo = repository()
    val vm: ListsViewModel = viewModel(factory = viewModelFactory { initializer { ListsViewModel(repo) } })
    val lists by vm.lists.collectAsStateWithLifecycle()
    SettingsPage("Lists", onMenu) {
        val current = lists
        if (current == null) {
            Box(Modifier.fillMaxWidth().padding(48.dp), contentAlignment = Alignment.Center) {
                CircularProgressIndicator()
            }
        } else {
            current.forEach { (key, summary) -> ListCard(key, summary) { onOpen(key) } }
        }
    }
}

private val CardShape = RoundedCornerShape(12.dp)
private val FanPosterShape = RoundedCornerShape(6.dp)
private val CardHeight = 116.dp
private val FanPosterWidth = 60.dp
private val FanPosterHeight = 90.dp

/** How far each poster sits from the one above it: 62% of a poster stays visible. */
private val FanStep = FanPosterWidth * 0.62f

/** The strip the chevron keeps clear of posters. */
private val ChevronGutter = 40.dp

/** Per slot, counted from the newest poster: its tilt in degrees and its drop as a share of the card's height. */
private val FanTilt = listOf(4.6f, 1.7f, -1.1f, -3.4f, -5.7f)
private val FanDrop = listOf(0f, 0.04f, -0.03f, 0.03f, -0.02f)

/** One list: its glyph, name and counts on the left, a fan of its newest posters on the right. */
@Composable
private fun ListCard(key: ListKey, summary: ListSummary, onClick: () -> Unit) {
    val surface = MaterialTheme.colorScheme.surfaceContainer
    Box(
        Modifier
            .fillMaxWidth()
            .height(CardHeight)
            .clip(CardShape)
            .background(surface)
            .clickable(onClickLabel = "Open ${key.title}", onClick = onClick),
    ) {
        PosterFan(summary.posters, Modifier.align(Alignment.CenterEnd).padding(end = ChevronGutter))
        // The card's own colour over the fan's left side: the posters dissolve under the caption.
        Box(
            Modifier.matchParentSize().background(
                Brush.horizontalGradient(
                    0f to surface,
                    0.35f to surface,
                    0.55f to surface.copy(alpha = 0.6f),
                    0.8f to surface.copy(alpha = 0f),
                ),
            ),
        )
        Row(
            Modifier.fillMaxSize().padding(start = 16.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Icon(
                key.icon,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.size(24.dp),
            )
            Spacer(Modifier.width(16.dp))
            Column(Modifier.weight(1f)) {
                Text(
                    key.title,
                    style = MaterialTheme.typography.titleMedium,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Text(
                    breakdown(summary),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            Box(Modifier.width(ChevronGutter), contentAlignment = Alignment.Center) {
                Icon(
                    DetailIcons.ChevronRight,
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

/** Up to five posters, newest rightmost and on top, each tilted and nudged a little. Empty draws nothing. */
@Composable
private fun PosterFan(paths: List<String>, modifier: Modifier) {
    Box(modifier) {
        // Oldest first, so the newest is drawn last and lies on top.
        paths.take(FanTilt.size).withIndex().reversed().forEach { (slot, path) ->
            AsyncImage(
                model = TmdbClient.posterUrl(path),
                contentDescription = null,
                contentScale = ContentScale.Crop,
                modifier = Modifier
                    .align(Alignment.CenterEnd)
                    .offset(x = -FanStep * slot, y = CardHeight * FanDrop[slot])
                    .graphicsLayer { rotationZ = FanTilt[slot] }
                    .shadow(6.dp, FanPosterShape)
                    .size(FanPosterWidth, FanPosterHeight)
                    .clip(FanPosterShape)
                    .background(MaterialTheme.colorScheme.surfaceVariant),
            )
        }
    }
}

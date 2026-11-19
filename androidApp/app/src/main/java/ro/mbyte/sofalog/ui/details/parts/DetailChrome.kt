package ro.mbyte.sofalog.ui.details.parts

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.IconButtonDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.State
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

/*
 * What a details page wears around its sections: the app bar laid over the hero, the status marks
 * in it, and the bottom sheet every choice on the page is asked in.
 */

/**
 * How far the hero has scrolled under the app bar, 0 (clear, over the artwork) to 1 (solid). The
 * hero is the list's first item; [loaded] false — nothing to lie over yet — reads as solid.
 */
@Composable
fun rememberHeroCollapse(list: LazyListState, loaded: Boolean, bandHeight: Dp): State<Float> {
    val density = LocalDensity.current.density
    return remember(loaded, bandHeight, density) {
        derivedStateOf {
            val hero = list.layoutInfo.visibleItemsInfo.firstOrNull { it.index == 0 }
            when {
                !loaded || list.firstVisibleItemIndex > 0 || hero == null -> 1f
                else -> {
                    // Solid by the time the title, which sits at the band's foot, slides under.
                    val range = 72 * density
                    val start = ((bandHeight.value - 150) * density).coerceAtLeast(0f)
                    ((list.firstVisibleItemScrollOffset - start) / range).coerceIn(0f, 1f)
                }
            }
        }
    }
}

/**
 * A real app bar laid over the page: clear above the hero, so the artwork runs behind it and the
 * status bar, then solid — with the title — once the hero has scrolled away. [badges] is handed
 * the disc colour its marks sit on while the bar is still clear.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun OverlaidTopBar(
    title: String,
    collapsed: Float,
    wide: Boolean,
    onMenu: () -> Unit,
    badges: @Composable (disc: Color) -> Unit,
) {
    val scheme = MaterialTheme.colorScheme
    val disc = scheme.surface.copy(alpha = 0.55f * (1f - collapsed))
    TopAppBar(
        title = {
            Text(title, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.graphicsLayer { alpha = collapsed })
        },
        navigationIcon = {
            IconButton(
                onClick = onMenu,
                // Over the artwork the glyph sits on a soft disc; on the solid bar it needs none.
                colors = IconButtonDefaults.iconButtonColors(containerColor = disc),
                modifier = Modifier.padding(start = if (wide) 20.dp else 4.dp),
            ) { Icon(DetailIcons.Menu, "Menu") }
        },
        actions = { Box(Modifier.padding(end = if (wide) 28.dp else 12.dp)) { badges(disc) } },
        colors = TopAppBarDefaults.topAppBarColors(containerColor = scheme.surfaceContainer.copy(alpha = collapsed)),
    )
}

/**
 * What the user has done with this title, readable at a glance from anywhere on the page: a mark
 * per state that holds, none for one that does not. They only report — the toggles are in the
 * History section. [disc] is the menu button's backing, there while the bar lies over artwork.
 */
@Composable
fun StatusBadges(watched: Boolean, watchlisted: Boolean, favorited: Boolean, disc: Color, modifier: Modifier = Modifier) {
    Row(modifier, horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
        val accent = MaterialTheme.colorScheme.primary
        if (watched) StatusBadge(DetailIcons.Check, "Watched", accent, disc)
        if (watchlisted) StatusBadge(DetailIcons.Bookmark, "On your watchlist", accent, disc)
        if (favorited) StatusBadge(DetailIcons.Heart, "In your favorites", HeartRed, disc)
    }
}

@Composable
private fun StatusBadge(icon: ImageVector, label: String, tint: Color, disc: Color) {
    Box(Modifier.size(32.dp).background(disc, CircleShape), contentAlignment = Alignment.Center) {
        Icon(icon, contentDescription = label, modifier = Modifier.size(18.dp), tint = tint)
    }
}

/** One button of a [ChoiceSheet]. [danger] is the destructive one; [secondary] the lesser of two. */
data class SheetAction(
    val label: String,
    val danger: Boolean = false,
    val secondary: Boolean = false,
    val onClick: () -> Unit,
)

/**
 * A question asked in a bottom sheet: what it is about, an optional word on the consequence, then
 * the answers as full-width buttons with Cancel last. The page's every confirmation is one.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ChoiceSheet(title: String, subject: String, note: String?, actions: List<SheetAction>, onDismiss: () -> Unit) {
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier.fillMaxWidth().padding(start = 24.dp, end = 24.dp, bottom = 24.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Text(title, style = MaterialTheme.typography.titleLarge, textAlign = TextAlign.Center)
            Text(subject, style = MaterialTheme.typography.titleSmall, textAlign = TextAlign.Center)
            note?.let {
                Text(
                    it,
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    textAlign = TextAlign.Center,
                )
            }
            actions.forEachIndexed { index, action ->
                val button = Modifier.fillMaxWidth().padding(top = if (index == 0) 12.dp else 0.dp).height(52.dp)
                when {
                    action.secondary -> OutlinedButton(onClick = action.onClick, modifier = button) { Text(action.label) }
                    action.danger -> Button(
                        onClick = action.onClick,
                        colors = ButtonDefaults.buttonColors(
                            containerColor = MaterialTheme.colorScheme.error,
                            contentColor = MaterialTheme.colorScheme.onError,
                        ),
                        modifier = button,
                    ) { Text(action.label) }
                    else -> Button(onClick = action.onClick, modifier = button) { Text(action.label) }
                }
            }
            TextButton(onClick = onDismiss, modifier = Modifier.fillMaxWidth().height(48.dp)) { Text("Cancel") }
        }
    }
}
